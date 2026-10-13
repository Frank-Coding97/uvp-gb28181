package recordcache

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	gbplayback "uvplatform.com/uvp-gb28181/app/gb28181/playback"
	gbrecording "uvplatform.com/uvp-gb28181/app/gb28181/recording"
	"uvplatform.com/uvp-gb28181/app/gb28181/recordquery"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/node"
)

// ---- fakes ----

type fakeRepo struct {
	mu   sync.Mutex
	rows map[string]*gbmodels.GbRecordCacheTask
	seq  uint64
}

func newFakeRepo() *fakeRepo { return &fakeRepo{rows: map[string]*gbmodels.GbRecordCacheTask{}} }

func (r *fakeRepo) Create(_ context.Context, task *gbmodels.GbRecordCacheTask) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	task.ID = r.seq
	copied := *task
	r.rows[task.TaskID] = &copied
	return nil
}

func (r *fakeRepo) Update(_ context.Context, task *gbmodels.GbRecordCacheTask) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	copied := *task
	r.rows[task.TaskID] = &copied
	return nil
}

func (r *fakeRepo) FindByTaskID(_ context.Context, taskID string, _ Scope) (*gbmodels.GbRecordCacheTask, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	row, ok := r.rows[taskID]
	if !ok {
		return nil, ErrTaskNotFound
	}
	copied := *row
	return &copied, nil
}

func (r *fakeRepo) List(_ context.Context, _ ListQuery) ([]gbmodels.GbRecordCacheTask, int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]gbmodels.GbRecordCacheTask, 0, len(r.rows))
	for _, row := range r.rows {
		out = append(out, *row)
	}
	return out, int64(len(out)), nil
}

func (r *fakeRepo) Delete(_ context.Context, id uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, row := range r.rows {
		if row.ID == id {
			delete(r.rows, key)
		}
	}
	return nil
}

func (r *fakeRepo) FindActiveByChannel(_ context.Context, channelID uint) (*gbmodels.GbRecordCacheTask, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, row := range r.rows {
		if row.ChannelID == channelID && gbmodels.RecordCacheTaskActive(row.State) {
			copied := *row
			return &copied, nil
		}
	}
	return nil, ErrTaskNotFound
}

func (r *fakeRepo) ListAdvanceable(_ context.Context, limit int) ([]gbmodels.GbRecordCacheTask, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]gbmodels.GbRecordCacheTask, 0, limit)
	for _, row := range r.rows {
		if gbmodels.RecordCacheTaskActive(row.State) && len(out) < limit {
			out = append(out, *row)
		}
	}
	return out, nil
}

func (r *fakeRepo) ListExpired(_ context.Context, now time.Time, _ int) ([]gbmodels.GbRecordCacheTask, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []gbmodels.GbRecordCacheTask
	for _, row := range r.rows {
		if row.ExpiresAt != nil && !row.ExpiresAt.After(now) &&
			(row.State == gbmodels.RecordCacheStateSucceeded || row.State == gbmodels.RecordCacheStateCancelled) {
			out = append(out, *row)
		}
	}
	return out, nil
}

type fakeTargets struct {
	target Target
	err    error
}

func (f fakeTargets) LoadTarget(context.Context, uint, string) (Target, error) {
	return f.target, f.err
}

type fakeSnapshots struct {
	snapshot recordquery.Snapshot
	err      error
}

func (f fakeSnapshots) Resolve(recordquery.ResolveRequest) (recordquery.Snapshot, error) {
	return f.snapshot, f.err
}

type fakeSpeed struct{ speed int }

func (f fakeSpeed) MaxDownloadSpeed(context.Context, int64, string) int { return f.speed }

type fakePlayback struct {
	mu        sync.Mutex
	sessions  map[string]*gbplayback.Session
	created   []gbplayback.CreateRequest
	createErr error
	stopped   []string
	seq       int
}

func newFakePlayback() *fakePlayback {
	return &fakePlayback{sessions: map[string]*gbplayback.Session{}}
}

func (f *fakePlayback) Create(_ context.Context, request gbplayback.CreateRequest) (gbplayback.CreateResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return gbplayback.CreateResult{}, f.createErr
	}
	f.created = append(f.created, request)
	f.seq++
	sessionID := "pb-session-" + strconv.Itoa(f.seq)
	session := &gbplayback.Session{
		ID: sessionID, OwnerID: request.OwnerID, NodeID: "1",
		StreamID:  "pb-stream-" + strconv.Itoa(f.seq),
		ChannelID: request.ChannelID, Mode: request.Mode, DownloadSpeed: request.DownloadSpeed,
		SegmentStart: request.SegmentStart, SegmentEnd: request.SegmentEnd,
		State: gbplayback.StatePlaying, CreatedAt: time.Now(),
	}
	f.sessions[sessionID] = session
	return gbplayback.CreateResult{Session: session}, nil
}

func (f *fakePlayback) StopForOwner(_ context.Context, sessionID, ownerID, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	session, ok := f.sessions[sessionID]
	if !ok || session.OwnerID != ownerID {
		return gbplayback.ErrPlaybackNotFound
	}
	delete(f.sessions, sessionID)
	f.stopped = append(f.stopped, sessionID)
	return nil
}

func (f *fakePlayback) GetForOwner(sessionID, ownerID string) (*gbplayback.Session, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	session, ok := f.sessions[sessionID]
	if !ok || session.OwnerID != ownerID {
		return nil, false
	}
	return session, true
}

// dropSessions 模拟"会话自然结束"（设备推完 / registry 清理）。
func (f *fakePlayback) dropSessions() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for key := range f.sessions {
		delete(f.sessions, key)
	}
}

type fakeRecorder struct {
	mu       sync.Mutex
	started  []string
	stopped  []string
	deleted  []string
	files    map[string][]zlm.MP4RecordFile
	startErr error
	// downloads 是「远程路径 → 下载内容」的表；没登记到的路径回空体（200）。
	downloads map[string][]byte
	// downloadHits 记录被请求过的路径，用来钉住「按分片顺序逐片拉」。
	downloadHits []string
	downloadErr  error
	// leases 非 nil 时，停录那一刻会记下租约是否还在 ——
	// 用来钉住「先停录、后放租约」的顺序。
	leases          *fakeLeases
	leaseHeldAtStop bool
	// mediaInfo / mediaErr 是 getMediaInfo 的注入点；mediaInfoHits 记录被查过的流，
	// 用来钉住「列表接口不许打 ZLM」。
	mediaInfo     *zlm.MediaInfo
	mediaErr      error
	mediaInfoHits []string
}

func newFakeRecorder() *fakeRecorder {
	return &fakeRecorder{files: map[string][]zlm.MP4RecordFile{}, downloads: map[string][]byte{}}
}

// serve 登记某个远程文件下载时返回的内容。
func (f *fakeRecorder) serve(path string, data []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.downloads[path] = data
}

func (f *fakeRecorder) StartRecordWithType(_ context.Context, _, _, stream string, _ zlm.RecorderType, _ int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.startErr != nil {
		return f.startErr
	}
	f.started = append(f.started, stream)
	return nil
}

func (f *fakeRecorder) StopRecordWithType(_ context.Context, _, _, stream string, _ zlm.RecorderType) error {
	held := f.leases == nil || f.leases.HasLease(stream)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.leaseHeldAtStop = f.leaseHeldAtStop || held
	f.stopped = append(f.stopped, stream)
	return nil
}

func (f *fakeRecorder) GetMP4RecordFiles(_ context.Context, _, _, stream, _ string) ([]zlm.MP4RecordFile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.files[stream], nil
}

func (f *fakeRecorder) DeleteMP4RecordFile(_ context.Context, _, _, _, _, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, name)
	return nil
}

func (f *fakeRecorder) GetMediaInfo(_ context.Context, _, _, _, stream string) (*zlm.MediaInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mediaInfoHits = append(f.mediaInfoHits, stream)
	if f.mediaErr != nil {
		return nil, f.mediaErr
	}
	if f.mediaInfo != nil {
		return f.mediaInfo, nil
	}
	return &zlm.MediaInfo{Online: true, BytesSpeed: 1024}, nil
}

// setMediaInfo 造 ZLM 上这条流的实时快照（速率 / 各 track 已推时长 / 累计字节）。
func (f *fakeRecorder) setMediaInfo(speed, totalBytes uint64, trackMillis ...float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	tracks := make([]zlm.MediaTrack, 0, len(trackMillis))
	for i, ms := range trackMillis {
		tracks = append(tracks, zlm.MediaTrack{CodecType: i % 2, Duration: ms})
	}
	f.mediaInfo = &zlm.MediaInfo{Online: true, BytesSpeed: speed, TotalBytes: totalBytes, Tracks: tracks}
}

// mediaInfoQueries 返回被查过的流名（副本，供断言）。
func (f *fakeRecorder) mediaInfoQueries() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.mediaInfoHits...)
}

func (f *fakeRecorder) DownloadFile(_ context.Context, filePath, _ string) (*zlm.DownloadResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.downloadHits = append(f.downloadHits, filePath)
	if f.downloadErr != nil {
		return nil, f.downloadErr
	}
	data := f.downloads[filePath]
	if data == nil {
		return &zlm.DownloadResponse{StatusCode: http.StatusOK, Header: http.Header{}, Body: http.NoBody}, nil
	}
	return &zlm.DownloadResponse{StatusCode: http.StatusOK, Header: http.Header{},
		Body: io.NopCloser(bytes.NewReader(data))}, nil
}

// addFile 给某条流挂一个"已落盘"的 MP4 文件。
func (f *fakeRecorder) addFile(stream, name string, size, ms int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files[stream] = append(f.files[stream], zlm.MP4RecordFile{
		FileName: name, FilePath: "rtp/" + stream + "/" + name,
		FileSize: &size, DurationMS: &ms,
	})
}

type fakeNodes struct{}

func (fakeNodes) Get(int64) (*node.Node, bool) { return &node.Node{ID: 1, Name: "n1"}, true }

type fakeContent struct {
	mu   sync.Mutex
	last gbrecording.ContentRequest
	err  error
}

func (f *fakeContent) Stream(_ context.Context, _ http.ResponseWriter, _ gbrecording.ContentDownloader, request gbrecording.ContentRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.last = request
	return f.err
}

// ---- harness ----

type harness struct {
	service      *Service
	repo         *fakeRepo
	playback     *fakePlayback
	recorder     *fakeRecorder
	content      *fakeContent
	leases       *fakeLeases
	fileIndex    *fakeFileIndex
	now          time.Time
	segmentStart time.Time
	segmentEnd   time.Time
}

func newHarness(t *testing.T, speed int, window time.Duration) *harness {
	t.Helper()
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	start := now.Add(-6 * time.Hour)
	end := start.Add(window)
	repo := newFakeRepo()
	playback := newFakePlayback()
	recorder := newFakeRecorder()
	content := &fakeContent{}
	leases := newFakeLeases()
	fileIndex := newFakeFileIndex()
	target := Target{
		ChannelPK: 11, ChannelCode: "34020000001320000001", ChannelName: "通道一",
		ChannelStatus: gbmodels.ChannelStatusOnline, StreamTransport: "UDP",
		DevicePK: 7, DeviceCode: "34020000001110000001", DeviceName: "设备一",
		DeviceStatus: gbmodels.DeviceStatusOnline, DeviceEpoch: 3, CleanupCompletedEpoch: 3,
		DeviceIP: "192.0.2.10", DevicePort: 5060, DeviceTransport: "UDP", OwnerDeptID: 2,
	}
	// ⛔ 时钟必须由 harness 持有：测试要把时间拨到保留期之后，
	// 如果闭包捕获的是局部变量，拨表就拨不动服务内部看到的"现在"。
	h := &harness{repo: repo, playback: playback, recorder: recorder, content: content,
		leases: leases, fileIndex: fileIndex,
		now: now, segmentStart: start, segmentEnd: end}
	// 让录制器能在"停录"那一刻检查租约是否还在（钉住"先停录、后放租约"的顺序）。
	recorder.leases = leases
	h.service = NewService(Options{
		Repo:     repo,
		Targets:  fakeTargets{target: target},
		Playback: playback,
		Snapshots: fakeSnapshots{snapshot: recordquery.Snapshot{RecordKey: "rk-1", OwnerUserID: 9, ChannelID: 11,
			DeviceCode: target.DeviceCode, ChannelCode: target.ChannelCode, SegmentStart: start, SegmentEnd: end}},
		Nodes:     fakeNodes{},
		Client:    func(*node.Node) Recorder { return recorder },
		Content:   content,
		Speed:     fakeSpeed{speed: speed},
		Leases:    leases,
		FileIndex: fileIndex,
		// 合并产物落到测试自己的临时目录：既避免污染系统临时目录，
		// 也让"产物在不在"这件事在每个用例之间天然隔离。
		MergedDir: t.TempDir(),
		Config:    Config{Workers: 10, RetentionDays: 7},
		Now:       func() time.Time { return h.now },
	})
	return h
}

// waitMerge 等收尾合并跑完，并返回库里最新的任务行。
//
// ⛔ 合并是异步的（合成期间不能占着 Tick 的推进循环），所以断言任务终态之前
// 必须先等到它结束，否则读到的是中间态 merging。
func (h *harness) waitMerge(t *testing.T, taskID string) gbmodels.GbRecordCacheTask {
	t.Helper()
	h.service.waitMerges()
	row, err := h.repo.FindByTaskID(context.Background(), taskID, nil)
	require.NoError(t, err)
	return *row
}

func (h *harness) create(t *testing.T, speed int) gbmodels.GbRecordCacheTask {
	t.Helper()
	view, err := h.service.Create(context.Background(), CreateRequest{
		OwnerUserID: 9, OwnerName: "张三", ChannelID: 11, RecordKey: "rk-1", DownloadSpeed: speed,
	})
	require.NoError(t, err)
	row, err := h.repo.FindByTaskID(context.Background(), view.TaskID, nil)
	require.NoError(t, err)
	return *row
}

func TestCreateRejectsChannelWithActiveTask(t *testing.T) {
	h := newHarness(t, 0, 40*time.Minute)
	created := h.create(t, 0)

	_, err := h.service.Create(context.Background(), CreateRequest{
		OwnerUserID: 9, ChannelID: 11, RecordKey: "rk-1",
	})
	require.ErrorIs(t, err, ErrChannelBusy, "同一通道再起一个缓存任务必然 429（download 会话按通道排他），必须在受理时就拒掉")
	require.Equal(t, gbmodels.RecordCacheStateQueued, created.State)
}

func TestCreatePrefersDeviceSpeedAndFallsBack(t *testing.T) {
	withCapability := newHarness(t, 8, 40*time.Minute)
	require.Equal(t, 8, withCapability.create(t, 0).DownloadSpeed, "设备上报过能力时取最大档位")

	withoutCapability := newHarness(t, 0, 40*time.Minute)
	require.Equal(t, DefaultDownloadSpeed, withoutCapability.create(t, 0).DownloadSpeed, "取不到能力时回退兜底倍速")

	explicit := newHarness(t, 8, 40*time.Minute)
	require.Equal(t, 4, explicit.create(t, 4).DownloadSpeed, "调用方显式指定倍速时以它为准")
}

func TestStartSegmentClampsSegmentLength(t *testing.T) {
	// 单片媒体上限 = 25 分钟墙钟 × 4 × 0.9 = 90 分钟。
	// 180 分钟录像：上限不够，这一片只覆盖前 90 分钟，剩下 90 分钟留到下一片。
	// 为什么必须夹：不夹的话请求区间会覆盖整段，一旦有效倍速打折就会撑到墙钟硬限。
	h := newHarness(t, 4, 180*time.Minute)
	task := h.create(t, 0)

	h.service.Tick(context.Background())

	require.Len(t, h.playback.created, 1)
	request := h.playback.created[0]
	require.Equal(t, h.segmentStart, request.SegmentStart.UTC())
	require.Equal(t, h.segmentStart.Add(segmentMediaLimit(4)), request.SegmentEnd.UTC())
	require.Equal(t, gbplayback.ModeDownload, request.Mode, "缓存必须走下载模式（倍速拉流）")
	require.Equal(t, uint32(4), request.DownloadSpeed)

	row, err := h.repo.FindByTaskID(context.Background(), task.TaskID, nil)
	require.NoError(t, err)
	require.Equal(t, gbmodels.RecordCacheStateRunning, row.State)
	require.NotNil(t, row.SessionID)
	require.Equal(t, "pb-stream-1", row.Stream)
	require.Equal(t, []string{"pb-stream-1"}, h.recorder.started, "会话就绪后必须真的开始在 ZLM 上录 MP4")
}

// TestShortRecordingFitsInOneSegment 是 2026-10-04 的产品口径：
// **1 小时以内的录像必须一次会话（= 一个文件）拉完**，请求区间夹到录像末尾，
// 而不是夹到单片上限后再排第二片。
//
// ⛔ 退回 SegmentSpeedSafety=0.5 时这里立刻红：4× 单片只给 50 分钟 ⇒
// 60 分钟录像会被切成 50+10 两片，用户拿到 2 个文件。
func TestShortRecordingFitsInOneSegment(t *testing.T) {
	for _, window := range []time.Duration{30 * time.Minute, 60 * time.Minute} {
		h := newHarness(t, 4, window)
		task := h.create(t, 0)

		h.service.Tick(context.Background())

		require.Len(t, h.playback.created, 1)
		require.Equal(t, task.EndTime.UTC(), h.playback.created[0].SegmentEnd.UTC(),
			"%s 的录像必须一次会话拉完：SegmentEnd 夹到录像末尾，不是单片上限", window)
	}
}

func TestFinishSegmentAdvancesCursorByMediaDuration(t *testing.T) {
	// 核心口径：游标按**媒体时长**推进，不是墙钟。
	// 4 倍速下 5 分钟墙钟能拉 20 分钟媒体，按墙钟推进会把任务算成"只缓存了 5 分钟"。
	h := newHarness(t, 4, 60*time.Minute)
	task := h.create(t, 0)

	h.service.Tick(context.Background())
	// 刻意只录到 10 分钟媒体（设备只推了一半），而不是请求区间的 20 分钟：
	// 断言必须能区分"按实际录到的内容推进"与"按请求的区间推进"。
	h.recorder.addFile("pb-stream-1", "seg-1.mp4", 200*1024*1024, 10*60*1000)
	h.playback.dropSessions()

	h.service.Tick(context.Background())

	row, err := h.repo.FindByTaskID(context.Background(), task.TaskID, nil)
	require.NoError(t, err)
	require.Equal(t, gbmodels.RecordCacheStateQueued, row.State, "还有剩余区间时应排下一片")
	require.NotNil(t, row.CursorAt)
	require.Equal(t, h.segmentStart.Add(10*time.Minute), row.CursorAt.UTC(),
		"游标必须按**实际录到的媒体时长**（10 分钟）推进，既不是墙钟、也不是请求区间的 20 分钟")

	segments := row.DecodeSegments()
	require.Len(t, segments, 1)
	require.Equal(t, "pb-stream-1", segments[0].Stream, "分片清单必须记住 stream（否则前几片的文件永远找不回来）")
	require.Equal(t, int64(10*60*1000), segments[0].MS)
	require.Equal(t, uint64(200*1024*1024), row.CachedBytes)
}

func TestTickRunsSegmentsUntilSucceededAndSetsExpiry(t *testing.T) {
	h := newHarness(t, 4, 40*time.Minute)
	task := h.create(t, 0)

	// 第一片
	h.service.Tick(context.Background())
	h.recorder.addFile("pb-stream-1", "seg-1.mp4", 100, 20*60*1000)
	h.playback.dropSessions()
	h.service.Tick(context.Background())

	// 第二次会话（剩余 20 分钟）——它录的是同一段录像的**延续**，
	// 所以必须并进同一片，而不是自成一片。
	h.service.Tick(context.Background())
	require.Len(t, h.playback.created, 2, "剩余区间要续播，不能一次会话拉到底")
	h.recorder.addFile("pb-stream-2", "seg-2.mp4", 100, 20*60*1000)
	h.playback.dropSessions()
	h.service.Tick(context.Background())

	// 多个源文件要先过一道"收尾整理"（合成一个文件）才算成功。
	row := h.waitMerge(t, task.TaskID)
	require.Equal(t, gbmodels.RecordCacheStateSucceeded, row.State)
	segments := row.DecodeSegments()
	require.Len(t, segments, 1,
		"两次会话录的是同一段录像的延续，必须并进**同一片** —— 按会话切片会让用户看到多个文件")
	require.Len(t, segments[0].Sources(), 2, "两个源文件都要留在清单里（合并与删除都以它为准）")
	require.Equal(t, int64(40*60*1000), segments[0].MS, "片级时长是各源文件之和")
	require.NotNil(t, row.ExpiresAt)
	require.Equal(t, h.now.Add(7*24*time.Hour), row.ExpiresAt.UTC(), "过期时间必须跟随统一保留配置")
	require.Equal(t, task.EndTime.UTC(), row.CursorAt.UTC())
}

// TestMultiSourceTaskSurvivesMergeFailure 钉住"整理失败不等于任务失败"：
// 源文件本身是好的，用户仍能按分段下载（下载接口会回 ErrMergeFailed 说明原因），
// 把任务标成 failed 等于把"能拿到的录像"说成"没有录像"。
//
// 本用例的假录制器不提供可下载的内容，正好构造出"合成必然失败"的现场。
func TestMultiSourceTaskSurvivesMergeFailure(t *testing.T) {
	h := newHarness(t, 4, 40*time.Minute)
	task := h.create(t, 0)

	h.service.Tick(context.Background())
	h.recorder.addFile("pb-stream-1", "seg-1.mp4", 100, 20*60*1000)
	h.playback.dropSessions()
	h.service.Tick(context.Background())
	h.service.Tick(context.Background())
	h.recorder.addFile("pb-stream-2", "seg-2.mp4", 100, 20*60*1000)
	h.playback.dropSessions()
	h.service.Tick(context.Background())

	// 最后一个源文件收尾后先转入 merging —— 这一刻下载必须是"稍候"而不是"没有文件"。
	mid, err := h.repo.FindByTaskID(context.Background(), task.TaskID, nil)
	require.NoError(t, err)
	require.Contains(t, []string{gbmodels.RecordCacheStateMerging, gbmodels.RecordCacheStateSucceeded}, mid.State)

	row := h.waitMerge(t, task.TaskID)
	require.Equal(t, gbmodels.RecordCacheStateSucceeded, row.State,
		"合不出来也必须算成功：分段能下，用户能拿到东西")
	segments := row.DecodeSegments()
	require.Len(t, segments, 1)
	require.Len(t, segments[0].Sources(), 2, "源文件清单必须原样保留 —— 它是按分段下载的唯一依据")
	_, _, ok := h.service.mergedArtifact(&row)
	require.False(t, ok, "这次合成注定失败，不该有产物")
}

// TestMergingTaskRefusesDownloadUntilReady 整理期间不能放行下载 ——
// 放行只会让用户拿到的第一次下载退化成"现场拼接"（进度长时间为 0）。
func TestMergingTaskRefusesDownloadUntilReady(t *testing.T) {
	h := newHarness(t, 4, 40*time.Minute)
	task := h.create(t, 0)
	row, err := h.repo.FindByTaskID(context.Background(), task.TaskID, nil)
	require.NoError(t, err)
	row.State = gbmodels.RecordCacheStateMerging
	require.NoError(t, row.EncodeSegments([]gbmodels.RecordCacheSegment{
		{Stream: "pb-1", Name: "a.mp4", Path: "p/a.mp4", Size: 10},
		{Stream: "pb-2", Name: "b.mp4", Path: "p/b.mp4", Size: 10},
	}))
	require.NoError(t, h.repo.Update(context.Background(), row))

	err = ensureDownloadable(row)
	require.ErrorIs(t, err, ErrTaskNotReady, "整理中要说「稍候」，不能说「暂无文件」")
}

func TestSegmentFailureMarksTaskFailed(t *testing.T) {
	h := newHarness(t, 4, 40*time.Minute)
	task := h.create(t, 0)
	h.playback.createErr = &gbplayback.ServiceError{Stage: "media_wait", Code: "timeout"}

	h.service.Tick(context.Background())

	row, err := h.repo.FindByTaskID(context.Background(), task.TaskID, nil)
	require.NoError(t, err)
	require.Equal(t, gbmodels.RecordCacheStateFailed, row.State)
	require.Contains(t, row.LastError, "推流超时", "失败原因要能让人看懂，而不是把 stage 原样抛给用户")
}

func TestSegmentWithoutRecordingFails(t *testing.T) {
	h := newHarness(t, 4, 40*time.Minute)
	task := h.create(t, 0)
	h.service.Tick(context.Background())
	// 会话结束但 ZLM 上一个文件都没有（设备没推、或录制没起来）。
	h.playback.dropSessions()
	h.service.Tick(context.Background())

	row, err := h.repo.FindByTaskID(context.Background(), task.TaskID, nil)
	require.NoError(t, err)
	require.Equal(t, gbmodels.RecordCacheStateFailed, row.State,
		"一片都没录到时必须失败：按请求区间推进会让任务假装成功")
}

func TestCancelStopsSessionAndRejectsTerminalTask(t *testing.T) {
	h := newHarness(t, 4, 40*time.Minute)
	task := h.create(t, 0)
	h.service.Tick(context.Background())

	view, err := h.service.Cancel(context.Background(), task.TaskID, nil)
	require.NoError(t, err)
	require.Equal(t, gbmodels.RecordCacheStateCancelled, view.State)
	require.Len(t, h.playback.stopped, 1, "取消必须真的把回放会话拆掉，否则通道被占到会话超时")
	require.NotEmpty(t, h.recorder.stopped, "取消也要停录")
	require.NotNil(t, view.ExpiresAt)

	_, err = h.service.Cancel(context.Background(), task.TaskID, nil)
	require.ErrorIs(t, err, ErrNotCancellable, "终态任务不能再取消")
}

func TestDeleteStopsActiveTaskRemovesFilesAndRow(t *testing.T) {
	h := newHarness(t, 4, 40*time.Minute)
	task := h.create(t, 0)
	h.service.Tick(context.Background())
	h.recorder.addFile("pb-stream-1", "seg-1.mp4", 10, 1000)
	h.playback.dropSessions()
	h.service.Tick(context.Background()) // 收尾本片，写入清单

	require.NoError(t, h.service.Delete(context.Background(), task.TaskID, nil))
	require.Equal(t, []string{"seg-1.mp4"}, h.recorder.deleted, "删除任务要连带删掉 ZLM 上的文件")
	_, err := h.repo.FindByTaskID(context.Background(), task.TaskID, nil)
	require.ErrorIs(t, err, ErrTaskNotFound)
}

func TestContentRejectsOutOfRangeSegment(t *testing.T) {
	h := newHarness(t, 4, 40*time.Minute)
	task := h.create(t, 0)
	h.service.Tick(context.Background())
	h.recorder.addFile("pb-stream-1", "seg-1.mp4", 10, 20*60*1000)
	h.playback.dropSessions()
	h.service.Tick(context.Background())

	err := h.service.Content(context.Background(), nil, task.TaskID, nil, 5, "")
	require.ErrorIs(t, err, ErrNotDownloadable, "不存在的分片序号必须报错，而不是随便挑一个给用户")

	require.NoError(t, h.service.Content(context.Background(), nil, task.TaskID, nil, 0, "bytes=0-9"))
	require.Equal(t, "seg-1.mp4", h.content.last.FileName)
	require.Equal(t, gbrecording.CapabilityModeDownload, h.content.last.Mode)
	require.Equal(t, "bytes=0-9", h.content.last.Range, "Range 必须透传给上游，浏览器才能续传/拖动")
}

func TestCleanupExpiredRemovesFilesAndMarksExpired(t *testing.T) {
	h := newHarness(t, 4, 40*time.Minute)
	task := h.create(t, 0)
	h.service.Tick(context.Background())
	h.recorder.addFile("pb-stream-1", "seg-1.mp4", 10, 20*60*1000)
	h.playback.dropSessions()
	h.service.Tick(context.Background())
	h.service.Tick(context.Background())
	h.recorder.addFile("pb-stream-2", "seg-2.mp4", 10, 20*60*1000)
	h.playback.dropSessions()
	h.service.Tick(context.Background())

	row := h.waitMerge(t, task.TaskID)
	require.Equal(t, gbmodels.RecordCacheStateSucceeded, row.State)

	// 把时钟拨到保留期之后，下一轮 tick 应清理文件并转 expired。
	h.now = h.now.Add(8 * 24 * time.Hour)
	h.service.Tick(context.Background())

	expired, err := h.repo.FindByTaskID(context.Background(), task.TaskID, nil)
	require.NoError(t, err)
	require.Equal(t, gbmodels.RecordCacheStateExpired, expired.State)
	require.Contains(t, h.recorder.deleted, "seg-1.mp4")
	require.Contains(t, h.recorder.deleted, "seg-2.mp4")
	// 收尾期合成的产物必须**跟着一起清**：不清就会在本地磁盘上留一个
	// 再也无人引用、也不会被回收的大文件。
	_, _, stillThere := h.service.mergedArtifact(&row)
	require.False(t, stillThere, "保留期清理必须连带删掉合并产物")
}

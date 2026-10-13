package recordcache

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	gbplayback "uvplatform.com/uvp-gb28181/app/gb28181/playback"
	gbrecording "uvplatform.com/uvp-gb28181/app/gb28181/recording"
	"uvplatform.com/uvp-gb28181/app/gb28181/recordquery"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/node"
)

// 媒体落点：GB28181 的 RTP 推流在本平台固定落在 __defaultVhost__ / rtp
// （与 streammonitor、playauth 的 RTP 步骤校验、play 授权用的是同一组常量）。
const (
	mediaVHost = "__defaultVhost__"
	mediaApp   = "rtp"
	// mediaSchema 是 GetMediaInfo 的 schema 维度。ZLM 的同一份媒体源在每个
	// 协议视图（rtsp/rtmp/hls/…）下各有一份 MediaInfo，查错 schema 会拿到"不存在"。
	mediaSchema = "rtmp"
)

// Options 是装配参数。
type Options struct {
	Repo      Repository
	Targets   TargetLoader
	Playback  PlaybackService
	Snapshots SnapshotResolver
	Nodes     NodeLookup
	// Client 从 ZLM 节点构造录制/查询客户端。
	Client func(*node.Node) Recorder
	// Content 是同源下载代理（复用 recording.ContentProxy）。
	Content ContentProxy
	// Speed 提供设备下载倍速能力（可为 nil ⇒ 一律用兜底倍速）。
	Speed SpeedSource
	// Downloads 是「一次性票据」下载任务的登记处（复用云端录像那套实现，
	// 换了独立实例：两者的并发预算与生命周期互不干扰）。
	// 为 nil 时原生下载入口返回未装配，旧的直连下载仍然可用。
	Downloads *gbrecording.DownloadRegistry
	// MergedDir 是收尾期合并产物的落盘目录（空 ⇒ 系统临时目录下的
	// uvp-record-cache-merged）。主要给测试用，生产一般不配。
	MergedDir string
	// Leases 登记本流的「非浏览器消费者」租约，防止 ZLM 在 20 秒无人观看后
	// 把正在录制的回放流回收掉（详见 SourceLeases 注释）。为 nil 时不保流。
	Leases SourceLeases
	// FileIndex 提供平台已入库的录制文件（真实时长/大小）。
	// 为 nil 时退回 ZLM 的文件列表（拿不到时长，进度会停在 0）。
	FileIndex SegmentFileIndex
	// RetentionDays 动态返回产出文件的保留天数（改配置立即生效，不必重启）。
	// 为空时退回 Config.RetentionDays。
	RetentionDays func() int
	// DefaultProtocol 是建会话时希望的播放协议（仅影响会话里的 URL 选择，
	// 缓存路径不播放，所以随便给一个合法的即可）。
	DefaultProtocol string
	Config          Config
	Now             func() time.Time
}

// Service 是设备录像缓存任务的编排器。
type Service struct {
	repo          Repository
	targets       TargetLoader
	playback      PlaybackService
	snapshots     SnapshotResolver
	nodes         NodeLookup
	client        func(*node.Node) Recorder
	content       ContentProxy
	speed         SpeedSource
	leases        SourceLeases
	fileIndex     SegmentFileIndex
	retention     func() int
	protocol      string
	config        Config
	now           func() time.Time
	downloads     *gbrecording.DownloadRegistry
	mergedDirPath string

	locks *keyedLocker

	// leaseMu/heldLeases 记录「本任务当前持有哪条流的保流租约」。
	// 按 taskID 存：一个任务任何时刻只可能有一条活跃 stream。
	leaseMu    sync.Mutex
	heldLeases map[string]SourceLease

	// mergingMu/merging 是「这条任务的收尾合并正在后台跑」的进程内标记。
	//
	// ⛔ 要有它：合并是异步的，但任务状态在整个合成期间都是 merging，
	// 下一轮 Tick 会再次挑到它。没有这个标记就会每次 tick 起一个新协程
	// 去写同一份产物（互相覆盖、白拉几遍分片）。
	mergingMu sync.Mutex
	merging   map[string]struct{}

	lifecycle context.Context
	stop      context.CancelFunc
	wg        sync.WaitGroup
	// mergeWg 只跟踪后台合并协程（s.wg 归 Tick 主循环）。
	mergeWg   sync.WaitGroup
	startOnce sync.Once
}

func NewService(options Options) *Service {
	service := &Service{
		repo: options.Repo, targets: options.Targets, playback: options.Playback,
		snapshots: options.Snapshots, nodes: options.Nodes, client: options.Client,
		content: options.Content, speed: options.Speed, retention: options.RetentionDays,
		leases: options.Leases, fileIndex: options.FileIndex,
		protocol: strings.TrimSpace(options.DefaultProtocol),
		config:   options.Config.withDefaults(),
		now:      options.Now, locks: newKeyedLocker(),
		downloads:     options.Downloads,
		mergedDirPath: strings.TrimSpace(options.MergedDir),
		heldLeases:    map[string]SourceLease{},
		merging:       map[string]struct{}{},
	}
	if service.now == nil {
		service.now = time.Now
	}
	service.lifecycle, service.stop = context.WithCancel(context.Background())
	return service
}

// Start 起状态机循环。幂等：重复调用只有第一次生效。
func (s *Service) Start(parent context.Context) {
	if s == nil || s.repo == nil {
		return
	}
	s.startOnce.Do(func() {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			ticker := time.NewTicker(s.config.TickInterval)
			defer ticker.Stop()
			for {
				select {
				case <-s.lifecycle.Done():
					return
				case <-parent.Done():
					return
				case <-ticker.C:
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					s.Tick(ctx)
					cancel()
				}
			}
		}()
	})
}

// Close 停止状态机循环并等待在途一轮结束。
func (s *Service) Close() {
	if s == nil {
		return
	}
	if s.stop != nil {
		s.stop()
	}
	s.wg.Wait()
	// 合并协程挂在 lifecycle 上，这里 stop 之后它们会尽快退出；
	// 等一等是为了不留下写到一半的产物临时文件。
	s.mergeWg.Wait()
	s.releaseAllLeases()
}

// releaseAllLeases 释放全部保流租约。卸载运行时（而非进程退出）时必须走一遍：
// 残留的租约会继续让 ZLM 认为流的"非浏览器消费者"还在，
// 于是那条回放流不会被回收，一直占着设备通道。
func (s *Service) releaseAllLeases() {
	if s == nil {
		return
	}
	s.leaseMu.Lock()
	held := s.heldLeases
	s.heldLeases = map[string]SourceLease{}
	s.leaseMu.Unlock()
	for _, lease := range held {
		if lease != nil {
			_ = lease.Release()
		}
	}
}

// Create 受理一个缓存任务。
//
// 这里只做"能不能做"的判断（目标存在、在线、通道空闲、录像段有效）并落一行 queued，
// **不在这里建会话**：建会话要等 SIP 往返（最长几十秒），
// 放在 HTTP 请求里会让"点一下缓存"卡住；交给 Tick 异步推进，
// 用户立刻在列表里看到任务，失败原因也落在同一行上。
func (s *Service) Create(ctx context.Context, req CreateRequest) (TaskView, error) {
	if s == nil || s.repo == nil || s.targets == nil || s.snapshots == nil {
		return TaskView{}, ErrInvalidRequest
	}
	if req.OwnerUserID == 0 || req.ChannelID == 0 {
		return TaskView{}, ErrInvalidRequest
	}
	recordKey := strings.TrimSpace(req.RecordKey)
	if recordKey == "" {
		return TaskView{}, ErrInvalidRequest
	}
	if req.RecordType == "" {
		req.RecordType = "all"
	}
	// 通道排他：download 回放会话按通道互斥，同一个通道再起一个必然 429，
	// 与其让第二个任务在 Tick 里反复失败，不如受理时就拒掉。
	if existing, err := s.repo.FindActiveByChannel(ctx, req.ChannelID); err == nil && existing != nil {
		return TaskView{}, ErrChannelBusy
	} else if err != nil && !errors.Is(err, ErrTaskNotFound) {
		return TaskView{}, err
	}

	target, err := s.targets.LoadTarget(ctx, req.ChannelID, strings.TrimSpace(req.DeviceID))
	if err != nil {
		return TaskView{}, err
	}
	if !target.Online() {
		return TaskView{}, fmt.Errorf("%w: 设备或通道当前离线", ErrInvalidRequest)
	}

	snapshot, err := s.snapshots.Resolve(recordquery.ResolveRequest{
		RecordKey: recordKey, OwnerUserID: req.OwnerUserID,
		ChannelID: req.ChannelID, PlayFrom: req.PlayFrom,
	})
	if err != nil {
		return TaskView{}, err
	}
	start, end := snapshot.SegmentStart, snapshot.SegmentEnd
	if !start.Before(end) {
		return TaskView{}, ErrInvalidRequest
	}

	speed := req.DownloadSpeed
	if speed == 0 {
		speed = s.resolveSpeed(ctx, target, snapshot.ChannelCode)
	}
	if !validDownloadSpeed(speed) {
		return TaskView{}, ErrInvalidRequest
	}

	now := s.now()
	channelName := strings.TrimSpace(req.ChannelName)
	if channelName == "" {
		channelName = target.ChannelName
	}
	deviceName := strings.TrimSpace(req.DeviceName)
	if deviceName == "" {
		deviceName = target.DeviceName
	}
	task := &gbmodels.GbRecordCacheTask{
		TaskID: newTaskID(), OwnerDeptID: target.OwnerDeptID,
		CreatedByUser: req.OwnerUserID, CreatedByName: strings.TrimSpace(req.OwnerName),
		ChannelID: req.ChannelID, DeviceID: target.DeviceCode,
		ChannelCode: snapshot.ChannelCode, ChannelName: channelName, DeviceName: deviceName,
		StartTime: start, EndTime: end, RecordType: req.RecordType, RecordKey: recordKey,
		DownloadSpeed: speed,
		VHost:         mediaVHost, App: mediaApp,
		State: gbmodels.RecordCacheStateQueued, RequestID: newRequestID(),
		CursorAt: &start,
	}
	if err := task.EncodeSegments(nil); err != nil {
		return TaskView{}, err
	}
	_ = now
	if err := s.repo.Create(ctx, task); err != nil {
		return TaskView{}, err
	}
	return s.viewOf(ctx, task, false), nil
}

// Detail 取任务详情（含实时进度）。
func (s *Service) Detail(ctx context.Context, taskID string, scope Scope) (TaskView, error) {
	task, err := s.repo.FindByTaskID(ctx, taskID, scope)
	if err != nil {
		return TaskView{}, err
	}
	return s.viewOf(ctx, task, true), nil
}

// List 分页查询任务。
func (s *Service) List(ctx context.Context, query ListQuery) (PageView, error) {
	rows, total, err := s.repo.List(ctx, query)
	if err != nil {
		return PageView{}, err
	}
	page := query.Page
	if page <= 0 {
		page = 1
	}
	size := query.PageSize
	if size <= 0 {
		size = 20
	}
	view := PageView{List: make([]TaskView, 0, len(rows)), Total: total, Page: page, Size: size}
	for i := range rows {
		view.List = append(view.List, s.viewOf(ctx, &rows[i], false))
	}
	return view, nil
}

// Cancel 取消一个活动任务：停录、拆会话、标记 cancelled，已落盘的分片保留。
func (s *Service) Cancel(ctx context.Context, taskID string, scope Scope) (TaskView, error) {
	task, err := s.repo.FindByTaskID(ctx, taskID, scope)
	if err != nil {
		return TaskView{}, err
	}
	if !gbmodels.RecordCacheTaskActive(task.State) {
		return TaskView{}, ErrNotCancellable
	}
	s.stopSegment(ctx, task, "cancelled by user")
	task.State = gbmodels.RecordCacheStateCancelled
	task.LastError = ""
	now := s.now()
	task.FinishedAt = &now
	expires := now.Add(time.Duration(s.retentionDays()) * 24 * time.Hour)
	task.ExpiresAt = &expires
	if err := s.repo.Update(ctx, task); err != nil {
		return TaskView{}, err
	}
	return s.viewOf(ctx, task, false), nil
}

// Delete 删除任务及其全部产出文件。
func (s *Service) Delete(ctx context.Context, taskID string, scope Scope) error {
	task, err := s.repo.FindByTaskID(ctx, taskID, scope)
	if err != nil {
		return err
	}
	if gbmodels.RecordCacheTaskActive(task.State) {
		// 活动任务先停干净再删，否则会话会一直占着设备通道。
		s.stopSegment(ctx, task, "deleted by user")
	}
	s.removeFiles(ctx, task)
	return s.repo.Delete(ctx, task.ID)
}

// SetFavorite 设置/取消收藏。
//
// 收藏的唯一语义是「**不参与保留期自动清理**」—— 文件还在，只是 cleanupExpired
// 不再把它算作到期对象（见 ListExpired 的 favorite 过滤）。手动删除不受影响：
// 用户显式删掉的仍然会删，收藏不是"防手滑"。
//
// ⛔ 收藏**不刷新 expires_at**：改过期时间会让人以为"续期了"，而这里做的是
// "跳过清理"，两件事的可见后果完全不同（取消收藏后，若早已过期，下一轮 Tick 就会清掉）。
func (s *Service) SetFavorite(ctx context.Context, taskID string, scope Scope, favorite bool) (TaskView, error) {
	task, err := s.repo.FindByTaskID(ctx, taskID, scope)
	if err != nil {
		return TaskView{}, err
	}
	if task.Favorite == favorite {
		// 幂等：重复点同一个状态不写库（也避免无意义的 updated_at 抖动）。
		return s.viewOf(ctx, task, false), nil
	}
	task.Favorite = favorite
	if err := s.repo.Update(ctx, task); err != nil {
		return TaskView{}, err
	}
	return s.viewOf(ctx, task, false), nil
}

// Content 直连下载（带 JWT 头的旧入口）：index 为空表示整段，给了序号就只要那一片。
//
// ⛔ 前端已经改走票据 + 浏览器原生下载（见 download.go），这条留着是给脚本 / 排障用的
// 受权限保护的直连通道；语义与票据那条**逐字节同源**（共用 serveTaskContent）。
func (s *Service) Content(ctx context.Context, writer http.ResponseWriter, taskID string, scope Scope, index int, rangeHeader string) error {
	task, err := s.repo.FindByTaskID(ctx, taskID, scope)
	if err != nil {
		return err
	}
	return s.serveTaskContent(ctx, writer, task, index, rangeHeader)
}

// serveTaskContent 是"把一个任务的产出发给 writer"的唯一实现。
//
// 直连下载（带 JWT 头的旧入口）与票据下载（浏览器原生下载）共用它 ——
// 两条路必须逐字节同源，否则"网页里下到的"和"下载栏里下到的"会悄悄不一致。
//
// index 的口径（与 download.go 的 WholeTaskIndex 一致）：
//   - WholeTaskIndex：整段。源文件多于一个时发收尾期已合好的那个文件，只有一个就是它本身；
//   - >= 0：只要第 index **片**，多片任务也一样。
//
// ⛔ 判"要不要发合成产物"用的是**源文件总数**而不是分片数（见 succeed）。
// ⛔ 最后那条是被修掉的旧行为：以前多分片时无条件忽略 index 发合并产物，
// 于是前端「第 2 段」按钮点下去拿到的是整段录像 —— 界面在说谎，且用户以为
// 下载失败时会重复点，每次都拉一遍几百 MB 的整段。
func (s *Service) serveTaskContent(ctx context.Context, writer http.ResponseWriter, task *gbmodels.GbRecordCacheTask, index int, rangeHeader string) error {
	if task == nil {
		return ErrNotDownloadable
	}
	segments := task.DecodeSegments()
	if len(segments) == 0 {
		return ErrNotDownloadable
	}
	if index != WholeTaskIndex {
		return s.serveSegment(ctx, writer, task, segments, index, rangeHeader)
	}
	// 源文件多于一个：优先发收尾期已备好的合成产物。这条分支用 recorder.DownloadFile
	// 而不是 ContentProxy，所以放在 s.content 依赖检查之前。
	if task.SourceFileCount() > 1 {
		if path, _, ok := s.mergedArtifact(task); ok {
			return serveLocalFile(writer, path, mergedFileName(task), rangeHeader)
		}
		// 产物缺失（重启后被系统清了临时目录 / 上次合并失败）才退回现场合并。
		return s.streamMerged(ctx, writer, task, segments, rangeHeader)
	}
	return s.serveSegment(ctx, writer, task, segments, 0, rangeHeader)
}

// serveSegment 把第 index 片发给 writer。
//
// 片内只有一个源文件时直接同源透传（含 Range），**不经后端落地** ——
// 否则每次下载都要在后端多写一份等大的临时文件。
// 片内有多个源文件时才现场合并：那时本来就没有"一个文件"可以透传。
func (s *Service) serveSegment(ctx context.Context, writer http.ResponseWriter, task *gbmodels.GbRecordCacheTask, segments []gbmodels.RecordCacheSegment, index int, rangeHeader string) error {
	if index < 0 || index >= len(segments) {
		return ErrNotDownloadable
	}
	segment := segments[index]
	sources := segment.Sources()
	if len(sources) == 0 {
		return ErrNotDownloadable
	}
	if len(sources) > 1 {
		return s.streamMerged(ctx, writer, task, []gbmodels.RecordCacheSegment{segment}, rangeHeader)
	}
	if s.content == nil {
		return ErrNotDownloadable
	}
	source := sources[0]
	if strings.TrimSpace(source.Path) == "" {
		return ErrNotDownloadable
	}
	recorder, err := s.recorderFor(task.NodeID)
	if err != nil {
		return err
	}
	var sizePtr *uint64
	if source.Size > 0 {
		size := uint64(source.Size)
		sizePtr = &size
	}
	return s.content.Stream(ctx, writer, recorder, gbrecording.ContentRequest{
		FilePath: source.Path,
		FileName: source.Name,
		FileSize: sizePtr,
		Mode:     gbrecording.CapabilityModeDownload,
		Range:    rangeHeader,
	})
}

// Tick 推进一轮状态机（导出以便测试与手动触发）。
func (s *Service) Tick(ctx context.Context) {
	if s == nil || s.repo == nil {
		return
	}
	tasks, err := s.repo.ListAdvanceable(ctx, s.config.Workers)
	if err != nil {
		return
	}
	for i := range tasks {
		task := tasks[i]
		unlock := s.locks.Lock(task.TaskID)
		s.advance(ctx, &task)
		unlock()
	}
	s.cleanupExpired(ctx)
}

// advance 推进一步：queued → 建会话拉第一片；running → 轮询或收尾。
func (s *Service) advance(ctx context.Context, task *gbmodels.GbRecordCacheTask) {
	switch task.State {
	case gbmodels.RecordCacheStateQueued:
		s.startSegment(ctx, task)
	case gbmodels.RecordCacheStateRunning:
		s.pollSegment(ctx, task)
	case gbmodels.RecordCacheStateMerging:
		// 收尾整理：全部片段已录完，正在合成一个文件。
		// 放进 Tick 是为了**自愈**：进程在合成途中重启，任务会停在 merging，
		// 下一轮 tick 重新驱动一次（幂等，已有产物就直接判定成功）。
		s.driveMerge(ctx, task)
	}
}

// startSegment 建 download 回放会话并开始对 ZLM 上的流录 MP4。
func (s *Service) startSegment(ctx context.Context, task *gbmodels.GbRecordCacheTask) {
	if s.playback == nil || s.targets == nil {
		s.fail(ctx, task, "回放服务未装配")
		return
	}
	target, err := s.targets.LoadTarget(ctx, task.ChannelID, task.DeviceID)
	if err != nil {
		s.fail(ctx, task, "通道不存在或归属迁移未完成")
		return
	}
	if !target.Online() {
		s.fail(ctx, task, "设备或通道离线")
		return
	}
	limit := segmentMediaLimit(task.DownloadSpeed)
	segStart := s.cursorOf(task)
	segEnd := segStart.Add(limit)
	// ⛔ 一次会话最多把**当前片**拉满，不许越到下一片：补拉时游标已经在片中间，
	// 若按"游标 + 上限"发请求，设备可能一口气推两片的量，这一片就装下了本该
	// 分成两片的内容（单文件几 GB、还可能超出自动合并上限）。
	if cap := segmentCapEnd(task.DecodeSegments(), segStart, limit); segEnd.After(cap) {
		segEnd = cap
	}
	if segEnd.After(task.EndTime) {
		segEnd = task.EndTime
	}
	if !segStart.Before(segEnd) {
		s.succeed(ctx, task)
		return
	}

	createCtx, cancel := context.WithTimeout(ctx, s.config.SessionTimeout)
	defer cancel()
	result, err := s.playback.Create(createCtx, gbplayback.CreateRequest{
		OwnerID:       s.ownerIDOf(task),
		DeviceID:      target.DeviceCode,
		Authorization: target.Authorization(),
		ChannelID:     strconv.FormatUint(uint64(task.ChannelID), 10),
		SIPChannelID:  target.ChannelCode,
		RecordKey:     task.RecordKey,
		// 幂等键按"任务 + 本段起点"生成：同一段重试复用同一个会话，
		// 不同段必须不同（否则补拉会被 registry 判成"已有会话"直接返回上一段的会话）。
		//
		// ⛔ 不能再用分片序号（len(segments)）：补拉现在并进同一片，序号在补拉期间
		// 根本不变 —— 拿它做键会让第二次补拉命中第一次的幂等键，拿回一个已经拆掉的会话。
		IdempotencyKey:  fmt.Sprintf("%s-%d", task.RequestID, segStart.UnixMilli()),
		PreferredNodeID: target.PreferredNodeID,
		Destination:     target.Destination(),
		Transport:       target.DeviceTransport,
		TCPMode:         target.TCPMode(),
		DefaultProtocol: s.protocol,
		Secure:          false,
		SegmentStart:    segStart,
		SegmentEnd:      segEnd,
		PlayFrom:        segStart,
		Mode:            gbplayback.ModeDownload,
		DownloadSpeed:   uint32(task.DownloadSpeed),
	})
	if err != nil {
		s.fail(ctx, task, describePlaybackError(err))
		return
	}
	session := result.Session
	if session == nil {
		s.fail(ctx, task, "回放会话未建立")
		return
	}
	nodeID, err := strconv.ParseInt(strings.TrimSpace(session.NodeID), 10, 64)
	if err != nil || nodeID <= 0 {
		_ = s.playback.StopForOwner(ctx, session.ID, s.ownerIDOf(task), "node unknown")
		s.fail(ctx, task, "回放会话未落在有效媒体节点上")
		return
	}
	recorder, err := s.recorderFor(nodeID)
	if err != nil {
		_ = s.playback.StopForOwner(ctx, session.ID, s.ownerIDOf(task), "node unavailable")
		s.fail(ctx, task, "媒体节点不可用")
		return
	}
	// 开始录制。maxSecond 给本片媒体时长 + 余量：ZLM 到点会自动切片/停录，
	// 但正常路径由我们主动停，这个上限只是防"忘了停"的兜底。
	if err := recorder.StartRecordWithType(ctx, mediaVHost, mediaApp, session.StreamID,
		zlm.RecorderTypeMP4, int(segmentMediaLimit(task.DownloadSpeed).Seconds())+120); err != nil {
		_ = s.playback.StopForOwner(ctx, session.ID, s.ownerIDOf(task), "start record failed")
		s.fail(ctx, task, "媒体节点开始录制失败")
		return
	}
	// ⛔ 录制已经开始，必须**立刻**登记保流租约：ZLM 的无人观看判定按 20 秒计，
	// 而且录制器在 ZLM 眼里不算 reader —— 晚一步登记就等于让这条流裸奔，
	// 20 秒后它会被平台的 none_reader 策略当成"没有通道归属的野流"关掉，
	// 本片随即被截成一分多钟。放在落库之前，即使落库失败走了 stopSegment，
	// 也会在 stopSegment 里被释放掉。
	s.holdLease(task, session.StreamID)

	sessionID := session.ID
	now := s.now()
	task.NodeID = nodeID
	task.VHost = mediaVHost
	task.App = mediaApp
	task.Stream = session.StreamID
	task.SessionID = &sessionID
	task.CursorAt = &segStart
	if task.StartedAt == nil {
		task.StartedAt = &now
	}
	task.State = gbmodels.RecordCacheStateRunning
	task.LastError = ""
	if err := s.repo.Update(ctx, task); err != nil {
		// 落库失败不能装作没发生：会话已经建起来了，但平台不记得它 ——
		// 立刻拆掉会话，避免留下一个没人管的通道占用。
		s.stopSegment(context.Background(), task, "state persist failed")
		return
	}
}

// pollSegment 检查本片进度：会话还在就更新实时速率，会话没了就收尾本片。
func (s *Service) pollSegment(ctx context.Context, task *gbmodels.GbRecordCacheTask) {
	if task.SessionID == nil || strings.TrimSpace(*task.SessionID) == "" {
		// 状态说 running 但没有会话（进程在"建会话→落库"之间被重启）——
		// 退回去重来一片，比卡在 running 上直到保留期结束强。
		task.State = gbmodels.RecordCacheStateQueued
		_ = s.repo.Update(ctx, task)
		return
	}
	session, ok := s.playback.GetForOwner(*task.SessionID, s.ownerIDOf(task))
	if !ok {
		s.finishSegment(ctx, task, "segment session closed")
		return
	}
	if session.State.IsTerminal() {
		s.finishSegment(ctx, task, "segment session "+string(session.State))
		return
	}
	// 墙钟保护：会话超时上限之上还没结束，说明设备没按预期推进（或倍速远低于标称），
	// 主动收掉，按真正录到的内容推进游标 —— 否则会一直占着通道直到 registry 硬掐。
	if !session.CreatedAt.IsZero() && s.now().Sub(session.CreatedAt) > SegmentWallGrace {
		s.finishSegment(ctx, task, "segment wall clock guard")
		return
	}
}

// finishSegment 收尾本片：停录 → 拆会话 → 采集文件 → 并入当前片 → 推进游标。
//
// ⛔ "片"是**内容区间**，不是"一次会话"：本次会话的文件默认并进当前片
// （设备中途静默后补拉，拉回来的还是同一段内容的延续）。只有当前片已经装到
// 单片上限时才另起一片。见 segment.go 的口径说明。
func (s *Service) finishSegment(ctx context.Context, task *gbmodels.GbRecordCacheTask, reason string) {
	segStart := s.cursorOf(task)
	stream := task.Stream
	s.stopSegment(ctx, task, reason)

	limit := segmentMediaLimit(task.DownloadSpeed)
	fresh := s.collectSegmentFiles(ctx, task.NodeID, stream, segStart)
	segments := appendSegmentFiles(task.DecodeSegments(), fresh, segStart, limit)
	if len(fresh) > 0 {
		last := segments[len(segments)-1]
		task.FileName = last.Name
		task.FilePath = last.Path
	}
	if err := task.EncodeSegments(segments); err != nil {
		s.fail(ctx, task, "分片清单写入失败")
		return
	}
	// 累计大小与时长（展示用）。
	var totalSize int64
	for _, segment := range segments {
		totalSize += segment.Size
	}
	if totalSize > 0 {
		task.CachedBytes = uint64(totalSize)
	}
	task.FileSize = uint64(totalSize)

	// 游标推进的唯一依据是**媒体时长**之和：倍速下载下墙钟与媒体时长差一个倍速，
	// 按墙钟推进会多算或少算整段录像。
	advanced := task.CachedMediaDuration()
	next := task.StartTime.Add(advanced)
	// 单片最长只覆盖本片的媒体上限，防止 ZLM 给出的异常 DurationMS 把游标推飞。
	if maxNext := segStart.Add(limit); next.After(maxNext) {
		next = maxNext
	}
	if next.Before(segStart) {
		next = segStart
	}
	// ⛔ 判据是"**整个任务**一个文件都没有"，不是"本次一行都没采到"：
	// 收尾阶段会再开一次会话去补最后几十毫秒，设备对这种极短区间常常什么都不推，
	// 这时按请求区间收尾是正确的（内容已经录到 99.9%）；报成失败等于把已经拿到的
	// 录像说成"没有录像"。
	if !next.After(segStart) {
		if len(segments) == 0 {
			s.fail(ctx, task, "本片未产生任何文件")
			return
		}
		next = segStart.Add(limit)
	}
	if next.After(task.EndTime) {
		next = task.EndTime
	}
	task.CursorAt = &next
	task.SessionID = nil

	if !next.Before(task.EndTime) {
		s.succeed(ctx, task)
		return
	}
	// 还有剩余 → 排下一段。换新 stream/会话，所以把本段的媒体落点清掉。
	task.State = gbmodels.RecordCacheStateQueued
	task.Stream = ""
	task.LastError = ""
	if err := s.repo.Update(ctx, task); err != nil {
		return
	}
}

// stopSegment 停录 + 拆会话（幂等：任一环节失败都不阻断其它环节）。
func (s *Service) stopSegment(ctx context.Context, task *gbmodels.GbRecordCacheTask, reason string) {
	stopCtx, cancel := context.WithTimeout(context.Background(), s.config.StopTimeout)
	defer cancel()
	if recorder, err := s.recorderFor(task.NodeID); err == nil && strings.TrimSpace(task.Stream) != "" {
		_ = recorder.StopRecordWithType(stopCtx, mediaVHost, mediaApp, task.Stream, zlm.RecorderTypeMP4)
	}
	if task.SessionID != nil && strings.TrimSpace(*task.SessionID) != "" && s.playback != nil {
		_ = s.playback.StopForOwner(stopCtx, *task.SessionID, s.ownerIDOf(task), reason)
	}
	// ⛔ 顺序不能颠倒：必须**先**停录、拆会话，**最后**才放租约。
	// 反过来会让流在 stopRecord 之前就失去保活，被 ZLM 按"无人观看"抢先回收 ——
	// 文件还没封口就被拆掉，最后一片永远拿不到完整内容。
	s.releaseLease(task)
}

// succeed 结束任务：只有一个源文件时直接判定成功；有多个源文件就先转入 merging
// 由后台把它们拼成一个文件，拼完才算成功。
//
// ⛔ 判据是**源文件总数 > 1**，不是"分片数 > 1"：补拉收进同一片之后，
// 30 分钟录像的常见形态是「1 片 3 个源文件」——按分片数判会直接跳过合成，
// 用户下到的只有第一段（还播得动，是最难发现的那类错）。
//
// ⛔ 为什么要多这一步而不是"下载时现拼"：浏览器用原生下载时，服务端需要
// 一开始就给出准确的响应体与长度；现场拼接会让用户在下载栏里干等一段
// 进度为 0 的时间，比改造前（内存里转圈）还难解释。放在收尾期，下载就是
// 纯文件传输 —— 点完立刻开始、进度是真实网速、可暂停续传。
func (s *Service) succeed(ctx context.Context, task *gbmodels.GbRecordCacheTask) {
	s.releaseLease(task)
	now := s.now()
	if task.SourceFileCount() > 1 {
		task.State = gbmodels.RecordCacheStateMerging
		task.LastError = ""
		task.SessionID = nil
		task.Stream = ""
		task.CursorAt = &task.EndTime
		if err := s.repo.Update(ctx, task); err == nil {
			// 立刻驱动一次，别等下一轮 tick —— 用户可能正盯着页面等下载。
			s.driveMerge(ctx, task)
			return
		}
		// 落库失败就退回直接成功：状态没进去，留在内存里的 merging 没意义。
	}
	task.State = gbmodels.RecordCacheStateSucceeded
	task.LastError = ""
	task.SessionID = nil
	task.Stream = ""
	task.FinishedAt = &now
	task.CursorAt = &task.EndTime
	expires := now.Add(time.Duration(s.retentionDays()) * 24 * time.Hour)
	task.ExpiresAt = &expires
	_ = s.repo.Update(ctx, task)
}

// driveMerge 在后台把多分片合成一个文件，完成后把任务判定为 succeeded。
//
// 异步的原因：合并要把全部分片从媒体节点拉回本地再重写索引，量级是几十秒到
// 几分钟。同步做会把 Tick 的推进循环整段卡住（其它任务进度、取消、超时保护
// 全都停摆）。
func (s *Service) driveMerge(ctx context.Context, task *gbmodels.GbRecordCacheTask) {
	if s == nil || task == nil || strings.TrimSpace(task.TaskID) == "" {
		return
	}
	if !s.beginMerge(task.TaskID) {
		return
	}
	taskID := task.TaskID
	snapshot := *task
	// ⛔ 用**独立**的 WaitGroup：s.wg 里还挂着 Tick 主循环，它只在 Close 时退出，
	// 拿它等合并会永远等不到（测试直接死锁）。
	s.mergeWg.Add(1)
	go func() {
		defer s.mergeWg.Done()
		defer s.endMerge(taskID)
		// ⛔ 用服务生命周期而不是 Tick 的 ctx：Tick 的 ctx 可能随请求/单轮结束，
		// 拿它做合并会在合成途中被取消，产出半截文件。
		mergeCtx, cancel := context.WithTimeout(s.lifecycle, s.config.MergeTimeout)
		defer cancel()
		err := s.ensureMergedArtifact(mergeCtx, &snapshot)
		s.completeMerge(mergeCtx, taskID, err)
	}()
}

// completeMerge 落终态：合并成功与否都让任务**成功**。
//
// ⛔ 合并失败不能让任务失败：分段文件本身是好的，用户仍能按分段下载；
// 报成失败等于把"能拿到的录像"说成"没有录像"。失败只记日志，
// 下载接口会回 ErrMergeFailed 并说明原因。
func (s *Service) completeMerge(ctx context.Context, taskID string, mergeErr error) {
	stored, err := s.repo.FindByTaskID(ctx, taskID, nil)
	if err != nil || stored == nil {
		// 任务已被删除/保留期清理：产物也跟着删掉，别留下无主的文件。
		s.removeMergedArtifact(taskID)
		return
	}
	if stored.State != gbmodels.RecordCacheStateMerging {
		// 状态被别的路径改过（取消/删除/重试）：不抢回控制权。
		s.removeMergedArtifact(taskID)
		return
	}
	if mergeErr != nil {
		s.removeMergedArtifact(taskID)
	}
	now := s.now()
	stored.State = gbmodels.RecordCacheStateSucceeded
	stored.LastError = ""
	stored.FinishedAt = &now
	stored.CursorAt = &stored.EndTime
	expires := now.Add(time.Duration(s.retentionDays()) * 24 * time.Hour)
	stored.ExpiresAt = &expires
	_ = s.repo.Update(ctx, stored)
}

func (s *Service) beginMerge(taskID string) bool {
	s.mergingMu.Lock()
	defer s.mergingMu.Unlock()
	if s.merging == nil {
		s.merging = map[string]struct{}{}
	}
	if _, running := s.merging[taskID]; running {
		return false
	}
	s.merging[taskID] = struct{}{}
	return true
}

func (s *Service) endMerge(taskID string) {
	s.mergingMu.Lock()
	defer s.mergingMu.Unlock()
	delete(s.merging, taskID)
}

// waitMerges 等所有后台合并结束（测试与停机用）。
func (s *Service) waitMerges() { s.mergeWg.Wait() }

func (s *Service) fail(ctx context.Context, task *gbmodels.GbRecordCacheTask, reason string) {
	// 失败路径可能没经过 stopSegment（例如建会话早退），租约必须在这里兜底释放，
	// 否则会留下一条没人认领的保活流，永久占着设备通道。
	s.releaseLease(task)
	now := s.now()
	task.State = gbmodels.RecordCacheStateFailed
	task.LastError = truncateReason(reason)
	task.SessionID = nil
	task.Stream = ""
	task.FinishedAt = &now
	_ = s.repo.Update(ctx, task)
}

// cleanupExpired 按保留期清理产出文件（保留任务行，状态标 expired，
// 这样用户还能看到"这条缓存曾经存在、文件已按保留策略删除"）。
//
// ⛔ 收藏（favorite=1）的任务不在清理集合里：过滤点在 ListExpired 的 SQL 上。
// 不要在循环里 `continue` —— 每轮只取 limit 条，被跳过的收藏任务会一直占着名额。
func (s *Service) cleanupExpired(ctx context.Context) {
	if s.repo == nil {
		return
	}
	tasks, err := s.repo.ListExpired(ctx, s.now(), 20)
	if err != nil {
		return
	}
	for i := range tasks {
		task := tasks[i]
		unlock := s.locks.Lock(task.TaskID)
		s.removeFiles(ctx, &task)
		task.State = gbmodels.RecordCacheStateExpired
		_ = s.repo.Update(ctx, &task)
		unlock()
	}
}

// removeFiles 删除任务的全部产出文件（尽力而为：删不掉的留下次再删）。
//
// ⛔ 必须遍历**片内的每个源文件**：一片可能由多次会话（补拉）拼成，
// 只删片级那一个名字会在媒体节点上留下几个没人再引用、也不会被回收的 MP4。
func (s *Service) removeFiles(ctx context.Context, task *gbmodels.GbRecordCacheTask) {
	recorder, err := s.recorderFor(task.NodeID)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, s.config.StopTimeout)
	defer cancel()
	for _, segment := range task.DecodeSegments() {
		for _, source := range segment.Sources() {
			if strings.TrimSpace(source.Stream) == "" || strings.TrimSpace(source.Name) == "" {
				continue
			}
			period := strings.TrimSpace(source.Period)
			if period == "" {
				period = strings.TrimSpace(segment.Period)
			}
			if period == "" {
				period = s.now().Format("2006-01-02")
			}
			_ = recorder.DeleteMP4RecordFile(ctx, mediaVHost, mediaApp, source.Stream, period, source.Name)
		}
	}
	// 收尾期合成的那个产物也要一起清：不清就会在本地磁盘上留下一个
	// 没人再引用、也不会被保留期回收的大文件（任务行删了，路径就找不回来了）。
	s.removeMergedArtifact(task.TaskID)
	_ = task.EncodeSegments(nil)
	task.CachedBytes = 0
	task.FileSize = 0
	task.FileName = ""
	task.FilePath = ""
}

// collectSegmentFiles 采集本片已落盘的文件（含**真实**媒体时长与字节数）。
//
// 优先读平台已入库的记录（on_record_mp4 hook 带 time_len / file_size）；
// 只有入库还没追上时才退回 ZLM 的文件列表 —— 那条路能拿到文件名与路径
// （下载仍然可用），但**时长与大小会是 0**，调用方据此走保守推进并告警。
func (s *Service) collectSegmentFiles(ctx context.Context, nodeID int64, stream string, segStart time.Time) []gbmodels.RecordCacheSegment {
	stream = strings.TrimSpace(stream)
	if stream == "" {
		return nil
	}
	if s.fileIndex != nil {
		indexed, err := s.fileIndex.IndexedSegments(ctx, stream)
		if err == nil && len(indexed) > 0 {
			out := make([]gbmodels.RecordCacheSegment, 0, len(indexed))
			for _, file := range indexed {
				start := file.StartedAt
				if start.IsZero() {
					start = segStart
				}
				period := strings.TrimSpace(file.Period)
				if period == "" {
					period = start.Format("2006-01-02")
				}
				out = append(out, gbmodels.RecordCacheSegment{
					Stream: stream, Name: file.Name, Path: file.Path,
					Size: file.Size, MS: file.DurationMS,
					Start:  start.Format(time.RFC3339),
					End:    start.Add(time.Duration(file.DurationMS) * time.Millisecond).Format(time.RFC3339),
					Period: period,
				})
			}
			return out
		}
	}
	recorder, err := s.recorderFor(nodeID)
	if err != nil {
		return nil
	}
	files := s.listSegmentFiles(ctx, recorder, stream, segStart)
	out := make([]gbmodels.RecordCacheSegment, 0, len(files))
	for _, file := range files {
		size := int64(0)
		if file.FileSize != nil {
			size = *file.FileSize
		}
		ms := int64(0)
		if file.DurationMS != nil {
			ms = *file.DurationMS
		}
		path := strings.TrimSpace(file.FilePath)
		if path == "" && strings.TrimSpace(file.Folder) != "" {
			path = strings.TrimRight(file.Folder, "/") + "/" + file.FileName
		}
		out = append(out, gbmodels.RecordCacheSegment{
			Stream: stream, Name: file.FileName, Path: path, Size: size, MS: ms,
			Start:  segStart.Format(time.RFC3339),
			End:    segStart.Add(time.Duration(ms) * time.Millisecond).Format(time.RFC3339),
			Period: segStart.Format("2006-01-02"),
		})
	}
	return out
}

// listSegmentFiles 列出某条 stream 在某天落盘的 MP4 文件。
// ZLM 的 getMP4Record 按「日期」分区查询，所以跨天的分片要查两天再合并。
func (s *Service) listSegmentFiles(ctx context.Context, recorder Recorder, stream string, segStart time.Time) []zlm.MP4RecordFile {
	seen := make(map[string]struct{})
	var files []zlm.MP4RecordFile
	days := []string{segStart.Format("2006-01-02")}
	// 分片最长 20 分钟媒体时长，但设备倍速下墙钟可能不到 1 分钟，
	// 因此跨天只可能跨"下一天"，查两天足够。
	if next := segStart.Add(24 * time.Hour).Format("2006-01-02"); next != days[0] {
		days = append(days, next)
	}
	for _, period := range days {
		list, err := recorder.GetMP4RecordFiles(ctx, mediaVHost, mediaApp, stream, period)
		if err != nil {
			continue
		}
		for _, file := range list {
			if strings.TrimSpace(file.FileName) == "" {
				continue
			}
			if _, ok := seen[file.FileName]; ok {
				continue
			}
			seen[file.FileName] = struct{}{}
			files = append(files, file)
		}
	}
	return files
}

func (s *Service) recorderFor(nodeID int64) (Recorder, error) {
	if s.nodes == nil || s.client == nil || nodeID <= 0 {
		return nil, ErrNodeUnavailable
	}
	n, ok := s.nodes.Get(nodeID)
	if !ok || n == nil {
		return nil, ErrNodeUnavailable
	}
	return s.client(n), nil
}

func (s *Service) cursorOf(task *gbmodels.GbRecordCacheTask) time.Time {
	if task.CursorAt != nil && !task.CursorAt.IsZero() {
		return *task.CursorAt
	}
	return task.StartTime
}

// cacheOwnerNamespace 把缓存任务与「用户在回放页开的会话」分到不同槽位。
//
// ⛔ 回放页建会话用的 owner 是登录用户 ID（如 "1"），而 playback registry 的
// 排他键是 (owner, channel)：缓存任务若复用同一个 ID，就等于和用户自己正在看
// 的那路回放抢同一个槽位 —— 点「缓存到服务器」必然吃 429「当前通道已有回放会话」
// （实测：任务 created 17:00:47 → finished 17:00:48，stream/session_id 全空，
// 一次会话都没建成）。加前缀后两者各占各的槽位，「边看回放边缓存」在平台侧成立。
//
// ⚠️ 这只解除**平台自己加**的互斥；设备能否同时扛两路历史流是另一回事，
// 需要实测（失败时设备侧表现为某一路收不到 RTP 或会话被设备拒绝）。
const cacheOwnerNamespace = "record-cache:"

func (s *Service) ownerIDOf(task *gbmodels.GbRecordCacheTask) string {
	return cacheOwnerNamespace + strconv.FormatUint(uint64(task.CreatedByUser), 10)
}

// holdLease 登记本片流的保流租约（防止 ZLM 的 20 秒无人观看判定把流回收）。
// 幂等：同一任务重复登记只有第一次生效。
func (s *Service) holdLease(task *gbmodels.GbRecordCacheTask, stream string) {
	if s.leases == nil || task == nil || strings.TrimSpace(stream) == "" {
		return
	}
	consumer := "record-cache:" + task.TaskID
	s.leaseMu.Lock()
	defer s.leaseMu.Unlock()
	if _, exists := s.heldLeases[task.TaskID]; exists {
		return
	}
	s.heldLeases[task.TaskID] = s.leases.Acquire(stream, 0, consumer)
}

// releaseLease 释放本任务的保流租约。幂等，且在任何终态路径上都必须调用 ——
// 漏掉会让一条已经没人管的流永久挂在 ZLM 上占着设备通道。
func (s *Service) releaseLease(task *gbmodels.GbRecordCacheTask) {
	if s == nil || task == nil {
		return
	}
	s.leaseMu.Lock()
	lease, exists := s.heldLeases[task.TaskID]
	if exists {
		delete(s.heldLeases, task.TaskID)
	}
	s.leaseMu.Unlock()
	if exists && lease != nil {
		_ = lease.Release()
	}
}

// resolveSpeed 判定本任务使用的下载倍速：优先设备能力，取不到用兜底值。
func (s *Service) resolveSpeed(ctx context.Context, target Target, channelCode string) int {
	if s.speed == nil {
		return DefaultDownloadSpeed
	}
	if speed := s.speed.MaxDownloadSpeed(ctx, target.DevicePK, channelCode); validDownloadSpeed(speed) {
		return speed
	}
	return DefaultDownloadSpeed
}

func validDownloadSpeed(speed int) bool {
	for _, option := range DownloadSpeedOptions {
		if speed == option {
			return true
		}
	}
	return false
}

// retentionDays 返回当前生效的保留天数（配置改动立即生效）。
func (s *Service) retentionDays() int {
	if s.retention != nil {
		if days := s.retention(); days > 0 {
			return days
		}
	}
	if s.config.RetentionDays > 0 {
		return s.config.RetentionDays
	}
	return 7
}

func truncateReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if len(reason) <= 500 {
		return reason
	}
	return reason[:500]
}

// describePlaybackError 把回放服务的失败翻译成用户能看懂的一句话。
func describePlaybackError(err error) string {
	var serviceErr *gbplayback.ServiceError
	if errors.As(err, &serviceErr) {
		switch serviceErr.Stage {
		case "media_wait":
			return "设备录像推流超时（设备未按预期推送媒体）"
		case "node":
			return "无可用媒体节点"
		case "rtp":
			return "媒体端口申请失败"
		case "invite":
			return "设备回放信令建立失败"
		default:
			return "设备录像拉流失败：" + serviceErr.Stage
		}
	}
	switch {
	case errors.Is(err, gbplayback.ErrPlaybackBusy):
		return "当前通道已有回放会话"
	case errors.Is(err, gbplayback.ErrPlaybackNotFound):
		return "回放会话不存在"
	default:
		return "设备录像拉流失败"
	}
}

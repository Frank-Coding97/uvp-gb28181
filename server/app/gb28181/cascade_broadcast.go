package gb28181

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/repository"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.com/uvp-gb28181/app/gb28181/ptz"
	"uvplatform.com/uvp-gb28181/app/gb28181/talk"
	"uvplatform.com/uvp-gb28181/app/gb28181/uac"
	"uvplatform.com/uvp-gb28181/app/global/app"
)

type cascadeBroadcastDeviceSender interface {
	SendMessageTracked(context.Context, string, string, string, []byte) (uac.TrackedMessageResult, error)
}

type cascadeBroadcastBinding struct {
	MessageHash        string     `json:"messageHash,omitempty"`
	NotifyStatus       int        `json:"notifyStatus,omitempty"`
	DeviceResult       string     `json:"deviceResult,omitempty"`
	ResultDelivered    bool       `json:"resultDelivered,omitempty"`
	ResultAttemptAt    *time.Time `json:"resultAttemptAt,omitempty"`
	ResultAttempts     uint       `json:"resultAttempts,omitempty"`
	LocalSourceID      string     `json:"localSourceID,omitempty"`
	AudioLeaseID       string     `json:"audioLeaseID,omitempty"`
	AudioCallID        string     `json:"audioCallID,omitempty"`
	AudioCSeq          uint32     `json:"audioCSeq,omitempty"`
	AudioFromTag       string     `json:"audioFromTag,omitempty"`
	AudioBranch        string     `json:"audioBranch,omitempty"`
	AudioRequestHash   string     `json:"audioRequestHash,omitempty"`
	UpstreamSN         int        `json:"upstreamSN"`
	DownstreamSN       int        `json:"downstreamSN"`
	UpstreamSourceID   string     `json:"upstreamSourceID"`
	DeviceCode         string     `json:"deviceCode"`
	ChannelCode        string     `json:"channelCode"`
	ConfigRevision     uint64     `json:"configRevision"`
	ProjectionRevision uint64     `json:"projectionRevision"`
	ProfileVersion     string     `json:"profileVersion"`
	PublishedChannelID string     `json:"publishedChannelID"`
	Source             ptz.Target `json:"source"`
	OwnerDeptID        uint       `json:"ownerDeptID"`
}

type cascadeBroadcastRuntime struct {
	store   *repository.GormRepository
	clients *cascadePlatformClientFactory
	sender  cascadeBroadcastDeviceSender
	db      *gorm.DB
	serial  atomic.Uint32

	mu                 sync.Mutex
	lifetime           context.Context
	cancel             context.CancelFunc
	active             int
	closed             bool
	sweepStarted       bool
	done               chan struct{}
	doneClosed         bool
	cleanupMu          sync.Mutex
	dialogClient       *sipgo.Client
	audioDialogFactory cascadeAudioServerDialogFactory
	audioMu            sync.Mutex
	audioSessions      map[string]*cascadeAudioSession
}

func newCascadeBroadcastRuntime(db *gorm.DB, store *repository.GormRepository, clients *cascadePlatformClientFactory, sender cascadeBroadcastDeviceSender, dialogClients ...*sipgo.Client) *cascadeBroadcastRuntime {
	lifetime, cancel := context.WithCancel(context.Background())
	runtime := &cascadeBroadcastRuntime{db: db, store: store, clients: clients, sender: sender, lifetime: lifetime, cancel: cancel, done: make(chan struct{}), audioSessions: make(map[string]*cascadeAudioSession)}
	if len(dialogClients) > 0 {
		runtime.dialogClient = dialogClients[0]
	}
	return runtime
}

func (m *cascadeBroadcastRuntime) Handle(req *sip.Request, tx sip.ServerTransaction) bool {
	if req == nil || tx == nil || req.Method != sip.MESSAGE {
		return false
	}
	head, err := manscdp.ParseHead(req.Body())
	if err != nil || head.CmdType != manscdp.CmdBroadcast {
		return false
	}
	// Device responses belong to the authenticated device MESSAGE router.
	if head.XMLName.Local == "Response" {
		return false
	}
	if m == nil || m.store == nil || m.clients == nil || m.sender == nil || m.db == nil {
		if app.ZapLog != nil {
			app.ZapLog.Warn("级联广播运行时未就绪",
				zap.String("event", "cascade.broadcast.unavailable"),
				zap.String("reason", "runtime_not_ready"),
				zap.String("call_id", cascadeCallID(req)),
				zap.String("peer", cascadePeerLabel(req)))
		}
		_ = tx.Respond(sip.NewResponseFromRequest(req, 503, "Cascade Broadcast Unavailable", nil))
		return true
	}
	ctx, release, ok := m.acquire(context.Background(), 10*time.Second)
	if !ok {
		if app.ZapLog != nil {
			app.ZapLog.Warn("级联广播运行时忙",
				zap.String("event", "cascade.broadcast.unavailable"),
				zap.String("reason", "runtime_busy"),
				zap.String("call_id", cascadeCallID(req)),
				zap.String("peer", cascadePeerLabel(req)))
		}
		_ = tx.Respond(sip.NewResponseFromRequest(req, 503, "Cascade Broadcast Unavailable", nil))
		return true
	}
	defer release()
	reject := func(status int, reason string) bool {
		// 对讲/广播失败在上游表现为「点了没反应」。被拒原因必须留痕，
		// 否则只能靠 SIP 抓包区分「平台没匹配上 / 目标不符 / 服务未就绪」。
		if app.ZapLog != nil {
			app.ZapLog.Warn("级联广播请求被拒绝",
				zap.String("event", "cascade.broadcast.rejected"),
				zap.Int("status", status),
				zap.String("reason", reason),
				zap.String("call_id", cascadeCallID(req)),
				zap.String("peer", cascadePeerLabel(req)),
				zap.String("device_id", strings.TrimSpace(head.DeviceID)))
		}
		_ = tx.Respond(sip.NewResponseFromRequest(req, status, reason, nil))
		return true
	}
	platforms, err := m.store.ListPlatforms(ctx)
	if err != nil {
		return reject(503, "Cascade Broadcast Store Unavailable")
	}
	platform, ambiguous := findCascadeControlPeer(req, platforms)
	if platform == nil || ambiguous || !platform.Enabled {
		return reject(403, "Cascade Broadcast Upstream Rejected")
	}
	if req.To() == nil || req.CallID() == nil || req.CSeq() == nil || req.CSeq().MethodName != sip.MESSAGE || req.CSeq().SeqNo == 0 {
		return reject(403, "Cascade Broadcast Target Mismatch")
	}
	profile := cascadeWireProfile(*platform)
	notify, err := manscdp.ParseCascadeBroadcastNotify(profile, req.Body())
	if err != nil {
		return reject(400, "Invalid Cascade Broadcast")
	}
	binding, err := resolveCascadeTalkAuthorization(ctx, m.db, m.store, *platform, notify.TargetID)
	if err != nil {
		return reject(403, "Cascade Broadcast Target Not Published")
	}
	// ⛔⛔ 目标校验必须放在**知道本次广播目标之后**，且要认两种写法：
	// 有的上级严格按「平台级联」把 MESSAGE 寻址到平台号；有的把级联平台当
	//「设备」用，RequestURI/To 写的是**被广播通道的编码**（LiveGBS 3.6.10 实测：
	// `MESSAGE sip:37010301021320000002@3402000001`，端口与 From 都是平台侧的）。
	// 只认平台号会让语音广播直接 403，对端 UI 表现为「点了没反应」。
	// 放宽范围仅限「平台号 / 本次广播的发布通道 ID / 该通道与设备的编码」，
	// 其余校验（TargetID 必须已发布、授权、租约、下行 Notify 结果）一律不变。
	if !cascadeBroadcastCalleeMatches(req.To().Address.User, platform.LocalDeviceID, notify.TargetID, binding.Source.ChannelCode, binding.Source.DeviceCode) ||
		!cascadeBroadcastCalleeMatches(req.Recipient.User, platform.LocalDeviceID, notify.TargetID, binding.Source.ChannelCode, binding.Source.DeviceCode) {
		if app.ZapLog != nil {
			app.ZapLog.Warn("级联广播目标不匹配",
				zap.String("event", "cascade.broadcast.target_mismatch"),
				zap.String("call_id", cascadeCallID(req)),
				zap.String("peer", cascadePeerLabel(req)),
				zap.String("request_uri_user", req.Recipient.User),
				zap.String("to_user", req.To().Address.User),
				zap.String("local_device_id", platform.LocalDeviceID),
				zap.String("published_channel_id", notify.TargetID),
				zap.String("channel_code", binding.Source.ChannelCode),
				zap.String("device_code", binding.Source.DeviceCode))
		}
		return reject(403, "Cascade Broadcast Target Mismatch")
	}
	if binding.Source.Profile.Version == "" {
		return reject(503, "Cascade Broadcast Protocol Profile Unavailable")
	}
	downstreamSN := int(m.serial.Add(1)%999999 + 1)
	downstreamProfile := binding.Source.Profile
	body, err := manscdp.MarshalProfiledXML(downstreamProfile, struct {
		XMLName  xml.Name `xml:"Notify"`
		CmdType  string   `xml:"CmdType"`
		SN       int      `xml:"SN"`
		SourceID string   `xml:"SourceID"`
		TargetID string   `xml:"TargetID"`
	}{CmdType: manscdp.CmdBroadcast, SN: downstreamSN, SourceID: platform.LocalDeviceID, TargetID: binding.Source.ChannelCode})
	if err != nil {
		return reject(400, "Cascade Broadcast Encoding Failed")
	}
	var saved cascadeBroadcastBinding
	rowKey := fmt.Sprintf("broadcast/%d/%s/%d/%d", platform.ID, req.CallID().Value(), req.CSeq().SeqNo, notify.SN)
	row := model.GbCascadeMediaSession{Kind: model.CascadeMediaSessionKindBroadcast, PlatformID: platform.ID, DialogKey: rowKey, CallID: req.CallID().Value(), CSeq: uint64(req.CSeq().SeqNo), SourceDeviceID: uint64(binding.Source.DeviceID), SourceChannelID: uint64(binding.Source.ChannelID), PublishedChannelID: notify.TargetID, ProfileVersion: string(platform.ProfileOverride), SDPSummary: "", State: model.CascadeMediaSessionStateReceived}
	saved = cascadeBroadcastBinding{UpstreamSN: notify.SN, DownstreamSN: downstreamSN, UpstreamSourceID: notify.SourceID, DeviceCode: binding.Source.DeviceCode, ChannelCode: binding.Source.ChannelCode, ConfigRevision: platform.ConfigRevision, ProjectionRevision: platform.ProjectionRevision, ProfileVersion: string(effectiveCascadeProfile(*platform)), PublishedChannelID: notify.TargetID, Source: binding.Source, OwnerDeptID: binding.OwnerDeptID}
	saved.LocalSourceID = platform.LocalDeviceID
	digest := sha256.Sum256(req.Body())
	saved.MessageHash = hex.EncodeToString(digest[:])
	created, err := m.reserveBroadcast(ctx, &row, &saved, binding)
	if errors.Is(err, talk.ErrLeaseConflict) {
		return reject(486, "Cascade Audio Channel Busy")
	}
	if errors.Is(err, errCascadeBroadcastConflict) {
		return reject(409, "Cascade Broadcast Transaction Changed")
	}
	if err != nil {
		return reject(503, "Cascade Broadcast Persistence Failed")
	}
	if !created {
		status := saved.NotifyStatus
		if status == 0 {
			return reject(491, "Cascade Broadcast In Progress")
		}
		return reject(status, "Cascade Broadcast Previous Result")
	}
	if err := binding.Validate(ctx, m.db, m.store); err != nil {
		m.cleanupBroadcast(row.DialogKey, 403, model.CascadeMediaSessionStateAborted, "authorization changed")
		return reject(403, "Cascade Broadcast Authorization Changed")
	}
	destination := net.JoinHostPort(binding.Source.IP, strconv.Itoa(binding.Source.Port))
	result, sendErr := m.sender.SendMessageTracked(ctx, binding.Source.DeviceCode, destination, binding.Source.Transport, body)
	if sendErr != nil || result.StatusCode < 200 || result.StatusCode >= 300 {
		m.cleanupBroadcast(row.DialogKey, 502, model.CascadeMediaSessionStateFailed, "downstream notification failed")
		return reject(502, "Cascade Broadcast Downstream Failed")
	}
	if err := binding.Validate(ctx, m.db, m.store); err != nil {
		m.cleanupBroadcast(row.DialogKey, 403, model.CascadeMediaSessionStateAborted, "authorization changed")
		return reject(403, "Cascade Broadcast Authorization Changed")
	}
	if err := m.updateBroadcast(ctx, row.DialogKey, 200, "", "", time.Now()); err != nil {
		return reject(503, "Cascade Broadcast Result Persistence Failed")
	}
	_ = tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil))
	return true
}

func (m *cascadeBroadcastRuntime) RouteBroadcastMessage(ctx context.Context, deviceCode string, body []byte) (bool, error) {
	if m == nil || m.store == nil || m.clients == nil || m.db == nil {
		return false, nil
	}
	ctx, release, ok := m.acquire(ctx, 10*time.Second)
	if !ok {
		return true, fmt.Errorf("cascade broadcast runtime is stopping")
	}
	defer release()
	response, err := manscdp.ParseBroadcastResponse(body)
	if err != nil {
		return false, err
	}
	sessions, err := m.store.ListNonterminalBroadcastSessions(ctx)
	if err != nil {
		return true, err
	}
	var row *model.GbCascadeMediaSession
	var binding cascadeBroadcastBinding
	for i := range sessions {
		if !strings.HasPrefix(sessions[i].DialogKey, "broadcast/") || sessions[i].SourceDeviceID == 0 {
			continue
		}
		var candidate cascadeBroadcastBinding
		if json.Unmarshal([]byte(sessions[i].SDPSummary), &candidate) != nil || candidate.DownstreamSN != response.SN || candidate.DeviceCode != strings.TrimSpace(deviceCode) || (response.TargetID != "" && candidate.ChannelCode != response.TargetID) {
			continue
		}
		if row != nil {
			return true, fmt.Errorf("ambiguous cascade broadcast response")
		}
		copy := sessions[i]
		row, binding = &copy, candidate
	}
	if row == nil {
		return false, nil
	}
	platform, err := m.store.FindPlatform(ctx, row.PlatformID)
	if err != nil || platform == nil || !platform.Enabled || platform.ConfigRevision != binding.ConfigRevision || platform.ProjectionRevision != binding.ProjectionRevision || string(effectiveCascadeProfile(*platform)) != binding.ProfileVersion {
		return true, fmt.Errorf("cascade broadcast authorization changed")
	}
	// 真机（大华，2026-10-24 抓包）在 Broadcast 应答里回的是**通道编码**而不是
	// 所属设备的父编码，且不带 TargetID：
	//   <Response><CmdType>Broadcast</CmdType><SN>3</SN>
	//   <DeviceID>37010301021320000006</DeviceID><Result>OK</Result></Response>
	// 而我们下发 Notify 时 TargetID 本来就是通道编码，所以「回显的是哪个 ID」本身
	// 没有歧义 —— 只接受父编码会把合法应答判成身份不符，进而让上级永远收不到
	// 广播结果。这里两个编码都认，其余校验（平台版本/来源绑定/租约）保持不变。
	if !broadcastResponseIDMatches(response.DeviceID, binding.DeviceCode, binding.ChannelCode) ||
		(response.TargetID != "" && !broadcastResponseIDMatches(response.TargetID, binding.DeviceCode, binding.ChannelCode)) {
		return true, fmt.Errorf("cascade broadcast response identity mismatch")
	}
	if binding.Source.DeviceEpoch <= 0 || uint64(binding.Source.DeviceID) != row.SourceDeviceID || uint64(binding.Source.ChannelID) != row.SourceChannelID || binding.PublishedChannelID != row.PublishedChannelID {
		return true, fmt.Errorf("cascade broadcast authorization binding missing or inconsistent")
	}
	authorization := cascadeTalkAuthorization{Platform: *platform, PublishedID: row.PublishedChannelID, Source: binding.Source, OwnerDeptID: binding.OwnerDeptID}
	if err := authorization.Validate(ctx, m.db, m.store); err != nil {
		return true, err
	}
	result := "ERROR"
	if response.Success() {
		result = "OK"
	}
	if binding.DeviceResult != "" && binding.DeviceResult != result {
		return true, fmt.Errorf("cascade broadcast device result changed")
	}
	if binding.ResultDelivered {
		return true, nil
	}
	if result == "ERROR" {
		if err := m.stopCascadeAudioForBusinessEnd(row.DialogKey); err != nil {
			return true, err
		}
	}
	if err := m.recordBroadcastResult(ctx, row.DialogKey, result, time.Now()); err != nil {
		return true, err
	}
	claimed, err := m.claimBroadcastResultDelivery(ctx, row.DialogKey, time.Now())
	if err != nil {
		return true, err
	}
	if !claimed {
		return true, nil
	}
	out, err := manscdp.BuildBroadcastResponseWithProfile(cascadeWireProfile(*platform), binding.UpstreamSN, row.PublishedChannelID, result)
	if err != nil {
		return true, err
	}
	client, err := m.clients.NewClient(*platform)
	if err != nil {
		return true, err
	}
	send := client.(*cascadePlatformClient).transactions.SendMessage(ctx, out, fmt.Sprintf("cascade-broadcast-%d-%s", platform.ID, row.CallID))
	if send.StatusCode < 200 || send.StatusCode >= 300 {
		return true, fmt.Errorf("cascade broadcast response delivery failed: status=%d", send.StatusCode)
	}
	return true, m.finishBroadcastResult(ctx, row.DialogKey, result, time.Now())
}

// broadcastResponseIDMatches 允许应答里的标识是「父设备编码」或「通道编码」之一。
// 两者都是我们下发 Notify 时用过的值（TargetID 用通道编码、SourceID 用平台编码），
// 不同厂商回显哪个并不统一，所以不能只认一个。
func broadcastResponseIDMatches(value string, candidates ...string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for _, candidate := range candidates {
		if candidate != "" && value == strings.TrimSpace(candidate) {
			return true
		}
	}
	return false
}

// cascadeBroadcastCalleeMatches 判定广播 MESSAGE 的收件人是否指向本次广播。
// 平台编码是规范写法；把级联平台当「设备」用的实现（LiveGBS 3.6.10 实测）
// 会把收件人写成通道编码。两者都算「打给我们」，其余取值一律不认。
func cascadeBroadcastCalleeMatches(value string, candidates ...string) bool {
	return broadcastResponseIDMatches(value, candidates...)
}

func (m *cascadeBroadcastRuntime) acquire(parent context.Context, timeout time.Duration) (context.Context, func(), bool) {
	if parent == nil {
		parent = context.Background()
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, nil, false
	}
	m.active++
	lifetime := m.lifetime
	m.mu.Unlock()
	ctx, cancel := context.WithCancel(lifetime)
	parentCancel := cancel
	stopParent := context.AfterFunc(parent, cancel)
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
	}
	return ctx, func() {
		stopParent()
		cancel()
		parentCancel()
		m.mu.Lock()
		m.active--
		m.closeDoneIfIdleLocked()
		m.mu.Unlock()
	}, true
}

func (m *cascadeBroadcastRuntime) closeDoneIfIdleLocked() {
	if m.closed && m.active == 0 && !m.doneClosed {
		close(m.done)
		m.doneClosed = true
	}
}

func (m *cascadeBroadcastRuntime) cleanupBroadcast(key string, notifyStatus int, state model.CascadeMediaSessionState, reason string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := m.updateBroadcast(ctx, key, notifyStatus, state, reason, time.Now()); err != nil {
		app.Log(ctx).Error("级联广播任务清理失败", zap.String("event", "cascade.broadcast.cleanup_failed"), zap.String("dialog_key", key), zap.Error(err))
	}
}

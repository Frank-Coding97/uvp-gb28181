package gb28181

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"
	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/repository"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.com/uvp-gb28181/app/gb28181/sdp"
	"uvplatform.com/uvp-gb28181/app/gb28181/talk"
)

const cascadeBroadcastPullTimeout = 30 * time.Second

type cascadeBroadcastPull struct {
	Row           model.GbCascadeMediaSession
	Binding       cascadeBroadcastBinding
	Authorization cascadeTalkAuthorization
	Offer         sdp.CascadeAudioOffer
	Peer          cascadePeer
}

// resolveCascadeBroadcastPull resolves only an existing broadcast notification;
// an arbitrary device audio INVITE cannot create an upstream source mapping.
// A nil match leaves ordinary browser broadcast requests to their own handler.
func resolveCascadeBroadcastPull(ctx context.Context, db *gorm.DB, store *repository.GormRepository, req *sip.Request, now time.Time) (*cascadeBroadcastPull, error) {
	if req == nil || req.Method != sip.INVITE {
		return nil, nil
	}
	if req.From() == nil || req.To() == nil {
		return nil, fmt.Errorf("incomplete audio INVITE identity")
	}
	rows, err := store.ListNonterminalBroadcastSessions(ctx)
	if err != nil {
		return nil, err
	}
	var match *cascadeBroadcastPull
	for _, row := range rows {
		if !strings.HasPrefix(row.DialogKey, "broadcast/") {
			continue
		}
		var binding cascadeBroadcastBinding
		if json.Unmarshal([]byte(row.SDPSummary), &binding) != nil {
			return nil, fmt.Errorf("invalid persisted broadcast binding")
		}
		if req.From().Address.User != binding.DeviceCode && req.From().Address.User != binding.ChannelCode {
			continue
		}
		platform, err := store.FindPlatform(ctx, row.PlatformID)
		if err != nil {
			return nil, err
		}
		if platform == nil {
			continue
		}
		localSource := binding.LocalSourceID
		if localSource == "" {
			localSource = platform.LocalDeviceID
		}
		// 真机（大华，2026-10-24 抓包）发起广播音频 INVITE 时，收件人写的是
		// **通道编码**（INVITE sip:37010301021320000006@...）而不是平台编码。
		// 粗筛阶段两个值都认（真正要求"两个收件人字段都指向本次广播"的严格
		// 校验在下面对信封的检查里）。
		if !cascadeAudioCalleeMatches(req.Recipient.User, localSource, binding.ChannelCode) &&
			!cascadeAudioCalleeMatches(req.To().Address.User, localSource, binding.ChannelCode) {
			continue
		}
		if match != nil {
			return nil, fmt.Errorf("ambiguous cascade broadcast pull")
		}
		binding.LocalSourceID = localSource
		match = &cascadeBroadcastPull{Row: row, Binding: binding, Authorization: cascadeTalkAuthorization{Platform: *platform, PublishedID: row.PublishedChannelID, Source: binding.Source, OwnerDeptID: binding.OwnerDeptID}}
	}
	if match == nil {
		return nil, nil
	}
	b, p := match.Binding, match.Authorization.Platform
	if match.Row.State != model.CascadeMediaSessionStateProvisioning && match.Row.State != model.CascadeMediaSessionStateAnswered && match.Row.State != model.CascadeMediaSessionStateActive {
		return nil, fmt.Errorf("cascade broadcast is not accepting audio")
	}
	if !p.Enabled || p.ConfigRevision != b.ConfigRevision || p.ProjectionRevision != b.ProjectionRevision || string(effectiveCascadeProfile(p)) != b.ProfileVersion || p.LocalDeviceID != b.LocalSourceID {
		return nil, fmt.Errorf("cascade broadcast authorization changed")
	}
	if b.Source.DeviceEpoch <= 0 || uint64(b.Source.DeviceID) != match.Row.SourceDeviceID || uint64(b.Source.ChannelID) != match.Row.SourceChannelID || b.PublishedChannelID != match.Row.PublishedChannelID || b.Source.DeviceCode != b.DeviceCode || b.Source.ChannelCode != b.ChannelCode || !validCascadeGBID(b.UpstreamSourceID) {
		return nil, fmt.Errorf("inconsistent cascade broadcast source binding")
	}
	if !cascadeAudioCalleeMatches(req.Recipient.User, b.LocalSourceID, b.ChannelCode) || !cascadeAudioCalleeMatches(req.To().Address.User, b.LocalSourceID, b.ChannelCode) || req.CallID() == nil || len(req.CallID().Value()) == 0 || len(req.CallID().Value()) > 255 || req.CSeq() == nil || req.CSeq().MethodName != sip.INVITE || req.CSeq().SeqNo == 0 || len(req.GetHeaders("Subject")) != 1 {
		return nil, fmt.Errorf("invalid cascade broadcast INVITE envelope")
	}
	if tag, _ := req.To().Params.Get("tag"); tag != "" {
		return nil, fmt.Errorf("cascade broadcast re-INVITE unsupported")
	}
	if tag, _ := req.From().Params.Get("tag"); tag == "" || len(tag) > 128 || req.Contact() == nil || req.Via() == nil {
		return nil, fmt.Errorf("incomplete cascade broadcast dialog headers")
	}
	peer, ok := cascadePeerFromRequest(req)
	if !ok || !cascadePeerHostMatches(peer.host, b.Source.IP) || peer.port != b.Source.Port || !strings.EqualFold(peer.transport, b.Source.Transport) {
		return nil, fmt.Errorf("cascade broadcast INVITE peer mismatch")
	}
	subject, err := parseCascadeBroadcastSourceIdentity(req.GetHeader("Subject").Value())
	if err != nil || !cascadeAudioSubjectMatches(subject, b) || !mediaSequenceToken(subject.SenderSequence) || !mediaSequenceToken(subject.ReceiverSequence) {
		// ⛔ Subject 只是「媒体流标识铭牌」，真正的鉴权锚是租约 + 已发布通道 +
		// 设备纪元（上面全都校验过了）。不同厂商往里写哪个编码并不统一：
		// 国标写平台号，有的实现写设备号或通道号（实测三家上级各不相同）。
		// 只认一种写法等于「换一家上级就播不了」，所以这里放宽到本次广播涉及的
		// 那几个编码，但**两边都必须是本次广播的编码之一**，不放行任意值。
		// 拒绝时把原文带出来，否则只能靠抓包判断是哪一家写了什么。
		return nil, fmt.Errorf("cascade broadcast INVITE Subject mismatch: subject=%q sender=%q receiver=%q local=%q device=%q channel=%q",
			req.GetHeader("Subject").Value(), subject.SenderID, subject.ReceiverID, b.LocalSourceID, b.DeviceCode, b.ChannelCode)
	}
	contentType := req.GetHeader("Content-Type")
	if contentType == nil || len(req.GetHeaders("Content-Type")) != 1 || !strings.EqualFold(strings.TrimSpace(strings.Split(contentType.Value(), ";")[0]), "application/sdp") {
		return nil, fmt.Errorf("cascade audio requires SDP")
	}
	var offer sdp.CascadeAudioOffer
	if b.Source.Profile.Version == protocol.Version2022 {
		offer, err = sdp.ParseCascadeBroadcastPullOffer(req.Body())
	} else {
		offer, err = sdp.ParseCascadeAudioOffer(req.Body())
		if err == nil && (offer.SessionName != "Play" || offer.Direction != "recvonly") {
			err = fmt.Errorf("cascade broadcast pull requires Play/recvonly")
		}
	}
	if err != nil {
		return nil, err
	}
	if b.AudioLeaseID == "" {
		return nil, fmt.Errorf("cascade broadcast has no channel reservation")
	}
	if b.AudioCallID == "" {
		if match.Row.ReceivedAt == nil || !match.Row.ReceivedAt.Add(cascadeBroadcastPullTimeout).After(now) {
			return nil, fmt.Errorf("cascade broadcast pull expired")
		}
	}
	if err := match.validateLease(ctx, db, now); err != nil {
		return nil, err
	}
	if err = match.Authorization.Validate(ctx, db, store); err != nil {
		return nil, err
	}
	match.Peer, match.Offer = peer, offer
	return match, nil
}

// cascadeAudioCalleeMatches 判定音频 INVITE 的收件人是否指向本次广播。
// 平台编码是常规写法；部分设备（大华实测）会把收件人写成被广播的通道编码，
// 这两种都算「打给我们」，其余取值一律不认。
func cascadeAudioCalleeMatches(value string, candidates ...string) bool {
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

// cascadeAudioSubjectMatches 判定广播音频 INVITE 的 Subject 是否描述本次广播。
//
// 国标写法是「发送者=本级平台编码，接收者=被广播通道编码」，但实测厂商各有取舍：
// 有的把发送者写成设备编码，有的把接收者写成平台编码。这里放宽成
//「发送者与接收者分别取自 {本级平台编码, 设备编码, 通道编码}，且两者不同」，
// 既不认任意值，也不再绑定单一写法。
func cascadeAudioSubjectMatches(subject cascadeMediaSourceIdentity, b cascadeBroadcastBinding) bool {
	senderOK := cascadeAudioCalleeMatches(subject.SenderID, b.LocalSourceID, b.DeviceCode, b.ChannelCode)
	receiverOK := cascadeAudioCalleeMatches(subject.ReceiverID, b.ChannelCode, b.DeviceCode, b.LocalSourceID)
	return senderOK && receiverOK && subject.SenderID != subject.ReceiverID
}

// mediaSequenceToken 校验 Subject 里 SSRC 那一段的形态。
// 国标写的是十进制 SSRC，但真机（大华，2026-10-24 抓包）发的是 19 位的
// **十六进制**串：
//
//	Subject: 34020000012000000001:03d7a8ef3276a8ef327,37010301021320000006:0552354c54f3354c54f
//
// 这一段只是媒体流标识，不参与鉴权与绑定（真正锁死身份的是 SenderID/ReceiverID
// 与租约），所以十进制与十六进制都认，只拦空值和超长/夹带分隔符的写法。
func mediaSequenceToken(value string) bool {
	if value == "" || len(value) > 20 {
		return false
	}
	return strings.Trim(value, "0123456789abcdefABCDEF") == ""
}

func cascadeAudioRequestHash(req *sip.Request) string {
	digest := sha256.Sum256(append(append([]byte(req.GetHeader("Subject").Value()), 0), req.Body()...))
	return hex.EncodeToString(digest[:])
}

func (m *cascadeBroadcastPull) validateLease(ctx context.Context, db *gorm.DB, now time.Time) error {
	lease, err := talk.NewGormRepo(db).FindCascadeLease(ctx, m.Binding.AudioLeaseID)
	if err != nil {
		return err
	}
	if lease == nil || lease.State.IsTerminal() || !lease.ExpiresAt.After(now) || uint64(lease.ChannelID) != m.Row.SourceChannelID || lease.DeviceID != m.Binding.DeviceCode || lease.TargetID != m.Binding.ChannelCode {
		return fmt.Errorf("cascade audio lease missing or expired")
	}
	return nil
}

// cascadeBroadcastBindingStable 只比较两份绑定里**授权相关**的部分。
// 结果类字段（DeviceResult / NotifyStatus / ResultDelivered / ResultAttempt*）与
// 音频认领字段，会由「设备应答处理」在 INVITE 到达前后并发写入同一行；
// 把它们纳入比较就等于要求"设备必须先 INVITE 再应答"，与真机顺序相反。
func cascadeBroadcastBindingStable(a, b cascadeBroadcastBinding) bool {
	a.DeviceResult, b.DeviceResult = "", ""
	a.NotifyStatus, b.NotifyStatus = 0, 0
	a.ResultDelivered, b.ResultDelivered = false, false
	a.ResultAttemptAt, b.ResultAttemptAt = nil, nil
	a.ResultAttempts, b.ResultAttempts = 0, 0
	a.AudioCallID, b.AudioCallID = "", ""
	a.AudioCSeq, b.AudioCSeq = 0, 0
	a.AudioFromTag, b.AudioFromTag = "", ""
	a.AudioBranch, b.AudioBranch = "", ""
	a.AudioRequestHash, b.AudioRequestHash = "", ""
	return reflect.DeepEqual(a, b)
}

// Claim binds the device transaction to the lease already reserved before
// Notify. Revalidation and the binding CAS use the same database transaction.
func (m *cascadeBroadcastPull) Claim(ctx context.Context, db *gorm.DB, req *sip.Request, now time.Time) error {
	key := cascadeInviteTransactionKey(req)
	hash := cascadeAudioRequestHash(req)
	if m.Binding.AudioLeaseID == "" {
		return fmt.Errorf("cascade broadcast has no channel reservation")
	}
	if m.Binding.AudioCallID != "" {
		if m.Binding.AudioCallID != key.CallID || m.Binding.AudioCSeq != key.CSeq || m.Binding.AudioFromTag != key.FromTag || m.Binding.AudioBranch != key.Branch || m.Binding.AudioRequestHash != hash {
			return fmt.Errorf("cascade broadcast already claimed by another audio request")
		}
	}
	// ⛔ 不能在事务外用 resolve 时拿到的 sdp_summary 做 CAS：设备发完 Broadcast
	// 应答会立刻发音频 INVITE，而应答处理（recordBroadcastResult）正好会把
	// DeviceResult 写进同一行的 sdp_summary。用旧快照比对必然 RowsAffected=0，
	// 表现为设备收到 486「Cascade Audio Reservation Changed」，广播永远建不起来
	// （2026-10-24 大华真机复现）。所以在事务内**重读当前行**，只改音频认领字段，
	// 其余字段（应答结果、投递状态）原样保留。
	var binding cascadeBroadcastBinding
	var summary []byte
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := m.Authorization.Validate(ctx, tx, repository.NewGormRepository(tx)); err != nil {
			return err
		}
		if err := m.validateLease(ctx, tx, now); err != nil {
			return err
		}
		var current model.GbCascadeMediaSession
		if err := tx.Where("id = ?", m.Row.ID).Take(&current).Error; err != nil {
			return err
		}
		if current.State != model.CascadeMediaSessionStateProvisioning && current.State != model.CascadeMediaSessionStateAnswered && current.State != model.CascadeMediaSessionStateActive {
			return fmt.Errorf("cascade broadcast is not accepting audio")
		}
		var live cascadeBroadcastBinding
		if err := json.Unmarshal([]byte(current.SDPSummary), &live); err != nil {
			return err
		}
		if !cascadeBroadcastBindingStable(m.Binding, live) {
			return fmt.Errorf("cascade broadcast binding changed while claiming audio")
		}
		if live.AudioCallID != "" && (live.AudioCallID != key.CallID || live.AudioCSeq != key.CSeq || live.AudioFromTag != key.FromTag || live.AudioBranch != key.Branch || live.AudioRequestHash != hash) {
			return fmt.Errorf("cascade broadcast already claimed by another audio request")
		}
		binding = live
		binding.AudioCallID, binding.AudioCSeq, binding.AudioFromTag, binding.AudioBranch, binding.AudioRequestHash = key.CallID, key.CSeq, key.FromTag, key.Branch, hash
		var marshalErr error
		summary, marshalErr = json.Marshal(binding)
		if marshalErr != nil {
			return marshalErr
		}
		result := tx.Model(&model.GbCascadeMediaSession{}).Where("id = ? AND sdp_summary = ? AND state IN ?", current.ID, current.SDPSummary, []model.CascadeMediaSessionState{model.CascadeMediaSessionStateProvisioning, model.CascadeMediaSessionStateAnswered, model.CascadeMediaSessionStateActive}).Updates(map[string]any{"sdp_summary": string(summary), "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("cascade broadcast binding changed while claiming audio")
		}
		return nil
	})
	if err != nil {
		return err
	}
	m.Binding = binding
	m.Row.SDPSummary = string(summary)
	return nil
}

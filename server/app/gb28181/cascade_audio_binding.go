package gb28181

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
		if req.Recipient.User != localSource && req.To().Address.User != localSource {
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
	if req.Recipient.User != b.LocalSourceID || req.To().Address.User != b.LocalSourceID || req.CallID() == nil || len(req.CallID().Value()) == 0 || len(req.CallID().Value()) > 255 || req.CSeq() == nil || req.CSeq().MethodName != sip.INVITE || req.CSeq().SeqNo == 0 || len(req.GetHeaders("Subject")) != 1 {
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
	subject, err := parseCascadeMediaSourceIdentity(req.GetHeader("Subject").Value(), false)
	if err != nil || subject.SenderID != b.LocalSourceID || subject.ReceiverID != b.ChannelCode || !decimalMediaSequence(subject.SenderSequence) || !decimalMediaSequence(subject.ReceiverSequence) {
		return nil, fmt.Errorf("cascade broadcast INVITE Subject mismatch")
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

func decimalMediaSequence(value string) bool {
	return value != "" && len(value) <= 20 && strings.Trim(value, "0123456789") == ""
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
	binding := m.Binding
	binding.AudioCallID, binding.AudioCSeq, binding.AudioFromTag, binding.AudioBranch, binding.AudioRequestHash = key.CallID, key.CSeq, key.FromTag, key.Branch, hash
	summary, err := json.Marshal(binding)
	if err != nil {
		return err
	}
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := m.Authorization.Validate(ctx, tx, repository.NewGormRepository(tx)); err != nil {
			return err
		}
		if err := m.validateLease(ctx, tx, now); err != nil {
			return err
		}
		result := tx.Model(&model.GbCascadeMediaSession{}).Where("id = ? AND sdp_summary = ? AND state IN ?", m.Row.ID, m.Row.SDPSummary, []model.CascadeMediaSessionState{model.CascadeMediaSessionStateProvisioning, model.CascadeMediaSessionStateAnswered, model.CascadeMediaSessionStateActive}).Updates(map[string]any{"sdp_summary": string(summary), "updated_at": now})
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

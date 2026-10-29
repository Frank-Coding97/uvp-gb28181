package gb28181

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.com/uvp-gb28181/app/global/app"
)

const cascadeAlarmMaxAttempts = 8

var errCascadeAlarmBindingRevoked = errors.New("cascade Alarm delivery authorization revoked")

type cascadeAlarmNotifyTarget struct {
	ID    string `json:"id"`
	State string `json:"state,omitempty"`
}

// The fingerprint binds queued data to the exact source rows, registration
// generation, ownership, sharing revision and upstream wire configuration.
func (m *cascadeEventSubscriptions) alarmBinding(ctx context.Context, p model.GbCascadePlatform, deviceCode, resourceCode, published string) (string, error) {
	current, _, allowed, err := m.resolveSource(ctx, p, cascadeEventAlarm, deviceCode, resourceCode)
	if err != nil {
		return "", err
	}
	if !allowed || current != published {
		return "", errCascadeAlarmBindingRevoked
	}
	var source struct {
		DeviceRowID           uint64
		OwnerDeptID           uint64
		AccessEpoch           *int64
		CleanupCompletedEpoch *int64
		ChannelRowID          uint64
		ChannelOwner          uint64
	}
	q := app.DB().WithContext(ctx).Table("gb_device AS d").
		Select("d.id AS device_row_id, d.owner_dept_id, d.access_epoch, d.cleanup_completed_epoch").
		Where("d.device_id = ? AND d.deleted_at IS NULL", deviceCode)
	if resourceCode != deviceCode {
		q = q.Select("d.id AS device_row_id, d.owner_dept_id, d.access_epoch, d.cleanup_completed_epoch, c.id AS channel_row_id, c.owner_dept_id AS channel_owner").
			Joins("JOIN gb_channel AS c ON c.device_id = d.device_id AND c.owner_dept_id = d.owner_dept_id").
			Where("c.channel_id = ? AND c.deleted_at IS NULL", resourceCode)
	}
	result := q.Limit(1).Find(&source)
	if result.Error != nil {
		return "", result.Error
	}
	if result.RowsAffected != 1 {
		return "", errCascadeAlarmBindingRevoked
	}
	data, err := json.Marshal([]any{p.ID, p.ConfigRevision, p.ProjectionRevision, cascadeWireProfile(p),
		p.Host, p.Port, p.Transport, p.UpstreamServerID, p.UpstreamDomain, p.LocalDeviceID, p.LocalDomain, p.LocalSIPIP, p.LocalSIPPort,
		deviceCode, resourceCode, published, source})
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}

func (m *cascadeEventSubscriptions) queueAlarm(ctx context.Context, p model.GbCascadePlatform, deviceCode, sourceCode, published, parent string, sn int, callID, cseq string, notify any) (string, error) {
	body, err := buildCascadeEventBody(cascadeWireProfile(p), cascadeEventAlarm, published, sn, notify)
	if err != nil {
		return "", err
	}
	binding, err := m.alarmBinding(ctx, p, deviceCode, sourceCode, published)
	if err != nil {
		return "", err
	}
	// A SIP transaction is immutable. Changing its body must not create another
	// alarm; when no transaction identity exists, the sanitized payload is the key.
	identity := []any{p.ID, deviceCode, callID, cseq}
	if callID == "" && cseq == "" {
		identity = append(identity, fmt.Sprintf("%x", sha256.Sum256(body)))
	}
	identityBytes, err := json.Marshal(identity)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(identityBytes)
	delivery := &model.GbCascadeAlarmDelivery{DeliveryKey: hex.EncodeToString(hash[:]), PlatformID: p.ID, SourceDeviceCode: deviceCode, SourceResourceCode: sourceCode, PublishedCode: published, ProfileVersion: string(cascadeWireProfile(p).Version), BindingFingerprint: binding, Payload: body, NextAttemptAt: time.Now()}
	// Persist the original dialog selection with the event. A retransmission must
	// never choose MESSAGE after its NOTIFY dialog has disappeared (or vice versa).
	m.mu.Lock()
	var targets []cascadeAlarmNotifyTarget
	selected := make(map[string]bool)
	for _, s := range m.sessions {
		if s.platform.ID != p.ID || s.kind != cascadeEventAlarm {
			continue
		}
		s.mu.Lock()
		if s.reason == "" && time.Now().Before(s.expires) && s.platform.ConfigRevision == p.ConfigRevision && cascadeWireProfile(s.platform) == cascadeWireProfile(p) &&
			(s.target == published || s.target == parent || (s.target == p.LocalDeviceID && p.PublishPlatform)) {
			if s.deliveryID == "" {
				s.deliveryID = uuid.NewString()
			}
			if !selected[s.deliveryID] {
				targets = append(targets, cascadeAlarmNotifyTarget{ID: s.deliveryID})
				selected[s.deliveryID] = true
			}
		}
		s.mu.Unlock()
	}
	m.mu.Unlock()
	if len(targets) > 0 {
		encoded, err := json.Marshal(targets)
		if err != nil {
			return "", err
		}
		delivery.NotifyTargets = string(encoded)
	} else {
		client, err := m.clients.NewClient(p)
		if err != nil {
			return "", err
		}
		if !strings.EqualFold(p.Transport, "TCP") && !client.(*cascadePlatformClient).transactions.MessageFitsUDP(body) {
			return "", fmt.Errorf("cascade Alarm MESSAGE exceeds UDP transport budget")
		}
	}
	if _, err = m.store.EnqueueAlarmDelivery(ctx, delivery); err != nil {
		return "", fmt.Errorf("persist cascade Alarm delivery: %w", err)
	}
	return delivery.DeliveryKey, nil
}

// Each invocation acquires only this delivery and uses a fresh fencing token.
func (m *cascadeEventSubscriptions) dispatchAlarmDelivery(ctx context.Context, keys ...string) error {
	_, err := m.dispatchAlarmDeliveryOnce(ctx, keys...)
	return err
}

func (m *cascadeEventSubscriptions) dispatchAlarmDeliveryOnce(ctx context.Context, keys ...string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	token := uuid.NewString()
	claimed, claimErr := m.store.ClaimAlarmDeliveries(ctx, token, time.Now(), 15*time.Second, 1, keys...)
	var deliveryErr error
	for _, item := range claimed {
		sendCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		deliveryErr = m.deliverAlarmMessage(sendCtx, item)
		cancel()
		// A canceled caller or shutdown must not prevent the lease result from being
		// persisted. A crash here is recovered by lease expiry (at-least-once).
		finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		now := time.Now()
		var ok bool
		var finishErr error
		if deliveryErr == nil {
			ok, finishErr = m.store.CompleteAlarmDelivery(finishCtx, item.ID, token, now)
		} else {
			attempts := cascadeAlarmMaxAttempts
			// Revocation is permanent for this original binding, even after re-sharing.
			if errors.Is(deliveryErr, errCascadeAlarmBindingRevoked) {
				attempts = 1
			}
			delay := time.Second << min(item.Attempts-1, 10)
			ok, finishErr = m.store.RetryAlarmDelivery(finishCtx, item.ID, token, now, now.Add(delay), deliveryErr.Error(), attempts)
		}
		finishCancel()
		if finishErr == nil && !ok {
			finishErr = fmt.Errorf("cascade Alarm delivery lease expired before completion")
		}
		deliveryErr = errors.Join(deliveryErr, finishErr)
	}
	return len(claimed) > 0, errors.Join(claimErr, deliveryErr)
}

func (m *cascadeEventSubscriptions) deliverAlarmMessage(ctx context.Context, item model.GbCascadeAlarmDelivery) error {
	if m == nil || m.store == nil || m.clients == nil {
		return fmt.Errorf("cascade Alarm delivery runtime is unavailable")
	}
	if item.Attempts > cascadeAlarmMaxAttempts {
		return fmt.Errorf("cascade Alarm delivery attempt limit exceeded")
	}
	p, err := m.store.FindPlatform(ctx, item.PlatformID)
	if err != nil {
		return err
	}
	if p == nil || !p.Enabled || string(cascadeWireProfile(*p).Version) != item.ProfileVersion {
		return errCascadeAlarmBindingRevoked
	}
	binding, err := m.alarmBinding(ctx, *p, item.SourceDeviceCode, item.SourceResourceCode, item.PublishedCode)
	if err != nil {
		return err
	}
	if binding != item.BindingFingerprint {
		return errCascadeAlarmBindingRevoked
	}
	if item.NotifyTargets != "" {
		return m.deliverAlarmNotifies(ctx, *p, item)
	}
	client, err := m.clients.NewClient(*p)
	if err != nil {
		return err
	}
	transactions := client.(*cascadePlatformClient).transactions
	if !strings.EqualFold(p.Transport, "TCP") && !transactions.MessageFitsUDP(item.Payload) {
		return fmt.Errorf("cascade Alarm MESSAGE exceeds UDP transport budget")
	}
	result := transactions.SendMessage(ctx, item.Payload, fmt.Sprintf("alarm-%d-%d", item.PlatformID, item.ID))
	if !result.Success || result.StatusCode != 200 {
		return errors.Join(fmt.Errorf("cascade Alarm MESSAGE failed with SIP status %d", result.StatusCode), result.BuildErr, result.TransportErr)
	}
	return nil
}

func (m *cascadeEventSubscriptions) runAlarmDeliveryWorker() {
	defer m.wg.Done()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			if m.store == nil || m.clients == nil {
				continue
			}
			for i := 0; i < 32 && m.ctx.Err() == nil; i++ {
				claimed, err := m.dispatchAlarmDeliveryOnce(m.ctx)
				if err != nil && m.ctx.Err() == nil {
					app.Log(m.ctx).Warn("级联告警队列投递失败", zap.String("event", "cascade.alarm.delivery_failed"), zap.Error(err))
				}
				if !claimed {
					break
				}
			}
		}
	}
}

func (m *cascadeEventSubscriptions) deliverAlarmNotifies(ctx context.Context, p model.GbCascadePlatform, item model.GbCascadeAlarmDelivery) error {
	var targets []cascadeAlarmNotifyTarget
	if err := json.Unmarshal([]byte(item.NotifyTargets), &targets); err != nil || len(targets) == 0 {
		return errors.Join(errCascadeAlarmBindingRevoked, fmt.Errorf("invalid persisted Alarm dialog selection"))
	}
	var retryErr error
	var revokedErr error
	for i := range targets {
		if targets[i].State == "sent" {
			continue
		}
		if targets[i].State == "revoked" {
			revokedErr = errors.Join(revokedErr, errCascadeAlarmBindingRevoked)
			continue
		}
		m.mu.Lock()
		var session *cascadeEventSubscription
		for _, candidate := range m.sessions {
			candidate.mu.Lock()
			matched := candidate.deliveryID == targets[i].ID
			candidate.mu.Unlock()
			if matched {
				session = candidate
				break
			}
		}
		m.mu.Unlock()
		err := m.deliverAlarmNotify(ctx, p, item, session)
		if err == nil {
			targets[i].State = "sent"
		} else if errors.Is(err, errCascadeAlarmBindingRevoked) {
			targets[i].State = "revoked"
			revokedErr = errors.Join(revokedErr, err)
		} else {
			retryErr = errors.Join(retryErr, err)
			continue
		}
		encoded, err := json.Marshal(targets)
		if err != nil {
			return err
		}
		// Record each dialog independently, so retrying one failed dialog does not
		// resend to dialogs whose successful SIP responses were already persisted.
		finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		ok, err := m.store.CheckpointAlarmDelivery(finishCtx, item.ID, item.LeaseOwner, time.Now(), string(encoded))
		cancel()
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("cascade Alarm delivery lease lost before checkpoint")
		}
	}
	if retryErr != nil {
		return retryErr
	}
	return revokedErr
}

func (m *cascadeEventSubscriptions) deliverAlarmNotify(ctx context.Context, p model.GbCascadePlatform, item model.GbCascadeAlarmDelivery, s *cascadeEventSubscription) error {
	if s == nil {
		return errCascadeAlarmBindingRevoked
	}
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	s.mu.Lock()
	if s.kind != cascadeEventAlarm || s.platform.ID != p.ID || s.reason != "" || !time.Now().Before(s.expires) || s.platform.ConfigRevision != p.ConfigRevision || cascadeWireProfile(s.platform) != cascadeWireProfile(p) {
		s.mu.Unlock()
		return errCascadeAlarmBindingRevoked
	}
	// Recheck the complete binding before each dialog, including when a previous
	// dialog's transport callback changed the source ownership or sharing.
	binding, err := m.alarmBinding(ctx, p, item.SourceDeviceCode, item.SourceResourceCode, item.PublishedCode)
	if err != nil || binding != item.BindingFingerprint {
		s.mu.Unlock()
		if err != nil {
			return err
		}
		return errCascadeAlarmBindingRevoked
	}
	_, parent, allowed, err := m.resolveSource(ctx, p, cascadeEventAlarm, item.SourceDeviceCode, item.SourceResourceCode)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	if !allowed || (s.target != item.PublishedCode && s.target != parent && (s.target != p.LocalDeviceID || !p.PublishPlatform)) {
		s.mu.Unlock()
		return errCascadeAlarmBindingRevoked
	}
	alarm, err := manscdp.ParseAlarmNotify(item.Payload)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	body, err := buildCascadeEventBody(cascadeWireProfile(p), cascadeEventAlarm, item.PublishedCode, s.sn, alarm)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	generation, dialog, expires := s.generation, s.dialog, s.expires
	s.notifySeq++
	seq := s.notifySeq
	notifyCtx, cancel := context.WithTimeout(ctx, min(time.Until(expires), 3*time.Second))
	s.sendCancel = cancel
	s.mu.Unlock()
	result := s.client.SendNotify(notifyCtx, dialog, seq, "active;expires="+strconv.Itoa(max(1, int(time.Until(expires).Seconds()))), body)
	cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notifySeq = max(s.notifySeq, result.LastCSeq)
	if s.generation != generation {
		return fmt.Errorf("cascade Alarm subscription changed during notification")
	}
	err = s.recordNotifyResult(result)
	if err != nil && s.reason != "" {
		return errors.Join(errCascadeAlarmBindingRevoked, err)
	}
	return err
}

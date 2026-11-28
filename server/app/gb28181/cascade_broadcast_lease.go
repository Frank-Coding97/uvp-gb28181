package gb28181

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/repository"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/talk"
	"uvplatform.com/uvp-gb28181/app/global/app"
)

var errCascadeBroadcastConflict = errors.New("cascade broadcast transaction content changed")
var errCascadeBroadcastDeliveryDeferred = errors.New("cascade broadcast result delivery is deferred")

// reserveBroadcast commits the task and shared lease together, before any device
// notification. An existing task is never sent again, including after a crash.
func (m *cascadeBroadcastRuntime) reserveBroadcast(ctx context.Context, row *model.GbCascadeMediaSession, binding *cascadeBroadcastBinding, authorization cascadeTalkAuthorization) (bool, error) {
	created := false
	acceptExisting := func(existing *model.GbCascadeMediaSession) error {
		var saved cascadeBroadcastBinding
		if json.Unmarshal([]byte(existing.SDPSummary), &saved) != nil || existing.Kind != model.CascadeMediaSessionKindBroadcast || existing.CSeq != row.CSeq || saved.MessageHash == "" || saved.MessageHash != binding.MessageHash {
			return errCascadeBroadcastConflict
		}
		*row, *binding = *existing, saved
		return nil
	}
	err := m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		store := repository.NewGormRepository(tx)
		existing, err := store.FindMediaSessionByDialog(ctx, row.DialogKey)
		if err != nil {
			return err
		}
		if existing != nil {
			return acceptExisting(existing)
		}
		if err := authorization.Validate(ctx, tx, store); err != nil {
			return err
		}
		binding.AudioLeaseID = "cascade-audio-" + uuid.NewString()
		if _, err := talk.NewGormRepo(tx).ReserveCascadeLease(ctx, binding.AudioLeaseID, uint(row.SourceChannelID), binding.DeviceCode, binding.ChannelCode, time.Now().Add(cascadeBroadcastPullTimeout)); err != nil {
			return err
		}
		summary, err := json.Marshal(binding)
		if err != nil {
			return err
		}
		row.SDPSummary = string(summary)
		row.State = model.CascadeMediaSessionStateProvisioning
		if err := store.CreateMediaSession(ctx, row); err != nil {
			return err
		}
		created = true
		return nil
	})
	if err != nil {
		// Another transaction may have won the unique dialog or channel key.
		// Re-read only this exact request; never retry a device notification.
		existing, readErr := m.store.FindMediaSessionByDialog(ctx, row.DialogKey)
		if readErr == nil && existing != nil {
			return false, acceptExisting(existing)
		}
	}
	return created, err
}

// updateBroadcast persists only after comparing the current summary. In
// particular, a fast reverse INVITE must not be overwritten by Notify completion.
func (m *cascadeBroadcastRuntime) updateBroadcast(ctx context.Context, key string, notifyStatus int, terminal model.CascadeMediaSessionState, reason string, at time.Time) error {
	return m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		store := repository.NewGormRepository(tx)
		row, err := store.FindMediaSessionByDialog(ctx, key)
		if err != nil {
			return err
		}
		if row == nil || row.Kind != model.CascadeMediaSessionKindBroadcast {
			return fmt.Errorf("cascade broadcast task missing")
		}
		var binding cascadeBroadcastBinding
		if err := json.Unmarshal([]byte(row.SDPSummary), &binding); err != nil {
			return err
		}
		if notifyStatus != 0 && binding.NotifyStatus == 0 {
			binding.NotifyStatus = notifyStatus
		}
		summary, err := json.Marshal(binding)
		if err != nil {
			return err
		}
		updates := map[string]any{"sdp_summary": string(summary), "updated_at": at}
		if terminal != "" {
			if !cascadeBroadcastTerminal(row.State) {
				updates["state"] = terminal
				updates["closed_at"] = at
				updates["failure_code"] = "broadcast_ended"
				updates["failure_message"] = reason
			}
			if binding.AudioLeaseID != "" {
				if _, err := talk.NewGormRepo(tx).ReleaseCascadeLease(ctx, binding.AudioLeaseID, gbmodels.TalkSessionFailed, reason, at); err != nil {
					return err
				}
			}
		} else if cascadeBroadcastTerminal(row.State) {
			return fmt.Errorf("cascade broadcast ended during notification")
		}
		result := tx.Model(&model.GbCascadeMediaSession{}).Where("id=? AND sdp_summary=? AND state=?", row.ID, row.SDPSummary, row.State).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("cascade broadcast task changed")
		}
		return nil
	})
}

func cascadeBroadcastTerminal(state model.CascadeMediaSessionState) bool {
	return state == model.CascadeMediaSessionStateClosed || state == model.CascadeMediaSessionStateFailed || state == model.CascadeMediaSessionStateCancelled || state == model.CascadeMediaSessionStateAborted
}

func (m *cascadeBroadcastRuntime) recordBroadcastResult(ctx context.Context, key, deviceResult string, at time.Time) error {
	if deviceResult != "OK" && deviceResult != "ERROR" {
		return fmt.Errorf("invalid cascade broadcast result")
	}
	return m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		store := repository.NewGormRepository(tx)
		row, err := store.FindMediaSessionByDialog(ctx, key)
		if err != nil {
			return err
		}
		if row == nil || row.Kind != model.CascadeMediaSessionKindBroadcast || cascadeBroadcastTerminal(row.State) {
			return fmt.Errorf("cascade broadcast task is no longer active")
		}
		var binding cascadeBroadcastBinding
		if err := json.Unmarshal([]byte(row.SDPSummary), &binding); err != nil {
			return err
		}
		if binding.DeviceResult != "" && binding.DeviceResult != deviceResult {
			return fmt.Errorf("cascade broadcast device result changed")
		}
		binding.DeviceResult = deviceResult
		summary, err := json.Marshal(binding)
		if err != nil {
			return err
		}
		if deviceResult == "ERROR" && binding.AudioLeaseID != "" {
			if _, err := talk.NewGormRepo(tx).ReleaseCascadeLease(ctx, binding.AudioLeaseID, gbmodels.TalkSessionFailed, "device rejected broadcast", at); err != nil {
				return err
			}
		}
		result := tx.Model(&model.GbCascadeMediaSession{}).Where("id=? AND sdp_summary=? AND state=?", row.ID, row.SDPSummary, row.State).Updates(map[string]any{"sdp_summary": string(summary), "updated_at": at})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("cascade broadcast result changed")
		}
		return nil
	})
}

func (m *cascadeBroadcastRuntime) releaseBroadcastLease(ctx context.Context, sessionID, reason string, at time.Time) error {
	if sessionID == "" {
		return nil
	}
	_, err := talk.NewGormRepo(m.db).ReleaseCascadeLease(ctx, sessionID, gbmodels.TalkSessionFailed, reason, at)
	return err
}

func (m *cascadeBroadcastRuntime) finishBroadcastResult(ctx context.Context, key, deviceResult string, at time.Time) error {
	return m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		store := repository.NewGormRepository(tx)
		row, err := store.FindMediaSessionByDialog(ctx, key)
		if err != nil {
			return err
		}
		if row == nil || row.Kind != model.CascadeMediaSessionKindBroadcast {
			return fmt.Errorf("cascade broadcast task missing")
		}
		var binding cascadeBroadcastBinding
		if err := json.Unmarshal([]byte(row.SDPSummary), &binding); err != nil {
			return err
		}
		if binding.DeviceResult != deviceResult {
			return fmt.Errorf("cascade broadcast result was not recorded")
		}
		if binding.ResultDelivered {
			return nil
		}
		binding.ResultDelivered = true
		summary, err := json.Marshal(binding)
		if err != nil {
			return err
		}
		updates := map[string]any{"sdp_summary": string(summary), "updated_at": at}
		if deviceResult == "ERROR" {
			updates["state"] = model.CascadeMediaSessionStateFailed
			updates["closed_at"] = at
			updates["failure_code"] = "device_rejected"
			updates["failure_message"] = "device rejected broadcast"
		} else if row.State == model.CascadeMediaSessionStateProvisioning {
			updates["state"] = model.CascadeMediaSessionStateAnswered
			updates["answered_at"] = at
		}
		result := tx.Model(&model.GbCascadeMediaSession{}).Where("id=? AND sdp_summary=? AND state=?", row.ID, row.SDPSummary, row.State).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("cascade broadcast result delivery state changed")
		}
		return nil
	})
}

func (m *cascadeBroadcastRuntime) claimBroadcastResultDelivery(ctx context.Context, key string, at time.Time) (bool, error) {
	claimed := false
	err := m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		store := repository.NewGormRepository(tx)
		row, err := store.FindMediaSessionByDialog(ctx, key)
		if err != nil {
			return err
		}
		if row == nil || row.Kind != model.CascadeMediaSessionKindBroadcast || cascadeBroadcastTerminal(row.State) {
			return fmt.Errorf("cascade broadcast task is no longer active")
		}
		var binding cascadeBroadcastBinding
		if err := json.Unmarshal([]byte(row.SDPSummary), &binding); err != nil {
			return err
		}
		if binding.DeviceResult == "" || binding.ResultDelivered {
			return fmt.Errorf("cascade broadcast result is not awaiting delivery")
		}
		if binding.ResultAttemptAt != nil && binding.ResultAttempts == 0 {
			return fmt.Errorf("cascade broadcast result delivery claim is invalid")
		}
		retryDelay := 5 * time.Second
		if binding.ResultAttempts > 1 {
			shift := binding.ResultAttempts - 1
			if shift > 6 {
				shift = 6
			}
			retryDelay *= time.Duration(uint(1) << shift)
		}
		if retryDelay > 5*time.Minute {
			retryDelay = 5 * time.Minute
		}
		if binding.ResultAttemptAt != nil && binding.ResultAttemptAt.Add(retryDelay).After(at) {
			return nil
		}
		binding.ResultAttemptAt = &at
		binding.ResultAttempts++
		summary, err := json.Marshal(binding)
		if err != nil {
			return err
		}
		result := tx.Model(&model.GbCascadeMediaSession{}).Where("id=? AND sdp_summary=? AND state=?", row.ID, row.SDPSummary, row.State).Updates(map[string]any{"sdp_summary": string(summary), "updated_at": at})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("cascade broadcast result delivery claim changed")
		}
		claimed = true
		return nil
	})
	return claimed, err
}

func (m *cascadeBroadcastRuntime) Start(ctx context.Context) error {
	if m == nil || m.store == nil || m.db == nil {
		return fmt.Errorf("cascade broadcast runtime unavailable")
	}
	acquireCtx, release, ok := m.acquire(ctx, 10*time.Second)
	if !ok {
		return fmt.Errorf("cascade broadcast runtime is stopping")
	}
	if err := m.recover(acquireCtx); err != nil {
		release()
		return err
	}
	release()
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return fmt.Errorf("cascade broadcast runtime is stopping")
	}
	if m.sweepStarted {
		m.mu.Unlock()
		return nil
	}
	m.sweepStarted = true
	m.active++
	lifetime := m.lifetime
	m.mu.Unlock()
	go func() {
		defer func() {
			m.mu.Lock()
			m.active--
			m.closeDoneIfIdleLocked()
			m.mu.Unlock()
		}()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-lifetime.Done():
				return
			case now := <-ticker.C:
				sweepCtx, cancel := context.WithTimeout(lifetime, 8*time.Second)
				if err := m.sweep(sweepCtx, now); err != nil && lifetime.Err() == nil {
					app.Log(sweepCtx).Warn("级联广播任务巡检失败", zap.String("event", "cascade.broadcast.sweep_failed"), zap.Error(err))
				}
				cancel()
			}
		}
	}()
	return nil
}

func (m *cascadeBroadcastRuntime) Recover(ctx context.Context) error {
	if m == nil {
		return nil
	}
	acquireCtx, release, ok := m.acquire(ctx, 10*time.Second)
	if !ok {
		return fmt.Errorf("cascade broadcast runtime is stopping")
	}
	defer release()
	return m.recover(acquireCtx)
}

func (m *cascadeBroadcastRuntime) recover(ctx context.Context) error {
	rows, err := m.store.ListNonterminalBroadcastSessions(ctx)
	if err != nil {
		return err
	}
	var failures []error
	for _, row := range rows {
		var binding cascadeBroadcastBinding
		if err := json.Unmarshal([]byte(row.SDPSummary), &binding); err != nil {
			failures = append(failures, fmt.Errorf("recover %s binding: %w", row.DialogKey, err))
			continue
		}
		at := time.Now()
		if binding.DeviceResult != "" && !binding.ResultDelivered {
			if err := m.releaseBroadcastLease(ctx, binding.AudioLeaseID, "runtime restarted", at); err != nil {
				failures = append(failures, fmt.Errorf("recover %s lease: %w", row.DialogKey, err))
				continue
			}
			if err := m.deliverPendingBroadcastResult(ctx, row, binding, at); errors.Is(err, errCascadeBroadcastDeliveryDeferred) {
				continue
			} else if err != nil {
				app.Log(ctx).Warn("级联广播待投递结果恢复失败", zap.String("event", "cascade.broadcast.result_recovery_failed"), zap.String("dialog_key", row.DialogKey), zap.Error(err))
				continue
			}
		}
		if err := m.updateBroadcast(ctx, row.DialogKey, 0, model.CascadeMediaSessionStateAborted, "runtime restarted", at); err != nil {
			failures = append(failures, fmt.Errorf("recover %s: %w", row.DialogKey, err))
		}
	}
	leases, err := talk.NewGormRepo(m.db).ListCascadeLeases(ctx)
	if err != nil {
		failures = append(failures, err)
	} else {
		for _, lease := range leases {
			if !strings.HasPrefix(lease.SessionID, "cascade-audio-") {
				continue
			}
			if _, err := talk.NewGormRepo(m.db).ReleaseCascadeLease(ctx, lease.SessionID, gbmodels.TalkSessionFailed, "runtime restarted", time.Now()); err != nil {
				failures = append(failures, fmt.Errorf("recover audio lease %s: %w", lease.SessionID, err))
			}
		}
	}
	return errors.Join(failures...)
}

func (m *cascadeBroadcastRuntime) Sweep(ctx context.Context, now time.Time) error {
	if m == nil {
		return nil
	}
	acquireCtx, release, ok := m.acquire(ctx, 10*time.Second)
	if !ok {
		return fmt.Errorf("cascade broadcast runtime is stopping")
	}
	defer release()
	return m.sweep(acquireCtx, now)
}

func (m *cascadeBroadcastRuntime) sweep(ctx context.Context, now time.Time) error {
	rows, err := m.store.ListNonterminalBroadcastSessions(ctx)
	if err != nil {
		return err
	}
	var failures []error
	for _, row := range rows {
		var binding cascadeBroadcastBinding
		if err := json.Unmarshal([]byte(row.SDPSummary), &binding); err != nil {
			failures = append(failures, fmt.Errorf("decode %s: %w", row.DialogKey, err))
			continue
		}
		if binding.DeviceResult != "" && !binding.ResultDelivered {
			if err := m.deliverPendingBroadcastResult(ctx, row, binding, now); err != nil && !errors.Is(err, errCascadeBroadcastDeliveryDeferred) {
				failures = append(failures, err)
			}
			continue
		}
		platform, err := m.store.FindPlatform(ctx, row.PlatformID)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		authorizationChanged := platform == nil || !platform.Enabled || platform.ConfigRevision != binding.ConfigRevision || platform.ProjectionRevision != binding.ProjectionRevision || string(effectiveCascadeProfile(*platform)) != binding.ProfileVersion
		if !authorizationChanged {
			authorization := cascadeTalkAuthorization{Platform: *platform, PublishedID: row.PublishedChannelID, Source: binding.Source, OwnerDeptID: binding.OwnerDeptID}
			authorizationChanged = authorization.Validate(ctx, m.db, m.store) != nil
		}
		lease, leaseErr := talk.NewGormRepo(m.db).FindCascadeLease(ctx, binding.AudioLeaseID)
		if leaseErr != nil {
			failures = append(failures, leaseErr)
			continue
		}
		expired := binding.AudioCallID == "" && (row.ReceivedAt == nil || !row.ReceivedAt.Add(cascadeBroadcastPullTimeout).After(now))
		if lease == nil || lease.State.IsTerminal() || !lease.ExpiresAt.After(now) {
			expired = true
		}
		if authorizationChanged || expired {
			reason := "audio reservation expired"
			state := model.CascadeMediaSessionStateFailed
			if authorizationChanged {
				reason = "authorization changed"
				state = model.CascadeMediaSessionStateAborted
			}
			if err := m.updateBroadcast(ctx, row.DialogKey, 0, state, reason, now); err != nil {
				failures = append(failures, err)
			}
		}
	}
	return errors.Join(failures...)
}

func (m *cascadeBroadcastRuntime) deliverPendingBroadcastResult(ctx context.Context, row model.GbCascadeMediaSession, binding cascadeBroadcastBinding, at time.Time) error {
	platform, err := m.store.FindPlatform(ctx, row.PlatformID)
	if err != nil {
		return err
	}
	if platform == nil || !platform.Enabled || platform.ConfigRevision != binding.ConfigRevision || platform.ProjectionRevision != binding.ProjectionRevision || string(effectiveCascadeProfile(*platform)) != binding.ProfileVersion {
		return m.updateBroadcast(ctx, row.DialogKey, 0, model.CascadeMediaSessionStateAborted, "authorization changed", at)
	}
	authorization := cascadeTalkAuthorization{Platform: *platform, PublishedID: row.PublishedChannelID, Source: binding.Source, OwnerDeptID: binding.OwnerDeptID}
	if err := authorization.Validate(ctx, m.db, m.store); err != nil {
		return m.updateBroadcast(ctx, row.DialogKey, 0, model.CascadeMediaSessionStateAborted, "authorization changed", at)
	}
	claimed, err := m.claimBroadcastResultDelivery(ctx, row.DialogKey, at)
	if err != nil {
		return err
	}
	if !claimed {
		return errCascadeBroadcastDeliveryDeferred
	}
	if binding.DeviceResult == "ERROR" {
		if err := m.releaseBroadcastLease(ctx, binding.AudioLeaseID, "device rejected broadcast", at); err != nil {
			return err
		}
	}
	out, err := manscdp.BuildBroadcastResponseWithProfile(cascadeWireProfile(*platform), binding.UpstreamSN, row.PublishedChannelID, binding.DeviceResult)
	if err != nil {
		return err
	}
	client, err := m.clients.NewClient(*platform)
	if err != nil {
		return err
	}
	send := client.(*cascadePlatformClient).transactions.SendMessage(ctx, out, fmt.Sprintf("cascade-broadcast-%d-%s", platform.ID, row.CallID))
	if send.StatusCode < 200 || send.StatusCode >= 300 {
		return fmt.Errorf("cascade broadcast response delivery failed: status=%d", send.StatusCode)
	}
	return m.finishBroadcastResult(ctx, row.DialogKey, binding.DeviceResult, at)
}

func (m *cascadeBroadcastRuntime) Shutdown(ctx context.Context) error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	if !m.closed {
		m.closed = true
		m.cancel()
	}
	m.closeDoneIfIdleLocked()
	done := m.done
	m.mu.Unlock()
	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	}
	m.cleanupMu.Lock()
	defer m.cleanupMu.Unlock()
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return m.recover(cleanupCtx)
}

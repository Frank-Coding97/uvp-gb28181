package playauth

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDeviceRTPRecoverySlotCanonicalAndSeparateFromOriginalFacts(t *testing.T) {
	f, store, id := newRTPStepFixture(t)
	ctx := context.Background()
	_, err := store.AddRTPResourceStep(ctx, id, 2, rtpStepIdentity(1))
	require.NoError(t, err)
	old, err := store.DispatchRTPResourceStep(ctx, id, 3, rtpStepIdentity(1).StepID)
	require.NoError(t, err)
	original := old.Steps[0]
	now := time.Now().UTC().Truncate(time.Microsecond)
	w := stepToWire(original)
	w.Recovery = &rtpRecoveryWire{Version: 1, Generation: 1, OwnerRunID: strings.Repeat("b", 32), OwnerProcessID: strings.Repeat("c", 32), ReservedAt: now}
	raw, err := json.Marshal(rtpStepsWire{Version: 1, Steps: []rtpStepWire{w}})
	require.NoError(t, err)
	require.NoError(t, f.db.Table("gb_device_operation_intent").Where("operation_id = ?", id.OperationID).
		Updates(map[string]any{"rtp_steps_json": string(raw), "row_version": 5, "updated_at": now}).Error)
	loaded, err := store.LoadRTPResourceSteps(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, loaded.Steps[0].Recovery)
	require.EqualValues(t, 1, loaded.Steps[0].Recovery.Generation)
	copy := loaded.Steps[0]
	copy.Recovery = nil
	require.Equal(t, original, copy, "a cleanup reservation does not alter original dispatch or observation facts")
	public, err := json.Marshal(loaded.Steps[0].Recovery)
	require.NoError(t, err)
	require.JSONEq(t, "{}", string(public), "internal cleanup state is not an API DTO")
	for name, mutate := range map[string]func(*rtpRecoveryWire){
		"unknown-version":    func(r *rtpRecoveryWire) { r.Version++ },
		"missing-process":    func(r *rtpRecoveryWire) { r.OwnerProcessID = "" },
		"missing-owner":      func(r *rtpRecoveryWire) { r.OwnerRunID = "" },
		"zero-generation":    func(r *rtpRecoveryWire) { r.Generation = 0 },
		"negative-sequence":  func(r *rtpRecoveryWire) { r.CallSequence = -1 },
		"future-reservation": func(r *rtpRecoveryWire) { r.ReservedAt = now.Add(time.Second) },
		"unpaired-result":    func(r *rtpRecoveryWire) { r.CurrentCall = &rtpCleanupCallWire{Result: "rtp_ingress_drained"} },
		"invented-action": func(r *rtpRecoveryWire) {
			r.CallSequence = 1
			r.CurrentCall = &rtpCleanupCallWire{Sequence: 1, Action: "open_rtp", DispatchStartedAt: now}
		},
	} {
		t.Run(name, func(t *testing.T) {
			bad := *w.Recovery
			mutate(&bad)
			entry := w
			entry.Recovery = &bad
			body, err := json.Marshal(rtpStepsWire{Version: 1, Steps: []rtpStepWire{entry}})
			require.NoError(t, err)
			require.NoError(t, f.db.Table("gb_device_operation_intent").Where("operation_id = ?", id.OperationID).Update("rtp_steps_json", string(body)).Error)
			_, err = store.LoadRTPResourceSteps(ctx, id)
			require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
		})
	}
}

func TestDeviceRTPRecoverySlotSurvivesOriginalOwnerReadback(t *testing.T) {
	f, store, id := newRTPStepFixture(t)
	ctx := context.Background()
	_, err := store.AddRTPResourceStep(ctx, id, 2, rtpStepIdentity(1))
	require.NoError(t, err)
	_, work, err := store.DispatchRTPResourceWork(ctx, id, 3, rtpStepIdentity(1).StepID)
	require.NoError(t, err)
	require.NoError(t, work.Quiesce(ctx))
	loaded, err := store.LoadRTPResourceSteps(ctx, id)
	require.NoError(t, err)
	now := time.Now().UTC().Truncate(time.Microsecond)
	w := stepToWire(loaded.Steps[0])
	w.Recovery = &rtpRecoveryWire{Version: 1, Generation: 1, OwnerRunID: strings.Repeat("b", 32), OwnerProcessID: strings.Repeat("c", 32), ReservedAt: now}
	raw, err := json.Marshal(rtpStepsWire{Version: 1, Steps: []rtpStepWire{w}})
	require.NoError(t, err)
	require.NoError(t, f.db.Table("gb_device_operation_intent").Where("operation_id = ?", id.OperationID).
		Updates(map[string]any{"rtp_steps_json": string(raw), "row_version": loaded.Intent.RowVersion + 1, "updated_at": now}).Error)
	require.NoError(t, work.Quiesce(ctx), "an old owner's acknowledgement retry must preserve independent recovery state")
	var actual string
	require.NoError(t, f.db.Table("gb_device_operation_intent").Select("rtp_steps_json").Where("operation_id = ?", id.OperationID).Scan(&actual).Error)
	require.Equal(t, string(raw), actual)
}

func TestDeviceRTPRecoverySlotRejectsMalformedCallsAndEvidence(t *testing.T) {
	f, store, id := newRTPStepFixture(t)
	ctx := context.Background()
	identity := rtpStepIdentity(1)
	identity.TCPMode = 0
	_, err := store.AddRTPResourceStep(ctx, id, 2, identity)
	require.NoError(t, err)
	out, err := store.DispatchRTPResourceStep(ctx, id, 3, identity.StepID)
	require.NoError(t, err)
	now := time.Now().UTC().Truncate(time.Microsecond)
	r := &rtpRecoveryWire{Version: 1, Generation: 1, OwnerProcessID: strings.Repeat("b", 32), OwnerRunID: strings.Repeat("c", 32), ReservedAt: now, CallSequence: 1,
		CurrentCall:     &rtpCleanupCallWire{Sequence: 1, Action: rtpCleanupIngress, DispatchStartedAt: now, Outcome: rtpCallObserved, Result: "rtp_ingress_drained", ResultObservedAt: &now, LocalQuiescedAt: &now},
		IngressEvidence: &rtpCleanupEvidenceWire{Sequence: 1, Result: "rtp_ingress_drained", ObservedAt: now}, LocalQuiescedAt: &now}
	w := stepToWire(out.Steps[0])
	w.Recovery = r
	valid, err := json.Marshal(rtpStepsWire{Version: 1, Steps: []rtpStepWire{w}})
	require.NoError(t, err)
	require.NoError(t, f.db.Table("gb_device_operation_intent").Where("operation_id = ?", id.OperationID).Updates(map[string]any{"row_version": 5, "updated_at": now, "rtp_steps_json": string(valid)}).Error)
	_, err = store.LoadRTPResourceSteps(ctx, id)
	require.NoError(t, err)
	for name, mutate := range map[string]func(*rtpStepWire){
		"fractional-nanoseconds":  func(w *rtpStepWire) { w.Recovery.ReservedAt = now.Add(-time.Nanosecond) },
		"offset-time":             func(w *rtpStepWire) { w.Recovery.ReservedAt = now.In(time.FixedZone("plus", 3600)) },
		"before-dispatch":         func(w *rtpStepWire) { w.Recovery.ReservedAt = w.PreparedAt.Add(-time.Second) },
		"wrong-sequence":          func(w *rtpStepWire) { w.Recovery.CurrentCall.Sequence = 2 },
		"zero-call":               func(w *rtpStepWire) { w.Recovery.CurrentCall.Sequence = 0 },
		"lost-call":               func(w *rtpStepWire) { w.Recovery.CurrentCall = nil },
		"future-dispatch":         func(w *rtpStepWire) { w.Recovery.CurrentCall.DispatchStartedAt = now.Add(time.Second) },
		"wrong-outcome":           func(w *rtpStepWire) { w.Recovery.CurrentCall.Outcome = "closed" },
		"unknown-with-result":     func(w *rtpStepWire) { w.Recovery.CurrentCall.Outcome = rtpCallUnknown },
		"not-invoked-with-result": func(w *rtpStepWire) { w.Recovery.CurrentCall.Outcome = rtpCallNotInvoked },
		"unjoined-result":         func(w *rtpStepWire) { w.Recovery.CurrentCall.LocalQuiescedAt = nil },
		"early-call-exit":         func(w *rtpStepWire) { early := now.Add(-time.Second); w.Recovery.CurrentCall.LocalQuiescedAt = &early },
		"early-owner-exit":        func(w *rtpStepWire) { early := now.Add(-time.Second); w.Recovery.LocalQuiescedAt = &early },
		"drained-tcp":             func(w *rtpStepWire) { w.TCPMode = 1 },
		"resource-is-not-ingress": func(w *rtpStepWire) { w.Recovery.CurrentCall.Action = rtpCleanupResource },
		"future-evidence":         func(w *rtpStepWire) { w.Recovery.IngressEvidence.ObservedAt = now.Add(time.Second) },
		"evidence-sequence":       func(w *rtpStepWire) { w.Recovery.IngressEvidence.Sequence = 2 },
		"evidence-result":         func(w *rtpStepWire) { w.Recovery.IngressEvidence.Result = "complete" },
		"missing-evidence":        func(w *rtpStepWire) { w.Recovery.IngressEvidence = nil },
		"prepared-recovery":       func(w *rtpStepWire) { w.State = RTPStepPrepared; w.RowVersion = 1; w.DispatchStartedAt = nil },
	} {
		t.Run(name, func(t *testing.T) {
			var wire rtpStepsWire
			require.NoError(t, json.Unmarshal(valid, &wire))
			mutate(&wire.Steps[0])
			raw, err := json.Marshal(wire)
			require.NoError(t, err)
			require.NoError(t, f.db.Table("gb_device_operation_intent").Where("operation_id = ?", id.OperationID).Update("rtp_steps_json", string(raw)).Error)
			_, err = store.LoadRTPResourceSteps(ctx, id)
			require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
		})
	}
	for name, raw := range map[string]string{
		"extra":         strings.Replace(string(valid), `"generation":1`, `"generation":1,"permission":true`, 1),
		"duplicate":     strings.Replace(string(valid), `"generation":1`, `"generation":1,"generation":1`, 1),
		"case-shadow":   strings.Replace(string(valid), `"generation":1`, `"generation":1,"Generation":1`, 1),
		"explicit-null": strings.Replace(string(valid), `"cleanup":`+string(mustMarshalRTPRecovery(t, r)), `"cleanup":null`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, f.db.Table("gb_device_operation_intent").Where("operation_id = ?", id.OperationID).Update("rtp_steps_json", raw).Error)
			_, err = store.LoadRTPResourceSteps(ctx, id)
			require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
		})
	}
}

func mustMarshalRTPRecovery(t *testing.T, r *rtpRecoveryWire) []byte {
	t.Helper()
	raw, err := json.Marshal(r)
	require.NoError(t, err)
	return raw
}

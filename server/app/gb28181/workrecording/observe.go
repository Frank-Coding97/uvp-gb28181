package workrecording

import (
	"context"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// Observe rechecks the server-side recording fact for one exact owner. It
// never starts or stops ZLM. A false read is enough to persist a stopped pair;
// an unknown or failed read keeps ownership and is surfaced as StateUnknown.
func (r *Recorder) Observe(ctx context.Context, h RecorderHandle) (RecorderHandle, string, error) {
	if r == nil || h.ChannelID == 0 {
		return h, StateUnknown, ErrInvalidRequest
	}
	gateUnlock, err := r.gate.lockMutation(ctx)
	if err != nil {
		return h, StateUnknown, err
	}
	defer gateUnlock()
	unlock := r.lock(h.ChannelID)
	defer unlock()

	row, err := r.owned(ctx, h)
	if err != nil {
		return h, StateUnknown, err
	}
	switch row.State {
	case StateStarting:
		return h, StateStarting, nil
	case StateStopped:
		return h, StateStopped, nil
	case StateRecording, StateUnknown, StateStopping:
		return r.observeActive(ctx, h, row)
	default:
		return h, StateUnknown, ErrVersionConflict
	}
}

func (r *Recorder) observeActive(ctx context.Context, h RecorderHandle, row *models.GbRecorderClaim) (RecorderHandle, string, error) {
	target := targetOf(row)
	if !target.valid() {
		return h, StateUnknown, ErrInvalidRequest
	}
	client, release, err := r.pinnedClient(ctx, target)
	if err != nil {
		return h, StateUnknown, err
	}
	defer release()
	active, err := client.IsRecording(ctx, target.VHost, target.App, target.Stream)
	if err != nil {
		return h, StateUnknown, err
	}
	if active {
		// An unknown claim remains unknown. A positive read is only a fact about
		// ZLM; it cannot prove that this owner started the recorder.
		return h, row.State, nil
	}
	next, err := r.pairState(ctx, h, StateStopped, false)
	if err != nil {
		return next, StateUnknown, err
	}
	return next, StateStopped, nil
}

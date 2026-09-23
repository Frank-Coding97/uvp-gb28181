package playauth

import "context"

// BorrowChildLease does not admit another operation or read a newer epoch.
// Only this barrier's still-active concrete parent can lend its original
// identity. Release ends the child context, never removes the parent lane.
// The parent remains responsible for joining children and persisting their
// final observations before releasing its own lease.
func (b *DeviceOperationBarrier) BorrowChildLease(ctx context.Context, parent DeviceOperationLease, id DeviceOperationIntentIdentity) (DeviceOperationLease, error) {
	lease, ok := parent.(*deviceOperationLease)
	if ctx == nil || ctx.Err() != nil || b == nil || !ok || lease == nil || lease.ref == nil || lease.ref.barrier != b ||
		id.DevicePK <= 0 || lease.lane == nil || int64(lease.lane.pk) != id.DevicePK || lease.deviceCode != id.DeviceCode || lease.epoch != id.DeviceEpoch {
		return nil, ErrDeviceOperationUnavailable
	}
	lease.lane.mu.Lock()
	defer lease.lane.mu.Unlock()
	if _, active := lease.lane.active[lease]; !active || lease.ctx.Err() != nil {
		return nil, ErrDeviceOperationUnavailable
	}
	childCtx, cancel := context.WithCancel(lease.ctx)
	stop := context.AfterFunc(ctx, cancel)
	return &deviceOperationChildLease{ctx: childCtx, epoch: lease.epoch, cancel: func() { stop(); cancel() }}, nil
}

type deviceOperationChildLease struct {
	ctx    context.Context
	epoch  int64
	cancel context.CancelFunc
}

func (c *deviceOperationChildLease) Context() context.Context { return c.ctx }
func (c *deviceOperationChildLease) OperationEpoch() int64    { return c.epoch }
func (c *deviceOperationChildLease) Release()                 { c.cancel() }

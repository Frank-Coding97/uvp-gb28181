package assign

import (
	"context"
	"reflect"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

const assignmentTransferTimeout = 5 * time.Second

// DeviceTransferBarrier is shared with media dispatch for this API process.
// Lock order is device lane -> DB transaction; no media network work is done
// under this guard. Commit is invoked only after the database confirms commit.
type DeviceTransferBarrier interface {
	LockTransfer(context.Context, uint) (playauth.DeviceTransferGuard, error)
}

func WithDeviceTransferBarrier(barrier DeviceTransferBarrier) ServiceOption {
	return func(s *Service) { s.transferBarrier = barrier }
}

func (s *Service) lockTransfer(ctx context.Context, devicePK uint) (playauth.DeviceTransferGuard, error) {
	if s == nil || s.db == nil || nilBarrierDependency(s.transferBarrier) || ctx == nil || devicePK == 0 {
		return nil, ErrAssignmentSecurityUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	guard, err := s.transferBarrier.LockTransfer(ctx, devicePK)
	if err != nil {
		return nil, err
	}
	if nilBarrierDependency(guard) {
		return nil, ErrAssignmentSecurityUnavailable
	}
	return guard, nil
}

func nilBarrierDependency(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

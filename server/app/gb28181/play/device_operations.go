package play

import (
	"context"

	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

func WithDeviceOperationBarrier(barrier *playauth.DeviceOperationBarrier) Option {
	return func(service *Service) {
		service.operationBarrierRequired = true
		service.operationBarrier = barrier
	}
}

func (s *Service) beginDeviceOperation(ctx context.Context, req Request) (playauth.DeviceOperationLease, error) {
	if s.operationBarrier == nil {
		return nil, ErrPlayAuthorizationUnavailable
	}
	if req.AuthorizationID != "" {
		issuer, ok := s.tokenIssuer.(interface {
			BeginQueuedOperation(context.Context, playauth.QueuedAuthorization) (playauth.DeviceOperationLease, error)
		})
		if !ok || issuer == nil {
			return nil, ErrPlayAuthorizationUnavailable
		}
		queued, err := s.queuedAuthorization(req)
		if err != nil {
			return nil, err
		}
		return issuer.BeginQueuedOperation(ctx, queued)
	}
	return s.operationBarrier.BeginEpoch(ctx, req.DeviceID, req.DeviceEpoch)
}

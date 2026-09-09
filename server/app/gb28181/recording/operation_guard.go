package recording

import (
	"context"
	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// SetOperationGuard is configured before publishing the service. The callback
// must share the work recorder gate and pass its context into the operation.
func (s *Service) SetOperationGuard(guard func(context.Context, uint, string, func(context.Context) error) error) {
	s.operationGuard = guard
}
func (s *Service) guardOperation(ctx context.Context, id uint, stream string, fn func(context.Context) error) error {
	if s.operationGuard == nil {
		return fn(ctx)
	}
	return s.operationGuard(ctx, id, stream, fn)
}
func (s *Service) Enable(ctx context.Context, id uint) (result *models.GbChannel, err error) {
	err = s.guardOperation(ctx, id, "", func(operationCtx context.Context) error { result, err = s.enable(operationCtx, id); return err })
	return
}
func (s *Service) Disable(ctx context.Context, id uint) (result *models.GbChannel, err error) {
	err = s.guardOperation(ctx, id, "", func(operationCtx context.Context) error { result, err = s.disable(operationCtx, id); return err })
	return
}
func (s *Service) StopSession(ctx context.Context, id uint, sessionID uint64) (result *models.GbChannel, err error) {
	err = s.guardOperation(ctx, id, "", func(operationCtx context.Context) error {
		result, err = s.stopSession(operationCtx, id, sessionID)
		return err
	})
	return
}
func (s *Service) ReconcileChannel(ctx context.Context, id uint) error {
	return s.guardOperation(ctx, id, "", func(operationCtx context.Context) error { return s.reconcileChannel(operationCtx, id) })
}
func (s *Service) guardPlayback(ctx context.Context, stream string, fn func(context.Context) error) error {
	channel, err := s.repo.FindChannelByStream(ctx, stream)
	if err != nil {
		return err
	}
	var id uint
	if channel != nil {
		id = channel.ID
	}
	return s.guardOperation(ctx, id, stream, fn)
}
func (s *Service) BeginPlayback(ctx context.Context, stream string) error {
	return s.guardPlayback(ctx, stream, func(operationCtx context.Context) error { return s.beginPlayback(operationCtx, stream) })
}
func (s *Service) EndPlayback(ctx context.Context, stream string) error {
	return s.guardPlayback(ctx, stream, func(operationCtx context.Context) error { return s.endPlayback(operationCtx, stream) })
}

package play

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	gbplay "uvplatform.cn/uvp-gb28181/app/gb28181/play"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/openapi/auth"
	"uvplatform.cn/uvp-gb28181/app/openapi/resource"
)

type liveStarter interface {
	StartAuthorized(context.Context, gbplay.AuthorizedRequest) (*gbplay.Result, error)
}

type Dispatcher struct {
	db      *gorm.DB
	service liveStarter
	quota   *ViewerQuota
	runtime ViewerRuntimeSnapshotter
}

func NewDispatcher(db *gorm.DB, service liveStarter, runtime ...ViewerRuntimeSnapshotter) *Dispatcher {
	dispatcher := &Dispatcher{db: db, service: service, quota: NewViewerQuota(db, time.Now)}
	if len(runtime) == 1 {
		dispatcher.runtime = runtime[0]
	}
	return dispatcher
}

func (d *Dispatcher) Ready() bool {
	return d != nil && d.db != nil && d.service != nil && d.quota != nil
}

func (d *Dispatcher) BindViewer(ctx context.Context, claims playauth.Claims, binding playauth.OpenAPIViewerBinding) error {
	if !d.Ready() {
		return ErrQuotaFailed
	}
	return d.quota.BindViewer(ctx, claims, binding)
}

func (d *Dispatcher) CloseViewer(ctx context.Context, report playauth.OpenAPIFlowReport) error {
	if !d.Ready() {
		return ErrQuotaFailed
	}
	return d.quota.CloseViewer(ctx, report)
}

func (d *Dispatcher) ReconcileViewers(ctx context.Context) error {
	if d == nil || d.quota == nil {
		return ErrQuotaFailed
	}
	if d.runtime == nil {
		return nil
	}
	_, err := d.quota.ReconcileViewers(ctx, d.runtime, 100)
	return err
}

func (d *Dispatcher) Start(ctx context.Context, request auth.PlayRequest) (any, error) {
	if !d.Ready() {
		return nil, auth.ErrPlayUnavailable
	}
	reservation, err := d.quota.Reserve(ctx, ReservationRequest{ClientID: request.ClientID, DeviceID: request.DeviceID, ChannelID: request.ChannelID})
	if err != nil {
		return nil, mapQuotaError(err)
	}
	scope := resource.DepartmentScope{OwnerDeptID: request.OwnerDeptID, DataScope: request.DataScope}
	if _, err := resource.New(d.db).GetChannelInScope(ctx, scope, request.DeviceID, request.ChannelID); err != nil {
		d.failReservation(ctx, request.ClientID, reservation.GrantID, "resource_not_found")
		if errors.Is(err, resource.ErrResourceNotFound) {
			return nil, auth.ErrPlayNotFound
		}
		return nil, auth.ErrPlayUnavailable
	}
	result, err := d.service.StartAuthorized(ctx, gbplay.AuthorizedRequest{
		DeviceID: request.DeviceID, ChannelID: request.ChannelID,
		ClientIP: request.ClientIP, DeviceEpoch: reservation.DeviceEpoch,
		OpenAPIClientID: request.ClientID, OpenAPIGrantID: reservation.GrantID,
	})
	if err == nil && result != nil && result.AuthorizationExpiresAt > 0 && result.AuthorizationCorrelationID != "" {
		return result, nil
	}
	d.failReservation(ctx, request.ClientID, reservation.GrantID, "play_start_failed")
	if err == nil {
		return nil, auth.ErrPlayUnavailable
	}
	switch {
	case errors.Is(err, gbplay.ErrDeviceNotFound), errors.Is(err, gbplay.ErrChannelNotFound):
		return nil, auth.ErrPlayNotFound
	case errors.Is(err, gbplay.ErrDeviceOffline):
		return nil, auth.ErrPlayConflict
	case errors.Is(err, gbplay.ErrPlayAuthorizationUnavailable):
		return nil, auth.ErrPlayUnavailable
	default:
		return nil, auth.ErrPlayUnavailable
	}
}

func (d *Dispatcher) failReservation(ctx context.Context, clientID int64, grantID, reason string) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	_ = d.quota.Fail(cleanupCtx, clientID, grantID, reason)
}

func mapQuotaError(err error) error {
	if errors.Is(err, ErrQuotaExceeded) {
		return auth.ErrPlayQuotaExceeded
	}
	return auth.ErrPlayUnavailable
}

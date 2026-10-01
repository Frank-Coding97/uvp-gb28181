package auth

import (
	"context"
	"errors"
	"net/http"

	"uvplatform.cn/uvp-gb28181/app/openapi/resource"
)

const PlayLiveScope = "play:live"

var (
	ErrPlayUnavailable   = errors.New("OpenAPI play dispatcher unavailable")
	ErrPlayInvalid       = errors.New("invalid OpenAPI play request")
	ErrPlayNotFound      = errors.New("OpenAPI play resource not found")
	ErrPlayConflict      = errors.New("OpenAPI play conflict")
	ErrPlayQuotaExceeded = errors.New("OpenAPI viewer quota exceeded")
)

type PlayRequest struct {
	ClientID    int64
	OwnerDeptID uint
	DataScope   int8
	DeviceID    string
	ChannelID   string
	ClientIP    string
}

type PlayDispatcher interface {
	Ready() bool
	Start(context.Context, PlayRequest) (any, error)
}

// PlayViewerMaintainer is optional so existing dispatchers and test doubles do
// not need to implement media-runtime reconciliation.
type PlayViewerMaintainer interface {
	ReconcileViewers(context.Context) error
}

func playPattern(scope string) string {
	if scope == PlayLiveScope {
		return "/openapi/v1/devices/:deviceId/channels/:channelId/live"
	}
	return ""
}

func (g *Gateway) playReady() (ready bool) {
	if g == nil || g.play == nil {
		return false
	}
	defer func() {
		if recover() != nil {
			ready = false
		}
	}()
	return g.play.Ready()
}

func playFailure(id string, err error) gatewayResponse {
	switch {
	case errors.Is(err, ErrPlayNotFound), errors.Is(err, resource.ErrResourceNotFound):
		return gatewayError(id, http.StatusNotFound, "RESOURCE_NOT_FOUND")
	case errors.Is(err, ErrPlayInvalid):
		return gatewayError(id, http.StatusBadRequest, "INVALID_REQUEST")
	case errors.Is(err, ErrPlayConflict):
		return gatewayError(id, http.StatusConflict, "CONFLICT")
	case errors.Is(err, ErrPlayQuotaExceeded):
		response := gatewayError(id, http.StatusTooManyRequests, "QUOTA_EXCEEDED")
		response.retryAfter = 1
		return response
	default:
		return gatewayError(id, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE")
	}
}

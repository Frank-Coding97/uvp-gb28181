package handler

import (
	"context"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"uvplatform.com/uvp-gb28181/app/gb28181/backendorigin"
	"uvplatform.com/uvp-gb28181/app/gb28181/playauth"
)

type BackendOriginObserver interface {
	Observe(context.Context, string, int64, string, string, string, string) error
}

func (h *HookController) SetBackendOriginObserver(observer BackendOriginObserver) {
	h.playAuthMu.Lock()
	defer h.playAuthMu.Unlock()
	h.backendOriginObserver = observer
}

func (h *HookController) handleBackendOriginProbe(c *gin.Context, body onPlayBody) bool {
	params, err := url.ParseQuery(strings.TrimPrefix(body.Params, "?"))
	values, present := params[backendorigin.QueryParameter]
	if !present && err == nil {
		return false
	}
	// A probe can establish a receipt but must never reach ordinary authorization
	// or permit ZLM to find/read the media source, even when playback auth is off.
	defer h.denyPlayback(c, body.Stream, "backend_origin_probe", "backend origin probe denied")
	identity, authenticated := AuthenticatedHookNode(c)
	if err != nil || !authenticated || identity.ID <= 0 || identity.MediaServerUUID != body.MediaServerID || len(values) != 1 || values[0] == "" || body.App != "rtp" {
		return true
	}
	if _, present := params[playauth.QueryParameter]; present {
		return true
	}
	h.playAuthMu.RLock()
	observer := h.backendOriginObserver
	h.playAuthMu.RUnlock()
	if observer != nil {
		_ = observer.Observe(c.Request.Context(), values[0], identity.ID, identity.MediaServerUUID, body.App, body.Stream, body.IP)
	}
	return true
}

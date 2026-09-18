package handler

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

// OpenAPIFlowObserver is the trusted internal sink for a final player flow
// observation. It is deliberately separate from traffic accounting: an
// absent observer must not disable the existing flow collector path.
type OpenAPIFlowObserver interface {
	ObserveFlow(context.Context, playauth.OpenAPIFlowReport) error
}

// SetOpenAPIFlowObserver installs the durable OpenAPI flow sink. The observer
// is read under playAuthMu so runtime bootstrap and Hook requests cannot race.
func (h *HookController) SetOpenAPIFlowObserver(observer OpenAPIFlowObserver) {
	if h == nil {
		return
	}
	h.playAuthMu.Lock()
	h.openAPIFlowObserver = observer
	h.playAuthMu.Unlock()
}

func (h *HookController) observeOpenAPIFlow(c *gin.Context, body onFlowReportBody) {
	if h == nil || c == nil || c.Request == nil {
		return
	}
	identity, authenticated := AuthenticatedHookNode(c)
	if !authenticated || identity.ID <= 0 || identity.MediaServerUUID == "" || identity.MediaServerUUID != body.MediaServerID {
		return
	}
	h.playAuthMu.RLock()
	observer := h.openAPIFlowObserver
	h.playAuthMu.RUnlock()
	if observer == nil || !body.Player {
		return
	}
	protocol, ok := openAPIHookFlowProtocol(body.Protocol)
	if !ok || !validOpenAPIHookFlowReport(identity.MediaServerUUID, body) {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), time.Second)
	defer cancel()
	err := observer.ObserveFlow(ctx, playauth.OpenAPIFlowReport{
		NodeUUID:   identity.MediaServerUUID,
		BootNonce:  body.BootNonce,
		Identifier: body.ID,
		Protocol:   protocol,
		Schema:     body.Schema,
		VHost:      body.VHost,
		App:        body.App,
		Stream:     body.Stream,
		Player:     true,
	})
	if err != nil && app.ZapLog != nil {
		// Keep the Hook fail-open and log only a fixed reason. The observer may
		// wrap a database error containing credentials or request material.
		app.ZapLog.Warn("OpenAPI flow observation failed", zap.String("event", "gb28181.hook.flow.observer_failed"), zap.String("reason", "observer-error"))
	}
}

func openAPIHookFlowProtocol(protocol string) (string, bool) {
	switch protocol {
	case "https":
		return "https-flv", true
	case "wss":
		return "wss-flv", true
	default:
		return "", false
	}
}

func validOpenAPIHookFlowReport(nodeUUID string, body onFlowReportBody) bool {
	return validOpenAPIHookFlowField(nodeUUID, 64) &&
		validOpenAPIHookFlowField(body.BootNonce, 32) && validOpenAPIHookFlowBootNonce(body.BootNonce) &&
		validOpenAPIHookFlowField(body.ID, 128) &&
		validOpenAPIHookFlowField(body.Schema, 32) &&
		validOpenAPIHookFlowField(body.VHost, 128) &&
		validOpenAPIHookFlowField(body.App, 64) &&
		validOpenAPIHookFlowField(body.Stream, 255)
}

func validOpenAPIHookFlowField(value string, max int) bool {
	if value == "" || len(value) > max || value != strings.TrimSpace(value) || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}

func validOpenAPIHookFlowBootNonce(value string) bool {
	if len(value) != 32 || value != strings.ToLower(value) {
		return false
	}
	for _, r := range value {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

package handler

import (
	"context"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

type OpenAPIPlayTokenVerifier interface {
	AuthenticateOpenAPI(string) (playauth.OpenAPIClaims, error)
}

type OpenAPIViewerBinder interface {
	BindViewer(context.Context, string, playauth.OpenAPIViewerBindRequest) (models.Viewer, error)
}

// SetOpenAPIPlayAuthorization installs the v3-only verification and durable
// binding boundary. It does not activate OpenAPI, latch media authentication,
// qualify nodes, or bypass the independent Hook event authentication.
func (h *HookController) SetOpenAPIPlayAuthorization(verifier OpenAPIPlayTokenVerifier, binder OpenAPIViewerBinder) {
	h.playAuthMu.Lock()
	defer h.playAuthMu.Unlock()
	h.openAPIPlayVerifier = verifier
	h.openAPIPlayBinder = binder
}

// A valid v3 signature selects this boundary, never the legacy v2 fast path.
// Unauthenticated payload claims are not decoded to select client authority.
// Invalid/non-v3 tokens remain subject to the existing v2 verification; the
// durable must-auth latch prevents an auth-off fallback after media activation.
func (h *HookController) handleOpenAPIPlay(c *gin.Context, body onPlayBody) bool {
	h.playAuthMu.RLock()
	verifier, binder, resolver := h.openAPIPlayVerifier, h.openAPIPlayBinder, h.playResolver
	h.playAuthMu.RUnlock()
	if verifier == nil {
		return false
	}
	params, err := url.ParseQuery(strings.TrimPrefix(body.Params, "?"))
	if err != nil {
		return false
	}
	token, ok := singleValue(params, playauth.QueryParameter)
	if !ok {
		return false
	}
	claims, err := verifier.AuthenticateOpenAPI(token)
	if err != nil {
		return false
	}
	deny := func() bool {
		h.denyPlayback(c, "wrong_resource", "playback authorization denied")
		return true
	}
	identity, authenticated := AuthenticatedHookNode(c)
	if !authenticated || identity.ID <= 0 || identity.MediaServerUUID != body.MediaServerID ||
		!gbconfig.CurrentPlayAuthSettings().Enabled || binder == nil || resolver == nil ||
		body.App != "rtp" || body.ID == "" || body.BootNonce == "" {
		return deny()
	}
	protocol := ""
	switch body.Protocol {
	case "https":
		protocol = "https-flv"
	case "wss":
		protocol = "wss-flv"
	default:
		return deny()
	}
	if claims.NodeUUID != body.MediaServerID || claims.BootNonce != body.BootNonce || claims.Protocol != protocol ||
		claims.Schema != body.Schema || claims.VHost != body.VHost || claims.App != body.App || claims.Stream != body.Stream {
		return deny()
	}
	// v3 is issued only after live media is ready. A missing/stale generation
	// never triggers the legacy cold-stream auto-start fallback.
	current, err := resolver.ResolvePlaybackMediaContext(body.App, body.Stream, body.MediaServerID)
	if err != nil || current.MediaGeneration == 0 || current.MediaGeneration != claims.MediaGeneration ||
		current.DeviceID != claims.DeviceID || current.ChannelID != claims.ChannelID ||
		current.MediaServerID != body.MediaServerID || current.App != body.App || current.Stream != body.Stream {
		return deny()
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), time.Second)
	defer cancel()
	_, err = binder.BindViewer(ctx, token, playauth.OpenAPIViewerBindRequest{
		NodeUUID: identity.MediaServerUUID, BootNonce: body.BootNonce, Identifier: body.ID,
		Protocol: protocol, Schema: body.Schema, VHost: body.VHost, App: body.App, Stream: body.Stream,
		MediaGeneration: current.MediaGeneration,
	})
	if err != nil {
		return deny()
	}
	// The binder's transaction has committed before this response is emitted.
	hookOK(c)
	return true
}

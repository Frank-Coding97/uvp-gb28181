// Package installationhttp exposes the loopback-only standalone installation
// coordinator. It owns only HTTP admission state; persistence and lifecycle
// work remain in the callbacks supplied by the launcher.
package installationhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"uvplatform.cn/uvp-gb28181/internal/standalone/bootstrapcredential"
)

const (
	PhasePendingAdmin = "pending_admin"
	PhasePendingSIP   = "pending_sip"
	PhaseComplete     = "complete"
	// PhaseReady is kept as a descriptive alias for callers that expose
	// readiness rather than the durable installation phase name.
	PhaseReady  = PhaseComplete
	PhaseFailed = "failed"

	adminBodyLimit   = 4096
	setupTokenHeader = "X-UVP-Setup-Token"
)

var (
	// ErrInvalidConfiguration indicates that the handler cannot safely serve
	// the installation endpoints with the supplied configuration.
	ErrInvalidConfiguration = errors.New("invalid standalone installation HTTP configuration")
	// ErrInvalidPhase indicates that a phase outside the coordinator contract
	// was supplied to New or SetPhase.
	ErrInvalidPhase = errors.New("invalid standalone installation phase")
	// ErrPhaseLocked indicates that a reload failure requires a fresh process.
	ErrPhaseLocked = errors.New("standalone installation phase is locked")
	// ErrInvalidInput lets the administrator transaction callback classify
	// configuration-policy validation failures without exposing its details.
	ErrInvalidInput = errors.New("invalid standalone installation input")

	errInvalidAdminRequest = errors.New("invalid standalone administrator request")
)

// Handler serves the two loopback-only setup endpoints.
type Handler struct {
	mu       sync.RWMutex
	phase    string
	accepted bool
	// reloadFailed is deliberately separate from phase: the database phase is
	// still pending_sip, while this process must not claim it is recoverable.
	reloadFailed bool

	origin string
	host   string

	verifier     *bootstrapcredential.Verifier
	createAdmin  func(context.Context, string, string) error
	reloadPolicy func(context.Context) error
}

// New constructs a standalone installation coordinator. A pending_admin
// handler needs both callbacks because a successful administrator transaction
// immediately consumes the one-time credential before policy reload runs.
func New(
	baseURL string,
	initialPhase string,
	verifier *bootstrapcredential.Verifier,
	createAdmin func(context.Context, string, string) error,
	reloadPolicy func(context.Context) error,
) (*Handler, error) {
	origin, host, err := trustedOrigin(baseURL)
	if err != nil {
		return nil, ErrInvalidConfiguration
	}
	if !validPhase(initialPhase) {
		return nil, ErrInvalidPhase
	}
	if verifier == nil {
		return nil, ErrInvalidConfiguration
	}
	if initialPhase == PhasePendingAdmin && (createAdmin == nil || reloadPolicy == nil) {
		return nil, ErrInvalidConfiguration
	}
	if initialPhase != PhasePendingAdmin {
		verifier.Invalidate()
	}
	return &Handler{
		phase:        initialPhase,
		accepted:     true,
		origin:       origin,
		host:         host,
		verifier:     verifier,
		createAdmin:  createAdmin,
		reloadPolicy: reloadPolicy,
	}, nil
}

// Register installs the setup status and administrator routes on engine.
func (h *Handler) Register(engine *gin.Engine) {
	if h == nil || engine == nil {
		return
	}
	engine.GET("/api/standalone/setup/status", h.handleStatus)
	engine.POST("/api/standalone/setup/admin", h.handleAdmin)
}

// Phase returns the raw coordinator phase, including pending_sip after a
// policy reload failure.
func (h *Handler) Phase() string {
	if h == nil {
		return PhaseFailed
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.phase
}

// AllowedPhase returns the phase exposed to the application admission gate.
// Setup phases remain visible so the setup routes can run; a policy reload
// failure maps the otherwise-pending phase to failed until process restart.
func (h *Handler) AllowedPhase() string {
	if h == nil {
		return PhaseFailed
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.reloadFailed {
		return PhaseFailed
	}
	return h.phase
}

// SetPhase records a validated phase. A process whose policy reload failed is
// intentionally locked until restart, so it cannot promote itself to ready.
func (h *Handler) SetPhase(phase string) error {
	if h == nil {
		return ErrInvalidConfiguration
	}
	if !validPhase(phase) {
		return ErrInvalidPhase
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.reloadFailed {
		return ErrPhaseLocked
	}
	h.phase = phase
	return nil
}

// CredentialAccepted reports whether the current process safely received the
// one-time bootstrap credential from its launcher. It is true after New
// succeeds, even before the administrator transaction is submitted.
func (h *Handler) CredentialAccepted() bool {
	if h == nil {
		return false
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.accepted
}

// Ready reports whether this HTTP coordinator can serve the current
// installation phase. Setup phases are serviceable before business activation;
// a failed policy reload keeps the process unavailable until restart.
func (h *Handler) Ready() bool {
	if h == nil {
		return false
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.reloadFailed {
		return false
	}
	switch h.phase {
	case PhasePendingAdmin, PhasePendingSIP, PhaseComplete:
		return true
	default:
		return false
	}
}

func (h *Handler) handleStatus(c *gin.Context) {
	noStore(c)
	if !h.authorized(c, false) {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	c.JSON(http.StatusOK, statusResponse{Standalone: true, Phase: h.Phase()})
}

func (h *Handler) handleAdmin(c *gin.Context) {
	noStore(c)
	if !h.authorized(c, true) {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}

	h.mu.RLock()
	phase, reloadFailed := h.phase, h.reloadFailed
	h.mu.RUnlock()
	if reloadFailed {
		writeError(c, http.StatusServiceUnavailable, "policy reload failed")
		return
	}
	if phase != PhasePendingAdmin {
		writeError(c, http.StatusConflict, "setup phase conflict")
		return
	}

	token, ok := singleHeader(c.Request.Header, setupTokenHeader)
	if !ok {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	payload, err := readAdminRequest(c.Request)
	if err != nil {
		writeError(c, http.StatusBadRequest, "invalid setup request")
		return
	}

	ctx := c.Request.Context()
	if err := h.verifier.WithCredential(token, func() error {
		if h.createAdmin == nil {
			return ErrInvalidConfiguration
		}
		return h.createAdmin(ctx, payload.Username, payload.Password)
	}); err != nil {
		if errors.Is(err, bootstrapcredential.ErrInvalidCredential) {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		if errors.Is(err, ErrInvalidInput) {
			writeError(c, http.StatusBadRequest, "invalid setup input")
			return
		}
		writeError(c, http.StatusInternalServerError, "administrator setup failed")
		return
	}

	if h.reloadPolicy == nil {
		h.markReloadFailed()
		writeError(c, http.StatusServiceUnavailable, "policy reload failed")
		return
	}
	if err := h.reloadPolicy(ctx); err != nil {
		h.markReloadFailed()
		writeError(c, http.StatusServiceUnavailable, "policy reload failed")
		return
	}
	// Keep login behind the pending-admin gate until policy publication finishes.
	h.mu.Lock()
	h.phase = PhasePendingSIP
	h.mu.Unlock()

	c.JSON(http.StatusCreated, statusResponse{Standalone: true, Phase: PhasePendingSIP})
}

func (h *Handler) authorized(c *gin.Context, requireOrigin bool) bool {
	if h == nil || c == nil || c.Request == nil {
		return false
	}
	request := c.Request
	h.mu.RLock()
	host, origin := h.host, h.origin
	h.mu.RUnlock()
	remoteHost, _, err := net.SplitHostPort(request.RemoteAddr)
	remoteIP := net.ParseIP(remoteHost)
	if err != nil || remoteIP == nil || !remoteIP.IsLoopback() {
		return false
	}
	if request.Host != host {
		return false
	}
	if requireOrigin {
		origins := request.Header.Values("Origin")
		if len(origins) != 1 || origins[0] != origin {
			return false
		}
	}
	return true
}

func (h *Handler) markReloadFailed() {
	h.mu.Lock()
	h.reloadFailed = true
	h.phase = PhasePendingSIP
	h.mu.Unlock()
}

type statusResponse struct {
	Standalone bool   `json:"standalone"`
	Phase      string `json:"phase"`
}

type adminRequest struct {
	Username string
	Password string
}

func readAdminRequest(request *http.Request) (adminRequest, error) {
	if request == nil || request.Body == nil {
		return adminRequest{}, errInvalidAdminRequest
	}
	contentTypes := request.Header.Values("Content-Type")
	if len(contentTypes) != 1 {
		return adminRequest{}, errInvalidAdminRequest
	}
	mediaType, _, err := mime.ParseMediaType(contentTypes[0])
	if err != nil || mediaType != "application/json" {
		return adminRequest{}, errInvalidAdminRequest
	}
	raw, err := io.ReadAll(io.LimitReader(request.Body, adminBodyLimit+1))
	if err != nil || len(raw) > adminBodyLimit {
		return adminRequest{}, errInvalidAdminRequest
	}
	return decodeAdminRequest(raw)
}

func decodeAdminRequest(raw []byte) (adminRequest, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	first, err := decoder.Token()
	if err != nil || first != json.Delim('{') {
		return adminRequest{}, errInvalidAdminRequest
	}
	fields := make(map[string]json.RawMessage, 2)
	for decoder.More() {
		keyToken, err := decoder.Token()
		key, ok := keyToken.(string)
		if err != nil || !ok || key == "" {
			return adminRequest{}, errInvalidAdminRequest
		}
		if _, exists := fields[key]; exists {
			return adminRequest{}, errInvalidAdminRequest
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return adminRequest{}, errInvalidAdminRequest
		}
		fields[key] = value
	}
	last, err := decoder.Token()
	if err != nil || last != json.Delim('}') {
		return adminRequest{}, errInvalidAdminRequest
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return adminRequest{}, errInvalidAdminRequest
	}
	if len(fields) != 2 {
		return adminRequest{}, errInvalidAdminRequest
	}
	usernameRaw, usernameOK := fields["username"]
	passwordRaw, passwordOK := fields["password"]
	if !usernameOK || !passwordOK {
		return adminRequest{}, errInvalidAdminRequest
	}
	var payload adminRequest
	if json.Unmarshal(usernameRaw, &payload.Username) != nil || json.Unmarshal(passwordRaw, &payload.Password) != nil {
		return adminRequest{}, errInvalidAdminRequest
	}
	if strings.TrimSpace(payload.Username) == "" || payload.Password == "" {
		return adminRequest{}, errInvalidAdminRequest
	}
	return payload, nil
}

func singleHeader(header http.Header, name string) (string, bool) {
	values := header.Values(name)
	returnValue := ""
	if len(values) == 1 {
		returnValue = values[0]
	}
	return returnValue, len(values) == 1 && returnValue != ""
}

func trustedOrigin(raw string) (origin, host string, err error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Opaque != "" {
		return "", "", ErrInvalidConfiguration
	}
	if parsed.Hostname() == "" {
		return "", "", ErrInvalidConfiguration
	}
	if port := parsed.Port(); port != "" {
		portNumber, err := strconv.Atoi(port)
		if err != nil || portNumber < 1 || portNumber > 65535 {
			return "", "", ErrInvalidConfiguration
		}
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", "", ErrInvalidConfiguration
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return "", "", ErrInvalidConfiguration
	}
	return scheme + "://" + parsed.Host, parsed.Host, nil
}

func validPhase(phase string) bool {
	switch phase {
	case PhasePendingAdmin, PhasePendingSIP, PhaseComplete, PhaseFailed:
		return true
	default:
		return false
	}
}

func noStore(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
}

func writeError(c *gin.Context, status int, message string) {
	c.AbortWithStatusJSON(status, gin.H{"code": status, "msg": message})
}

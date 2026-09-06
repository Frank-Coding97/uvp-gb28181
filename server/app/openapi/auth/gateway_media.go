package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/openapi/client"
	"uvplatform.cn/uvp-gb28181/app/openapi/limit"
	"uvplatform.cn/uvp-gb28181/app/openapi/resource"
)

const (
	mediaScope   = limit.PlayLiveApplyScope
	mediaPattern = "/openapi/v1/devices/:deviceId/channels/:channelId/live-authorizations"
)

// MediaTarget is the exact resource and protocol selected by one external
// request. It is built only after the path, owner and body have been checked.
type MediaTarget struct {
	DeviceID  string
	ChannelID string
	Protocol  string
}

// MediaTicket is opaque to the HTTP gateway. The dispatcher must retain the
// ticket's meaning and require this exact value again in Apply.
type MediaTicket string

// MediaAdmittedRequest is the post-commit handoff to the media application.
// ClientID and GrantID are server-derived; neither can be supplied by HTTP.
type MediaAdmittedRequest struct {
	ClientID    int64
	GrantID     string
	DeviceEpoch int64
	Target      MediaTarget
	Ticket      MediaTicket
}

// MediaAuthorization is the public allowlisted response data. Internal node,
// grant, ticket and diagnostic fields deliberately have no response path.
type MediaAuthorization struct {
	AuthorizationID string    `json:"authorizationId"`
	Protocol        string    `json:"protocol"`
	URL             string    `json:"url"`
	ExpiresAt       time.Time `json:"expiresAt"`
}

// MediaDispatcher is the only media dependency of the auth gateway. It is
// injected during startup; there is intentionally no runtime setter. Prepare
// may perform bounded qualification but must not start media work. Apply runs
// only after Admission has durably committed nonce, audit and the pending
// quota reservation.
type MediaDispatcher interface {
	Ready() bool
	Prepare(context.Context, MediaTarget) (MediaTicket, error)
	Apply(context.Context, MediaAdmittedRequest) (MediaAuthorization, error)
}

// ErrMediaQuotaExceeded is a stable classification for a safe 429 response.
// The Admission transaction normalizes callback failures to ErrUnavailable,
// so the gateway retains the original callback error separately.
var ErrMediaQuotaExceeded = limit.ErrQuotaExceeded

// GatewayOption is applied once while constructing a Gateway. Keeping the
// option private to startup prevents an in-flight request from seeing a
// dispatcher swap.
type GatewayOption func(*Gateway)

func WithMediaDispatcher(dispatcher MediaDispatcher) GatewayOption {
	return func(gateway *Gateway) {
		if gateway != nil {
			gateway.media = dispatcher
		}
	}
}

func (g *Gateway) handleMedia(c *gin.Context, requestID string, respond func(gatewayResponse)) {
	r := c.Request
	failEarly := func(response gatewayResponse) {
		// The request body is intentionally not drained on an early rejection.
		// Closing the connection keeps an unbounded/chunked sender from tying up
		// a handler after the response has been written.
		c.Header("Connection", "close")
		respond(response)
	}
	if !g.mediaReady() {
		failEarly(gatewayError(requestID, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE"))
		return
	}
	if r == nil || g.tls == nil || !g.tls.IsHTTPS(r) {
		failEarly(gatewayError(requestID, http.StatusUnauthorized, "AUTHENTICATION_FAILED"))
		return
	}
	if r.Method != http.MethodPost || r.URL == nil || r.URL.RawPath != "" || r.URL.Path == "" {
		failEarly(gatewayError(requestID, http.StatusBadRequest, "INVALID_REQUEST"))
		return
	}
	if r.ContentLength <= 0 || r.ContentLength > maxBodyBytes || len(r.TransferEncoding) != 0 {
		failEarly(gatewayError(requestID, http.StatusBadRequest, "INVALID_REQUEST"))
		return
	}
	if len(r.Header.Values("Content-Encoding")) != 0 || len(r.Header.Values("X-HTTP-Method-Override")) != 0 || len(r.Header.Values("X-HTTP-Method")) != 0 || len(r.Header.Values("X-Method-Override")) != 0 {
		failEarly(gatewayError(requestID, http.StatusBadRequest, "INVALID_REQUEST"))
		return
	}
	contentTypes := r.Header.Values("Content-Type")
	if len(contentTypes) != 1 || contentTypes[0] != "application/json" {
		failEarly(gatewayError(requestID, http.StatusBadRequest, "INVALID_REQUEST"))
		return
	}
	if !mediaRouteMatches(gatewayRequest{method: r.Method, path: r.URL.Path, rawURI: r.RequestURI, rawQuery: r.URL.RawQuery, pattern: c.FullPath(), scope: mediaScope, deviceID: c.Param("deviceId"), channelID: c.Param("channelId")}) {
		failEarly(gatewayError(requestID, http.StatusBadRequest, "INVALID_REQUEST"))
		return
	}

	if g.slots == nil {
		failEarly(gatewayError(requestID, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE"))
		return
	}
	select {
	case g.slots <- struct{}{}:
	default:
		failEarly(gatewayError(requestID, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE"))
		return
	}
	slotHeld := true
	defer func() {
		if slotHeld {
			<-g.slots
		}
	}()

	hardDeadline := time.Now().Add(g.config.Timeout)
	if requestDeadline, ok := r.Context().Deadline(); ok && requestDeadline.Before(hardDeadline) {
		hardDeadline = requestDeadline
	}
	if err := http.NewResponseController(c.Writer).SetReadDeadline(hardDeadline); err != nil {
		failEarly(gatewayError(requestID, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE"))
		return
	}

	hardContext, cancel := context.WithDeadline(r.Context(), hardDeadline)
	defer cancel()
	if r.Body == nil {
		failEarly(gatewayError(requestID, http.StatusBadRequest, "INVALID_REQUEST"))
		return
	}
	defer r.Body.Close()
	limitedBody := http.MaxBytesReader(unwrapResponseWriter(c.Writer), r.Body, maxBodyBytes)
	body, err := io.ReadAll(limitedBody)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) || errors.Is(err, io.ErrUnexpectedEOF) {
			failEarly(gatewayError(requestID, http.StatusBadRequest, "INVALID_REQUEST"))
			return
		}
		failEarly(gatewayError(requestID, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE"))
		return
	}
	if int64(len(body)) != r.ContentLength {
		failEarly(gatewayError(requestID, http.StatusBadRequest, "INVALID_REQUEST"))
		return
	}
	softDeadline := hardDeadline.Add(-g.config.AuditReserve)
	if hardContext.Err() != nil || !time.Now().Before(softDeadline) {
		failEarly(gatewayError(requestID, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE"))
		return
	}

	q := gatewayRequest{
		method:      r.Method,
		path:        r.URL.Path,
		rawURI:      r.RequestURI,
		rawQuery:    r.URL.RawQuery,
		pattern:     c.FullPath(),
		scope:       mediaScope,
		deviceID:    c.Param("deviceId"),
		channelID:   c.Param("channelId"),
		requestID:   requestID,
		source:      c.ClientIP(),
		contentType: contentTypes[0],
		body:        append([]byte(nil), body...),
		headers:     captureGatewayHeaders(r),
	}
	finished := make(chan gatewayResponse, 1)
	slotHeld = false
	go func() {
		defer func() { <-g.slots }()
		defer func() {
			if recover() != nil {
				finished <- gatewayError(requestID, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE")
			}
		}()
		finished <- g.run(hardContext, q)
	}()
	select {
	case <-hardContext.Done():
		respond(gatewayError(requestID, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE"))
	case response := <-finished:
		if hardContext.Err() != nil {
			response = gatewayError(requestID, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE")
		}
		respond(response)
	}
}

func (g *Gateway) mediaReady() (ready bool) {
	if g == nil || g.media == nil {
		return false
	}
	defer func() {
		if recover() != nil {
			ready = false
		}
	}()
	return g.media.Ready()
}

func unwrapResponseWriter(writer http.ResponseWriter) http.ResponseWriter {
	for i := 0; i < 4; i++ {
		unwrapper, ok := writer.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			break
		}
		unwrapped := unwrapper.Unwrap()
		if unwrapped == nil || unwrapped == writer {
			break
		}
		writer = unwrapped
	}
	return writer
}

func captureGatewayHeaders(r *http.Request) HeaderValues {
	if r == nil {
		return HeaderValues{}
	}
	return HeaderValues{
		SignVersion:     append([]string(nil), r.Header.Values("X-UVP-Sign-Version")...),
		AccessKey:       append([]string(nil), r.Header.Values("X-UVP-Access-Key")...),
		Timestamp:       append([]string(nil), r.Header.Values("X-UVP-Timestamp")...),
		Nonce:           append([]string(nil), r.Header.Values("X-UVP-Nonce")...),
		Signature:       append([]string(nil), r.Header.Values("X-UVP-Signature")...),
		ContentType:     append([]string(nil), r.Header.Values("Content-Type")...),
		ContentEncoding: append([]string(nil), r.Header.Values("Content-Encoding")...),
		MethodOverride:  append(append(append([]string(nil), r.Header.Values("X-HTTP-Method-Override")...), r.Header.Values("X-HTTP-Method")...), r.Header.Values("X-Method-Override")...),
	}
}

func mediaRouteMatches(q gatewayRequest) bool {
	if q.scope != mediaScope || q.pattern != mediaPattern || q.method != http.MethodPost || q.rawQuery != "" || validatePath(q.path) != nil {
		return false
	}
	path := strings.ReplaceAll(strings.ReplaceAll(mediaPattern, ":deviceId", q.deviceID), ":channelId", q.channelID)
	rawPath, _, hasQuery := strings.Cut(q.rawURI, "?")
	return path == q.path && rawPath == q.path && !hasQuery
}

func parseMediaTarget(q gatewayRequest) (MediaTarget, error) {
	var fields map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(q.body))
	if err := decoder.Decode(&fields); err != nil {
		return MediaTarget{}, err
	}
	if len(fields) != 1 {
		return MediaTarget{}, errors.New("media request must contain one field")
	}
	protocolJSON, ok := fields["protocol"]
	if !ok {
		return MediaTarget{}, errors.New("media request requires exact protocol key")
	}
	var protocol string
	if err := json.Unmarshal(protocolJSON, &protocol); err != nil {
		return MediaTarget{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return MediaTarget{}, errors.New("media request has trailing JSON")
	}
	target := MediaTarget{DeviceID: q.deviceID, ChannelID: q.channelID, Protocol: protocol}
	if err := validateMediaTarget(target); err != nil {
		return MediaTarget{}, err
	}
	return target, nil
}

func validateMediaTarget(target MediaTarget) error {
	if !validMediaID(target.DeviceID) || !validMediaID(target.ChannelID) {
		return errors.New("invalid media target")
	}
	if target.Protocol != "https-flv" && target.Protocol != "wss-flv" {
		return errors.New("unsupported media protocol")
	}
	return nil
}

func validMediaID(value string) bool {
	if len(value) != 20 {
		return false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}

func (g *Gateway) processMedia(hardContext context.Context, q gatewayRequest) (output gatewayResponse) {
	var verifiedClientID int64
	admitted := false
	start := time.Now()
	defer func() {
		output.verifiedClientID = verifiedClientID
		output.admitted = admitted
	}()
	invalid := func(status int, code string) gatewayResponse { return gatewayError(q.requestID, status, code) }
	if !g.mediaReady() || !mediaRouteMatches(q) {
		return invalid(http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE")
	}
	deadline, ok := hardContext.Deadline()
	if !ok {
		return invalid(http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE")
	}
	ctx, cancel := context.WithDeadline(hardContext, deadline.Add(-g.config.AuditReserve))
	defer cancel()
	if ctx.Err() != nil {
		return invalid(http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE")
	}
	if len(q.headers.SignVersion) == 0 || len(q.headers.AccessKey) == 0 || len(q.headers.Timestamp) == 0 || len(q.headers.Nonce) == 0 || len(q.headers.Signature) == 0 {
		return invalid(http.StatusUnauthorized, "AUTHENTICATION_FAILED")
	}
	headers, err := ParseHeaders(q.method, q.headers)
	if err != nil {
		return invalid(http.StatusBadRequest, "INVALID_REQUEST")
	}
	input := SignatureInput{Method: q.method, Path: q.path, RawQuery: q.rawQuery, ContentType: headers.ContentType, Body: q.body, AccessKey: headers.AccessKey, Timestamp: headers.Timestamp, Nonce: headers.Nonce, Audience: g.config.Audience}
	material, err := g.clients.LoadVerificationMaterial(ctx, headers.AccessKey)
	if errors.Is(err, client.ErrDependencyUnavailable) {
		return invalid(http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE")
	}
	if err != nil || Verify(input, material.SecretKey, headers.Signature) != nil {
		return invalid(http.StatusUnauthorized, "AUTHENTICATION_FAILED")
	}
	verifiedClientID = material.ClientID
	now, err := g.admission.clock()
	if err != nil {
		return invalid(http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE")
	}
	requestUnix, parseErr := parseUnixTimestamp(headers.Timestamp)
	if parseErr != nil || requestUnix < now.Unix()-300 || requestUnix > now.Unix()+300 {
		return invalid(http.StatusUnauthorized, "REQUEST_EXPIRED")
	}
	view, err := g.clients.Get(ctx, material.ClientID)
	if err != nil {
		return invalid(http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE")
	}
	permission, err := g.clients.GetScope(ctx, material.ClientID, mediaScope)
	if errors.Is(err, client.ErrNotFound) || (err == nil && !permission.Enabled) {
		return invalid(http.StatusForbidden, "CAPABILITY_DENIED")
	}
	if err != nil {
		return invalid(http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE")
	}
	target, err := parseMediaTarget(q)
	if err != nil {
		return invalid(http.StatusBadRequest, "INVALID_REQUEST")
	}
	metadata := metadataInput{owner: view.OwnerDeptID, scope: mediaScope, deviceID: target.DeviceID, channelID: target.ChannelID}
	if err = checkMetadata(ctx, g.db, metadata); err != nil {
		return metadataFailure(q.requestID, err)
	}
	retry, err := g.limiter.Allow(view.ID, view.RateLimit, view.Burst, true)
	if err != nil {
		if !errors.Is(err, limit.ErrRateLimited) {
			return invalid(http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE")
		}
		response := invalid(http.StatusTooManyRequests, "RATE_LIMITED")
		response.retryAfter = int((retry + time.Second - 1) / time.Second)
		return response
	}
	if !g.mediaReady() {
		return invalid(http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE")
	}
	ticket, err := prepareMedia(g.media, ctx, target)
	if errors.Is(err, limit.ErrQuotaExceeded) {
		return invalid(http.StatusTooManyRequests, "QUOTA_EXCEEDED")
	}
	if err != nil || ticket == "" || len(ticket) > 1024 {
		return invalid(http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE")
	}
	var reservation limit.Reservation
	var reserveErr error
	err = g.admission.Admit(ctx, AdmissionRequest{ClientID: view.ID, SecretVersion: material.SecretVersion, AuthEpoch: material.AuthEpoch, ScopeEpoch: permission.ScopeEpoch, Scope: mediaScope, Nonce: headers.Nonce, Timestamp: headers.Timestamp, RequestID: q.requestID, ResourceType: metadata.resourceType(), ResourceID: metadata.resourceID(), Source: q.source}, func(tx *gorm.DB) error {
		if reserveErr = checkMetadata(ctx, tx, metadata); reserveErr != nil {
			return reserveErr
		}
		if g.quota == nil {
			reserveErr = limit.ErrQuotaUnavailable
			return reserveErr
		}
		reservation, reserveErr = g.quota.ReservePendingTx(ctx, tx, limit.ReservationRequest{ClientID: view.ID, Scope: mediaScope, DeviceID: target.DeviceID, ChannelID: target.ChannelID})
		return reserveErr
	})
	if err != nil {
		switch {
		case errors.Is(reserveErr, limit.ErrQuotaExceeded):
			return invalid(http.StatusTooManyRequests, "QUOTA_EXCEEDED")
		case errors.Is(reserveErr, resource.ErrResourceNotFound):
			return invalid(http.StatusNotFound, "RESOURCE_NOT_FOUND")
		case errors.Is(err, ErrReplay):
			return invalid(http.StatusUnauthorized, "REQUEST_REPLAYED")
		case errors.Is(err, ErrExpired):
			return invalid(http.StatusUnauthorized, "REQUEST_EXPIRED")
		case errors.Is(err, ErrDenied):
			return invalid(http.StatusUnauthorized, "AUTHENTICATION_FAILED")
		default:
			return invalid(http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE")
		}
	}
	admitted = true
	if reservation.GrantID == "" || !g.mediaReady() || ctx.Err() != nil {
		return g.completeMedia(hardContext, q, invalid(http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE"), "SERVICE_UNAVAILABLE", start)
	}
	authorization, err := applyMedia(g.media, ctx, MediaAdmittedRequest{ClientID: view.ID, GrantID: reservation.GrantID, DeviceEpoch: reservation.DeviceEpoch, Target: target, Ticket: ticket})
	if err != nil || validateMediaAuthorization(authorization, reservation.GrantID, target) != nil || ctx.Err() != nil {
		return g.completeMedia(hardContext, q, invalid(http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE"), "SERVICE_UNAVAILABLE", start)
	}
	authorization.ExpiresAt = authorization.ExpiresAt.UTC()
	return g.completeMedia(hardContext, q, encodeGateway(q.requestID, http.StatusOK, "OK", authorization), "OK", start)
}

func parseUnixTimestamp(value string) (int64, error) {
	return strconv.ParseInt(value, 10, 64)
}

func prepareMedia(dispatcher MediaDispatcher, ctx context.Context, target MediaTarget) (ticket MediaTicket, err error) {
	defer func() {
		if recover() != nil {
			ticket = ""
			err = ErrUnavailable
		}
	}()
	if dispatcher == nil {
		return "", ErrUnavailable
	}
	return dispatcher.Prepare(ctx, target)
}

func applyMedia(dispatcher MediaDispatcher, ctx context.Context, request MediaAdmittedRequest) (authorization MediaAuthorization, err error) {
	defer func() {
		if recover() != nil {
			authorization = MediaAuthorization{}
			err = ErrUnavailable
		}
	}()
	if dispatcher == nil {
		return MediaAuthorization{}, ErrUnavailable
	}
	return dispatcher.Apply(ctx, request)
}

func validateMediaAuthorization(authorization MediaAuthorization, grantID string, target MediaTarget) error {
	if grantID == "" || authorization.AuthorizationID != grantID || len(authorization.AuthorizationID) > 128 || validateLineField(authorization.AuthorizationID) != nil || authorization.Protocol != target.Protocol || authorization.URL == "" || len(authorization.URL) > 4096 || validateLineField(authorization.URL) != nil || authorization.ExpiresAt.IsZero() || !authorization.ExpiresAt.After(time.Now().UTC()) {
		return errors.New("invalid media authorization")
	}
	parsed, err := url.Parse(authorization.URL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return errors.New("invalid media authorization URL")
	}
	if (target.Protocol == "https-flv" && parsed.Scheme != "https") || (target.Protocol == "wss-flv" && parsed.Scheme != "wss") {
		return errors.New("media authorization URL protocol mismatch")
	}
	return nil
}

func (g *Gateway) completeMedia(hardContext context.Context, q gatewayRequest, response gatewayResponse, code string, start time.Time) gatewayResponse {
	if g.complete == nil || g.complete(hardContext, q.requestID, code, time.Since(start)) != nil {
		return gatewayError(q.requestID, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE")
	}
	return response
}

package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/openapi/audit"
	"uvplatform.cn/uvp-gb28181/app/openapi/client"
	"uvplatform.cn/uvp-gb28181/app/openapi/limit"
	"uvplatform.cn/uvp-gb28181/app/openapi/resource"
)

type GatewayConfig struct {
	Audience              string
	TLSProxies            []string
	Timeout, AuditReserve time.Duration
	MaxInFlight           int
}

// Gateway owns a single JSON response. Its workers never receive Gin contexts
// or writers; a worker ignoring cancellation cannot write after the deadline.
type Gateway struct {
	config    GatewayConfig
	db        *gorm.DB
	clients   *client.Service
	admission *Admission
	limiter   *limit.Manager
	tls       *TLSBoundary
	slots     chan struct{}
	run       func(context.Context, gatewayRequest) gatewayResponse
	read      func(context.Context, *gorm.DB, metadataInput) (any, error)
	complete  func(context.Context, string, string, time.Duration) error
}
type gatewayRequest struct {
	method, path, rawURI, rawQuery, pattern, scope, deviceID, channelID, requestID, source string
	headers                                                                                HeaderValues
}
type gatewayResponse struct {
	status     int
	body       []byte
	retryAfter int
}

func NewGateway(ctx context.Context, db *gorm.DB, keys *client.SecretManager, config GatewayConfig) (*Gateway, error) {
	if db == nil || keys == nil || validateLineField(config.Audience) != nil || config.Timeout <= 0 || config.AuditReserve <= 0 || config.AuditReserve >= config.Timeout || config.MaxInFlight <= 0 {
		return nil, ErrUnavailable
	}
	transport, err := NewTLSBoundary(config.TLSProxies)
	if err != nil {
		return nil, err
	}
	service, err := client.NewService(db, keys)
	if err != nil {
		return nil, ErrUnavailable
	}
	store := audit.New(db, time.Now)
	if err = store.RecoverInterrupted(ctx); err != nil {
		return nil, ErrUnavailable
	}
	g := &Gateway{config: config, db: db, clients: service, admission: NewAdmission(db, time.Now), limiter: limit.New(time.Now), tls: transport, slots: make(chan struct{}, config.MaxInFlight), complete: store.Complete, read: readMetadata}
	g.run = g.process
	return g, nil
}

func (g *Gateway) Handler(scope string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		var id [16]byte
		if _, err := rand.Read(id[:]); err != nil {
			writeGateway(c, gatewayError("", 503, "SERVICE_UNAVAILABLE"))
			return
		}
		requestID := hex.EncodeToString(id[:])
		c.Set("requestId", requestID)
		// The media branch remains unavailable before T09--T12; no nonce, quota,
		// audit-started row, or business dispatch is created by this stub.
		if g == nil || scope == "play:live:apply" {
			writeGateway(c, gatewayError(requestID, 503, "SERVICE_UNAVAILABLE"))
			return
		}
		if !g.tls.IsHTTPS(c.Request) {
			writeGateway(c, gatewayError(requestID, 401, "AUTHENTICATION_FAILED"))
			return
		}
		if c.Request.ContentLength != 0 || len(c.Request.TransferEncoding) != 0 || c.Request.URL.RawPath != "" {
			writeGateway(c, gatewayError(requestID, 400, "INVALID_REQUEST"))
			return
		}
		r := c.Request
		q := gatewayRequest{method: r.Method, path: r.URL.Path, rawURI: r.RequestURI, rawQuery: r.URL.RawQuery, pattern: c.FullPath(), scope: scope, deviceID: c.Param("deviceId"), channelID: c.Param("channelId"), requestID: requestID, source: c.ClientIP()}
		q.headers = HeaderValues{SignVersion: append([]string(nil), r.Header.Values("X-UVP-Sign-Version")...), AccessKey: append([]string(nil), r.Header.Values("X-UVP-Access-Key")...), Timestamp: append([]string(nil), r.Header.Values("X-UVP-Timestamp")...), Nonce: append([]string(nil), r.Header.Values("X-UVP-Nonce")...), Signature: append([]string(nil), r.Header.Values("X-UVP-Signature")...), ContentType: append([]string(nil), r.Header.Values("Content-Type")...), ContentEncoding: append([]string(nil), r.Header.Values("Content-Encoding")...)}
		for _, name := range []string{"X-HTTP-Method-Override", "X-HTTP-Method", "X-Method-Override"} {
			q.headers.MethodOverride = append(q.headers.MethodOverride, r.Header.Values(name)...)
		}
		select {
		case g.slots <- struct{}{}:
		default:
			writeGateway(c, gatewayError(requestID, 503, "SERVICE_UNAVAILABLE"))
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), g.config.Timeout)
		defer cancel()
		finished := make(chan gatewayResponse, 1)
		go func() {
			defer func() { <-g.slots }()
			defer func() {
				if recover() != nil {
					finished <- gatewayError(requestID, 503, "SERVICE_UNAVAILABLE")
				}
			}()
			finished <- g.run(ctx, q)
		}()
		select {
		case <-ctx.Done():
			writeGateway(c, gatewayError(requestID, 503, "SERVICE_UNAVAILABLE"))
		case response := <-finished:
			if ctx.Err() != nil {
				response = gatewayError(requestID, 503, "SERVICE_UNAVAILABLE")
			}
			writeGateway(c, response)
		}
	}
}

func writeGateway(c *gin.Context, response gatewayResponse) {
	if response.retryAfter > 0 {
		c.Header("Retry-After", strconv.Itoa(response.retryAfter))
	}
	c.Data(response.status, "application/json; charset=utf-8", response.body)
}

func gatewayError(id string, status int, code string) gatewayResponse {
	return encodeGateway(id, status, code, nil)
}
func encodeGateway(id string, status int, code string, data any) gatewayResponse {
	messages := map[string]string{"OK": "success", "INVALID_REQUEST": "invalid request", "AUTHENTICATION_FAILED": "authentication failed", "REQUEST_EXPIRED": "request expired", "REQUEST_REPLAYED": "request replayed", "CAPABILITY_DENIED": "capability denied", "RESOURCE_NOT_FOUND": "resource not found", "RATE_LIMITED": "rate limited", "SERVICE_UNAVAILABLE": "service unavailable"}
	body, err := json.Marshal(struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"requestId"`
		Data      any    `json:"data"`
	}{code, messages[code], id, data})
	if err != nil {
		return gatewayError(id, 503, "SERVICE_UNAVAILABLE")
	}
	return gatewayResponse{status: status, body: body}
}

func (g *Gateway) process(hardContext context.Context, q gatewayRequest) gatewayResponse {
	start := time.Now()
	deadline, _ := hardContext.Deadline()
	ctx, cancel := context.WithDeadline(hardContext, deadline.Add(-g.config.AuditReserve))
	defer cancel()
	invalid := func(status int, code string) gatewayResponse { return gatewayError(q.requestID, status, code) }
	if !metadataRouteMatches(q) {
		return invalid(400, "INVALID_REQUEST")
	}
	headers, err := ParseHeaders(q.method, q.headers)
	if err != nil {
		return invalid(401, "AUTHENTICATION_FAILED")
	}
	input := SignatureInput{Method: q.method, Path: q.path, RawQuery: q.rawQuery, AccessKey: headers.AccessKey, Timestamp: headers.Timestamp, Nonce: headers.Nonce, Audience: g.config.Audience}
	if _, err = CanonicalString(input); err != nil {
		return invalid(400, "INVALID_REQUEST")
	}
	material, err := g.clients.LoadVerificationMaterial(ctx, headers.AccessKey)
	if err != nil || Verify(input, material.SecretKey, headers.Signature) != nil {
		return invalid(401, "AUTHENTICATION_FAILED")
	}
	now, err := g.admission.clock()
	if err != nil {
		return invalid(503, "SERVICE_UNAVAILABLE")
	}
	ts, _ := strconv.ParseInt(headers.Timestamp, 10, 64)
	if ts < now.Unix()-300 || ts > now.Unix()+300 {
		return invalid(401, "REQUEST_EXPIRED")
	}
	view, err := g.clients.Get(ctx, material.ClientID)
	if err != nil {
		return invalid(503, "SERVICE_UNAVAILABLE")
	}
	permission, err := g.clients.GetScope(ctx, material.ClientID, q.scope)
	if errors.Is(err, client.ErrNotFound) || (err == nil && !permission.Enabled) {
		return invalid(403, "CAPABILITY_DENIED")
	}
	if err != nil {
		return invalid(503, "SERVICE_UNAVAILABLE")
	}
	metadata, err := parseMetadata(q, view.OwnerDeptID)
	if err != nil {
		return invalid(400, "INVALID_REQUEST")
	}
	if err = checkMetadata(ctx, g.db, metadata); err != nil {
		return metadataFailure(q.requestID, err)
	}
	retry, err := g.limiter.Allow(view.ID, view.RateLimit, view.Burst, false)
	if err != nil {
		if !errors.Is(err, limit.ErrRateLimited) {
			return invalid(503, "SERVICE_UNAVAILABLE")
		}
		response := invalid(429, "RATE_LIMITED")
		response.retryAfter = int((retry + time.Second - 1) / time.Second)
		return response
	}
	err = g.admission.Admit(ctx, AdmissionRequest{ClientID: view.ID, SecretVersion: material.SecretVersion, AuthEpoch: material.AuthEpoch, ScopeEpoch: permission.ScopeEpoch, Scope: q.scope, Nonce: headers.Nonce, Timestamp: headers.Timestamp, RequestID: q.requestID, ResourceType: metadata.resourceType(), ResourceID: metadata.resourceID(), Source: q.source}, func(tx *gorm.DB) error { return checkMetadata(ctx, tx, metadata) })
	if err != nil {
		switch {
		case errors.Is(err, ErrReplay):
			return invalid(409, "REQUEST_REPLAYED")
		case errors.Is(err, ErrExpired):
			return invalid(401, "REQUEST_EXPIRED")
		case errors.Is(err, ErrDenied):
			return invalid(401, "AUTHENTICATION_FAILED")
		default:
			return invalid(503, "SERVICE_UNAVAILABLE")
		}
	}
	var response gatewayResponse
	code := "SERVICE_UNAVAILABLE"
	if ctx.Err() == nil {
		data, readErr := g.read(ctx, g.db, metadata)
		if readErr == nil && ctx.Err() == nil {
			response = encodeGateway(q.requestID, 200, "OK", data)
			if response.status == 200 {
				code = "OK"
			}
		} else {
			response = metadataFailure(q.requestID, readErr)
			if response.status == 404 {
				code = "RESOURCE_NOT_FOUND"
			}
		}
	}
	if response.status == 0 {
		response = invalid(503, "SERVICE_UNAVAILABLE")
	}
	if err = g.complete(hardContext, q.requestID, code, time.Since(start)); err != nil {
		return invalid(503, "SERVICE_UNAVAILABLE")
	}
	return response
}

func metadataFailure(id string, err error) gatewayResponse {
	if errors.Is(err, resource.ErrResourceNotFound) {
		return gatewayError(id, 404, "RESOURCE_NOT_FOUND")
	}
	return gatewayError(id, 503, "SERVICE_UNAVAILABLE")
}

func metadataPattern(scope string) string {
	switch scope {
	case "device:list":
		return "/openapi/v1/devices"
	case "device:detail":
		return "/openapi/v1/devices/:deviceId"
	case "device:status":
		return "/openapi/v1/devices/:deviceId/status"
	case "channel:list":
		return "/openapi/v1/devices/:deviceId/channels"
	case "channel:detail":
		return "/openapi/v1/devices/:deviceId/channels/:channelId"
	case "channel:status":
		return "/openapi/v1/devices/:deviceId/channels/:channelId/status"
	default:
		return ""
	}
}
func metadataRouteMatches(q gatewayRequest) bool {
	pattern := metadataPattern(q.scope)
	if pattern == "" || q.pattern != pattern || q.method != "GET" || validatePath(q.path) != nil {
		return false
	}
	path := strings.ReplaceAll(strings.ReplaceAll(pattern, ":deviceId", q.deviceID), ":channelId", q.channelID)
	rawPath, rawQuery, _ := strings.Cut(q.rawURI, "?")
	return path == q.path && rawPath == q.path && rawQuery == q.rawQuery
}

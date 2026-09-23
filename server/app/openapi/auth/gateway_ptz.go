package auth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/openapi/client"
	"uvplatform.cn/uvp-gb28181/app/openapi/limit"
)

func (g *Gateway) handlePTZ(c *gin.Context, requestID, scope string, respond func(gatewayResponse)) {
	failEarly := func(response gatewayResponse) {
		c.Header("Connection", "close")
		respond(response)
	}
	if !g.ptzReady() || g.tls == nil || !g.tls.IsHTTPS(c.Request) {
		failEarly(gatewayError(requestID, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE"))
		return
	}
	r := c.Request
	if r == nil || r.URL == nil {
		failEarly(gatewayError(requestID, http.StatusBadRequest, "INVALID_REQUEST"))
		return
	}
	method := r.Method
	q := gatewayRequest{method: method, path: r.URL.Path, rawURI: r.RequestURI, rawQuery: r.URL.RawQuery, pattern: c.FullPath(), scope: scope, deviceID: c.Param("deviceId"), channelID: c.Param("channelId"), presetID: c.Param("presetId"), operationID: c.Param("operationId"), requestID: requestID, source: c.ClientIP()}
	q.headers = captureGatewayHeaders(r)
	if len(q.headers.IdempotencyKey) > 1 {
		failEarly(gatewayError(requestID, http.StatusBadRequest, "INVALID_REQUEST"))
		return
	}
	if len(q.headers.IdempotencyKey) == 1 {
		q.idempotencyKey = q.headers.IdempotencyKey[0]
	}
	if q.method == http.MethodGet {
		if r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
			failEarly(gatewayError(requestID, http.StatusBadRequest, "INVALID_REQUEST"))
			return
		}
	} else {
		if r.ContentLength < 0 || r.ContentLength > maxBodyBytes || len(r.TransferEncoding) != 0 || r.ContentLength == 0 {
			failEarly(gatewayError(requestID, http.StatusBadRequest, "INVALID_REQUEST"))
			return
		}
		contentTypes := r.Header.Values("Content-Type")
		if len(contentTypes) != 1 || contentTypes[0] != "application/json" {
			failEarly(gatewayError(requestID, http.StatusBadRequest, "INVALID_REQUEST"))
			return
		}
		q.contentType = contentTypes[0]
		body, err := io.ReadAll(http.MaxBytesReader(unwrapResponseWriter(c.Writer), r.Body, maxBodyBytes))
		if err != nil || int64(len(body)) != r.ContentLength {
			failEarly(gatewayError(requestID, http.StatusBadRequest, "INVALID_REQUEST"))
			return
		}
		q.body = append([]byte(nil), body...)
	}
	if !ptzRouteMatches(q) || validatePTZRequest(q) != nil {
		failEarly(gatewayError(requestID, http.StatusBadRequest, "INVALID_REQUEST"))
		return
	}
	hardDeadline := time.Now().Add(g.config.Timeout)
	if deadline, ok := r.Context().Deadline(); ok && deadline.Before(hardDeadline) {
		hardDeadline = deadline
	}
	hardContext, cancel := context.WithDeadline(r.Context(), hardDeadline)
	defer cancel()
	finished := make(chan gatewayResponse, 1)
	select {
	case g.slots <- struct{}{}:
	default:
		failEarly(gatewayError(requestID, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE"))
		return
	}
	go func() {
		defer func() { <-g.slots }()
		finished <- g.processPTZ(hardContext, q)
	}()
	select {
	case <-hardContext.Done():
		respond(gatewayError(requestID, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE"))
	case response := <-finished:
		respond(response)
	}
}

func (g *Gateway) processPTZ(hardContext context.Context, q gatewayRequest) (output gatewayResponse) {
	var verifiedClientID int64
	admitted := false
	defer func() {
		output.verifiedClientID = verifiedClientID
		output.admitted = admitted
	}()
	start := time.Now()
	deadline, ok := hardContext.Deadline()
	if !ok {
		return gatewayError(q.requestID, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE")
	}
	ctx, cancel := context.WithDeadline(hardContext, deadline.Add(-g.config.AuditReserve))
	defer cancel()
	invalid := func(status int, code string) gatewayResponse { return gatewayError(q.requestID, status, code) }
	if !ptzRouteMatches(q) || validatePTZRequest(q) != nil {
		return invalid(http.StatusBadRequest, "INVALID_REQUEST")
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
	requestUnix, err := parseUnixTimestamp(headers.Timestamp)
	if err != nil || requestUnix < now.Unix()-300 || requestUnix > now.Unix()+300 {
		return invalid(http.StatusUnauthorized, "REQUEST_EXPIRED")
	}
	view, err := g.clients.Get(ctx, material.ClientID)
	if err != nil {
		return invalid(http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE")
	}
	permission, err := g.clients.GetScope(ctx, material.ClientID, q.scope)
	if errors.Is(err, client.ErrNotFound) || (err == nil && !permission.Enabled) {
		return invalid(http.StatusForbidden, "CAPABILITY_DENIED")
	}
	if err != nil {
		return invalid(http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE")
	}
	metadata, err := ptzMetadata(q, view.OwnerDeptID, view.DataScope)
	if err != nil {
		return ptzMetadataFailure(q.requestID, err)
	}
	if err := checkMetadata(ctx, g.db, metadata); err != nil {
		return metadataFailure(q.requestID, err)
	}
	retry, err := g.limiter.Allow(view.ID, view.RateLimit, view.Burst, false)
	if err != nil {
		if !errors.Is(err, limit.ErrRateLimited) {
			return invalid(http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE")
		}
		response := invalid(http.StatusTooManyRequests, "RATE_LIMITED")
		response.retryAfter = int((retry + time.Second - 1) / time.Second)
		return response
	}
	if err := g.admission.Admit(ctx, AdmissionRequest{ClientID: view.ID, SecretVersion: material.SecretVersion, AuthEpoch: material.AuthEpoch, ScopeEpoch: permission.ScopeEpoch, Scope: q.scope, Nonce: headers.Nonce, Timestamp: headers.Timestamp, RequestID: q.requestID, ResourceType: metadata.resourceType(), ResourceID: metadata.resourceID(), Source: q.source}, func(tx *gorm.DB) error { return checkMetadata(ctx, tx, metadata) }); err != nil {
		switch {
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
	request := PTZRequest{Scope: q.scope, ClientID: view.ID, OwnerDeptID: view.OwnerDeptID, DataScope: view.DataScope, DeviceID: q.deviceID, ChannelID: q.channelID, PresetID: q.presetID, OperationID: q.operationID, IdempotencyKey: q.idempotencyKey, Body: append([]byte(nil), q.body...)}
	data, dispatchErr := g.ptz.Handle(ctx, request)
	response := gatewayError(q.requestID, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE")
	code := "SERVICE_UNAVAILABLE"
	if dispatchErr == nil {
		response = encodeGateway(q.requestID, http.StatusOK, "OK", data)
		code = "OK"
	} else {
		response = ptzMetadataFailure(q.requestID, dispatchErr)
		switch response.code {
		case "RESOURCE_NOT_FOUND":
			code = "RESOURCE_NOT_FOUND"
		case "CONFLICT":
			code = "CONFLICT"
		case "INVALID_REQUEST":
			code = "INVALID_REQUEST"
		}
	}
	if err := g.complete(hardContext, q.requestID, code, time.Since(start)); err != nil {
		return invalid(http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE")
	}
	return response
}

func (g *Gateway) ptzReady() (ready bool) {
	if g == nil || g.ptz == nil {
		return false
	}
	defer func() {
		if recover() != nil {
			ready = false
		}
	}()
	return g.ptz.Ready()
}

func isPTZRequestScope(scope string) bool { return isPTZScope(scope) }

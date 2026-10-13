package auth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"uvplatform.com/uvp-gb28181/app/openapi/adapters"
	catalogruntime "uvplatform.com/uvp-gb28181/app/openapi/catalog/runtime"
	"uvplatform.com/uvp-gb28181/app/openapi/contracts"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

func contractOperation(scope string) (contracts.Operation, bool) {
	for _, operation := range contracts.Operations() {
		if operation.Scope == scope {
			return operation, true
		}
	}
	return contracts.Operation{}, false
}

func isResourceRead(scope string) bool {
	operation, ok := contractOperation(scope)
	return ok && strings.HasPrefix(operation.AdapterKey, "resource.")
}

// The transport reads bytes exactly once. Signature verification uses these
// same bytes, and workers receive their own immutable request copy.
func captureCatalogBody(c *gin.Context, q *gatewayRequest) error {
	r := c.Request
	operation, ok := contractOperation(q.scope)
	if !ok || len(r.TransferEncoding) != 0 || r.ContentLength < 0 || r.ContentLength > maxBodyBytes {
		return ErrDenied
	}
	jsonBody := r.Method != http.MethodGet && operation.RequestSchema != "{}"
	if !jsonBody {
		if r.ContentLength != 0 {
			return ErrDenied
		}
	} else {
		if r.ContentLength == 0 || r.Body == nil {
			return ErrDenied
		}
		contentTypes := r.Header.Values("Content-Type")
		if len(contentTypes) != 1 || contentTypes[0] != "application/json" {
			return ErrDenied
		}
		body, err := io.ReadAll(http.MaxBytesReader(unwrapResponseWriter(c.Writer), r.Body, maxBodyBytes))
		if err != nil || int64(len(body)) != r.ContentLength || validateJSONBody(body) != nil {
			return ErrDenied
		}
		if err := validateCatalogObject(operation, body); err != nil {
			return err
		}
		q.body = append([]byte(nil), body...)
		q.contentType = contentTypes[0]
	}
	if operation.IdempotencyMode == contracts.IdempotencyRequired {
		keys := r.Header.Values("Idempotency-Key")
		if len(keys) != 1 || !validCatalogIdempotencyKey(keys[0]) {
			return ErrDenied
		}
	}
	return nil
}

func validCatalogIdempotencyKey(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if r < 0x21 || r > 0x7e {
			return false
		}
	}
	return true
}

// The gateway enforces the top-level contract and explicit confirmation.
// Domain adapters own numeric ranges, action enums and nested request models.
func validateCatalogObject(operation contracts.Operation, body []byte) error {
	var schema struct {
		Type                 string                     `json:"type"`
		Required             []string                   `json:"required"`
		Properties           map[string]json.RawMessage `json:"properties"`
		AdditionalProperties *bool                      `json:"additionalProperties"`
	}
	if json.Unmarshal([]byte(operation.RequestSchema), &schema) != nil || schema.Type != "object" {
		return ErrDenied
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil || fields == nil {
		return ErrDenied
	}
	for _, name := range schema.Required {
		value, exists := fields[name]
		if !exists || string(value) == "null" {
			return ErrDenied
		}
	}
	if schema.AdditionalProperties != nil && !*schema.AdditionalProperties {
		for name := range fields {
			if _, exists := schema.Properties[name]; !exists {
				return ErrDenied
			}
		}
	}
	if operation.RequiresConfirmation {
		var confirmed bool
		if json.Unmarshal(fields["confirmed"], &confirmed) != nil || !confirmed {
			return ErrDenied
		}
	}
	return nil
}

func (g *Gateway) dispatchCatalog(ctx context.Context, q gatewayRequest, clientID int64, metadata metadataInput) (any, error) {
	if g.catalog == nil || g.catalog.Active() == nil {
		if q.scope == PlayLiveScope && g.playReady() {
			return g.play.Start(ctx, PlayRequest{ClientID: clientID, OwnerDeptID: metadata.owner, DataScope: metadata.dataScope, DeviceID: metadata.deviceID, ChannelID: metadata.channelID, ClientIP: q.source})
		}
		return nil, adapters.ErrUnavailable
	}
	var value any
	if q.scope == PlayLiveScope {
		value = PlayRequest{ClientID: clientID, OwnerDeptID: metadata.owner, DataScope: metadata.dataScope, DeviceID: metadata.deviceID, ChannelID: metadata.channelID, ClientIP: q.source}
	} else {
		value = adapters.Request{ClientID: clientID, IdempotencyKey: q.idempotencyKey, ResourceScope: resource.DepartmentScope{OwnerDeptID: metadata.owner, DataScope: metadata.dataScope}, DeviceID: metadata.deviceID, ChannelID: metadata.channelID, Cursor: metadata.cursor, Query: metadata.query, List: adapters.ListOptions{Page: metadata.page, PageSize: metadata.size, Keyword: metadata.keyword, Status: metadata.status}}
	}
	return g.catalog.Dispatch(ctx, q.scope, catalogruntime.Invocation{
		Method: q.method, Path: q.path, Params: q.params, Query: metadata.query, Body: append([]byte(nil), q.body...), Value: value,
	})
}

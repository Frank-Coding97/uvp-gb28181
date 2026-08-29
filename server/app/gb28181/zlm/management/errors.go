package management

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// ManagementErrorCode is the stable machine-readable error code returned by
// the ZLM management facade.
type ManagementErrorCode string

const (
	CodeInvalidRequest        ManagementErrorCode = "invalid_request"
	CodeNodeNotFound          ManagementErrorCode = "node_not_found"
	CodeForbidden             ManagementErrorCode = "forbidden"
	CodeNodeMaintenance       ManagementErrorCode = "node_maintenance"
	CodeNodeOffline           ManagementErrorCode = "node_offline"
	CodeUpstreamTimeout       ManagementErrorCode = "upstream_timeout"
	CodeUnsupportedCapability ManagementErrorCode = "unsupported_capability"
	CodeOwnershipConflict     ManagementErrorCode = "ownership_conflict"
	CodeInternal              ManagementErrorCode = "internal_error"

	// ErrorCode* aliases follow the naming used by existing domain packages
	// while Code* remains the short wire-contract spelling.
	ErrorCodeInvalidRequest        = CodeInvalidRequest
	ErrorCodeNodeNotFound          = CodeNodeNotFound
	ErrorCodeForbidden             = CodeForbidden
	ErrorCodeNodeMaintenance       = CodeNodeMaintenance
	ErrorCodeNodeOffline           = CodeNodeOffline
	ErrorCodeUpstreamTimeout       = CodeUpstreamTimeout
	ErrorCodeUnsupportedCapability = CodeUnsupportedCapability
	ErrorCodeOwnershipConflict     = CodeOwnershipConflict
	ErrorCodeInternal              = CodeInternal
)

// Common causes let NodeExecutor and services classify wrapped failures
// without exposing their underlying ZLM response to an HTTP client.
var (
	ErrNodeOffline           = errors.New("node offline")
	ErrUpstreamTimeout       = errors.New("upstream timeout")
	ErrUnsupportedCapability = errors.New("unsupported capability")
	ErrOwnershipConflict     = errors.New("ownership conflict")
)

// ValidationError is a stable field-level request error. Field messages are
// intentionally generic and never echo the invalid value.
type ValidationError struct {
	Code    ManagementErrorCode `json:"code"`
	Message string              `json:"message"`
	Fields  map[string]string   `json:"fields"`
	cause   error
}

func newValidationError(fields map[string]string) *ValidationError {
	copyFields := make(map[string]string, len(fields))
	for field, message := range fields {
		copyFields[field] = sanitizeErrorText(message)
	}
	return &ValidationError{
		Code:    CodeInvalidRequest,
		Message: "request validation failed",
		Fields:  copyFields,
	}
}

// NewValidationError creates a field-level request error for management
// services. The input map is copied and its messages are redacted.
func NewValidationError(fields map[string]string) *ValidationError {
	return newValidationError(fields)
}

func (e *ValidationError) Error() string {
	if e == nil {
		return ""
	}
	if len(e.Fields) == 0 {
		return sanitizeErrorText(e.Message)
	}
	fields := make([]string, 0, len(e.Fields))
	for field, message := range e.Fields {
		fields = append(fields, field+": "+sanitizeErrorText(message))
	}
	sort.Strings(fields)
	return sanitizeErrorText(e.Message) + ": " + strings.Join(fields, "; ")
}

func (e *ValidationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// HTTPStatus returns the status prescribed by the management error contract.
func (e *ValidationError) HTTPStatus() int { return http.StatusBadRequest }

// Impact is the safe, bounded summary attached to an ownership conflict.
// It intentionally has no URL, Secret, token or raw ZLM response field.
type Impact struct {
	ResourceType  string         `json:"resourceType,omitempty"`
	ResourceKey   string         `json:"resourceKey,omitempty"`
	Owner         string         `json:"owner,omitempty"`
	Reason        string         `json:"reason,omitempty"`
	MediaIdentity *MediaIdentity `json:"media,omitempty"`
}

// ManagementError is the common HTTP error body. The cause is retained for
// server-side errors.Is checks but is never serialized or appended verbatim.
type ManagementError struct {
	Code      ManagementErrorCode `json:"code"`
	Message   string              `json:"message"`
	NodeID    string              `json:"nodeId"`
	Retryable bool                `json:"retryable"`
	Impacts   []Impact            `json:"impacts,omitempty"`
	cause     error
}

func (e *ManagementError) Error() string {
	if e == nil {
		return ""
	}
	message := sanitizeErrorText(e.Message)
	if message == "" {
		return string(e.Code)
	}
	return string(e.Code) + ": " + message
}

func (e *ManagementError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// HTTPStatus maps the stable code to its wire status. Unknown codes fail
// closed as internal errors instead of accidentally returning success.
func (e *ManagementError) HTTPStatus() int {
	if e == nil {
		return http.StatusInternalServerError
	}
	return HTTPStatusForCode(e.Code)
}

// StatusCode is an alias convenient for HTTP adapters.
func (e *ManagementError) StatusCode() int { return e.HTTPStatus() }

func HTTPStatusForCode(code ManagementErrorCode) int {
	switch code {
	case CodeInvalidRequest:
		return http.StatusBadRequest
	case CodeNodeNotFound:
		return http.StatusNotFound
	case CodeForbidden:
		return http.StatusForbidden
	case CodeNodeMaintenance:
		return http.StatusConflict
	case CodeNodeOffline:
		return http.StatusServiceUnavailable
	case CodeUpstreamTimeout:
		return http.StatusGatewayTimeout
	case CodeUnsupportedCapability:
		return http.StatusUnprocessableEntity
	case CodeOwnershipConflict:
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

// NewManagementError constructs a custom code while keeping status mapping
// centralised. Domain constructors below should be preferred for stable
// retryability semantics.
func NewManagementError(code ManagementErrorCode, nodeID, message string, retryable bool, causes ...error) *ManagementError {
	var cause error
	if len(causes) > 0 {
		cause = causes[0]
	}
	return &ManagementError{
		Code:      code,
		Message:   sanitizeErrorText(message),
		NodeID:    sanitizeErrorText(nodeID),
		Retryable: retryable,
		cause:     cause,
	}
}

func NewNodeOfflineError(nodeID string, causes ...error) *ManagementError {
	return NewManagementError(CodeNodeOffline, nodeID, "media node is offline", true, causes...)
}

func NewNodeNotFoundError(nodeID string, causes ...error) *ManagementError {
	return NewManagementError(CodeNodeNotFound, nodeID, "media node was not found", false, causes...)
}

func NewForbiddenError(nodeID string, causes ...error) *ManagementError {
	return NewManagementError(CodeForbidden, nodeID, "media node access is forbidden", false, causes...)
}

func NewNodeMaintenanceError(nodeID string, causes ...error) *ManagementError {
	return NewManagementError(CodeNodeMaintenance, nodeID, "media node is in maintenance", false, causes...)
}

func NewUpstreamTimeoutError(nodeID string, causes ...error) *ManagementError {
	return NewManagementError(CodeUpstreamTimeout, nodeID, "upstream request timed out", true, causes...)
}

func NewUnsupportedCapabilityError(nodeID, capability string, causes ...error) *ManagementError {
	message := "requested ZLM capability is unsupported"
	if capability != "" {
		message += ": " + capability
	}
	return NewManagementError(CodeUnsupportedCapability, nodeID, message, false, causes...)
}

func NewOwnershipConflictError(nodeID, reason string, causes ...error) *ManagementError {
	if strings.TrimSpace(reason) == "" {
		reason = "resource is owned or changed"
	}
	return NewManagementError(CodeOwnershipConflict, nodeID, reason, false, causes...)
}

func NewInternalError(nodeID, message string, causes ...error) *ManagementError {
	if strings.TrimSpace(message) == "" {
		message = "internal management error"
	}
	return NewManagementError(CodeInternal, nodeID, message, false, causes...)
}

// WithImpacts returns a copy with safe conflict summaries attached. It keeps
// the original error immutable when a batch operation adds its preflight set.
func (e *ManagementError) WithImpacts(impacts []Impact) *ManagementError {
	if e == nil {
		return nil
	}
	clone := *e
	clone.Impacts = append([]Impact(nil), impacts...)
	return &clone
}

// NormalizeError turns common transport/domain causes into the stable error
// body. Existing ManagementError values are preserved.
func NormalizeError(err error, nodeID string) error {
	if err == nil {
		return nil
	}
	var managementErr *ManagementError
	if errors.As(err, &managementErr) && managementErr != nil {
		return managementErr
	}
	switch {
	case errors.Is(err, ErrNodeOffline):
		return NewNodeOfflineError(nodeID, err)
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, ErrUpstreamTimeout):
		return NewUpstreamTimeoutError(nodeID, err)
	case errors.Is(err, ErrUnsupportedCapability):
		return NewUnsupportedCapabilityError(nodeID, "", err)
	case errors.Is(err, ErrOwnershipConflict):
		return NewOwnershipConflictError(nodeID, "resource ownership conflict", err)
	default:
		return NewInternalError(nodeID, "internal management error", err)
	}
}

// RedactSensitiveText is shared by later management layers when a known
// Secret is available alongside an upstream error. It first removes explicit
// sensitive values, then applies key, URL credential and URL query redaction.
func RedactSensitiveText(text string, sensitiveValues ...string) string {
	for _, value := range sensitiveValues {
		if value == "" {
			continue
		}
		text = strings.ReplaceAll(text, value, "[redacted]")
		text = strings.ReplaceAll(text, url.QueryEscape(value), "[redacted]")
	}
	return sanitizeErrorText(text)
}

func sanitizeErrorText(text string) string {
	text = sensitiveURLCredentials.ReplaceAllString(text, `$1[redacted]@`)
	text = sensitiveURLQuery.ReplaceAllString(text, `$1?[redacted]`)
	text = sensitiveURL.ReplaceAllString(text, `[redacted-url]`)
	text = sensitivePayload.ReplaceAllString(text, `[redacted-upstream-response]`)
	text = sensitiveJSONObject.ReplaceAllString(text, `[redacted-upstream-response]`)
	text = sensitiveKeyValue.ReplaceAllString(text, `[redacted]`)
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, text)
	text = strings.Join(strings.Fields(text), " ")
	if len([]rune(text)) > 512 {
		text = string([]rune(text)[:512]) + "..."
	}
	return text
}

var (
	// URL userinfo can contain credentials even when no query is present.
	sensitiveURLCredentials = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)[^/\s@]+@`)
	// A management error must not carry a complete tokenized/authenticated URL.
	sensitiveURLQuery = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://[^\s?#]+)\?[^\s#]*`)
	// Remove the complete URL after the credential/query passes. Keeping an
	// endpoint in a management error is not worth the risk of a token embedded
	// in its path or fragment.
	sensitiveURL = regexp.MustCompile(`(?i)(?:[a-z][a-z0-9+.-]*://)[^\s]+`)
	// Do not echo a raw JSON/XML response body from an upstream management API.
	sensitivePayload = regexp.MustCompile(`(?i)(?:zlm\s+)?(?:response|body|payload)\s*[:=]?\s*\{[^{}]*\}`)
	// Also catch a response object when the caller omitted a descriptive
	// prefix. Error bodies are diagnostics, not a transport for upstream JSON.
	sensitiveJSONObject = regexp.MustCompile(`\{[^{}]*\}`)
	// Keys are removed along with their values so error text cannot reveal even
	// the presence of an apiSecret field in an HTTP response.
	sensitiveKeyValue = regexp.MustCompile(`(?i)(?:"?(?:api[_-]?secret|secret|password|passwd|access[_-]?token|token|authorization|auth|cap|key)"?\s*[:=]\s*)(?:"[^"]*"|'[^']*'|[^&\s,;}]+)`)
)

// MarshalJSON applies the same redaction boundary to direct JSON encoding of
// an error, including values supplied through a struct literal.
func (e *ManagementError) MarshalJSON() ([]byte, error) {
	if e == nil {
		return []byte("null"), nil
	}
	type wireImpact struct {
		ResourceType  string         `json:"resourceType,omitempty"`
		ResourceKey   string         `json:"resourceKey,omitempty"`
		Owner         string         `json:"owner,omitempty"`
		Reason        string         `json:"reason,omitempty"`
		MediaIdentity *MediaIdentity `json:"media,omitempty"`
	}
	impacts := make([]wireImpact, 0, len(e.Impacts))
	for _, impact := range e.Impacts {
		impacts = append(impacts, wireImpact{
			ResourceType:  sanitizeErrorText(impact.ResourceType),
			ResourceKey:   sanitizeErrorText(impact.ResourceKey),
			Owner:         sanitizeErrorText(impact.Owner),
			Reason:        sanitizeErrorText(impact.Reason),
			MediaIdentity: impact.MediaIdentity,
		})
	}
	return json.Marshal(struct {
		Code      ManagementErrorCode `json:"code"`
		Message   string              `json:"message"`
		NodeID    string              `json:"nodeId"`
		Retryable bool                `json:"retryable"`
		Impacts   []wireImpact        `json:"impacts,omitempty"`
	}{
		Code:      e.Code,
		Message:   sanitizeErrorText(e.Message),
		NodeID:    sanitizeErrorText(e.NodeID),
		Retryable: e.Retryable,
		Impacts:   impacts,
	})
}

// MarshalJSON prevents a direct encoding of ValidationError from bypassing
// its sanitized field messages.
func (e *ValidationError) MarshalJSON() ([]byte, error) {
	if e == nil {
		return []byte("null"), nil
	}
	fields := make(map[string]string, len(e.Fields))
	for field, message := range e.Fields {
		fields[sanitizeErrorText(field)] = sanitizeErrorText(message)
	}
	return json.Marshal(struct {
		Code    ManagementErrorCode `json:"code"`
		Message string              `json:"message"`
		Fields  map[string]string   `json:"fields"`
	}{
		Code:    e.Code,
		Message: sanitizeErrorText(e.Message),
		Fields:  fields,
	})
}

// AsManagementError is a small convenience for controllers that want to
// handle wrapped errors without copying the error contract fields.
func AsManagementError(err error) (*ManagementError, bool) {
	if err == nil {
		return nil, false
	}
	var managementErr *ManagementError
	if !errors.As(err, &managementErr) || managementErr == nil {
		return nil, false
	}
	return managementErr, true
}

// ErrorSummary returns a safe string for operation logs and diagnostics.
func ErrorSummary(err error) string {
	if err == nil {
		return ""
	}
	return sanitizeErrorText(fmt.Sprint(err))
}

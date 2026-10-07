// Package adapters contains code-owned OpenAPI operation adapters. Published
// capability data may select an adapter key, but it can never provide code or
// a transport target for execution.
package adapters

import (
	"context"
	"errors"
	"net/http"

	catalogruntime "uvplatform.com/uvp-gb28181/app/openapi/catalog/runtime"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

var (
	// ErrNotFound is the only resource absence classification adapters expose.
	ErrNotFound = errors.New("openapi adapter resource not found")
	// ErrInvalidRequest is reserved for malformed values reaching an adapter.
	// The HTTP boundary normally rejects these before dispatch.
	ErrInvalidRequest = errors.New("openapi adapter request is invalid")
	// ErrUnavailable hides dependency and configuration details from callers.
	ErrUnavailable = errors.New("openapi adapter unavailable")
)

// Request is built by the authenticated OpenAPI gateway after it has selected
// a published operation. ResourceScope is trusted client state, never an HTTP
// field. DeviceID and ChannelID are stable external GB identifiers.
type Request struct {
	ResourceScope resource.DepartmentScope
	DeviceID      string
	ChannelID     string
	List          ListOptions
}

// ListOptions is deliberately shared by device and channel reads. The
// resource application service remains the source of pagination/filter rules.
type ListOptions struct {
	Page     int
	PageSize int
	Keyword  string
	Status   string
}

// Adapter is a fixed, typed in-process boundary. It must not accept a URL,
// SQL fragment, function name, or controller reference from capability data.
type Adapter interface {
	catalogruntime.Adapter
	Key() string
	Dispatch(context.Context, Request) (any, error)
}

// ResourceAdapterContractVersion is stored with each catalog operation. A
// future wire/DTO change must register a new version instead of changing the
// meaning of an already published operation.
const ResourceAdapterContractVersion = "v1"

// NewDeviceChannelAdapterRegistrations bridges this typed domain package to
// the shared catalog runtime registry. The runtime registry is the only
// process-level registry; database catalog rows can select this key/version,
// but cannot install executable behavior.
func NewDeviceChannelAdapterRegistrations(reader ResourceReader) []catalogruntime.AdapterRegistration {
	adapters := NewDeviceChannelAdapters(reader)
	registrations := make([]catalogruntime.AdapterRegistration, 0, len(adapters))
	for _, adapter := range adapters {
		registrations = append(registrations, catalogruntime.AdapterRegistration{
			Key:             adapter.Key(),
			ContractVersion: ResourceAdapterContractVersion,
			Adapter:         adapter,
		})
	}
	return registrations
}

// ErrorCode and HTTPStatus give the unified dispatcher one canonical mapping
// for every adapter family. The gateway owns response serialization/auditing.
func ErrorCode(err error) string {
	switch {
	case err == nil:
		return "OK"
	case errors.Is(err, ErrNotFound):
		return "RESOURCE_NOT_FOUND"
	case errors.Is(err, ErrInvalidRequest):
		return "INVALID_REQUEST"
	default:
		return "SERVICE_UNAVAILABLE"
	}
}

func HTTPStatus(err error) int {
	switch ErrorCode(err) {
	case "OK":
		return http.StatusOK
	case "RESOURCE_NOT_FOUND":
		return http.StatusNotFound
	case "INVALID_REQUEST":
		return http.StatusBadRequest
	default:
		return http.StatusServiceUnavailable
	}
}

// Package devicecatalog exposes the platform's manual Catalog refresh: one
// operation asks the device to re-report its channel tree, the other reports
// how far that asynchronous exchange has progressed.
package devicecatalog

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gorm.io/gorm"

	"uvplatform.com/uvp-gb28181/app/gb28181/catalogprogress"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/openapi/adapters"
	catalogruntime "uvplatform.com/uvp-gb28181/app/openapi/catalog/runtime"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

const (
	RefreshScope      = "device:catalog:refresh"
	RefreshReadScope  = "device:catalog:refresh:read"
	RefreshAdapterKey = "device.catalog.refresh.v1"
	// RefreshReadAdapterKey is the progress read, published separately so a
	// caller can be granted polling without the right to trigger SIP traffic.
	RefreshReadAdapterKey = "device.catalog.refresh-progress.v1"
	AdapterVersion        = "v1"
)

// Trigger is the SIP capability that sends the Catalog query. It is injected
// after the UAC is ready, exactly like the platform controller's trigger.
type Trigger interface {
	Trigger(ctx context.Context, deviceID, dest, transport string)
}

var processTrigger atomic.Pointer[triggerBox]

type triggerBox struct{ trigger Trigger }

// SetTrigger installs the process-wide Catalog trigger. It mirrors how the
// platform controller receives its trigger: the OpenAPI dispatcher cannot
// construct SIP itself, and the process serves one SIP stack.
func SetTrigger(trigger Trigger) {
	if trigger == nil {
		processTrigger.Store(nil)
		return
	}
	processTrigger.Store(&triggerBox{trigger: trigger})
}

// SetTracker replaces the process tracker. Tests use it to isolate progress
// state; production relies on catalogprogress.Default, which the SIP layer also
// writes into.
func SetTracker(tracker *catalogprogress.Tracker) {
	trackerMu.Lock()
	defer trackerMu.Unlock()
	currentTracker = tracker
}

var (
	trackerMu      sync.RWMutex
	currentTracker *catalogprogress.Tracker
)

func tracker() *catalogprogress.Tracker {
	trackerMu.RLock()
	defer trackerMu.RUnlock()
	if currentTracker != nil {
		return currentTracker
	}
	return catalogprogress.Default
}

func currentTrigger() Trigger {
	box := processTrigger.Load()
	if box == nil {
		return nil
	}
	return box.trigger
}

// RefreshResult reports the accepted request. It deliberately omits the device
// source address: an external caller has no use for the platform's internal
// signalling endpoint.
type RefreshResult struct {
	DeviceID     string `json:"deviceId"`
	OperationID  string `json:"operationId"`
	Deduplicated bool   `json:"deduplicated"`
}

// RefreshProgress is the externally visible state of one Catalog exchange.
type RefreshProgress struct {
	DeviceID      string     `json:"deviceId"`
	OperationID   string     `json:"operationId"`
	Status        string     `json:"status"`
	ReceivedCount int        `json:"receivedCount"`
	TotalCount    *int       `json:"totalCount"`
	ErrorMessage  string     `json:"errorMessage"`
	StartedAt     time.Time  `json:"startedAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
	CompletedAt   *time.Time `json:"completedAt"`
}

type Dispatcher struct {
	db      *gorm.DB
	trigger Trigger
	refresh catalogruntime.Adapter
	read    catalogruntime.Adapter
}

// NewDispatcher builds the code-owned adapters. A nil DB leaves both
// operations unavailable instead of exposing a partial surface.
func NewDispatcher(db *gorm.DB, triggers ...Trigger) *Dispatcher {
	dispatcher := &Dispatcher{db: db}
	if len(triggers) == 1 && triggers[0] != nil {
		dispatcher.trigger = triggers[0]
	}
	dispatcher.refresh = operationAdapter{dispatcher: dispatcher, scope: RefreshScope, read: false}
	dispatcher.read = operationAdapter{dispatcher: dispatcher, scope: RefreshReadScope, read: true}
	return dispatcher
}

func (d *Dispatcher) Adapter(key string) catalogruntime.Adapter {
	if d == nil {
		return nil
	}
	switch key {
	case RefreshAdapterKey:
		return d.refresh
	case RefreshReadAdapterKey:
		return d.read
	default:
		return nil
	}
}

func NewAdapterRegistrations(dispatcher *Dispatcher) []catalogruntime.AdapterRegistration {
	if dispatcher == nil {
		return nil
	}
	return []catalogruntime.AdapterRegistration{
		{Key: RefreshAdapterKey, ContractVersion: AdapterVersion, Adapter: dispatcher.refresh},
		{Key: RefreshReadAdapterKey, ContractVersion: AdapterVersion, Adapter: dispatcher.read},
	}
}

type operationAdapter struct {
	dispatcher *Dispatcher
	scope      string
	read       bool
}

func (a operationAdapter) Key() string {
	if a.dispatcher == nil {
		return ""
	}
	if a.read {
		return RefreshReadAdapterKey
	}
	return RefreshAdapterKey
}

func (a operationAdapter) Execute(ctx context.Context, invocation catalogruntime.Invocation) (any, error) {
	if a.dispatcher == nil {
		return nil, adapters.ErrUnavailable
	}
	if invocation.Scope != "" && invocation.Scope != a.scope {
		return nil, adapters.ErrInvalidRequest
	}
	expected := http.MethodPost
	if a.read {
		expected = http.MethodGet
	}
	if invocation.Method != "" && !strings.EqualFold(invocation.Method, expected) {
		return nil, adapters.ErrInvalidRequest
	}
	request, ok := invocation.Value.(adapters.Request)
	if !ok {
		return nil, adapters.ErrInvalidRequest
	}
	if param := invocation.Params["deviceId"]; param != "" && param != request.DeviceID {
		return nil, adapters.ErrInvalidRequest
	}
	if a.read {
		if len(invocation.Body) != 0 || len(invocation.Query) != 0 {
			return nil, adapters.ErrInvalidRequest
		}
		operationID := strings.TrimSpace(invocation.Params["operationId"])
		if operationID == "" {
			return nil, adapters.ErrInvalidRequest
		}
		return a.dispatcher.progress(ctx, request, operationID)
	}
	if len(invocation.Body) != 0 {
		return nil, adapters.ErrInvalidRequest
	}
	return a.dispatcher.refreshCatalog(ctx, request)
}

func (d *Dispatcher) refreshCatalog(ctx context.Context, request adapters.Request) (RefreshResult, error) {
	device, err := d.onlineDevice(ctx, request)
	if err != nil {
		return RefreshResult{}, err
	}
	trigger := d.trigger
	if trigger == nil {
		trigger = currentTrigger()
	}
	if trigger == nil {
		return RefreshResult{}, adapters.ErrUnavailable
	}
	destination := fmt.Sprintf("%s:%d", device.IP, device.Port)
	started := tracker().Start(device.DeviceID)
	if !started.Deduplicated {
		// The SIP exchange outlives this HTTP request, so the operation id must
		// survive cancellation of the caller's connection.
		triggerCtx := catalogprogress.WithOperationID(context.WithoutCancel(ctx), started.Snapshot.OperationID)
		trigger.Trigger(triggerCtx, device.DeviceID, destination, device.Transport)
	}
	return RefreshResult{
		DeviceID: device.DeviceID, OperationID: started.Snapshot.OperationID, Deduplicated: started.Deduplicated,
	}, nil
}

func (d *Dispatcher) progress(ctx context.Context, request adapters.Request, operationID string) (RefreshProgress, error) {
	device, err := d.onlineDevice(ctx, request)
	if err != nil {
		return RefreshProgress{}, err
	}
	snapshot, found := tracker().Get(operationID)
	// The tracker is process-wide, so the device binding is the only thing that
	// keeps one client from reading another device's exchange.
	if !found || snapshot.DeviceID != device.DeviceID {
		return RefreshProgress{}, adapters.ErrNotFound
	}
	return RefreshProgress{
		DeviceID: snapshot.DeviceID, OperationID: snapshot.OperationID, Status: string(snapshot.Status),
		ReceivedCount: snapshot.ReceivedCount, TotalCount: snapshot.TotalCount, ErrorMessage: snapshot.ErrorMessage,
		StartedAt: snapshot.StartedAt, UpdatedAt: snapshot.UpdatedAt, CompletedAt: snapshot.CompletedAt,
	}, nil
}

// onlineDevice resolves the device inside the client's department scope and
// refuses a refresh the SIP stack could not deliver. The entity is re-read here
// because the resource view carries no signalling endpoint.
func (d *Dispatcher) onlineDevice(ctx context.Context, request adapters.Request) (gbmodels.GbDevice, error) {
	if d == nil || d.db == nil {
		return gbmodels.GbDevice{}, adapters.ErrUnavailable
	}
	if _, err := resource.New(d.db).GetDeviceInScope(ctx, request.ResourceScope, request.DeviceID); err != nil {
		if errors.Is(err, resource.ErrResourceNotFound) {
			return gbmodels.GbDevice{}, adapters.ErrNotFound
		}
		if errors.Is(err, resource.ErrInvalidDepartmentScope) {
			return gbmodels.GbDevice{}, adapters.ErrInvalidRequest
		}
		return gbmodels.GbDevice{}, adapters.ErrUnavailable
	}
	departmentIDs, err := resource.ResolveDepartmentIDs(ctx, d.db, request.ResourceScope)
	if err != nil {
		return gbmodels.GbDevice{}, adapters.ErrUnavailable
	}
	var devices []gbmodels.GbDevice
	if err := d.db.WithContext(ctx).
		Where("device_id = ? AND owner_dept_id IN ? AND deleted_at IS NULL", request.DeviceID, departmentIDs).
		Limit(2).Find(&devices).Error; err != nil {
		return gbmodels.GbDevice{}, adapters.ErrUnavailable
	}
	if len(devices) != 1 {
		return gbmodels.GbDevice{}, adapters.ErrNotFound
	}
	device := devices[0]
	if device.Status != gbmodels.DeviceStatusOnline {
		return gbmodels.GbDevice{}, adapters.ErrDeviceOffline
	}
	if strings.TrimSpace(device.IP) == "" || device.Port <= 0 {
		return gbmodels.GbDevice{}, adapters.ErrUnavailable
	}
	return device, nil
}

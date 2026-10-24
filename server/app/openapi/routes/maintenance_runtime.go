package routes

import (
	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/openapi/adapters"
	catalogruntime "uvplatform.com/uvp-gb28181/app/openapi/catalog/runtime"
)

// MaintenanceRuntime keeps stable adapter forwarders registered with the
// catalog while a SIP generation supplies the live PTZ executor. Clearing or
// replacing a generation never changes the published operation identities.
type MaintenanceRuntime struct {
	db       *gorm.DB
	bindings *catalogruntime.AdapterBindingSet
}

func NewMaintenanceRuntime(db *gorm.DB) (*MaintenanceRuntime, error) {
	if db == nil {
		return nil, adapters.ErrUnavailable
	}
	return &MaintenanceRuntime{db: db, bindings: catalogruntime.NewAdapterBindingSet()}, nil
}

func (runtime *MaintenanceRuntime) Forwarders() map[string]catalogruntime.Adapter {
	if runtime == nil || runtime.bindings == nil {
		return nil
	}
	keys := make([]string, 0, 8)
	for _, registration := range adapters.NewMaintenanceAdapterRegistrations(nil, nil, nil) {
		keys = append(keys, registration.Key)
	}
	return runtime.bindings.Forwarders(keys)
}

func (runtime *MaintenanceRuntime) Replace(service adapters.MaintenanceExecutor) error {
	if runtime == nil || runtime.db == nil || runtime.bindings == nil || service == nil {
		return adapters.ErrUnavailable
	}
	registrations := adapters.NewMaintenanceAdapterRegistrations(
		adapters.NewGormMaintenanceResolver(runtime.db), service,
		adapters.NewGormMaintenanceOperationReader(runtime.db),
	)
	return runtime.bindings.Bind(registrations)
}

func (runtime *MaintenanceRuntime) Clear() {
	if runtime == nil || runtime.bindings == nil {
		return
	}
	runtime.bindings.Clear()
}

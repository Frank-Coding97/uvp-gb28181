package gb28181

import (
	"encoding/base64"
	"os"

	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/gb28181/subscribe"
	catalogruntime "uvplatform.com/uvp-gb28181/app/openapi/catalog/runtime"
	openapideviceevents "uvplatform.com/uvp-gb28181/app/openapi/deviceevents"
)

const openAPIMasterKeyEnv = "UVP_OPENAPI_MASTER_KEY"

var _ openapideviceevents.SubscriptionLifecycle = (*subscribe.Service)(nil)

func newOpenAPIDeviceEventAdapterRegistrations(db *gorm.DB, upstream openapideviceevents.SubscriptionLifecycle, masterKey []byte) ([]catalogruntime.AdapterRegistration, error) {
	if db == nil || upstream == nil {
		return nil, openapideviceevents.ErrUnavailable
	}
	if service, ok := upstream.(*subscribe.Service); ok && service == nil {
		return nil, openapideviceevents.ErrUnavailable
	}
	cursorKey, err := openapideviceevents.DeriveCursorKey(masterKey)
	if err != nil {
		return nil, err
	}
	service := openapideviceevents.NewService(db, upstream, openapideviceevents.Config{CursorKey: cursorKey})
	clear(cursorKey)
	return openapideviceevents.NewAdapterRegistrations(openapideviceevents.NewDispatcher(service)), nil
}

func openAPIDeviceEventAdapterRegistrations(db *gorm.DB, upstream openapideviceevents.SubscriptionLifecycle) ([]catalogruntime.AdapterRegistration, error) {
	encoded := os.Getenv(openAPIMasterKeyEnv)
	masterKey, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil || len(masterKey) != 32 {
		clear(masterKey)
		return nil, openapideviceevents.ErrUnavailable
	}
	defer clear(masterKey)
	return newOpenAPIDeviceEventAdapterRegistrations(db, upstream, masterKey)
}

package uac

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/internal/authoritytest"
)

func newAuthorizedIntentTestStore(t *testing.T, db *gorm.DB) *playauth.DeviceOperationIntentStore {
	t.Helper()
	store, err := playauth.NewAuthorizedDeviceOperationIntentStore(db, authoritytest.Authority(t, db))
	require.NoError(t, err)
	return store
}

func newAuthorizedBarrierTest(t *testing.T, db *gorm.DB) *playauth.DeviceOperationBarrier {
	t.Helper()
	barrier, err := playauth.NewAuthorizedDeviceOperationBarrier(playauth.NewDeviceSecurityStore(db), authoritytest.Authority(t, db))
	require.NoError(t, err)
	return barrier
}

package security

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func createAuthenticatedEndpointTestTable(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.Exec(`
		CREATE TABLE IF NOT EXISTS gb_device (
			device_id TEXT,
			transport TEXT,
			ip TEXT,
			register_time DATETIME,
			register_expire_at DATETIME,
			deleted_at DATETIME
		)
	`).Error)
}

func insertAuthenticatedEndpointTestDevice(t *testing.T, db *gorm.DB, deviceID, transport, ip string, registerTime, registerExpireAt, deletedAt *time.Time) {
	t.Helper()
	require.NoError(t, db.Exec(`
		INSERT INTO gb_device (device_id, transport, ip, register_time, register_expire_at, deleted_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, deviceID, transport, ip, registerTime, registerExpireAt, deletedAt).Error)
}

func TestGormStoreLoadsAuthenticatedEndpointsFromRegisteredDevices(t *testing.T) {
	db := newSecurityStoreTestDB(t)
	createAuthenticatedEndpointTestTable(t, db)
	store := NewGormStore(db)
	now := time.Now().UTC().Truncate(time.Second)
	registeredAt := now.Add(-2 * time.Hour)
	validUntil := now.Add(2 * time.Hour)
	expiredAt := now.Add(-time.Minute)
	deletedAt := now.Add(-time.Minute)
	zeroTime := time.Time{}
	futureAt := now.Add(time.Hour)

	insertAuthenticatedEndpointTestDevice(t, db, "34020000001320000001", " udp ", " 192.0.2.1 ", &registeredAt, &validUntil, nil)
	insertAuthenticatedEndpointTestDevice(t, db, "34020000001320000002", "tcp", "2001:db8::2", &registeredAt, &expiredAt, nil)
	insertAuthenticatedEndpointTestDevice(t, db, "34020000001320000003", "tls", "2001:0db8:0:0:0:0:0:3", &registeredAt, nil, nil)

	// A manually preallocated device has no successful registration timestamp.
	insertAuthenticatedEndpointTestDevice(t, db, "34020000001320000004", "UDP", "192.0.2.4", nil, &validUntil, nil)
	insertAuthenticatedEndpointTestDevice(t, db, "34020000001320000005", "UDP", "192.0.2.5", &registeredAt, &validUntil, &deletedAt)
	insertAuthenticatedEndpointTestDevice(t, db, "34020000001320000006", "UDP", "192.0.2.6", &zeroTime, &validUntil, nil)
	insertAuthenticatedEndpointTestDevice(t, db, "34020000001320000007", "UDP", "192.0.2.7", &futureAt, &validUntil, nil)
	insertAuthenticatedEndpointTestDevice(t, db, "34020000001320000008", "UDP", "0.0.0.0", &registeredAt, &validUntil, nil)
	insertAuthenticatedEndpointTestDevice(t, db, "3402000000132000000A", "UDP", "192.0.2.10", &registeredAt, &validUntil, nil)
	insertAuthenticatedEndpointTestDevice(t, db, "34020000001320000009", "SCTP", "192.0.2.9", &registeredAt, &validUntil, nil)

	endpoints, err := store.LoadAuthenticatedEndpoints(context.Background())
	require.NoError(t, err)
	require.Len(t, endpoints, 3)

	byDeviceID := make(map[string]Endpoint, len(endpoints))
	for _, endpoint := range endpoints {
		byDeviceID[endpoint.DeviceID] = endpoint
	}

	valid := byDeviceID["34020000001320000001"]
	require.Equal(t, "UDP", valid.Transport)
	require.Equal(t, "192.0.2.1", valid.Address)
	require.Equal(t, validUntil, valid.ExpiresAt)
	require.Equal(t, registeredAt, valid.UpdatedAt)

	expiredOffline := byDeviceID["34020000001320000002"]
	require.Equal(t, "TCP", expiredOffline.Transport)
	require.Equal(t, "2001:db8::2", expiredOffline.Address)
	require.Equal(t, expiredAt, expiredOffline.ExpiresAt)
	require.Equal(t, registeredAt, expiredOffline.UpdatedAt)
	require.True(t, expiredOffline.ExpiresAt.Before(time.Now()))

	withoutExpiry := byDeviceID["34020000001320000003"]
	require.Equal(t, "TLS", withoutExpiry.Transport)
	require.Equal(t, "2001:db8::3", withoutExpiry.Address)
	require.False(t, withoutExpiry.ExpiresAt.IsZero())
	require.True(t, withoutExpiry.ExpiresAt.Before(time.Now()))
	require.Equal(t, registeredAt, withoutExpiry.UpdatedAt)
}

func TestGormStoreLoadAuthenticatedEndpointsReturnsQueryError(t *testing.T) {
	db := newSecurityStoreTestDB(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
	store := NewGormStore(db)

	endpoints, err := store.LoadAuthenticatedEndpoints(context.Background())
	require.Error(t, err)
	require.Nil(t, endpoints)
}

func TestGormStoreZeroExpiryCannotRestoreUnlimitedInviteTrust(t *testing.T) {
	db := newSecurityStoreTestDB(t)
	store := NewGormStore(db)
	registered := time.Now().Add(-time.Hour)
	zero := time.Time{}
	insertAuthenticatedEndpointTestDevice(t, db, "34020000001320000001", "UDP", "198.51.100.10", &registered, &zero, nil)
	endpoints, err := store.LoadAuthenticatedEndpoints(context.Background())
	require.NoError(t, err)
	require.Len(t, endpoints, 1)
	require.False(t, endpoints[0].ExpiresAt.IsZero(), "zero means unlimited trust in Scorer")
	require.True(t, endpoints[0].ExpiresAt.Before(time.Now()))
}

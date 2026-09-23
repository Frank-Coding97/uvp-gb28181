package integration

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Called only after TestOpenAPIDatabaseCoreMigration has checked that the
// explicitly selected isolated database was empty before fixture creation.
func checkNativeMediaSchema(t *testing.T, db *gorm.DB) {
	t.Helper()
	now := time.Now().UTC()
	var device struct {
		AccessEpoch         int64
		LegacyRevokedBefore *time.Time
	}
	require.NoError(t, db.Table("gb_device").First(&device).Error)
	require.Equal(t, int64(1), device.AccessEpoch)
	require.Nil(t, device.LegacyRevokedBefore)
	cutoff := time.Date(2026, 9, 6, 1, 0, 1, 0, time.UTC)
	require.NoError(t, db.Table("gb_device").Where("id = ?", 1).Update("legacy_revoked_before", cutoff).Error)
	require.NoError(t, db.Table("gb_device").First(&device).Error)
	require.NotNil(t, device.LegacyRevokedBefore)
	require.Equal(t, cutoff.Unix(), device.LegacyRevokedBefore.Unix(), "UTC second must survive native DB round-trip")
	require.NoError(t, db.Table("gb_device").Where("id = ?", 1).Update("legacy_revoked_before", nil).Error)
	var sequence int
	grant := func() map[string]any {
		sequence++
		return map[string]any{
			"grant_id":  fmt.Sprintf("00000000-0000-4000-8000-%012d", sequence),
			"client_id": int64(1), "scope": "play:live:apply",
			"client_epoch": int64(1), "scope_epoch": int64(1), "device_epoch": int64(1),
			"state": "pending", "issued_at": now, "expires_at": now.Add(120 * time.Second),
			"created_at": now, "updated_at": now,
		}
	}
	insertGrant := func(row map[string]any) {
		t.Helper()
		require.NoError(t, db.Table("gb_openapi_play_grant").Create(row).Error)
	}
	pending := grant()
	insertGrant(pending)
	for _, state := range []string{"issued", "bound", "bogus"} {
		row := grant()
		row["state"] = state
		require.Error(t, db.Table("gb_openapi_play_grant").Create(row).Error, "invalid grant accepted: "+state)
	}
	for _, field := range []string{"client_epoch", "scope_epoch", "device_epoch"} {
		row := grant()
		row[field] = 0
		require.Error(t, db.Table("gb_openapi_play_grant").Create(row).Error, field)
	}
	bound := grant()
	for key, value := range map[string]any{
		"state": "issued", "device_id": "34020000001320000001", "channel_id": "34020000001320000002",
		"node_uuid": "fixture-node", "boot_nonce": "000102030405060708090a0b0c0d0e0f",
		"schema": "http", "vhost": "__defaultVhost__", "app": "rtp", "stream": "fixture-stream",
		"media_generation": int64(1), "protocol": "https-flv",
	} {
		bound[key] = value
	}
	insertGrant(bound)
	for _, field := range []string{"device_id", "channel_id", "node_uuid", "schema", "vhost", "app", "stream", "protocol"} {
		row := cloneNativeRow(bound)
		row["grant_id"] = grant()["grant_id"]
		row[field] = ""
		require.Error(t, db.Table("gb_openapi_play_grant").Create(row).Error, "empty grant binding: "+field)
	}
	viewer := map[string]any{
		"grant_id": bound["grant_id"], "node_uuid": "fixture-node", "boot_nonce": "000102030405060708090a0b0c0d0e0f",
		"identifier": "1--1", "schema": "http", "vhost": "__defaultVhost__", "app": "rtp", "stream": "fixture-stream",
		"media_generation": int64(1), "state": "pending", "attempts": 0, "created_at": now, "updated_at": now,
	}
	require.NoError(t, db.Table("gb_openapi_viewer").Create(cloneNativeRow(viewer)).Error)
	otherGrant := grant()
	insertGrant(otherGrant)
	duplicate := cloneNativeRow(viewer)
	duplicate["grant_id"] = otherGrant["grant_id"]
	require.Error(t, db.Table("gb_openapi_viewer").Create(cloneNativeRow(duplicate)).Error, "same process/session identity accepted twice")
	duplicate["boot_nonce"] = "100102030405060708090a0b0c0d0e0f"
	require.NoError(t, db.Table("gb_openapi_viewer").Create(cloneNativeRow(duplicate)).Error, "new process must have independent identity")
	duplicate["identifier"] = "2--1"
	require.Error(t, db.Table("gb_openapi_viewer").Create(cloneNativeRow(duplicate)).Error, "one grant accepted multiple viewers")
	for _, field := range []string{"node_uuid", "boot_nonce", "identifier", "schema", "vhost", "app", "stream", "media_generation", "attempts", "state"} {
		row := cloneNativeRow(viewer)
		g := grant()
		insertGrant(g)
		row["grant_id"] = g["grant_id"]
		row["identifier"] = fmt.Sprintf("%d--1", sequence)
		switch field {
		case "media_generation":
			row[field] = 0
		case "attempts":
			row[field] = -1
		case "state":
			row[field] = "bogus"
		default:
			row[field] = ""
		}
		require.Error(t, db.Table("gb_openapi_viewer").Create(row).Error, "invalid viewer accepted: "+field)
	}
	require.Error(t, db.Exec("DELETE FROM gb_openapi_play_grant WHERE grant_id = ?", bound["grant_id"]).Error, "grant deleted while a viewer references it")
}

func cloneNativeRow(source map[string]any) map[string]any {
	copy := make(map[string]any, len(source))
	for key, value := range source {
		copy[key] = value
	}
	return copy
}

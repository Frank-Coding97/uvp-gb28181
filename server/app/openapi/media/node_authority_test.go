package media

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

const (
	testAuthorityNodeUUID = "node-authority-a"
	testAuthorityBoot     = "0123456789abcdef0123456789abcdef"
)

func TestNodeAuthorityAcceptsOnlyCurrentRuntimeTuple(t *testing.T) {
	for _, protocol := range []string{"https-flv", "wss-flv"} {
		t.Run(protocol, func(t *testing.T) {
			db := newNodeAuthorityDB(t)
			insertNodeAuthorityRow(t, db, validNodeAuthorityValues())

			err := NewNodeAuthority().AuthorizeOpenAPI(context.Background(), db, playauth.OpenAPINodeAuthorization{
				NodeUUID: testAuthorityNodeUUID, BootNonce: testAuthorityBoot, Protocol: protocol,
			})
			require.NoError(t, err)
		})
	}
}

func TestNodeAuthorityRejectsEveryMalformedRuntimeField(t *testing.T) {
	tests := []struct {
		name    string
		update  string
		request *playauth.OpenAPINodeAuthorization
	}{
		{name: "zero id", update: "id = 0"},
		{name: "null revision", update: "revision = NULL"},
		{name: "zero revision", update: "revision = 0"},
		{name: "inactive state", update: "state = 'maintenance'"},
		{name: "null state", update: "state = NULL"},
		{name: "null uuid", update: "media_server_uuid = NULL"},
		{name: "boot mismatch", request: authorityRequestPtr(playauth.OpenAPINodeAuthorization{NodeUUID: testAuthorityNodeUUID, BootNonce: strings.Repeat("f", 32), Protocol: "https-flv"})},
		{name: "null boot", update: "current_boot_nonce = NULL"},
		{name: "null runtime epoch", update: "runtime_epoch = NULL"},
		{name: "zero runtime epoch", update: "runtime_epoch = 0"},
		{name: "null runtime protocol", update: "runtime_protocol_version = NULL"},
		{name: "unsupported runtime protocol", update: "runtime_protocol_version = 2"},
		{name: "null confirmed revision", update: "runtime_confirmed_revision = NULL"},
		{name: "zero confirmed revision", update: "runtime_confirmed_revision = 0"},
		{name: "confirmed revision mismatch", update: "runtime_confirmed_revision = 8"},
		{name: "null confirmed timestamp", update: "runtime_confirmed_at = NULL"},
		{name: "zero confirmed timestamp", update: "runtime_confirmed_at = '0001-01-01 00:00:00'"},
		{name: "null runtime identity", update: "runtime_identity_status = NULL"},
		{name: "unknown runtime identity", update: "runtime_identity_status = 'unknown'"},
		{name: "unsupported request protocol", request: authorityRequestPtr(playauth.OpenAPINodeAuthorization{NodeUUID: testAuthorityNodeUUID, BootNonce: testAuthorityBoot, Protocol: "http-flv"})},
		{name: "empty node uuid", request: authorityRequestPtr(playauth.OpenAPINodeAuthorization{BootNonce: testAuthorityBoot, Protocol: "https-flv"})},
		{name: "invalid boot nonce", request: authorityRequestPtr(playauth.OpenAPINodeAuthorization{NodeUUID: testAuthorityNodeUUID, BootNonce: strings.Repeat("A", 32), Protocol: "https-flv"})},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := newNodeAuthorityDB(t)
			insertNodeAuthorityRow(t, db, validNodeAuthorityValues())
			if tt.update != "" {
				require.NoError(t, db.Exec("UPDATE meta_node SET "+tt.update+" WHERE id = 1").Error)
			}
			request := validNodeAuthorityRequest()
			if tt.request != nil {
				request = *tt.request
			}
			err := NewNodeAuthority().AuthorizeOpenAPI(context.Background(), db, request)
			require.ErrorIs(t, err, playauth.ErrOpenAPIGrantUnavailable)
		})
	}
}

func TestNodeAuthorityRejectsDuplicateUUIDAndMissingColumns(t *testing.T) {
	db := newNodeAuthorityDB(t)
	insertNodeAuthorityRow(t, db, validNodeAuthorityValues())
	duplicate := validNodeAuthorityValues()
	duplicate["id"] = 2
	insertNodeAuthorityRow(t, db, duplicate)

	err := NewNodeAuthority().AuthorizeOpenAPI(context.Background(), db, validNodeAuthorityRequest())
	require.ErrorIs(t, err, playauth.ErrOpenAPIGrantUnavailable)
	require.NotContains(t, err.Error(), "sqlite")

	missing := newNodeAuthorityDBWithoutRuntimeStatus(t)
	missingValues := validNodeAuthorityValues()
	delete(missingValues, "runtime_identity_status")
	insertNodeAuthorityRow(t, missing, missingValues)
	err = NewNodeAuthority().AuthorizeOpenAPI(context.Background(), missing, validNodeAuthorityRequest())
	require.ErrorIs(t, err, playauth.ErrOpenAPIGrantUnavailable)
	require.NotContains(t, err.Error(), "no such column")
}

func TestNodeAuthorityUsesSuppliedTransactionAndSeesRollbackState(t *testing.T) {
	db := newNodeAuthorityDB(t)
	tx := db.Begin()
	require.NoError(t, tx.Error)
	insertNodeAuthorityRow(t, tx, validNodeAuthorityValues())
	authority := NewNodeAuthority()

	require.NoError(t, authority.AuthorizeOpenAPI(context.Background(), tx, validNodeAuthorityRequest()))
	require.NoError(t, tx.Table("meta_node").Where("id = ?", 1).Update("state", "maintenance").Error)
	require.ErrorIs(t, authority.AuthorizeOpenAPI(context.Background(), tx, validNodeAuthorityRequest()), playauth.ErrOpenAPIGrantUnavailable)
	require.NoError(t, tx.Rollback().Error)
	require.ErrorIs(t, authority.AuthorizeOpenAPI(context.Background(), db, validNodeAuthorityRequest()), playauth.ErrOpenAPIGrantUnavailable)
}

func TestNodeAuthorityHasNoGlobalDatabaseFallback(t *testing.T) {
	validDB := newNodeAuthorityDB(t)
	insertNodeAuthorityRow(t, validDB, validNodeAuthorityValues())
	emptyDB := newNodeAuthorityDB(t)

	err := NewNodeAuthority().AuthorizeOpenAPI(context.Background(), emptyDB, validNodeAuthorityRequest())
	require.ErrorIs(t, err, playauth.ErrOpenAPIGrantUnavailable)
}

func TestNodeAuthorityRejectsNilAndCanceledInputs(t *testing.T) {
	db := newNodeAuthorityDB(t)
	insertNodeAuthorityRow(t, db, validNodeAuthorityValues())
	authority := NewNodeAuthority()

	require.ErrorIs(t, authority.AuthorizeOpenAPI(nil, db, validNodeAuthorityRequest()), playauth.ErrOpenAPIGrantUnavailable)
	require.ErrorIs(t, authority.AuthorizeOpenAPI(context.Background(), nil, validNodeAuthorityRequest()), playauth.ErrOpenAPIGrantUnavailable)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, authority.AuthorizeOpenAPI(ctx, db, validNodeAuthorityRequest()), playauth.ErrOpenAPIGrantUnavailable)
}

func validNodeAuthorityRequest() playauth.OpenAPINodeAuthorization {
	return playauth.OpenAPINodeAuthorization{NodeUUID: testAuthorityNodeUUID, BootNonce: testAuthorityBoot, Protocol: "https-flv"}
}

func authorityRequestPtr(request playauth.OpenAPINodeAuthorization) *playauth.OpenAPINodeAuthorization {
	return &request
}

func validNodeAuthorityValues() map[string]any {
	return map[string]any{
		"id":                         1,
		"revision":                   9,
		"state":                      "active",
		"media_server_uuid":          testAuthorityNodeUUID,
		"current_boot_nonce":         testAuthorityBoot,
		"runtime_epoch":              1,
		"runtime_protocol_version":   1,
		"runtime_confirmed_revision": 9,
		"runtime_confirmed_at":       time.Date(2026, 9, 6, 1, 0, 0, 0, time.UTC),
		"runtime_identity_status":    "active",
	}
}

func insertNodeAuthorityRow(t *testing.T, db *gorm.DB, values map[string]any) {
	t.Helper()
	require.NoError(t, db.Table("meta_node").Create(values).Error)
}

func newNodeAuthorityDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:node-authority-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	require.NoError(t, db.Exec(nodeAuthorityTableSQL).Error)
	return db
}

func newNodeAuthorityDBWithoutRuntimeStatus(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:node-authority-missing-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	require.NoError(t, db.Exec(nodeAuthorityTableWithoutRuntimeStatusSQL).Error)
	return db
}

const nodeAuthorityTableSQL = `CREATE TABLE meta_node (
 id INTEGER PRIMARY KEY,
 revision INTEGER,
 state TEXT,
 media_server_uuid TEXT,
 current_boot_nonce TEXT,
 runtime_epoch INTEGER,
 runtime_protocol_version INTEGER,
 runtime_confirmed_revision INTEGER,
 runtime_confirmed_at DATETIME,
 runtime_identity_status TEXT
)`

const nodeAuthorityTableWithoutRuntimeStatusSQL = `CREATE TABLE meta_node (
 id INTEGER PRIMARY KEY,
 revision INTEGER,
 state TEXT,
 media_server_uuid TEXT,
 current_boot_nonce TEXT,
 runtime_epoch INTEGER,
 runtime_protocol_version INTEGER,
 runtime_confirmed_revision INTEGER,
 runtime_confirmed_at DATETIME
)`

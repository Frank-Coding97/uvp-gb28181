package models

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestOpenAPICoreUniqueKeys(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	require.NoError(t, db.AutoMigrate(&Client{}, &ClientScope{}, &Nonce{}, &Audit{}))
	first := Client{AK: "uvp_000102030405060708090a0b0c0d0e0f", Name: "A", OwnerDeptID: 10,
		SecretCiphertext: []byte("encrypted-test-only"), SecretIV: []byte("test-iv-12345"), SecretKeyID: "test-v1"}
	require.NoError(t, db.Create(&first).Error)
	require.Equal(t, int64(1), first.AuthEpoch)
	require.Equal(t, int64(1), first.RowVersion)
	require.Equal(t, StatusDisabled, first.Status)
	duplicate := first
	duplicate.ID = 0
	require.Error(t, db.Create(&duplicate).Error)
	scope := ClientScope{ClientID: first.ID, Scope: "device:list"}
	require.NoError(t, db.Create(&scope).Error)
	require.False(t, scope.Enabled)
	require.Error(t, db.Create(&ClientScope{ClientID: first.ID, Scope: scope.Scope}).Error)
	accepted := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	newNonce := func(clientID int64) Nonce {
		return Nonce{ClientID: clientID, Value: "000102030405060708090a0b0c0d0e0f", AcceptedAt: accepted, ExpiresAt: accepted.Add(660 * time.Second)}
	}
	n := newNonce(first.ID)
	require.NoError(t, db.Create(&n).Error)
	n = newNonce(first.ID)
	require.Error(t, db.Create(&n).Error)
	n = newNonce(first.ID + 1)
	require.NoError(t, db.Create(&n).Error)
}

func TestOpenAPIClientJSONNeverIncludesVerificationMaterial(t *testing.T) {
	b, err := json.Marshal(Client{AK: "test-ak", SecretCiphertext: []byte("private-ciphertext"), SecretIV: []byte("private-iv"), SecretKeyID: "private-key-id", SecretVersion: 42})
	require.NoError(t, err)
	var fields map[string]any
	require.NoError(t, json.Unmarshal(b, &fields))
	for _, forbidden := range []string{"SecretCiphertext", "secretCiphertext", "SecretIV", "secretIV", "SecretKeyID", "secretKeyId", "SecretVersion", "secretVersion"} {
		require.NotContains(t, fields, forbidden)
	}
}

func TestOpenAPICoreTableNames(t *testing.T) {
	require.Equal(t, "sys_openapi_client", (Client{}).TableName())
	require.Equal(t, "sys_openapi_client_scope", (ClientScope{}).TableName())
	require.Equal(t, "sys_openapi_nonce", (Nonce{}).TableName())
	require.Equal(t, "sys_openapi_audit", (Audit{}).TableName())
}

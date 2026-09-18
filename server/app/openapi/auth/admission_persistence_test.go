package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

// This helper exits without closing the database after a confirmed admission.
// The parent then starts a different OS process against the same durable file.
func TestOpenAPINonceProcessHelper(t *testing.T) {
	mode := os.Getenv("UVP_OPENAPI_NONCE_PROCESS_MODE")
	if mode == "" {
		return
	}
	path := os.Getenv("UVP_OPENAPI_NONCE_PROCESS_DB")
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.True(t, info.Mode().IsRegular())
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	now := time.Unix(1790000000, 0)
	err = NewAdmission(db, func() time.Time { return now }).Admit(context.Background(), admissionRequest(now), allowed)
	switch mode {
	case "accept":
		require.NoError(t, err)
	case "replay":
		require.ErrorIs(t, err, ErrReplay)
	default:
		t.Fatal("unsupported isolated nonce process mode")
	}
	os.Exit(0)
}

func TestOpenAPINonceProcessRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonce-process.db")
	open := func() *gorm.DB {
		db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		require.NoError(t, err)
		return db
	}
	db := open()
	require.NoError(t, db.AutoMigrate(&models.Client{}, &models.ClientScope{}, &models.Nonce{}, &models.Audit{}))
	require.NoError(t, db.Create(&models.Client{ID: 1, AK: "uvp_000102030405060708090a0b0c0d0e0f",
		Name: "process fixture", Status: models.StatusActive, OwnerDeptID: 10,
		SecretCiphertext: []byte{1}, SecretIV: []byte{1}, SecretKeyID: "test"}).Error)
	require.NoError(t, db.Create(&models.ClientScope{ClientID: 1, Scope: "device:list", Enabled: true}).Error)
	raw, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	binary, err := os.Executable()
	require.NoError(t, err)
	for _, mode := range []string{"accept", "replay"} {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		cmd := exec.CommandContext(ctx, binary, "-test.run=^TestOpenAPINonceProcessHelper$", "-test.count=1")
		cmd.Env = append(os.Environ(), "UVP_OPENAPI_NONCE_PROCESS_MODE="+mode, "UVP_OPENAPI_NONCE_PROCESS_DB="+path)
		output, err := cmd.CombinedOutput()
		cancel()
		require.NoError(t, err, "isolated %s process: %s", mode, output)
	}
	db = open()
	raw, err = db.DB()
	require.NoError(t, err)
	defer raw.Close()
	var count int64
	require.NoError(t, db.Model(&models.Nonce{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	var audit models.Audit
	require.NoError(t, db.First(&audit).Error)
	require.Equal(t, "started", audit.Result, "process exit is not a successful business completion")
}

func TestOpenAPIGatewayAdmissionDatabaseFailuresNeverDispatch(t *testing.T) {
	for _, failure := range []string{"read-only", "deadline", "commit-unknown"} {
		t.Run(failure, func(t *testing.T) {
			gate, db, secret := gatewayFixture(t)
			var calls atomic.Int32
			gate.read = func(context.Context, *gorm.DB, metadataInput) (any, error) {
				calls.Add(1)
				return []any{}, nil
			}
			realTransaction := gate.admission.transact
			switch failure {
			case "read-only":
				require.NoError(t, db.Exec("PRAGMA query_only=ON").Error)
			case "deadline":
				gate.admission.transact = func(ctx context.Context, fn func(*gorm.DB) error) error {
					expired, cancel := context.WithDeadline(ctx, time.Now().Add(-time.Second))
					defer cancel()
					return realTransaction(expired, fn)
				}
			case "commit-unknown":
				gate.admission.transact = func(ctx context.Context, fn func(*gorm.DB) error) error {
					if err := realTransaction(ctx, fn); err != nil {
						return err
					}
					return errors.New("isolated lost commit acknowledgement")
				}
			}
			nonce := strings.Repeat("a", 32)
			response := gatewayCall(t, gate, secret, nonce, nil)
			require.Equal(t, 503, response.Code)
			require.Zero(t, calls.Load(), "failed SQL admission must never dispatch business")
			require.NotContains(t, response.Body.String(), "commit acknowledgement")
			var count int64
			require.NoError(t, db.Model(&models.Nonce{}).Count(&count).Error)
			if failure == "commit-unknown" {
				require.EqualValues(t, 1, count)
			} else {
				require.Zero(t, count)
			}
			// Recover only the injected dependency failure. A nonce with an
			// uncertain commit remains consumed; rollback failures do not burn it.
			require.NoError(t, db.Exec("PRAGMA query_only=OFF").Error)
			gate.admission.transact = realTransaction
			response = gatewayCall(t, gate, secret, nonce, nil)
			if failure == "commit-unknown" {
				require.Equal(t, 401, response.Code)
				require.Contains(t, response.Body.String(), "REQUEST_REPLAYED")
				require.Zero(t, calls.Load())
			} else {
				require.Equal(t, 200, response.Code)
				require.EqualValues(t, 1, calls.Load())
			}
		})
	}
}

func TestOpenAPIGatewayRejectedRequestDoesNotReserveNonce(t *testing.T) {
	for _, failure := range []string{"signature", "expired-invalid-signature", "capability", "department"} {
		t.Run(failure, func(t *testing.T) {
			gate, db, secret := gatewayFixture(t)
			var calls atomic.Int32
			gate.read = func(context.Context, *gorm.DB, metadataInput) (any, error) {
				calls.Add(1)
				return []any{}, nil
			}
			status := 401
			if failure == "capability" {
				require.NoError(t, db.Model(&models.ClientScope{}).Where("client_id = ?", 1).Update("enabled", false).Error)
				status = 403
			}
			if failure == "department" {
				require.NoError(t, db.Exec("UPDATE sys_department SET status=0 WHERE id=10").Error)
				status = 404
			}
			nonce := strings.Repeat("b", 32)
			response := gatewayCall(t, gate, secret, nonce, func(r *http.Request) {
				if failure == "signature" {
					r.Header.Set("X-UVP-Signature", strings.Repeat("0", 64))
				}
				if failure == "expired-invalid-signature" {
					r.Header.Set("X-UVP-Timestamp", fmt.Sprint(time.Now().Add(-301*time.Second).Unix()))
				}
			})
			require.Equal(t, status, response.Code)
			if failure == "expired-invalid-signature" {
				require.Contains(t, response.Body.String(), "AUTHENTICATION_FAILED")
				require.NotContains(t, response.Body.String(), "REQUEST_EXPIRED")
			}
			require.Zero(t, calls.Load())
			var count int64
			require.NoError(t, db.Model(&models.Nonce{}).Count(&count).Error)
			require.Zero(t, count)
			require.NoError(t, db.Model(&models.ClientScope{}).Where("client_id = ?", 1).Update("enabled", true).Error)
			require.NoError(t, db.Exec("UPDATE sys_department SET status=1 WHERE id=10").Error)
			response = gatewayCall(t, gate, secret, nonce, nil)
			require.Equal(t, 200, response.Code)
			require.EqualValues(t, 1, calls.Load())
		})
	}
}

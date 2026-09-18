package playauth

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/internal/authoritytest"
)

func TestDeviceOperationBarrierWithoutAuthorityCannotAdmit(t *testing.T) {
	for _, mode := range []string{"epoch", "legacy", "queued-v2", "queued-v4"} {
		t.Run(mode, func(t *testing.T) {
			f, _ := newDeviceOperationBarrierFixture(t)
			b := NewDeviceOperationBarrier(NewDeviceSecurityStore(f.db))
			var lease DeviceOperationLease
			var err error
			switch mode {
			case "epoch":
				lease, err = b.BeginEpoch(context.Background(), operationBarrierDevice, 1)
			case "legacy":
				lease, err = b.beginLegacy(context.Background(), operationBarrierDevice, f.clock.Unix())
			default:
				epoch := int64(0)
				if mode == "queued-v4" {
					epoch = 1
				}
				s, q := newOperationBarrierAuthorizationService(t, f, b, epoch, mode)
				lease, err = s.BeginQueuedOperation(context.Background(), q)
			}
			if lease != nil {
				lease.Release()
			}
			require.ErrorIs(t, err, ErrDeviceOperationUnavailable)
			require.Nil(t, lease)
			require.Empty(t, b.lanes)
			// Read-only epoch checks and transfer coordination are not permission
			// to dispatch; leave those available without a process handle.
			require.NoError(t, b.AuthorizeEpoch(context.Background(), operationBarrierDevice, 1))
			g, err := b.LockTransfer(context.Background(), 1)
			require.NoError(t, err)
			g.Release()
		})
	}
}

func TestDeviceOperationBarrierAuthorityPrecedesDeviceInSameTransaction(t *testing.T) {
	for _, mode := range []string{"epoch", "legacy", "queued-v2", "queued-v4"} {
		for _, reject := range []bool{false, true} {
			name := mode + "/admitted"
			if reject {
				name = mode + "/fenced"
			}
			t.Run(name, func(t *testing.T) {
				f, _ := newDeviceOperationBarrierFixture(t)
				var checked *sql.Tx
				checks, deviceQueries := 0, 0
				checker := &intentCheckingAuthority{check: func(tx *gorm.DB) error {
					var ok bool
					checked, ok = tx.Statement.ConnPool.(*sql.Tx)
					require.True(t, ok)
					checks++
					if reject {
						return errors.New("authority unavailable")
					}
					return nil
				}}
				b := newDeviceOperationBarrier(NewDeviceSecurityStore(f.db), checker)
				require.NoError(t, f.db.Callback().Query().Before("gorm:query").Register("barrier_authority_order", func(tx *gorm.DB) {
					if actual, ok := tx.Statement.ConnPool.(*sql.Tx); ok && tx.Statement.Table == "gb_device" {
						deviceQueries++
						require.NotNil(t, checked)
						require.Same(t, checked, actual)
					}
				}))
				t.Cleanup(func() { _ = f.db.Callback().Query().Remove("barrier_authority_order") })
				var lease DeviceOperationLease
				var err error
				switch mode {
				case "epoch":
					lease, err = b.BeginEpoch(context.Background(), operationBarrierDevice, 1)
				case "legacy":
					lease, err = b.beginLegacy(context.Background(), operationBarrierDevice, f.clock.Unix())
				default:
					epoch := int64(0)
					if mode == "queued-v4" {
						epoch = 1
					}
					s, q := newOperationBarrierAuthorizationService(t, f, b, epoch, name)
					lease, err = s.BeginQueuedOperation(context.Background(), q)
				}
				require.Equal(t, 1, checks)
				if reject {
					require.ErrorIs(t, err, ErrDeviceOperationUnavailable)
					require.Nil(t, lease)
					require.Zero(t, deviceQueries)
				} else {
					require.NoError(t, err)
					require.Equal(t, 1, deviceQueries)
					lease.Release()
				}
				require.Empty(t, b.lanes, "failed admission and released lease must both release the lane")
			})
		}
	}
}

func TestDeviceOperationBarrierRealAuthorityCommitBoundary(t *testing.T) {
	for _, mode := range []string{"confirmed", "rollback", "commit-receipt-lost", "sealed", "wrong-pool"} {
		t.Run(mode, func(t *testing.T) {
			if !authoritytest.InProcess(t) {
				return
			}
			db := authoritytest.OpenSQLite(t, filepath.Join(t.TempDir(), "barrier.sqlite"))
			require.NoError(t, db.Exec(`CREATE TABLE gb_device (id INTEGER PRIMARY KEY, device_id TEXT,
				access_epoch INTEGER, cleanup_completed_epoch INTEGER, legacy_revoked_before DATETIME, deleted_at DATETIME)`).Error)
			require.NoError(t, db.Exec("INSERT INTO gb_device (id,device_id,access_epoch,cleanup_completed_epoch) VALUES (1,?,1,1)", operationBarrierDevice).Error)
			a := authoritytest.Authority(t, db)
			readVersion := func() int64 {
				var v int64
				require.NoError(t, db.Table("sys_openapi_process_authority").Pluck("row_version", &v).Error)
				return v
			}
			before := readVersion()
			candidate := db
			switch mode {
			case "rollback", "commit-receipt-lost":
				candidate = authoritytest.CommitFaultDB(t, db, 1, mode == "commit-receipt-lost")
			case "sealed":
				a.Seal()
			case "wrong-pool":
				candidate = authoritytest.OpenSQLite(t, filepath.Join(t.TempDir(), "other.sqlite"))
				require.NoError(t, candidate.Exec(`CREATE TABLE gb_device (id INTEGER PRIMARY KEY, device_id TEXT,
					access_epoch INTEGER, cleanup_completed_epoch INTEGER, legacy_revoked_before DATETIME, deleted_at DATETIME)`).Error)
				require.NoError(t, candidate.Exec("INSERT INTO gb_device (id,device_id,access_epoch,cleanup_completed_epoch) VALUES (1,?,1,1)", operationBarrierDevice).Error)
			}
			b, err := NewAuthorizedDeviceOperationBarrier(NewDeviceSecurityStore(candidate), a)
			require.NoError(t, err, "construction alone is not authority permission")
			lease, err := b.BeginEpoch(context.Background(), operationBarrierDevice, 1)
			if mode == "confirmed" {
				require.NoError(t, err)
				require.NotNil(t, lease)
				lease.Release()
			} else {
				require.ErrorIs(t, err, ErrDeviceOperationUnavailable)
				require.Nil(t, lease, "unknown commit cannot authorize a device effect")
			}
			require.Empty(t, b.lanes)
			if mode == "confirmed" || mode == "commit-receipt-lost" {
				require.Equal(t, before+1, readVersion())
			} else {
				require.Equal(t, before, readVersion())
			}
		})
	}
}

package controllers

import (
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"uvplatform.cn/uvp-gb28181/app/gb28181/ptz"
)

func TestPTZAuthorizationSnapshotRejectsMixedRoot(t *testing.T) {
	for _, mutation := range []string{"", "UPDATE gb_device SET access_epoch=2", "UPDATE gb_device SET owner_dept_id=20", "UPDATE gb_channel SET channel_id='changed'"} {
		t.Run(mutation, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "ptz.db")), &gorm.Config{})
			require.NoError(t, err)
			raw, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { _ = raw.Close() })
			require.NoError(t, db.Exec(`CREATE TABLE gb_device(id INTEGER, device_id TEXT, owner_dept_id INTEGER, access_epoch INTEGER, cleanup_completed_epoch INTEGER, ip TEXT, port INTEGER, transport TEXT, status INTEGER, effective_version TEXT, deleted_at DATETIME)`).Error)
			require.NoError(t, db.Exec(`CREATE TABLE gb_channel(id INTEGER, device_id TEXT, channel_id TEXT, owner_dept_id INTEGER, status INTEGER, deleted_at DATETIME)`).Error)
			require.NoError(t, db.Exec(`INSERT INTO gb_device VALUES(1,'device',10,1,1,'192.0.2.1',5060,'UDP',1,'2022',NULL)`).Error)
			require.NoError(t, db.Exec(`INSERT INTO gb_channel VALUES(2,'device','channel',10,1,NULL)`).Error)
			if mutation != "" {
				require.NoError(t, db.Exec(mutation).Error)
			}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("GET", "/", nil)
			got, err := capturePTZAuthorization(c, db, ptz.Target{DeviceID: 1, DeviceCode: "device", ChannelID: 2, ChannelCode: "channel"})
			if mutation != "" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.EqualValues(t, 1, got.DeviceEpoch)
				require.Equal(t, "192.0.2.1", got.IP)
			}
		})
	}
}

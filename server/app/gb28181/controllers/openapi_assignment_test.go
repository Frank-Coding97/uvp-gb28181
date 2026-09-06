package controllers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	basemodels "uvplatform.cn/uvp-gb28181/app/models"
	openapilimit "uvplatform.cn/uvp-gb28181/app/openapi/limit"
	openapimodels "uvplatform.cn/uvp-gb28181/app/openapi/models"
)

// These controller tests use only isolated SQLite state. They verify the
// durable assignment boundary, not media-session cleanup or root JWT routing.
func TestOpenAPIAssignmentControllerCommitsOrRollsBackSecurity(t *testing.T) {
	for _, mode := range []string{"devices", "departments"} {
		for _, outcome := range []string{"commit", "rollback", "canceled"} {
			name := mode + "/" + outcome
			failViewer := outcome == "rollback"
			t.Run(name, func(t *testing.T) {
				// The existing controller still bumps the legacy global threshold;
				// removal is verified separately with the v4 production wiring.
				previousCutoff := playauth.RevokedBefore()
				t.Cleanup(func() { playauth.BumpRevocation(time.Unix(previousCutoff, 0)) })
				r, db := newDeviceMgmtRouter(t)
				registerPermissionWorkbenchRoutes(r, db)
				require.NoError(t, db.AutoMigrate(&gbmodels.GbRecordingFile{}, &gbmodels.GbAlarmResource{},
					&openapimodels.PlayGrant{}, &openapimodels.Viewer{}))
				require.NoError(t, db.Exec("ALTER TABLE gb_device ADD COLUMN access_epoch INTEGER NOT NULL DEFAULT 1").Error)
				require.NoError(t, db.Exec("ALTER TABLE gb_device ADD COLUMN legacy_revoked_before DATETIME NULL").Error)
				require.NoError(t, db.Exec("CREATE TABLE gb_cascade_device_projection (id INTEGER PRIMARY KEY, source_device_id INTEGER)").Error)
				active := int8(1)
				require.NoError(t, db.Create(&[]basemodels.SysDepartment{
					{BaseModel: basemodels.BaseModel{ID: 10}, Name: "source", Status: &active},
					{BaseModel: basemodels.BaseModel{ID: 20}, Name: "target", Status: &active},
				}).Error)
				devices := []gbmodels.GbDevice{
					{DeviceID: "34020000001320000001", OwnerDeptID: 10},
					{DeviceID: "34020000001320000002", OwnerDeptID: 30},
				}
				require.NoError(t, db.Create(&devices).Error)
				now := time.Now().UTC().Truncate(time.Microsecond)
				grantID := "00000000-0000-4000-8000-000000000001"
				channel := "34020000001320000101"
				node, boot := "assignment-node", "0123456789abcdef0123456789abcdef"
				schema, vhost, mediaApp, stream, protocol := "rtmp", "__defaultVhost__", "rtp", "assignment", "https-flv"
				generation := uint64(1)
				require.NoError(t, db.Create(&openapimodels.PlayGrant{
					GrantID: grantID, ClientID: 7, Scope: openapilimit.PlayLiveApplyScope,
					DeviceID: &devices[0].DeviceID, ChannelID: &channel, ClientEpoch: 1, ScopeEpoch: 1, DeviceEpoch: 1,
					NodeUUID: &node, BootNonce: &boot, Schema: &schema, VHost: &vhost, App: &mediaApp,
					Stream: &stream, MediaGeneration: &generation, Protocol: &protocol,
					IssuedAt: now, ExpiresAt: now.Add(time.Minute), State: openapimodels.GrantStateBound,
					CreatedAt: now, UpdatedAt: now,
				}).Error)
				require.NoError(t, db.Create(&openapimodels.Viewer{
					ID: 1, GrantID: grantID, NodeUUID: node, BootNonce: boot, Identifier: "assignment-viewer",
					Schema: schema, VHost: vhost, App: mediaApp, Stream: stream, MediaGeneration: generation,
					State: openapimodels.ViewerStateActive, CreatedAt: now, UpdatedAt: now,
				}).Error)
				if failViewer {
					require.NoError(t, db.Exec("CREATE TRIGGER assignment_viewer_failure BEFORE UPDATE ON gb_openapi_viewer BEGIN SELECT RAISE(ABORT, 'fixture failure'); END").Error)
				}
				path := "/api/gb28181/device-mgmt/permission-workbench/assignments"
				body := map[string]any{"items": []map[string]any{{"deviceId": devices[0].ID, "expectedOwnerDeptId": 10}}, "targetDeptId": 20}
				if mode == "departments" {
					path += "/departments"
					body = map[string]any{"sourceDeptId": 10, "targetDeptId": 20, "includeChildren": false, "expectedCount": 1}
				}
				encoded, err := json.Marshal(body)
				require.NoError(t, err)
				req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(encoded))
				if outcome == "canceled" {
					ctx, cancel := context.WithCancel(req.Context())
					cancel()
					req = req.WithContext(ctx)
				}
				req.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)
				var summary map[string]any
				if outcome == "canceled" {
					require.NotEqual(t, http.StatusOK, w.Code, "canceled request must not commit: %s", w.Body.String())
				} else {
					require.Equal(t, http.StatusOK, w.Code, w.Body.String())
					summary = unmarshal(t, w)["data"].(map[string]any)["summary"].(map[string]any)
				}
				require.NotContains(t, w.Body.String(), "receipt")
				var states []struct {
					OwnerDeptID         uint
					AccessEpoch         int64
					LegacyRevokedBefore *time.Time
				}
				require.NoError(t, db.Table("gb_device").Select("owner_dept_id, access_epoch, legacy_revoked_before").Order("id").Find(&states).Error)
				require.Len(t, states, 2)
				var grant openapimodels.PlayGrant
				var viewer openapimodels.Viewer
				require.NoError(t, db.First(&grant, "grant_id = ?", grantID).Error)
				require.NoError(t, db.First(&viewer, 1).Error)
				if outcome != "commit" {
					if failViewer {
						require.EqualValues(t, 1, summary["failed"])
						require.EqualValues(t, 0, summary["changed"])
					}
					require.EqualValues(t, 10, states[0].OwnerDeptID)
					require.EqualValues(t, 1, states[0].AccessEpoch)
					require.Nil(t, states[0].LegacyRevokedBefore)
					require.Equal(t, openapimodels.GrantStateBound, grant.State)
					require.Equal(t, openapimodels.ViewerStateActive, viewer.State)
				} else {
					require.EqualValues(t, 1, summary["changed"])
					require.EqualValues(t, 0, summary["failed"])
					require.EqualValues(t, 20, states[0].OwnerDeptID)
					require.EqualValues(t, 2, states[0].AccessEpoch)
					require.NotNil(t, states[0].LegacyRevokedBefore)
					require.Greater(t, states[0].LegacyRevokedBefore.Unix(), now.Unix())
					require.Equal(t, openapimodels.GrantStateRevoked, grant.State)
					require.Equal(t, openapimodels.ViewerStateRevokePending, viewer.State)
				}
				require.EqualValues(t, 30, states[1].OwnerDeptID)
				require.EqualValues(t, 1, states[1].AccessEpoch)
				require.Nil(t, states[1].LegacyRevokedBefore)
			})
		}
	}
}

package gb28181

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/internal/authoritytest"
)

func TestCascadeTalkAuthorizationBindsPublishedSource(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}
	db, platform, store, _ := cascadeAuthorizedControlFixture(t, "2022", nil)
	// Publication, rather than an unrelated PTZ grant, identifies the source.
	require.NoError(t, db.Exec("UPDATE gb_cascade_platform SET ptz_enabled=0,device_control_enabled=0,maintenance_enabled=0,config_enabled=0,upgrade_enabled=0").Error)
	binding, err := resolveCascadeTalkAuthorization(t.Context(), db, store, platform, "34020000001320000012")
	require.NoError(t, err)
	require.EqualValues(t, 10, binding.Source.DeviceID)
	require.EqualValues(t, 20, binding.Source.ChannelID)
	require.Equal(t, "34020000001320000001", binding.Source.DeviceCode)
	require.Equal(t, "34020000001320000002", binding.Source.ChannelCode)
	require.EqualValues(t, 1, binding.Source.DeviceEpoch)
	require.NoError(t, binding.Validate(t.Context(), db, store))
	for _, target := range []string{platform.LocalDeviceID, "34020000001320000002", "34020000001320000099"} {
		_, err := resolveCascadeTalkAuthorization(t.Context(), db, store, platform, target)
		require.Error(t, err, target)
	}
}

func TestCascadeTalkAuthorizationRejectsChangedBinding(t *testing.T) {
	for name, statement := range map[string]string{
		"platform disabled":           "UPDATE gb_cascade_platform SET enabled=0",
		"config revision":             "UPDATE gb_cascade_platform SET config_revision=config_revision+1",
		"publication revision":        "UPDATE gb_cascade_platform SET projection_revision=projection_revision+1",
		"hidden channel":              "UPDATE gb_cascade_channel_projection SET active=0",
		"deleted projection":          "UPDATE gb_cascade_channel_projection SET deleted_at=CURRENT_TIMESTAMP",
		"hidden device projection":    "UPDATE gb_cascade_device_projection SET active=0",
		"source device deleted":       "UPDATE gb_device SET deleted_at=CURRENT_TIMESTAMP WHERE id=10",
		"source channel deleted":      "UPDATE gb_channel SET deleted_at=CURRENT_TIMESTAMP WHERE id=20",
		"channel owner changed":       "UPDATE gb_channel SET owner_dept_id=1 WHERE id=20",
		"both owners changed":         "UPDATE gb_channel SET owner_dept_id=1 WHERE id=20; UPDATE gb_device SET owner_dept_id=1 WHERE id=10",
		"channel reassigned":          "UPDATE gb_channel SET device_id='another-device' WHERE id=20",
		"source code changed":         "UPDATE gb_channel SET channel_id='34020000001320000099' WHERE id=20",
		"device offline":              "UPDATE gb_device SET status=0 WHERE id=10",
		"channel offline":             "UPDATE gb_channel SET status=0 WHERE id=20",
		"epoch missing":               "UPDATE gb_device SET access_epoch=NULL WHERE id=10",
		"cleanup pending":             "UPDATE gb_device SET access_epoch=2 WHERE id=10",
		"new valid epoch":             "UPDATE gb_device SET access_epoch=2,cleanup_completed_epoch=2 WHERE id=10",
		"protocol changed":            "UPDATE gb_device SET effective_version='2016' WHERE id=10",
		"signaling address changed":   "UPDATE gb_device SET ip='192.0.2.99' WHERE id=10",
		"signaling port changed":      "UPDATE gb_device SET port=5090 WHERE id=10",
		"signaling transport changed": "UPDATE gb_device SET transport='TCP' WHERE id=10",
	} {
		t.Run(name, func(t *testing.T) {
			if !authoritytest.InProcess(t) {
				return
			}
			db, platform, store, _ := cascadeAuthorizedControlFixture(t, "2022", nil)
			require.NoError(t, db.Exec("UPDATE gb_device SET effective_version='2022' WHERE id=10").Error)
			binding, err := resolveCascadeTalkAuthorization(t.Context(), db, store, platform, "34020000001320000012")
			require.NoError(t, err)
			for _, sql := range strings.Split(statement, ";") {
				require.NoError(t, db.Exec(sql).Error)
			}
			require.Error(t, binding.Validate(t.Context(), db, store))
		})
	}
}

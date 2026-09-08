package playauth

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestDeviceSIPInvitePersistsProcessIdentity(t *testing.T) {
	f, store, id := newSIPStepFixture(t)
	ctx := context.Background()
	out, err := store.AddSIPInviteStep(ctx, id, 2, sipStepIdentity(1))
	require.NoError(t, err)
	processID, err := sipCleanupProcessID()
	require.NoError(t, err)
	var raw string
	require.NoError(t, f.db.Table("gb_device_operation_intent").Select("sip_steps_json").Scan(&raw).Error)
	var wire struct {
		Steps []struct {
			OwnerProcessID string `json:"ownerProcessID"`
		} `json:"steps"`
	}
	require.NoError(t, json.Unmarshal([]byte(raw), &wire))
	require.Equal(t, processID, wire.Steps[0].OwnerProcessID)
	loaded, err := NewDeviceOperationIntentStore(f.db).LoadSIPInviteSteps(ctx, id)
	require.NoError(t, err)
	require.Equal(t, out, loaded)
	dispatched, err := store.DispatchSIPInviteStep(ctx, id, out.Intent.RowVersion, sipStepIdentity(1).StepID)
	require.NoError(t, err)
	require.Equal(t, SIPStepMayHaveDispatched, dispatched.Steps[0].State)
}

func TestDeviceSIPInviteLegacyAndForeignProcessCannotDispatch(t *testing.T) {
	for _, owner := range []string{"", strings.Repeat("f", 32)} {
		t.Run("owner-"+owner, func(t *testing.T) {
			f, store, id := newSIPStepFixture(t)
			ctx := context.Background()
			out, err := store.AddSIPInviteStep(ctx, id, 2, sipStepIdentity(1))
			require.NoError(t, err)
			wire := sipInviteStepsWire{Version: 1, Steps: []sipInviteStepWire{sipStepToWire(out.Steps[0])}}
			wire.Steps[0].OwnerProcessID = owner
			raw, err := json.Marshal(wire)
			require.NoError(t, err)
			require.NoError(t, f.db.Exec("UPDATE gb_device_operation_intent SET sip_steps_json=?", string(raw)).Error)
			loaded, err := store.LoadSIPInviteSteps(ctx, id)
			require.NoError(t, err, "legacy and foreign evidence remains readable")
			roundtrip, err := encodeSIPInviteSteps(loaded)
			require.NoError(t, err)
			require.Equal(t, raw, roundtrip, "loading history must not infer or replace its process")
			duplicate, err := store.AddSIPInviteStep(ctx, id, 3, sipStepIdentity(1))
			require.NoError(t, err)
			require.Equal(t, loaded, duplicate, "idempotent prepare must not claim an existing step")
			for _, candidate := range []*DeviceOperationIntentStore{store, NewDeviceOperationIntentStore(f.db)} {
				result, err := candidate.DispatchSIPInviteStep(ctx, id, 3, sipStepIdentity(1).StepID)
				require.ErrorIs(t, err, ErrDeviceIntentConflict)
				require.Empty(t, result.Intent.OperationID)
			}
			var after string
			require.NoError(t, f.db.Table("gb_device_operation_intent").Select("sip_steps_json").Scan(&after).Error)
			require.Equal(t, string(raw), after)
		})
	}
}

func TestDeviceSIPInviteProcessWireRejectsDamagedIdentity(t *testing.T) {
	f, store, id := newSIPStepFixture(t)
	ctx := context.Background()
	out, err := store.AddSIPInviteStep(ctx, id, 2, sipStepIdentity(1))
	require.NoError(t, err)
	for _, owner := range []string{"bad", strings.Repeat("A", 32), strings.Repeat("0", 32), strings.Repeat("a", 31), strings.Repeat("a", 33)} {
		t.Run(owner, func(t *testing.T) {
			wire := sipInviteStepsWire{Version: 1, Steps: []sipInviteStepWire{sipStepToWire(out.Steps[0])}}
			wire.Steps[0].OwnerProcessID = owner
			raw, err := json.Marshal(wire)
			require.NoError(t, err)
			require.NoError(t, f.db.Exec("UPDATE gb_device_operation_intent SET sip_steps_json=?", string(raw)).Error)
			_, err = store.LoadSIPInviteSteps(ctx, id)
			require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
			_, err = store.DispatchSIPInviteStep(ctx, id, 3, sipStepIdentity(1).StepID)
			require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
		})
	}
}

func TestDeviceSIPInviteLegacyProcessPreservesLateBranchEvidence(t *testing.T) {
	for _, extraBranch := range []bool{false, true} {
		t.Run(map[bool]string{false: "v1", true: "v2"}[extraBranch], func(t *testing.T) {
			f, store, id := sipCleanupFixture(t)
			ctx := context.Background()
			out, err := store.LoadSIPInviteSteps(ctx, id)
			require.NoError(t, err)
			if extraBranch {
				out, err = store.ObserveSIPAdditionalBranch(ctx, id, out.Intent.RowVersion, sipExtraBranch(1))
				require.NoError(t, err)
			}
			wire := sipInviteStepsWire{Version: 1, Steps: []sipInviteStepWire{sipStepToWire(out.Steps[0])}}
			wire.Steps[0].OwnerProcessID = ""
			raw, err := json.Marshal(wire)
			require.NoError(t, err)
			require.NotContains(t, string(raw), "ownerProcessID")
			require.NoError(t, f.db.Exec("UPDATE gb_device_operation_intent SET sip_steps_json=?", string(raw)).Error)
			loaded, err := store.LoadSIPInviteSteps(ctx, id)
			require.NoError(t, err)
			roundtrip, err := encodeSIPInviteSteps(loaded)
			require.NoError(t, err)
			require.Equal(t, raw, roundtrip)
			require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error)
			observed, err := store.ObserveSIPAdditionalBranch(ctx, id, loaded.Intent.RowVersion, sipExtraBranch(2))
			require.NoError(t, err, "missing process does not suppress late facts after transfer")
			require.Empty(t, observed.Steps[0].OwnerProcessID)
			require.Equal(t, loaded.Steps[0].KnownBranch, observed.Steps[0].KnownBranch)
			require.Len(t, observed.Steps[0].AdditionalBranches, len(loaded.Steps[0].AdditionalBranches)+1)
		})
	}
}

func TestDeviceSIPInviteProcessHelper(t *testing.T) {
	path := os.Getenv("UVP_SIP_INVITE_PROCESS_DB")
	if path == "" {
		return
	}
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	defer pool.Close()
	store, ctx, id := NewDeviceOperationIntentStore(db), context.Background(), intentIdentity(1)
	loaded, err := store.LoadSIPInviteSteps(ctx, id)
	require.NoError(t, err)
	out, err := store.DispatchSIPInviteStep(ctx, id, loaded.Intent.RowVersion, sipStepIdentity(1).StepID)
	require.ErrorIs(t, err, ErrDeviceIntentConflict, "another actual process cannot dispatch the prepared original INVITE")
	require.Empty(t, out.Intent.OperationID)
}

func TestDeviceSIPInviteActualOtherProcessCannotDispatch(t *testing.T) {
	f, store, id := newSIPStepFixture(t)
	_, err := store.AddSIPInviteStep(context.Background(), id, 2, sipStepIdentity(1))
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "invite.db")
	require.NoError(t, f.db.Exec("VACUUM INTO ?", path).Error)
	binary, err := os.Executable()
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "-test.run=^TestDeviceSIPInviteProcessHelper$", "-test.count=1")
	cmd.Env = append(os.Environ(), "UVP_SIP_INVITE_PROCESS_DB="+path)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", out)
}

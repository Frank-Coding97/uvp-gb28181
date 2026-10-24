package controllers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/app/utils/response"
	upgrade "uvplatform.com/uvp-gb28181/internal/upgrade"
)

func TestServiceUpgradePolicyEditInvalidatesWaitingAutomaticApproval(t *testing.T) {
	root := t.TempDir()
	t.Setenv("UVP_UPDATER_STATE", root)
	store, err := upgrade.OpenTaskStore(root)
	require.NoError(t, err)
	controller := newServiceUpgradeController(serviceUpgradeOptions{Store: store, VersionPath: filepath.Join(root, "version.json")})
	policy := upgrade.AutoUpgradePolicy{Enabled: true, PolicyVersion: 1, Timezone: "UTC", WindowStart: "02:00", WindowEnd: "04:00", Channel: "stable", AllowRestart: true}
	require.NoError(t, controller.policyStore.Save(policy))
	task, err := store.Submit(upgrade.TaskRequest{IdempotencyKey: "policy-task", Actor: "policy", TargetDigest: strings.Repeat("a", 64)})
	require.NoError(t, err)
	require.NoError(t, store.UpdateMetadata(task.TaskID, func(record *upgrade.TaskRecord) error {
		record.PolicyVersion = 1
		record.PolicySnapshot = &policy
		return nil
	}))
	for _, phase := range []upgrade.TaskPhase{upgrade.PhaseVerifying, upgrade.PhasePreparing, upgrade.PhaseAwaitingConfirmation} {
		require.NoError(t, store.MarkPhase(task.TaskID, phase))
	}
	oldResponse, oldLog := app.Response, app.ZapLog
	app.Response, app.ZapLog = response.NewResponseHandler(), zap.NewNop()
	t.Cleanup(func() { app.Response, app.ZapLog = oldResponse, oldLog })
	policy.WindowStart, policy.PolicyVersion = "03:00", 999
	body, err := json.Marshal(policy)
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/system/upgrade/policy", strings.NewReader(string(body)))
	ctx.Request.Header.Set("Content-Type", "application/json")
	controller.UpdatePolicy(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	saved, err := controller.policyStore.Load()
	require.NoError(t, err)
	require.Equal(t, uint64(2), saved.PolicyVersion)
	require.True(t, saved.Enabled)
	updated, err := store.Get(task.TaskID)
	require.NoError(t, err)
	require.Equal(t, upgrade.PhaseFailed, updated.Phase)
	// A later cancellation pass must preserve tasks approved by the new policy.
	next, err := store.Submit(upgrade.TaskRequest{IdempotencyKey: "new-policy-task", Actor: "policy", TargetDigest: strings.Repeat("b", 64)})
	require.NoError(t, err)
	require.NoError(t, store.UpdateMetadata(next.TaskID, func(record *upgrade.TaskRecord) error { record.PolicyVersion = saved.PolicyVersion; return nil }))
	controller.cancelPendingAutomaticTasks(context.Background(), saved.PolicyVersion)
	latest, err := store.Get(next.TaskID)
	require.NoError(t, err)
	require.Equal(t, upgrade.PhaseFetching, latest.Phase)
}

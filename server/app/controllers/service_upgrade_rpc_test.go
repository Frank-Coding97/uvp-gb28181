package controllers

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
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

type recordingUpgradeRPC struct {
	requests     []upgrade.ProtocolRequest
	task         upgrade.TaskRecord
	staged       *upgrade.StagedArtifact
	imported     *upgrade.ImportRecord
	importErr    error
	getImportErr error
	// watch is returned by the status action so the notice handler can be
	// exercised without a real helper.
	watch       *upgrade.UpdateWatchState
	onlineCheck bool
	statusErr   error
}

func (f *recordingUpgradeRPC) Call(_ context.Context, _ string, request upgrade.ProtocolRequest) (upgrade.ProtocolResponse, error) {
	f.requests = append(f.requests, request)
	response := upgrade.ProtocolResponse{Protocol: upgrade.UpdaterProtocolVersion, OK: true}
	switch request.Action {
	case upgrade.ActionStatus:
		if f.statusErr != nil {
			return upgrade.ProtocolResponse{}, f.statusErr
		}
		response.Status = &upgrade.ControlStatus{
			Deployment: "standalone", Tasks: []upgrade.TaskRecord{f.task}, ActiveTask: &f.task,
			OnlineCheck: f.onlineCheck, UpdateCheck: f.watch,
		}
	case upgrade.ActionCheck:
		response.Check = &upgrade.ControlCheckResult{CurrentVersion: "1.1.0", HasUpdate: true, Artifact: upgrade.Artifact{Filename: "uvp.tar.gz"}}
	case upgrade.ActionCreateTask, upgrade.ActionGetTask, upgrade.ActionCancel, upgrade.ActionApply, upgrade.ActionRollback:
		response.Task = &f.task
	case upgrade.ActionImport:
		if f.imported != nil && f.imported.InboxToken == "" {
			f.imported.InboxToken = request.InboxToken
		}
		if f.importErr != nil {
			return upgrade.ProtocolResponse{}, f.importErr
		}
		response.Staged = f.staged
		if f.imported != nil {
			response.Import = f.imported
		} else if f.staged == nil {
			response.Import = &upgrade.ImportRecord{ImportID: strings.Repeat("b", 32), InboxToken: request.InboxToken, Phase: upgrade.ImportPhaseQueued}
		}
	case upgrade.ActionGetImport:
		if f.getImportErr != nil {
			return upgrade.ProtocolResponse{}, f.getImportErr
		}
		if f.imported != nil {
			response.Import = f.imported
		} else {
			response.OK = false
			response.Error = "import not found"
		}
	}
	return response, nil
}

func TestProductionUpgradeControllerImportsThroughHelperInbox(t *testing.T) {
	root := t.TempDir()
	inbox := filepath.Join(root, "inbox")
	require.NoError(t, os.Mkdir(inbox, 0700))
	t.Setenv("UVP_UPGRADE_INBOX", inbox)
	t.Setenv("UVP_UPDATER_STATE", filepath.Join(root, "helper-state"))
	versionPath := filepath.Join(root, "version.json")
	require.NoError(t, os.WriteFile(versionPath, []byte(`{"name":"uvp-gb28181","version":"1.1.0"}`), 0600))
	fixture := &recordingUpgradeRPC{}
	controller := newServiceUpgradeController(serviceUpgradeOptions{VersionPath: versionPath, RPC: fixture})
	require.False(t, controller.local)

	body, contentType := serviceUpgradeMultipartBody(t, []byte("upload"))
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/system/upgrade/import", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", contentType)
	oldResponse, oldLog := app.Response, app.ZapLog
	app.Response, app.ZapLog = response.NewResponseHandler(), zap.NewNop()
	t.Cleanup(func() { app.Response, app.ZapLog = oldResponse, oldLog })

	invokeServiceUpgradeImport(controller, ctx)

	require.Equal(t, http.StatusAccepted, recorder.Code)
	require.Len(t, fixture.requests, 1)
	require.Equal(t, upgrade.ActionImport, fixture.requests[0].Action)
	token := fixture.requests[0].InboxToken
	require.Len(t, token, 32)
	_, err := hex.DecodeString(token)
	require.NoError(t, err)
	require.NotContains(t, token, "/")
	require.NotContains(t, token, "\\")
	require.JSONEq(t, `{"code":0,"message":"离线升级包已接收，正在后台校验","data":{"upload_id":"`+token+`","job_id":"`+strings.Repeat("b", 32)+`","phase":"queued","status":"queued","created_at":"","updated_at":""}}`, recorder.Body.String())
	require.FileExists(t, filepath.Join(inbox, token))
	_, err = os.Stat(controller.stagingRoot())
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestProductionUpgradeControllerReadsAsyncImportStatusFromHelper(t *testing.T) {
	root := t.TempDir()
	importID := strings.Repeat("d", 32)
	fixture := &recordingUpgradeRPC{imported: &upgrade.ImportRecord{
		ImportID: importID, Phase: upgrade.ImportPhaseSucceeded,
		Staged: &upgrade.StagedArtifact{Path: filepath.Join(root, "private", "artifact.tar"), Filename: "artifact.tar", Size: 11,
			SHA256: strings.Repeat("a", 64), ReleaseID: "release-2", Version: "1.2.0", BuildID: "build-2", Deployment: "standalone", OS: "linux", Arch: "amd64"},
	}}
	controller := newServiceUpgradeController(serviceUpgradeOptions{VersionPath: filepath.Join(root, "version.json"), RPC: fixture})
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/system/upgrade/import/"+importID, nil)
	ctx.Params = gin.Params{{Key: "upload_id", Value: importID}}
	oldResponse, oldLog := app.Response, app.ZapLog
	app.Response, app.ZapLog = response.NewResponseHandler(), zap.NewNop()
	t.Cleanup(func() { app.Response, app.ZapLog = oldResponse, oldLog })

	controller.GetImport(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Len(t, fixture.requests, 1)
	require.Equal(t, upgrade.ActionGetImport, fixture.requests[0].Action)
	require.Equal(t, importID, fixture.requests[0].ImportID)
	require.NotContains(t, recorder.Body.String(), filepath.Join(root, "private"))
	require.Contains(t, recorder.Body.String(), `"phase":"succeeded"`)
	require.Contains(t, recorder.Body.String(), `"sha256":"`+strings.Repeat("a", 64)+`"`)
}

func TestProductionUpgradeControllerCleansInboxWhenHelperRejectsImport(t *testing.T) {
	root := t.TempDir()
	inbox := filepath.Join(root, "inbox")
	require.NoError(t, os.Mkdir(inbox, 0700))
	t.Setenv("UVP_UPGRADE_INBOX", inbox)
	fixture := &recordingUpgradeRPC{importErr: errors.New("helper rejected archive")}
	controller := newServiceUpgradeController(serviceUpgradeOptions{VersionPath: filepath.Join(root, "version.json"), RPC: fixture})
	body, contentType := serviceUpgradeMultipartBody(t, []byte("upload"))
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/system/upgrade/import", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", contentType)
	oldResponse, oldLog := app.Response, app.ZapLog
	app.Response, app.ZapLog = response.NewResponseHandler(), zap.NewNop()
	t.Cleanup(func() { app.Response, app.ZapLog = oldResponse, oldLog })

	invokeServiceUpgradeImport(controller, ctx)

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Len(t, fixture.requests, 2)
	require.Equal(t, upgrade.ActionGetImport, fixture.requests[1].Action)
	entries, err := os.ReadDir(inbox)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestProductionUpgradeControllerRecoversAcceptedImportAfterLostRPCResponse(t *testing.T) {
	root := t.TempDir()
	inbox := filepath.Join(root, "inbox")
	require.NoError(t, os.Mkdir(inbox, 0700))
	t.Setenv("UVP_UPGRADE_INBOX", inbox)
	fixture := &recordingUpgradeRPC{
		importErr: errors.New("updater RPC response was lost"),
		imported:  &upgrade.ImportRecord{ImportID: strings.Repeat("a", 32), Phase: upgrade.ImportPhaseVerifying},
	}
	controller := newServiceUpgradeController(serviceUpgradeOptions{VersionPath: filepath.Join(root, "version.json"), RPC: fixture})
	body, contentType := serviceUpgradeMultipartBody(t, []byte("upload"))
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/system/upgrade/import", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", contentType)
	oldResponse, oldLog := app.Response, app.ZapLog
	app.Response, app.ZapLog = response.NewResponseHandler(), zap.NewNop()
	t.Cleanup(func() { app.Response, app.ZapLog = oldResponse, oldLog })

	invokeServiceUpgradeImport(controller, ctx)

	require.Equal(t, http.StatusAccepted, recorder.Code)
	require.Len(t, fixture.requests, 2)
	require.Equal(t, upgrade.ActionImport, fixture.requests[0].Action)
	require.Equal(t, upgrade.ActionGetImport, fixture.requests[1].Action)
	token := fixture.requests[0].InboxToken
	require.Equal(t, token, fixture.requests[1].ImportID)
	require.FileExists(t, filepath.Join(inbox, token))
	require.Contains(t, recorder.Body.String(), `"job_id":"`+strings.Repeat("a", 32)+`"`)
}

func TestProductionUpgradeControllerKeepsInboxWhenAcceptanceCannotBeConfirmed(t *testing.T) {
	root := t.TempDir()
	inbox := filepath.Join(root, "inbox")
	require.NoError(t, os.Mkdir(inbox, 0700))
	t.Setenv("UVP_UPGRADE_INBOX", inbox)
	fixture := &recordingUpgradeRPC{
		importErr:    errors.New("updater RPC response timed out"),
		getImportErr: errors.New("updater status probe timed out"),
	}
	controller := newServiceUpgradeController(serviceUpgradeOptions{VersionPath: filepath.Join(root, "version.json"), RPC: fixture})
	body, contentType := serviceUpgradeMultipartBody(t, []byte("upload"))
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/system/upgrade/import", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", contentType)
	oldResponse, oldLog := app.Response, app.ZapLog
	app.Response, app.ZapLog = response.NewResponseHandler(), zap.NewNop()
	t.Cleanup(func() { app.Response, app.ZapLog = oldResponse, oldLog })

	invokeServiceUpgradeImport(controller, ctx)

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Len(t, fixture.requests, 2)
	token := fixture.requests[0].InboxToken
	require.FileExists(t, filepath.Join(inbox, token))
}

func TestProductionUpgradeControllerCleansInterruptedUploadWithoutRPC(t *testing.T) {
	root := t.TempDir()
	inbox := filepath.Join(root, "inbox")
	require.NoError(t, os.Mkdir(inbox, 0700))
	t.Setenv("UVP_UPGRADE_INBOX", inbox)
	fixture := &recordingUpgradeRPC{}
	controller := newServiceUpgradeController(serviceUpgradeOptions{VersionPath: filepath.Join(root, "version.json"), RPC: fixture})
	body, contentType := serviceUpgradeMultipartBody(t, []byte("upload"))
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/system/upgrade/import", strings.NewReader(""))
	ctx.Request.Body = io.NopCloser(&interruptedUploadReader{data: body, limit: len(body) / 2})
	ctx.Request.Header.Set("Content-Type", contentType)
	oldResponse, oldLog := app.Response, app.ZapLog
	app.Response, app.ZapLog = response.NewResponseHandler(), zap.NewNop()
	t.Cleanup(func() { app.Response, app.ZapLog = oldResponse, oldLog })

	invokeServiceUpgradeImport(controller, ctx)

	require.NotEqual(t, http.StatusOK, recorder.Code)
	require.Empty(t, fixture.requests)
	entries, err := os.ReadDir(inbox)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func serviceUpgradeMultipartBody(t *testing.T, data []byte) ([]byte, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "upgrade.tar")
	require.NoError(t, err)
	_, err = part.Write(data)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return body.Bytes(), writer.FormDataContentType()
}

type interruptedUploadReader struct {
	data   []byte
	offset int
	limit  int
}

func invokeServiceUpgradeImport(controller *ServiceUpgradeController, ctx *gin.Context) {
	defer func() { _ = recover() }()
	controller.Import(ctx)
}

func (r *interruptedUploadReader) Read(buffer []byte) (int, error) {
	if r.offset >= r.limit {
		return 0, errors.New("upload interrupted")
	}
	end := r.offset + len(buffer)
	if end > r.limit {
		end = r.limit
	}
	n := copy(buffer, r.data[r.offset:end])
	r.offset += n
	return n, nil
}

func TestProductionUpgradeControllerRoutesStateOperationsToHelperRPC(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "updater-state")
	t.Setenv("UVP_UPDATER_STATE", state)
	versionPath := filepath.Join(root, "version.json")
	require.NoError(t, os.WriteFile(versionPath, []byte(`{"name":"uvp-gb28181","version":"1.1.0"}`), 0600))
	fixture := &recordingUpgradeRPC{task: upgrade.TaskRecord{TaskID: strings.Repeat("a", 32), TargetDigest: strings.Repeat("b", 64), Phase: upgrade.PhaseAwaitingConfirmation}}
	controller := newServiceUpgradeController(serviceUpgradeOptions{VersionPath: versionPath, RPC: fixture})
	require.False(t, controller.local)
	require.Nil(t, controller.store)
	_, err := os.Stat(state)
	require.ErrorIs(t, err, os.ErrNotExist)

	oldResponse, oldLog := app.Response, app.ZapLog
	app.Response, app.ZapLog = response.NewResponseHandler(), zap.NewNop()
	t.Cleanup(func() { app.Response, app.ZapLog = oldResponse, oldLog })

	newContext := func(method, path string, body any, id string) (*gin.Context, *httptest.ResponseRecorder) {
		var reader *strings.Reader
		if body == nil {
			reader = strings.NewReader("")
		} else {
			data, marshalErr := json.Marshal(body)
			require.NoError(t, marshalErr)
			reader = strings.NewReader(string(data))
		}
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(method, path, reader)
		ctx.Request.Header.Set("Content-Type", "application/json")
		if id != "" {
			ctx.Params = gin.Params{{Key: "id", Value: id}}
		}
		return ctx, recorder
	}

	ctx, _ := newContext(http.MethodGet, "/api/system/upgrade", nil, "")
	controller.Status(ctx)
	ctx, _ = newContext(http.MethodPost, "/api/system/upgrade/check", nil, "")
	controller.Check(ctx)
	ctx, _ = newContext(http.MethodPost, "/api/system/upgrade/tasks", map[string]string{"idempotencyKey": "rpc-test", "targetDigest": strings.Repeat("c", 64)}, "")
	controller.CreateTask(ctx)
	ctx, _ = newContext(http.MethodGet, "/api/system/upgrade/tasks/"+fixture.task.TaskID, nil, fixture.task.TaskID)
	controller.GetTask(ctx)
	ctx, _ = newContext(http.MethodPost, "/api/system/upgrade/tasks/"+fixture.task.TaskID+"/cancel", nil, fixture.task.TaskID)
	controller.CancelTask(ctx)
	ctx, _ = newContext(http.MethodPost, "/api/system/upgrade/tasks/"+fixture.task.TaskID+"/apply", nil, fixture.task.TaskID)
	controller.ApplyTask(ctx)
	ctx, _ = newContext(http.MethodPost, "/api/system/upgrade/tasks/"+fixture.task.TaskID+"/rollback", nil, fixture.task.TaskID)
	controller.RollbackTask(ctx)

	actions := make([]upgrade.Action, 0, len(fixture.requests))
	for _, request := range fixture.requests {
		actions = append(actions, request.Action)
	}
	require.Equal(t, []upgrade.Action{
		upgrade.ActionStatus,
		upgrade.ActionCheck,
		upgrade.ActionCreateTask,
		upgrade.ActionGetTask,
		upgrade.ActionCancel,
		upgrade.ActionGetTask,
		upgrade.ActionApply,
		upgrade.ActionGetTask,
		upgrade.ActionRollback,
	}, actions)
}

func TestProductionUpgradeStatusUsesHelperOnlineCapability(t *testing.T) {
	root := t.TempDir()
	fixture := &recordingUpgradeRPC{}
	controller := newServiceUpgradeController(serviceUpgradeOptions{VersionPath: filepath.Join(root, "version.json"), RPC: fixture})
	oldResponse, oldLog := app.Response, app.ZapLog
	app.Response, app.ZapLog = response.NewResponseHandler(), zap.NewNop()
	t.Cleanup(func() { app.Response, app.ZapLog = oldResponse, oldLog })
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/system/upgrade/status", nil)
	controller.Status(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	var payload struct {
		Data struct {
			Capabilities struct {
				OnlineCheck bool `json:"onlineCheck"`
			} `json:"capabilities"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	require.False(t, payload.Data.Capabilities.OnlineCheck)
}

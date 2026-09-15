package controllers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/service"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/global/consts"
	"uvplatform.cn/uvp-gb28181/app/middleware"
)

type t14AuditResponse struct{}

func (t14AuditResponse) ReturnJson(c *gin.Context, httpCode, dataCode int, message string, data interface{}) {
	c.JSON(httpCode, gin.H{"code": dataCode, "data": data, "message": message})
}

func (t14AuditResponse) Success(c *gin.Context, data ...interface{}) {
	var payload interface{}
	if len(data) > 0 {
		payload = data[0]
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": payload, "message": "ok"})
}

func (t14AuditResponse) Fail(c *gin.Context, message string, _ ...interface{}) {
	c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": message})
}

func (t14AuditResponse) ErrorSystem(c *gin.Context, message string, data interface{}) {
	c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "data": data, "message": message})
}

type t14AuditRepo struct {
	row    node.Node
	exists bool
}

func (r *t14AuditRepo) List(context.Context) ([]node.Node, error) {
	if !r.exists {
		return nil, nil
	}
	return []node.Node{r.row}, nil
}

func (r *t14AuditRepo) Get(_ context.Context, id int64) (*node.Node, error) {
	if !r.exists || r.row.ID != id {
		return nil, nil
	}
	copy := r.row
	return &copy, nil
}

func (r *t14AuditRepo) Create(_ context.Context, candidate node.Node) (int64, error) {
	candidate.ID = 7
	r.row = candidate
	r.exists = true
	return candidate.ID, nil
}

func (r *t14AuditRepo) Update(_ context.Context, candidate node.Node) error {
	r.row = candidate
	r.exists = true
	return nil
}

func (r *t14AuditRepo) Delete(context.Context, int64) error {
	r.exists = false
	return nil
}

type t14AuditProbe struct{}

func (t14AuditProbe) GetServerConfig(context.Context, *node.Node) (map[string]string, error) {
	return map[string]string{"http.port": "80"}, nil
}

func (t14AuditProbe) ApplyConfigForNode(context.Context, *node.Node, service.MediaTuning) error {
	return nil
}

func (t14AuditProbe) KickSessions(context.Context, *node.Node) (int, error) { return 0, nil }

func (t14AuditProbe) RestartServer(context.Context, *node.Node, int) error { return nil }

func invokeT14AuditHandler(t *testing.T, method, path, body string, params gin.Params, handler gin.HandlerFunc) (*gin.Context, map[string]any) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	previousResponse := app.Response
	previousLogger := app.ZapLog
	app.Response = t14AuditResponse{}
	app.ZapLog = zap.NewNop()
	t.Cleanup(func() {
		app.Response = previousResponse
		app.ZapLog = previousLogger
	})

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Params = params
	ctx.Request = httptest.NewRequest(method, path, strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	func() {
		defer func() {
			if recovered := recover(); recovered != nil && recovered != consts.RequestAborted {
				panic(recovered)
			}
		}()
		handler(ctx)
	}()
	metadata, marked := middleware.SensitiveOperationMetadata(ctx)
	require.True(t, marked)
	return ctx, metadata
}

func TestZLMLegacyAuditT14_MarksSensitiveBodiesBeforeBinding(t *testing.T) {
	tests := []struct {
		name    string
		action  string
		body    string
		params  gin.Params
		handler gin.HandlerFunc
	}{
		{
			name:    "node create",
			action:  "node.create",
			body:    `{"apiSecret":"node-super-secret"`,
			handler: (&ZLMNodeController{}).Create,
		},
		{
			name:    "node update",
			action:  "node.update",
			body:    `{"apiSecret":"replacement-super-secret"`,
			params:  gin.Params{{Key: "id", Value: "7"}},
			handler: (&ZLMNodeController{}).Update,
		},
		{
			name:    "config update",
			action:  "config.update",
			body:    `{"changes":{"hook.timeoutSec":"config-super-secret"}`,
			params:  gin.Params{{Key: "id", Value: "7"}},
			handler: (&ZLMConfigController{}).Update,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, metadata := invokeT14AuditHandler(t, http.MethodPut, "/nodes/7", tt.body, tt.params, tt.handler)
			require.Equal(t, tt.action, metadata["action"])
			require.Equal(t, "failed", metadata["result"])
			if len(tt.params) > 0 {
				require.Equal(t, int64(7), metadata["nodeId"])
			}
			encoded, err := json.Marshal(metadata)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "super-secret")
		})
	}
}

func TestZLMLegacyAuditT14_RestartAcceptedHasBoundedMetadata(t *testing.T) {
	repo := &t14AuditRepo{}
	registry := node.NewRegistry(repo)
	created, err := registry.Add(context.Background(), node.Node{
		Name: "node", Host: "127.0.0.1", APIPort: 8080, APISecret: "node-secret", State: node.StateActive,
	})
	require.NoError(t, err)
	nodeService := service.NewNodeService(registry, t14AuditProbe{}, service.MediaTuning{})
	t.Cleanup(func() { nodeService.RestartNotifier().(*service.RestartCoordinator).Close() })
	controller := NewZLMNodeController(nodeService)

	_, metadata := invokeT14AuditHandler(t, http.MethodPost, "/nodes/7/restart", `{"graceMS":20}`, gin.Params{{Key: "id", Value: "7"}}, controller.Restart)
	require.Equal(t, "node.restart", metadata["action"])
	require.Equal(t, created.ID, metadata["nodeId"])
	require.Equal(t, "accepted", metadata["result"])
	encoded, err := json.Marshal(metadata)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "node-secret")
}

func TestZLMNodeControllerT14_RestartStatusSupportsOperationPolling(t *testing.T) {
	repo := &t14AuditRepo{}
	registry := node.NewRegistry(repo)
	created, err := registry.Add(context.Background(), node.Node{
		Name: "node", Host: "127.0.0.1", APIPort: 8080, APISecret: "node-secret", State: node.StateActive,
	})
	require.NoError(t, err)
	nodeService := service.NewNodeService(registry, t14AuditProbe{}, service.MediaTuning{})
	t.Cleanup(func() { nodeService.RestartNotifier().(*service.RestartCoordinator).Close() })
	accepted, err := nodeService.RestartAccepted(context.Background(), created.ID, 0)
	require.NoError(t, err)
	controller := NewZLMNodeController(nodeService)

	previousResponse := app.Response
	app.Response = t14AuditResponse{}
	t.Cleanup(func() { app.Response = previousResponse })
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: "7"}}
	ctx.Request = httptest.NewRequest(http.MethodGet, "/nodes/7/restart?operationId="+accepted.OperationID, nil)
	controller.RestartStatus(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var envelope struct {
		Data service.RestartOperation `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.Equal(t, accepted.OperationID, envelope.Data.OperationID)
	require.Equal(t, service.RestartStatusWaitingOffline, envelope.Data.Status)

	unknownRecorder := httptest.NewRecorder()
	unknownContext, _ := gin.CreateTestContext(unknownRecorder)
	unknownContext.Params = gin.Params{{Key: "id", Value: "7"}}
	unknownContext.Request = httptest.NewRequest(http.MethodGet, "/nodes/7/restart?operationId=00000000-0000-4000-8000-000000000001", nil)
	controller.RestartStatus(unknownContext)
	require.Equal(t, http.StatusOK, unknownRecorder.Code)
	require.Contains(t, unknownRecorder.Body.String(), `"status":"unknown"`)
	require.NotContains(t, unknownRecorder.Body.String(), "node-secret")
}

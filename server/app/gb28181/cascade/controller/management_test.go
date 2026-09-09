package controller

import (
	"encoding/json"
	"github.com/go-sql-driver/mysql"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"net/http"
	"net/http/httptest"
	"testing"
	"uvplatform.cn/uvp-gb28181/app/global/app"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/repository"
)

func TestManagementControllerUnconfiguredReturns503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	controller := NewManagementController(nil, nil)
	router.GET("/platforms", controller.List)
	router.GET("/platforms/:id/shares", controller.GetShares)

	for _, path := range []string{"/platforms", "/platforms/1/shares"} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		router.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusServiceUnavailable, recorder.Code, path)
	}
}

func TestShareRequestScopesAndEmptyChannelsCancelProjection(t *testing.T) {
	request := shareRequest{
		Scope:    "channels",
		Devices:  []shareDevice{{SourceDeviceID: 7, PublishedDeviceID: "34020000001320000007"}},
		Channels: []shareChannel{{SourceDeviceID: 7, SourceChannelID: 8, PublishedChannelID: "34020000001320000008"}},
	}
	devices, channels, err := request.projection()
	require.NoError(t, err)
	require.Empty(t, devices)
	require.Len(t, channels, 1)

	devices, channels, err = (shareRequest{}).projection()
	require.NoError(t, err)
	require.Empty(t, devices)
	require.Empty(t, channels)

	_, _, err = (shareRequest{Scope: "unknown"}).projection()
	require.ErrorIs(t, err, repository.ErrInvalidProjection)
}

func TestShareRequestPreservesPTZAndPublishedIdentity(t *testing.T) {
	ptz := true
	devices, channels, err := (shareRequest{
		Scope:    "all",
		Devices:  []shareDevice{{SourceDeviceID: 1, PublishedDeviceID: "34020000001320000001", Name: "device"}},
		Channels: []shareChannel{{SourceDeviceID: 1, SourceChannelID: 2, PublishedChannelID: "34020000001320000002", PTZAllowed: &ptz}},
	}).projection()
	require.NoError(t, err)
	require.Equal(t, "34020000001320000001", devices[0].PublishedDeviceID)
	require.Equal(t, "34020000001320000002", channels[0].PublishedChannelID)
	require.True(t, channels[0].PTZAllowed)
}

func TestShareResponseIncludesChannelSourceDeviceID(t *testing.T) {
	response := buildShareResponse(3, &repository.ProjectionSnapshot{
		Revision: 5,
		Devices:  []model.GbCascadeDeviceProjection{{ID: 7, SourceDeviceID: 11}},
		Channels: []model.GbCascadeChannelProjection{{ID: 9, DeviceProjectionID: 7, SourceChannelID: 13}},
	})

	require.EqualValues(t, 11, response.Channels[0].SourceDeviceID)
	payload, err := json.Marshal(response)
	require.NoError(t, err)
	require.JSONEq(t, `{"platformId":3,"revision":5,"devices":[{"id":7,"platformId":0,"sourceDeviceId":11,"publishedDeviceId":"","name":"","manufacturer":"","model":"","owner":"","civilCode":"","address":"","parental":0,"secrecy":0,"active":false,"revision":0,"createdAt":"0001-01-01T00:00:00Z","updatedAt":"0001-01-01T00:00:00Z"}],"channels":[{"id":9,"platformId":0,"deviceProjectionId":7,"sourceChannelId":13,"publishedChannelId":"","name":"","parentOverride":"","ptzAllowed":false,"active":false,"revision":0,"createdAt":"0001-01-01T00:00:00Z","updatedAt":"0001-01-01T00:00:00Z","sourceDeviceId":11}]}`, string(payload))
}

func TestPlatformRequestUsesCamelCaseAndKeepsPasswordWriteOnly(t *testing.T) {
	var request platformRequest
	err := json.Unmarshal([]byte(`{"name":"upstream","upstreamServerId":"34020000002000000001","localDeviceId":"34020000002000000009","expectedRevision":4,"password":"secret"}`), &request)
	require.NoError(t, err)
	require.Equal(t, "upstream", request.Name)
	require.Equal(t, "34020000002000000001", request.UpstreamServerID)
	require.EqualValues(t, 4, request.ExpectedRevision)
	require.NotNil(t, request.Password)
	require.Equal(t, "secret", *request.Password)
}

func TestManagementFailureLogsDuplicateWithoutFieldValues(t *testing.T) {
	core, logs := observer.New(zap.ErrorLevel)
	old := app.ZapLog
	app.ZapLog = zap.New(core)
	t.Cleanup(func() { app.ZapLog = old })
	router := gin.New()
	router.POST("/platforms", func(ctx *gin.Context) {
		(&ManagementController{}).fail(ctx, &mysql.MySQLError{Number: 1062, Message: "duplicate sensitive-value"})
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/platforms", nil))
	require.Equal(t, 409, recorder.Code)
	require.Contains(t, recorder.Body.String(), "平台名称或上级接入关系")
	require.Equal(t, 1, logs.Len())
	require.Equal(t, uint16(1062), logs.All()[0].ContextMap()["mysql_errno"])
	require.NotContains(t, recorder.Body.String(), "sensitive-value")
	require.NotContains(t, logs.All()[0].ContextMap(), "error")
}

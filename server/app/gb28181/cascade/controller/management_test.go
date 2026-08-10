package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
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

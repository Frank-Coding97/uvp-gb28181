package controllers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	sqlite "uvplatform.cn/uvp-gb28181/internal/sqlitedialect"

	gbcontrollers "uvplatform.cn/uvp-gb28181/app/gb28181/controllers"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func TestDeviceMgmt_GetPTZStateAndOperation(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &gbmodels.GbPTZState{}, &gbmodels.GbPTZOperation{}))
	device := &gbmodels.GbDevice{DeviceID: "D", Status: gbmodels.DeviceStatusOnline}
	require.NoError(t, db.Create(device).Error)
	channel := &gbmodels.GbChannel{DeviceID: "D", ChannelID: "C", Status: gbmodels.ChannelStatusOnline, PTZType: 1}
	require.NoError(t, db.Create(channel).Error)
	pan := 12.5
	require.NoError(t, db.Create(&gbmodels.GbPTZState{DeviceID: device.ID, ChannelID: channel.ID, ChannelCode: "C", Pan: &pan, Freshness: gbmodels.PTZFreshnessFresh}).Error)
	require.NoError(t, db.Create(&gbmodels.GbPTZOperation{OperationID: "op-1", IdempotencyKey: "k", DeviceID: device.ID, DeviceCode: "D", ChannelID: channel.ID, ChannelCode: "C", CmdType: "DeviceControl", Status: gbmodels.PTZOperationSent}).Error)
	controller := gbcontrollers.NewDeviceMgmtController()
	controller.SetDB(func() *gorm.DB { return db })
	r := gin.New()
	r.GET("/channel/:id/ptz/precise-status", controller.GetPTZState)
	r.GET("/channel/:id/ptz/operations/:operationId", controller.GetPTZOperation)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/channel/"+uintStr(channel.ID)+"/ptz/precise-status", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "12.5")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/channel/"+uintStr(channel.ID)+"/ptz/operations/op-1", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.True(t, strings.Contains(w.Body.String(), "op-1"))
}

func TestDeviceMgmt_GetCruiseTrackAcceptsNumberZero(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &gbmodels.GbPTZCruiseTrack{}))
	device := &gbmodels.GbDevice{DeviceID: "D", Status: gbmodels.DeviceStatusOnline}
	require.NoError(t, db.Create(device).Error)
	channel := &gbmodels.GbChannel{DeviceID: "D", ChannelID: "C", Status: gbmodels.ChannelStatusOnline, PTZType: 1}
	require.NoError(t, db.Create(channel).Error)
	enabled := true
	require.NoError(t, db.Create(&gbmodels.GbPTZCruiseTrack{
		DeviceID: device.ID, ChannelID: channel.ID, TrackID: 0, Name: "第一条轨迹", Enabled: &enabled, UpdatedAt: time.Now(),
	}).Error)

	controller := gbcontrollers.NewDeviceMgmtController()
	controller.SetDB(func() *gorm.DB { return db })
	router := gin.New()
	router.GET("/channel/:id/ptz/cruise-tracks/:trackId", controller.GetCruiseTrack)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet,
		"/channel/"+uintStr(channel.ID)+"/ptz/cruise-tracks/0", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"trackId":0`)
}

func homePositionQueryFixture(t *testing.T) homePositionPatchFixture {
	t.Helper()
	return homePositionQueryFixtureForActor(t, "dept")
}

func homePositionQueryFixtureForActor(t *testing.T, actorMode string) homePositionPatchFixture {
	t.Helper()
	fixture := newHomePositionPatchFixture(t, actorMode)
	fixture.router.GET("/channel/:id/ptz/home-position", fixture.controller.GetPTZHomePosition)
	fixture.router.GET("/channel/:id/ptz/operations/:operationId", fixture.controller.GetPTZOperation)
	return fixture
}

func getHomePosition(t *testing.T, fixture homePositionPatchFixture, refresh bool, idempotencyKey string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/channel/"+uintStr(fixture.channel.ID)+"/ptz/home-position", nil)
	if refresh {
		request.URL.RawQuery = "refresh=true"
	}
	if idempotencyKey != "" {
		request.Header.Set("Idempotency-Key", idempotencyKey)
	}
	response := httptest.NewRecorder()
	fixture.router.ServeHTTP(response, request)
	return response
}

func homePositionReadData(t *testing.T, recorder *httptest.ResponseRecorder) map[string]json.RawMessage {
	t.Helper()
	var envelope struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope), recorder.Body.String())
	return envelope.Data
}

func homePositionRefreshOperationID(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	data := homePositionReadData(t, recorder)
	var refresh struct {
		OperationID *string `json:"operationId"`
	}
	require.NoError(t, json.Unmarshal(data["refresh"], &refresh), recorder.Body.String())
	require.NotNil(t, refresh.OperationID, recorder.Body.String())
	return *refresh.OperationID
}

func TestDeviceMgmtGetHomePositionReturnsDedicatedUnknownReadModelWithoutRefresh(t *testing.T) {
	fixture := homePositionQueryFixture(t)
	result := getHomePosition(t, fixture, false, "")
	require.Equal(t, http.StatusOK, result.Code, result.Body.String())
	data := homePositionReadData(t, result)
	var home interface{}
	require.NoError(t, json.Unmarshal(data["homePosition"], &home))
	require.Nil(t, home)
	var freshness string
	require.NoError(t, json.Unmarshal(data["freshness"], &freshness))
	require.Equal(t, "unknown", freshness)
	var control, refresh struct {
		Status string `json:"status"`
	}
	require.NoError(t, json.Unmarshal(data["control"], &control))
	require.NoError(t, json.Unmarshal(data["refresh"], &refresh))
	require.Equal(t, "idle", control.Status)
	require.Equal(t, "idle", refresh.Status)
	require.Contains(t, result.Body.String(), `"controlSupport"`)
	require.Contains(t, result.Body.String(), `"querySupport"`)
	require.Zero(t, homePositionOperationCount(t, fixture.db))
	require.Zero(t, sentHomePositionMessages(fixture.sender))
	require.NotContains(t, result.Body.String(), `"pan"`)
	require.NotContains(t, result.Body.String(), `"tilt"`)
	require.NotContains(t, result.Body.String(), `"zoom"`)
}

func TestDeviceMgmtGetHomePositionPreservesNullableAndZeroValues(t *testing.T) {
	fixture := homePositionQueryFixture(t)
	resetTime, presetID := 0, 255
	require.NoError(t, fixture.db.Create(&gbmodels.GbPTZHomePosition{
		DeviceID: fixture.channel.ID, ChannelID: fixture.channel.ID, ChannelCode: fixture.channel.ChannelID,
		Enabled: true, ResetTime: &resetTime, PresetID: &presetID,
		ConfirmedAt: time.Now(), Source: gbmodels.PTZHomePositionSourceDeviceQuery,
		Verification: gbmodels.PTZHomePositionVerificationVerified,
	}).Error)
	result := getHomePosition(t, fixture, false, "")
	require.Equal(t, http.StatusOK, result.Code, result.Body.String())
	data := homePositionReadData(t, result)
	var home struct {
		Enabled   bool `json:"enabled"`
		ResetTime *int `json:"resetTime"`
		PresetID  *int `json:"presetId"`
	}
	require.NoError(t, json.Unmarshal(data["homePosition"], &home))
	require.True(t, home.Enabled)
	require.NotNil(t, home.ResetTime)
	require.Zero(t, *home.ResetTime)
	require.NotNil(t, home.PresetID)
	require.Equal(t, 255, *home.PresetID)
	require.NotContains(t, result.Body.String(), `"deviceId"`)
	require.NotContains(t, result.Body.String(), `"channelId"`)
	require.NotContains(t, result.Body.String(), `"sourceSn"`)
}

func TestDeviceMgmtGetHomePositionRefreshUsesActorHintsAndExactIdempotentOperation(t *testing.T) {
	fixture := homePositionQueryFixture(t)
	fixture.channel.Capabilities = homePositionStringValue(`{"home_position_query":false}`)
	require.NoError(t, fixture.db.Save(fixture.channel).Error)

	first := getHomePosition(t, fixture, true, "refresh-key")
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())
	operationID := homePositionRefreshOperationID(t, first)
	var original gbmodels.GbPTZOperation
	require.NoError(t, fixture.db.Where("operation_id = ?", operationID).First(&original).Error)
	require.Equal(t, gbmodels.PTZOperationQueued, original.Status)
	require.Equal(t, "refresh_home_position", original.Action)
	require.True(t, original.ResponseRequired)
	require.Equal(t, 3, original.MaxAttempts)
	require.Zero(t, original.Attempt)
	require.Equal(t, uint(100), original.ActorID)
	require.Equal(t, uint(10), original.ActorDeptID)

	// A newer unrelated query must not replace the operation returned for an
	// idempotent refresh replay.
	require.NoError(t, fixture.db.Create(&gbmodels.GbPTZOperation{
		OperationID: "newer-query", IdempotencyKey: "newer-query", DeviceID: original.DeviceID,
		DeviceCode: original.DeviceCode, ChannelID: original.ChannelID, ChannelCode: original.ChannelCode,
		CmdType: "HomePositionQuery", Action: "refresh_home_position", SN: original.SN + 1,
		Status: gbmodels.PTZOperationTimeout, Attempt: 1, MaxAttempts: 3, CreatedAt: time.Now(),
	}).Error)
	second := getHomePosition(t, fixture, true, "refresh-key")
	require.Equal(t, http.StatusOK, second.Code, second.Body.String())
	require.Equal(t, operationID, homePositionRefreshOperationID(t, second))
	require.EqualValues(t, 2, homePositionOperationCount(t, fixture.db))
	require.Zero(t, sentHomePositionMessages(fixture.sender))
	require.Contains(t, second.Body.String(), `"status":"unsupported"`)
}

func TestDeviceMgmtGetHomePositionRefreshCapabilitiesAreHintsOnly(t *testing.T) {
	tests := []struct {
		name         string
		capabilities *string
		status       string
	}{
		{name: "unknown", status: "unknown"},
		{name: "unsupported", capabilities: homePositionStringValue(`{"home_position_query":false}`), status: "unsupported"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := homePositionQueryFixture(t)
			fixture.channel.Capabilities = test.capabilities
			require.NoError(t, fixture.db.Save(fixture.channel).Error)
			result := getHomePosition(t, fixture, true, "refresh-"+test.name)
			require.Equal(t, http.StatusOK, result.Code, result.Body.String())
			require.NotEmpty(t, homePositionRefreshOperationID(t, result))
			require.EqualValues(t, 1, homePositionOperationCount(t, fixture.db))
			require.Contains(t, result.Body.String(), `"status":"`+test.status+`"`)
		})
	}
}

func TestDeviceMgmtGetHomePositionRefreshMapsOfflineAndActorFailures(t *testing.T) {
	t.Run("offline", func(t *testing.T) {
		fixture := homePositionQueryFixture(t)
		fixture.channel.Status = gbmodels.ChannelStatusOffline
		require.NoError(t, fixture.db.Save(fixture.channel).Error)
		result := getHomePosition(t, fixture, true, "offline-refresh")
		require.Equal(t, http.StatusConflict, result.Code, result.Body.String())
		require.Equal(t, "HOME_POSITION_DEVICE_OFFLINE", homePositionErrorCode(t, result))
		require.Zero(t, homePositionOperationCount(t, fixture.db))
	})

	t.Run("missing actor", func(t *testing.T) {
		fixture := homePositionQueryFixtureForActor(t, "missing")
		result := getHomePosition(t, fixture, true, "missing-actor-refresh")
		require.Equal(t, http.StatusInternalServerError, result.Code, result.Body.String())
		require.Equal(t, "HOME_POSITION_INTERNAL_ERROR", homePositionErrorCode(t, result))
		require.Zero(t, homePositionOperationCount(t, fixture.db))
	})

	t.Run("service unavailable", func(t *testing.T) {
		fixture := homePositionQueryFixture(t)
		fixture.controller.SetPTZService(nil)
		result := getHomePosition(t, fixture, false, "")
		require.Equal(t, http.StatusServiceUnavailable, result.Code, result.Body.String())
		require.Equal(t, "HOME_POSITION_UNAVAILABLE", homePositionErrorCode(t, result))
		require.Zero(t, homePositionOperationCount(t, fixture.db))
	})
}

func TestDeviceMgmtGetPTZOperationBindsURLChannelAndWhitelistsDeadline(t *testing.T) {
	fixture := homePositionQueryFixture(t)
	otherChannel := &gbmodels.GbChannel{OwnerDeptID: 10, DeviceID: "D", ChannelID: "C2", Status: gbmodels.ChannelStatusOnline}
	require.NoError(t, fixture.db.Create(otherChannel).Error)
	queueDeadline := time.Now().Add(5 * time.Minute).UTC().Truncate(time.Second)
	transportDeadline := queueDeadline.Add(time.Minute)
	applicationDeadline := queueDeadline.Add(2 * time.Minute)
	completed := queueDeadline.Add(-time.Minute)
	operation := &gbmodels.GbPTZOperation{
		OperationID: "op-whitelist", IdempotencyKey: "op-whitelist", DeviceID: 1, DeviceCode: "D",
		ChannelID: fixture.channel.ID, ChannelCode: fixture.channel.ChannelID, CmdType: "HomePositionQuery",
		Action: "refresh_home_position", Status: gbmodels.PTZOperationAccepted, Attempt: 1, MaxAttempts: 3,
		ErrorCode: "DEVICE_REJECTED", ErrorMessage: "device message", CreatedAt: time.Now(),
		CompletedAt: &completed, QueueDeadlineAt: &queueDeadline, TransportDeadlineAt: &transportDeadline,
		DeadlineAt: &applicationDeadline,
	}
	require.NoError(t, fixture.db.Create(operation).Error)

	request := httptest.NewRequest(http.MethodGet, "/channel/"+uintStr(fixture.channel.ID)+"/ptz/operations/"+operation.OperationID, nil)
	result := httptest.NewRecorder()
	fixture.router.ServeHTTP(result, request)
	require.Equal(t, http.StatusOK, result.Code, result.Body.String())
	var envelope struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(result.Body.Bytes(), &envelope), result.Body.String())
	keys := make([]string, 0, len(envelope.Data))
	for key := range envelope.Data {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	require.Equal(t, []string{"completedAt", "deadlineAt", "errorCode", "errorMessage", "operationId", "status"}, keys)
	require.NotContains(t, result.Body.String(), `"actorId"`)
	require.NotContains(t, result.Body.String(), `"channelId"`)
	require.NotContains(t, result.Body.String(), `"sipStatus"`)
	require.Contains(t, result.Body.String(), applicationDeadline.Format(time.RFC3339))

	wrongChannel := httptest.NewRecorder()
	fixture.router.ServeHTTP(wrongChannel, httptest.NewRequest(http.MethodGet,
		"/channel/"+uintStr(otherChannel.ID)+"/ptz/operations/"+operation.OperationID, nil))
	require.Equal(t, http.StatusNotFound, wrongChannel.Code, wrongChannel.Body.String())
	require.Equal(t, "HOME_POSITION_NOT_FOUND", homePositionErrorCode(t, wrongChannel))

	foreignChannel := &gbmodels.GbChannel{OwnerDeptID: 20, DeviceID: "D", ChannelID: "C3", Status: gbmodels.ChannelStatusOnline}
	require.NoError(t, fixture.db.Create(foreignChannel).Error)
	foreignOperation := &gbmodels.GbPTZOperation{
		OperationID: "foreign-op", IdempotencyKey: "foreign-op", DeviceID: 1, DeviceCode: "D",
		ChannelID: foreignChannel.ID, ChannelCode: foreignChannel.ChannelID, CmdType: "HomePositionQuery",
		Status: gbmodels.PTZOperationQueued, CreatedAt: time.Now(),
	}
	require.NoError(t, fixture.db.Create(foreignOperation).Error)
	foreign := httptest.NewRecorder()
	fixture.router.ServeHTTP(foreign, httptest.NewRequest(http.MethodGet,
		"/channel/"+uintStr(foreignChannel.ID)+"/ptz/operations/"+foreignOperation.OperationID, nil))
	require.Equal(t, http.StatusNotFound, foreign.Code, foreign.Body.String())
	require.Equal(t, "HOME_POSITION_NOT_FOUND", homePositionErrorCode(t, foreign))
}

package controllers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbcontrollers "uvplatform.cn/uvp-gb28181/app/gb28181/controllers"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	basemodels "uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/utils/response"
)

type homePositionPatchFixture struct {
	router     *gin.Engine
	db         *gorm.DB
	channel    *gbmodels.GbChannel
	sender     *resourcePTZSender
	controller *gbcontrollers.DeviceMgmtController
}

type homePositionSkipUserConfig struct{ scopedTestConfig }

func (homePositionSkipUserConfig) GetUintSlice(key string) []uint {
	if key == "server.notcheckuser" {
		return []uint{100}
	}
	return nil
}

func newHomePositionPatchFixture(t *testing.T, actorMode string) homePositionPatchFixture {
	t.Helper()
	db := newScopedDeviceDB(t)
	require.NoError(t, db.AutoMigrate(
		&gbmodels.GbPTZOperation{},
		&gbmodels.GbPTZOperationAttempt{},
		&gbmodels.GbPTZHomePosition{},
	))
	app.Response = response.NewResponseHandler()

	switch actorMode {
	case "dept":
		seedDeptScopedUser(t, db, 100, 10)
	case "other-dept":
		seedDeptScopedUser(t, db, 100, 20)
	case "zero":
		app.ConfigYml = homePositionSkipUserConfig{}
		require.NoError(t, db.Create(&basemodels.User{
			BaseModel: basemodels.BaseModel{ID: 100}, Username: "root", Password: "x", DeptID: 0,
		}).Error)
	case "missing":
		app.ConfigYml = homePositionSkipUserConfig{}
	default:
		t.Fatalf("unknown actor mode %q", actorMode)
	}

	device := &gbmodels.GbDevice{
		OwnerDeptID: 10, DeviceID: "D", IP: "192.0.2.10", Port: 5060,
		Transport: "UDP", Status: gbmodels.DeviceStatusOnline,
	}
	require.NoError(t, db.Create(device).Error)
	channel := &gbmodels.GbChannel{
		OwnerDeptID: 10, DeviceID: "D", ChannelID: "C", Status: gbmodels.ChannelStatusOnline,
	}
	require.NoError(t, db.Create(channel).Error)

	sender := &resourcePTZSender{}
	controller := gbcontrollers.NewDeviceMgmtController()
	controller.SetDB(func() *gorm.DB { return db })
	controller.SetPTZService(mustPTZService(t, db, sender))
	router := gin.New()
	router.Use(gin.Recovery(), withClaims(100))
	router.PATCH("/channel/:id/ptz/home-position", controller.UpdatePTZHomePosition)
	return homePositionPatchFixture{router: router, db: db, channel: channel, sender: sender, controller: controller}
}

func patchHomePosition(t *testing.T, fixture homePositionPatchFixture, body, headerKey string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(
		http.MethodPatch,
		"/channel/"+uintStr(fixture.channel.ID)+"/ptz/home-position",
		strings.NewReader(body),
	)
	request.Header.Set("Content-Type", "application/json")
	if headerKey != "" {
		request.Header.Set("Idempotency-Key", headerKey)
	}
	response := httptest.NewRecorder()
	fixture.router.ServeHTTP(response, request)
	return response
}

func homePositionErrorCode(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	var envelope struct {
		Data struct {
			ErrorCode string `json:"errorCode"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope), recorder.Body.String())
	return envelope.Data.ErrorCode
}

func homePositionOperationCount(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var count int64
	require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).Count(&count).Error)
	return count
}

func homePositionAttemptCount(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var count int64
	require.NoError(t, db.Model(&gbmodels.GbPTZOperationAttempt{}).Count(&count).Error)
	return count
}

func homePositionOperationID(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	var envelope struct {
		Data struct {
			OperationID string `json:"operationId"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope), recorder.Body.String())
	require.NotEmpty(t, envelope.Data.OperationID, recorder.Body.String())
	return envelope.Data.OperationID
}

func sentHomePositionMessages(sender *resourcePTZSender) int {
	sender.mu.Lock()
	defer sender.mu.Unlock()
	return len(sender.bodies)
}

func homePositionStringValue(value string) *string { return &value }

func TestDeviceMgmtHomePositionPatchRejectsNonStrictJSONWithoutOperation(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "empty object", body: `{}`},
		{name: "misspelled enabled", body: `{"enable":true,"resetTime":10,"presetId":0}`},
		{name: "unknown field", body: `{"enabled":false,"future":1}`},
		{name: "wrong type", body: `{"enabled":"false"}`},
		{name: "trailing json", body: `{"enabled":false}{"enabled":true,"resetTime":10,"presetId":0}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newHomePositionPatchFixture(t, "dept")
			result := patchHomePosition(t, fixture, test.body, "")
			require.Equal(t, http.StatusUnprocessableEntity, result.Code, result.Body.String())
			require.Equal(t, "HOME_POSITION_INVALID_ARGUMENT", homePositionErrorCode(t, result))
			require.Zero(t, homePositionOperationCount(t, fixture.db))
			require.Zero(t, sentHomePositionMessages(fixture.sender))
		})
	}
}

func TestDeviceMgmtHomePositionPatchValidatesEnableBoundaries(t *testing.T) {
	valid := []string{
		`{"enabled":true,"resetTime":10,"presetId":0}`,
		`{"enabled":true,"resetTime":3600,"presetId":255}`,
	}
	for _, body := range valid {
		fixture := newHomePositionPatchFixture(t, "dept")
		result := patchHomePosition(t, fixture, body, "")
		require.Equal(t, http.StatusOK, result.Code, result.Body.String())
		require.EqualValues(t, 1, homePositionOperationCount(t, fixture.db))
		var operation gbmodels.GbPTZOperation
		require.NoError(t, fixture.db.First(&operation).Error)
		require.True(t, operation.ResponseRequired)
		require.Zero(t, operation.Attempt)
		require.Equal(t, 1, operation.MaxAttempts)
		require.Zero(t, sentHomePositionMessages(fixture.sender), "response-required control must be sent by scheduler")
	}

	invalid := []string{
		`{"enabled":true}`,
		`{"enabled":true,"resetTime":9,"presetId":0}`,
		`{"enabled":true,"resetTime":3601,"presetId":0}`,
		`{"enabled":true,"resetTime":10,"presetId":-1}`,
		`{"enabled":true,"resetTime":10,"presetId":256}`,
	}
	for _, body := range invalid {
		fixture := newHomePositionPatchFixture(t, "dept")
		result := patchHomePosition(t, fixture, body, "")
		require.Equal(t, http.StatusUnprocessableEntity, result.Code, result.Body.String())
		require.Equal(t, "HOME_POSITION_INVALID_ARGUMENT", homePositionErrorCode(t, result))
		require.Zero(t, homePositionOperationCount(t, fixture.db))
	}
}

func TestDeviceMgmtHomePositionPatchNormalizesDisableForIdempotency(t *testing.T) {
	fixture := newHomePositionPatchFixture(t, "dept")
	first := patchHomePosition(t, fixture, `{"enabled":false,"resetTime":9,"presetId":255,"idempotencyKey":"disable-key"}`, "")
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())
	second := patchHomePosition(t, fixture, `{"enabled":false}`, "disable-key")
	require.Equal(t, http.StatusOK, second.Code, second.Body.String())
	require.EqualValues(t, 1, homePositionOperationCount(t, fixture.db))

	var operation gbmodels.GbPTZOperation
	require.NoError(t, fixture.db.First(&operation).Error)
	require.JSONEq(t, `{"enabled":false}`, operation.PayloadJSON)
	require.Zero(t, sentHomePositionMessages(fixture.sender))
}

func TestDeviceMgmtHomePositionPatchRejectsHeaderBodyKeyConflict(t *testing.T) {
	fixture := newHomePositionPatchFixture(t, "dept")
	result := patchHomePosition(t, fixture, `{"enabled":false,"idempotencyKey":"body-key"}`, "header-key")
	require.Equal(t, http.StatusUnprocessableEntity, result.Code, result.Body.String())
	require.Equal(t, "HOME_POSITION_INVALID_ARGUMENT", homePositionErrorCode(t, result))
	require.Zero(t, homePositionOperationCount(t, fixture.db))
}

func TestDeviceMgmtHomePositionPatchRejectsUnsafeIdempotencyKeys(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		headerKey string
	}{
		{
			name:      "header more than 128 bytes",
			body:      `{"enabled":false}`,
			headerKey: strings.Repeat("h", 129),
		},
		{
			name: "body more than 128 bytes",
			body: `{"enabled":false,"idempotencyKey":"` + strings.Repeat("b", 129) + `"}`,
		},
		{
			name: "body nul",
			body: `{"enabled":false,"idempotencyKey":"\u0000"}`,
		},
		{
			name: "body invalid utf8",
			body: `{"enabled":false,"idempotencyKey":"` + string([]byte{0xff}) + `"}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newHomePositionPatchFixture(t, "dept")
			result := patchHomePosition(t, fixture, test.body, test.headerKey)

			require.Equal(t, http.StatusUnprocessableEntity, result.Code, result.Body.String())
			require.Equal(t, "HOME_POSITION_INVALID_ARGUMENT", homePositionErrorCode(t, result))
			require.Zero(t, homePositionOperationCount(t, fixture.db))
			require.Zero(t, homePositionAttemptCount(t, fixture.db))
			require.Zero(t, sentHomePositionMessages(fixture.sender))
		})
	}
}

func TestDeviceMgmtHomePositionPatchReplaysOriginalAfterChannelGoesOffline(t *testing.T) {
	fixture := newHomePositionPatchFixture(t, "dept")
	first := patchHomePosition(t, fixture, `{"enabled":false}`, "offline-replay")
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())
	firstOperationID := homePositionOperationID(t, first)

	fixture.channel.Status = gbmodels.ChannelStatusOffline
	require.NoError(t, fixture.db.Save(fixture.channel).Error)
	second := patchHomePosition(t, fixture, `{"enabled":false}`, "offline-replay")

	require.Equal(t, http.StatusOK, second.Code, second.Body.String())
	require.Equal(t, firstOperationID, homePositionOperationID(t, second))
	require.EqualValues(t, 1, homePositionOperationCount(t, fixture.db))
	require.Zero(t, homePositionAttemptCount(t, fixture.db))
	require.Zero(t, sentHomePositionMessages(fixture.sender))
}

func TestDeviceMgmtHomePositionPatchHidesCrossDepartmentChannel(t *testing.T) {
	fixture := newHomePositionPatchFixture(t, "other-dept")
	result := patchHomePosition(t, fixture, `{"enabled":false}`, "cross-dept")

	require.Equal(t, http.StatusNotFound, result.Code, result.Body.String())
	require.Equal(t, "HOME_POSITION_NOT_FOUND", homePositionErrorCode(t, result))
	require.Zero(t, homePositionOperationCount(t, fixture.db))
	require.Zero(t, homePositionAttemptCount(t, fixture.db))
	require.Zero(t, sentHomePositionMessages(fixture.sender))
}

func TestDeviceMgmtHomePositionPatchCapabilitiesAreHintsOnly(t *testing.T) {
	for _, capabilities := range []*string{nil, homePositionStringValue(`{"home_position_control":false}`)} {
		fixture := newHomePositionPatchFixture(t, "dept")
		fixture.channel.Capabilities = capabilities
		require.NoError(t, fixture.db.Save(fixture.channel).Error)
		result := patchHomePosition(t, fixture, `{"enabled":false}`, "")
		require.Equal(t, http.StatusOK, result.Code, result.Body.String())
		require.EqualValues(t, 1, homePositionOperationCount(t, fixture.db))
		require.Zero(t, sentHomePositionMessages(fixture.sender))
	}
}

func TestDeviceMgmtHomePositionPatchStoresActorAndAcceptsZeroDepartment(t *testing.T) {
	for _, actorMode := range []string{"dept", "zero"} {
		fixture := newHomePositionPatchFixture(t, actorMode)
		result := patchHomePosition(t, fixture, `{"enabled":false}`, "")
		require.Equal(t, http.StatusOK, result.Code, result.Body.String())
		var operation gbmodels.GbPTZOperation
		require.NoError(t, fixture.db.First(&operation).Error)
		require.Equal(t, uint(100), operation.ActorID)
		if actorMode == "dept" {
			require.Equal(t, uint(10), operation.ActorDeptID)
		} else {
			require.Zero(t, operation.ActorDeptID)
		}
	}
}

func TestDeviceMgmtHomePositionPatchActorLookupFailureCreatesNoOperation(t *testing.T) {
	fixture := newHomePositionPatchFixture(t, "missing")
	result := patchHomePosition(t, fixture, `{"enabled":false}`, "")
	require.Equal(t, http.StatusInternalServerError, result.Code, result.Body.String())
	require.Equal(t, "HOME_POSITION_INTERNAL_ERROR", homePositionErrorCode(t, result))
	require.Zero(t, homePositionOperationCount(t, fixture.db))
}

func TestDeviceMgmtHomePositionPatchMapsOfflineAndIdempotencyConflict(t *testing.T) {
	offline := newHomePositionPatchFixture(t, "dept")
	offline.channel.Status = gbmodels.ChannelStatusOffline
	require.NoError(t, offline.db.Save(offline.channel).Error)
	result := patchHomePosition(t, offline, `{"enabled":false}`, "")
	require.Equal(t, http.StatusConflict, result.Code, result.Body.String())
	require.Equal(t, "HOME_POSITION_DEVICE_OFFLINE", homePositionErrorCode(t, result))
	require.Zero(t, homePositionOperationCount(t, offline.db))

	conflict := newHomePositionPatchFixture(t, "dept")
	first := patchHomePosition(t, conflict, `{"enabled":true,"resetTime":10,"presetId":0}`, "same-key")
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())
	second := patchHomePosition(t, conflict, `{"enabled":false}`, "same-key")
	require.Equal(t, http.StatusConflict, second.Code, second.Body.String())
	require.Equal(t, "HOME_POSITION_IDEMPOTENCY_CONFLICT", homePositionErrorCode(t, second))
	require.EqualValues(t, 1, homePositionOperationCount(t, conflict.db))
}

func TestDeviceMgmtHomePositionPatchMapsUnavailableAndNotFound(t *testing.T) {
	unavailable := newHomePositionPatchFixture(t, "dept")
	unavailable.controller.SetPTZService(nil)
	result := patchHomePosition(t, unavailable, `{"enabled":false}`, "")
	require.Equal(t, http.StatusServiceUnavailable, result.Code, result.Body.String())
	require.Equal(t, "HOME_POSITION_UNAVAILABLE", homePositionErrorCode(t, result))
	require.Zero(t, homePositionOperationCount(t, unavailable.db))

	notFound := newHomePositionPatchFixture(t, "dept")
	require.NoError(t, notFound.db.Delete(notFound.channel).Error)
	result = patchHomePosition(t, notFound, `{"enabled":false}`, "")
	require.Equal(t, http.StatusNotFound, result.Code, result.Body.String())
	require.Equal(t, "HOME_POSITION_NOT_FOUND", homePositionErrorCode(t, result))
	require.Zero(t, homePositionOperationCount(t, notFound.db))
}

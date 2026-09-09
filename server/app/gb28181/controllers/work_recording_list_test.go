package controllers_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbcontrollers "uvplatform.cn/uvp-gb28181/app/gb28181/controllers"
	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func workRecordingListRouter(t *testing.T, db *gorm.DB, actor uint) *gin.Engine {
	t.Helper()
	controller := gbcontrollers.NewWorkRecordingController(&workRecordingAPIFake{})
	controller.SetDB(func() *gorm.DB { return db })
	router := gin.New()
	router.Use(gin.Recovery(), withClaims(actor))
	router.GET("/work-recordings", controller.List)
	return router
}

func createWorkRecordingListJob(t *testing.T, db *gorm.DB, id string, channelID, actor uint, state string, createdAt time.Time) {
	t.Helper()
	desired := "start"
	fileState := "pending"
	if state == "stopped" {
		desired = "stop"
		fileState = "finalizing"
	}
	require.NoError(t, db.Create(&models.GbWorkRecording{
		ID: id, ChannelID: channelID, CreatedBy: actor, RequestID: id,
		State: state, DesiredAction: desired, Version: 3, RecorderClaimVersion: 4,
		RecordingRoot: "/secret/root/" + id, StartedAt: ptrTime(createdAt.Add(-time.Minute)),
		StoppedAt: ptrTimeIf(state == "stopped", createdAt.Add(-time.Second)), LastCheckedAt: ptrTime(createdAt.Add(-30 * time.Second)),
		LastError: "internal detail", FileState: fileState, FormState: "draft", SchemaVersion: 1, FormJSON: `{"secret":"value"}`,
		CreatedAt: createdAt, UpdatedAt: createdAt,
	}).Error)
}

func ptrTime(value time.Time) *time.Time { return &value }

func ptrTimeIf(ok bool, value time.Time) *time.Time {
	if !ok {
		return nil
	}
	return &value
}

func TestWorkRecordingControllerListFiltersOwnerVisibilityAndPaginates(t *testing.T) {
	db := newScopedDeviceDB(t)
	seedDeptScopedUser(t, db, 100, 10)
	require.NoError(t, db.AutoMigrate(&models.GbWorkRecording{}))

	ownedDevice := models.GbDevice{DeviceID: "list-owned-device", Name: "owned", OwnerDeptID: 10}
	sharedDevice := models.GbDevice{DeviceID: "list-shared-device", Name: "shared", OwnerDeptID: 20}
	hiddenDevice := models.GbDevice{DeviceID: "list-hidden-device", Name: "hidden", OwnerDeptID: 20}
	require.NoError(t, db.Create(&ownedDevice).Error)
	require.NoError(t, db.Create(&sharedDevice).Error)
	require.NoError(t, db.Create(&hiddenDevice).Error)
	require.NoError(t, db.Create(&models.GbDeviceGrant{
		DeviceID: sharedDevice.ID, TargetType: models.GrantTargetTypeUser, TargetID: 100,
	}).Error)

	ownedChannel := models.GbChannel{DeviceID: ownedDevice.DeviceID, Name: "owned channel", OwnerDeptID: 10}
	sharedChannel := models.GbChannel{DeviceID: sharedDevice.DeviceID, Name: "shared channel", OwnerDeptID: 20}
	hiddenChannel := models.GbChannel{DeviceID: hiddenDevice.DeviceID, Name: "hidden channel", OwnerDeptID: 20}
	deletedChannel := models.GbChannel{DeviceID: ownedDevice.DeviceID, Name: "deleted channel", OwnerDeptID: 10}
	for _, channel := range []*models.GbChannel{&ownedChannel, &sharedChannel, &hiddenChannel, &deletedChannel} {
		require.NoError(t, db.Create(channel).Error)
	}
	require.NoError(t, db.Delete(&deletedChannel).Error)

	base := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	createWorkRecordingListJob(t, db, "job-new", ownedChannel.ID, 100, "recording", base.Add(4*time.Minute))
	createWorkRecordingListJob(t, db, "job-shared", sharedChannel.ID, 100, "stopped", base.Add(3*time.Minute))
	createWorkRecordingListJob(t, db, "job-old", ownedChannel.ID, 100, "unknown", base.Add(1*time.Minute))
	createWorkRecordingListJob(t, db, "job-hidden", hiddenChannel.ID, 100, "recording", base.Add(5*time.Minute))
	createWorkRecordingListJob(t, db, "job-deleted", deletedChannel.ID, 100, "recording", base.Add(6*time.Minute))
	createWorkRecordingListJob(t, db, "job-foreign", ownedChannel.ID, 200, "recording", base.Add(7*time.Minute))

	router := workRecordingListRouter(t, db, 100)
	response := workRequest(router, http.MethodGet, "/work-recordings?page=1&pageSize=2", "")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var envelope struct {
		Data struct {
			Items    []map[string]any `json:"items"`
			Total    int64            `json:"total"`
			Page     int              `json:"page"`
			PageSize int              `json:"pageSize"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
	require.EqualValues(t, 3, envelope.Data.Total)
	require.Equal(t, 1, envelope.Data.Page)
	require.Equal(t, 2, envelope.Data.PageSize)
	require.Len(t, envelope.Data.Items, 2)
	require.Equal(t, "job-new", envelope.Data.Items[0]["id"])
	require.Equal(t, "owned channel", envelope.Data.Items[0]["channelName"])
	require.Equal(t, "job-shared", envelope.Data.Items[1]["id"])
	require.Equal(t, "stopped", envelope.Data.Items[1]["state"])
	require.NotContains(t, response.Body.String(), "recordingRoot")
	require.NotContains(t, response.Body.String(), "/secret/root")

	response = workRequest(router, http.MethodGet, "/work-recordings?page=2&pageSize=2", "")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
	require.Len(t, envelope.Data.Items, 1)
	require.Equal(t, "job-old", envelope.Data.Items[0]["id"])
}

func TestWorkRecordingControllerListActiveFilterAndStoppedHistory(t *testing.T) {
	db := newScopedDeviceDB(t)
	seedDeptScopedUser(t, db, 100, 10)
	require.NoError(t, db.AutoMigrate(&models.GbWorkRecording{}))
	device := models.GbDevice{DeviceID: "list-filter-device", OwnerDeptID: 10}
	require.NoError(t, db.Create(&device).Error)
	channel := models.GbChannel{DeviceID: device.DeviceID, Name: "filter channel", OwnerDeptID: 10}
	require.NoError(t, db.Create(&channel).Error)
	base := time.Date(2026, 9, 9, 13, 0, 0, 0, time.UTC)
	createWorkRecordingListJob(t, db, "filter-recording", channel.ID, 100, "recording", base.Add(2*time.Minute))
	createWorkRecordingListJob(t, db, "filter-starting", channel.ID, 100, "starting", base.Add(time.Minute))
	createWorkRecordingListJob(t, db, "filter-stopped", channel.ID, 100, "stopped", base)

	router := workRecordingListRouter(t, db, 100)
	response := workRequest(router, http.MethodGet, "/work-recordings?state=active", "")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var envelope struct {
		Data struct {
			Items []map[string]any `json:"items"`
			Total int64            `json:"total"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
	require.EqualValues(t, 2, envelope.Data.Total)
	require.Len(t, envelope.Data.Items, 2)
	require.Equal(t, "filter-recording", envelope.Data.Items[0]["id"])
	require.Equal(t, "filter-starting", envelope.Data.Items[1]["id"])

	response = workRequest(router, http.MethodGet, "/work-recordings", "")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
	require.EqualValues(t, 3, envelope.Data.Total)
	require.Len(t, envelope.Data.Items, 3)
	require.Equal(t, "filter-stopped", envelope.Data.Items[2]["id"])
}

func TestWorkRecordingControllerListRequiresLoginAndStrictPagination(t *testing.T) {
	db := newScopedDeviceDB(t)
	require.NoError(t, db.AutoMigrate(&models.GbWorkRecording{}))
	unauthorized := workRequest(workRecordingListRouter(t, db, 0), http.MethodGet, "/work-recordings", "")
	require.Equal(t, http.StatusUnauthorized, unauthorized.Code)

	router := workRecordingListRouter(t, db, 100)
	for _, query := range []string{
		"?page=0", "?page=-1", "?page=invalid", "?page=9223372036854775807&pageSize=50",
		"?pageSize=0", "?pageSize=51", "?pageSize=invalid", "?state=stopped",
	} {
		response := workRequest(router, http.MethodGet, "/work-recordings"+query, "")
		require.Equal(t, http.StatusBadRequest, response.Code, query)
	}
}

func TestWorkRecordingControllerListReturnsUnavailableWhenServiceIsUnassembled(t *testing.T) {
	controller := gbcontrollers.NewWorkRecordingController(nil)
	router := gin.New()
	router.Use(gin.Recovery(), withClaims(100))
	router.GET("/work-recordings", controller.List)

	response := workRequest(router, http.MethodGet, "/work-recordings", "")
	require.Equal(t, http.StatusServiceUnavailable, response.Code, response.Body.String())
}

func TestWorkRecordingControllerListEmptyIsSuccessWhenNotFoundErrorsAreDisabled(t *testing.T) {
	db := newScopedDeviceDB(t)
	seedDeptScopedUser(t, db, 100, 10)
	require.NoError(t, db.AutoMigrate(&models.GbWorkRecording{}))
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("work:list:not_found", func(tx *gorm.DB) {
		tx.Statement.RaiseErrorOnNotFound = false
	}))

	response := workRequest(workRecordingListRouter(t, db, 100), http.MethodGet, "/work-recordings", "")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var envelope struct {
		Data struct {
			Items []map[string]any `json:"items"`
			Total int64            `json:"total"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
	require.Empty(t, envelope.Data.Items)
	require.Zero(t, envelope.Data.Total)
}

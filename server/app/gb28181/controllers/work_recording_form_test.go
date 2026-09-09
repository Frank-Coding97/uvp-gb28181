package controllers_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/workrecording"
)

type workFormEnvelope struct {
	Code int                      `json:"code"`
	Data workrecording.FormRecord `json:"data"`
}

func createWorkFormFixture(t *testing.T) (*gorm.DB, models.GbChannel, models.GbWorkRecording) {
	t.Helper()
	db := newScopedDeviceDB(t)
	seedDeptScopedUser(t, db, 100, 10)
	require.NoError(t, db.AutoMigrate(&models.GbWorkRecording{}))
	channel := models.GbChannel{DeviceID: "34020000002000000001", ChannelID: "34020000001310000001", Name: "本部门通道", OwnerDeptID: 10}
	require.NoError(t, db.Create(&channel).Error)
	started := time.Date(2026, 9, 9, 9, 0, 0, 0, time.UTC)
	stopped := started.Add(time.Hour)
	job := models.GbWorkRecording{
		ID: uuid.NewString(), ChannelID: channel.ID, CreatedBy: 100, RequestID: uuid.NewString(),
		State: workrecording.StateRecording, DesiredAction: workrecording.DesiredActionStart, Version: 9,
		FormState: workrecording.FormDraft, FormVersion: 0, SchemaVersion: 1,
		DeviceID: "34020000002000000001", FormJSON: "{}", StartedAt: &started, StoppedAt: &stopped,
	}
	require.NoError(t, db.Create(&job).Error)
	return db, channel, job
}

func decodeWorkForm(t *testing.T, body []byte) workFormEnvelope {
	t.Helper()
	var response workFormEnvelope
	require.NoError(t, json.Unmarshal(body, &response))
	return response
}

func TestWorkRecordingFormControllerGetsVisibleFormAndOnlyOwnerDraftIsEditable(t *testing.T) {
	db, channel, job := createWorkFormFixture(t)
	router := workRecordingRouter(t, db, &workRecordingAPIFake{}, 100)
	response := workRequest(router, http.MethodGet, "/work-recordings/"+job.ID+"/form", "")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	payload := decodeWorkForm(t, response.Body.Bytes())
	require.Equal(t, 0, payload.Code)
	require.Equal(t, job.ID, payload.Data.JobID)
	require.Equal(t, channel.ID, payload.Data.ChannelID)
	require.EqualValues(t, 0, payload.Data.FormVersion)
	require.Equal(t, workrecording.FormDraft, payload.Data.FormState)
	require.EqualValues(t, 1, payload.Data.SchemaVersion)
	require.Equal(t, job.DeviceID, payload.Data.DeviceID)
	require.True(t, payload.Data.Editable)
	require.NotNil(t, payload.Data.Form.WorkPersonnel)

	require.NoError(t, db.Model(&models.GbWorkRecording{}).Where("id = ?", job.ID).Update("created_by", 200).Error)
	response = workRequest(router, http.MethodGet, "/work-recordings/"+job.ID+"/form", "")
	require.Equal(t, http.StatusOK, response.Code)
	require.False(t, decodeWorkForm(t, response.Body.Bytes()).Data.Editable)
	response = workRequest(router, http.MethodPut, "/work-recordings/"+job.ID+"/form", `{"formVersion":0,"form":{}}`)
	require.Equal(t, http.StatusForbidden, response.Code)

	foreign := models.GbChannel{DeviceID: "34020000002000000002", ChannelID: "34020000001310000002", OwnerDeptID: 20}
	require.NoError(t, db.Create(&foreign).Error)
	hiddenJob := models.GbWorkRecording{
		ID: uuid.NewString(), ChannelID: foreign.ID, CreatedBy: 100, RequestID: uuid.NewString(),
		State: workrecording.StateRecording, DesiredAction: workrecording.DesiredActionStart, Version: 1,
		FormState: workrecording.FormDraft, SchemaVersion: 1, FormJSON: "{}",
	}
	require.NoError(t, db.Create(&hiddenJob).Error)
	response = workRequest(router, http.MethodGet, "/work-recordings/"+hiddenJob.ID+"/form", "")
	require.Equal(t, http.StatusNotFound, response.Code)
	response = workRequest(router, http.MethodPut, "/work-recordings/"+hiddenJob.ID+"/form", `{"formVersion":0,"form":{}}`)
	require.Equal(t, http.StatusNotFound, response.Code)
}

func TestWorkRecordingFormControllerSavesChineseDraftWithCASAndKeepsRecordingFacts(t *testing.T) {
	db, _, job := createWorkFormFixture(t)
	router := workRecordingRouter(t, db, &workRecordingAPIFake{}, 100)
	body := `{"formVersion":0,"form":{"projectName":"测试项目","major":"接触网","stationArea":"北京南","mileage":"K12+300","anchorSectionNo":"锚段一","startAnchorPillarNo":"起锚一","endAnchorPillarNo":"落锚二","workLeader":"张三","workPersonnel":["张三","李四"],"tensionWireCarModel":"车型","tensionWireCarNo":"车号","setTension":"设定","straightenerStatus":"正常","straightenerInspector":"王五","wireLayingProcess":"过程记录","remark":"备注"}}`
	response := workRequest(router, http.MethodPut, "/work-recordings/"+job.ID+"/form", body)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	payload := decodeWorkForm(t, response.Body.Bytes())
	require.EqualValues(t, 1, payload.Data.FormVersion)
	require.Equal(t, "测试项目", payload.Data.Form.ProjectName)
	require.Equal(t, []string{"张三", "李四"}, payload.Data.Form.WorkPersonnel)

	var persisted models.GbWorkRecording
	require.NoError(t, db.First(&persisted, "id = ?", job.ID).Error)
	require.Equal(t, job.State, persisted.State)
	require.Equal(t, job.Version, persisted.Version)
	require.Equal(t, job.DeviceID, persisted.DeviceID)
	require.Equal(t, job.StartedAt, persisted.StartedAt)
	require.Equal(t, job.StoppedAt, persisted.StoppedAt)

	response = workRequest(router, http.MethodPut, "/work-recordings/"+job.ID+"/form", body)
	require.Equal(t, http.StatusConflict, response.Code)
	require.NoError(t, db.Model(&models.GbWorkRecording{}).Where("id = ?", job.ID).Update("form_state", workrecording.FormSubmitted).Error)
	response = workRequest(router, http.MethodPut, "/work-recordings/"+job.ID+"/form", `{"formVersion":1,"form":{}}`)
	require.Equal(t, http.StatusConflict, response.Code)
	response = workRequest(router, http.MethodGet, "/work-recordings/"+job.ID+"/form", "")
	require.Equal(t, http.StatusOK, response.Code)
	require.False(t, decodeWorkForm(t, response.Body.Bytes()).Data.Editable)
}

func TestWorkRecordingFormControllerRejectsUnknownTrailingForgedAndOversizedInput(t *testing.T) {
	db, _, job := createWorkFormFixture(t)
	router := workRecordingRouter(t, db, &workRecordingAPIFake{}, 100)
	bodies := []string{
		`{"formVersion":0,"form":{},"deviceId":"forged"}`,
		`{"formVersion":0,"form":{"deviceId":"forged"}}`,
		`{"formVersion":0,"form":{}} {}`,
		`{"formVersion":0,"form":{"projectName":"` + strings.Repeat("界", 257) + `"}}`,
		`{"formVersion":0,"form":{}}` + strings.Repeat(" ", 64*1024),
	}
	for _, body := range bodies {
		response := workRequest(router, http.MethodPut, "/work-recordings/"+job.ID+"/form", body)
		require.Equal(t, http.StatusBadRequest, response.Code, body[:min(len(body), 80)])
	}
	var persisted models.GbWorkRecording
	require.NoError(t, db.First(&persisted, "id = ?", job.ID).Error)
	require.EqualValues(t, 0, persisted.FormVersion)
	require.Equal(t, "{}", persisted.FormJSON)

	response := workRequest(workRecordingRouter(t, db, &workRecordingAPIFake{}, 0), http.MethodGet, "/work-recordings/"+job.ID+"/form", "")
	require.Equal(t, http.StatusUnauthorized, response.Code)
}

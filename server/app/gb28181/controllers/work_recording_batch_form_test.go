package controllers_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gbcontrollers "uvplatform.cn/uvp-gb28181/app/gb28181/controllers"
	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/workrecording"
)

type batchFormAPIStub struct{}

func (batchFormAPIStub) Start(context.Context, uint, workrecording.BatchStartRequest) (workrecording.BatchSnapshot, error) {
	return workrecording.BatchSnapshot{}, nil
}
func (batchFormAPIStub) Stop(context.Context, string) (workrecording.BatchSnapshot, error) {
	return workrecording.BatchSnapshot{}, nil
}
func (batchFormAPIStub) Get(context.Context, string) (workrecording.BatchSnapshot, error) {
	return workrecording.BatchSnapshot{}, nil
}
func (batchFormAPIStub) List(context.Context, uint, int, int) ([]workrecording.BatchSnapshot, int64, error) {
	return nil, 0, nil
}

func TestWorkRecordingControllerBatchFormUsesLedgerAndCAS(t *testing.T) {
	db := newScopedDeviceDB(t)
	seedDeptScopedUser(t, db, 100, 10)
	require.NoError(t, db.AutoMigrate(&models.GbWorkRecordingBatch{}, &models.GbWorkRecording{}))
	channels := make([]models.GbChannel, 0, workrecording.BatchMaxCameraCount)
	for i := 0; i < workrecording.BatchMaxCameraCount; i++ {
		channel := models.GbChannel{DeviceID: uuid.NewString(), ChannelID: uuid.NewString(), Name: "摄像头", OwnerDeptID: 10}
		require.NoError(t, db.Create(&channel).Error)
		channels = append(channels, channel)
	}
	batch := models.GbWorkRecordingBatch{ID: uuid.NewString(), CreatedBy: 100, RequestID: uuid.NewString(), State: workrecording.StateRecording, Version: 1, FormState: workrecording.FormDraft, FormJSON: "{}", SchemaVersion: 1}
	require.NoError(t, db.Create(&batch).Error)
	for _, channel := range channels {
		require.NoError(t, db.Create(&models.GbWorkRecording{ID: uuid.NewString(), BatchID: batch.ID, ChannelID: channel.ID, CreatedBy: 100, RequestID: uuid.NewString(), State: workrecording.StateRecording, DesiredAction: workrecording.DesiredActionStart, Version: 1, FormJSON: "{}"}).Error)
	}

	controller := gbcontrollers.NewWorkRecordingController(&workRecordingAPIFake{})
	controller.SetDB(func() *gorm.DB { return db })
	controller.SetBatchService(batchFormAPIStub{})
	router := gin.New()
	router.Use(gin.Recovery(), withClaims(100))
	router.GET("/work-recordings/batches/:batchId/form", controller.BatchForm)
	router.PUT("/work-recordings/batches/:batchId/form", controller.SaveBatchForm)

	response := workRequest(router, http.MethodGet, "/work-recordings/batches/"+batch.ID+"/form", "")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	response = workRequest(router, http.MethodPut, "/work-recordings/batches/"+batch.ID+"/form", `{"formVersion":0,"form":{"projectName":"四路台账","workPersonnel":["张三"]}}`)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), "四路台账")
	response = workRequest(router, http.MethodPut, "/work-recordings/batches/"+batch.ID+"/form", `{"formVersion":0,"form":{}}`)
	require.Equal(t, http.StatusConflict, response.Code)
}

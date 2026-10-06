package controllers

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func TestVideoCapabilityAggregatesSpeedResolutionAndObservation(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDeviceConfig{}))
	first := time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC)
	second := first.Add(time.Minute)
	require.NoError(t, db.Create(&[]gbmodels.GbDeviceConfig{
		{DeviceID: 7, TargetCode: "D1", ConfigType: "BasicParam", PayloadJSON: `{"name":"门口设备","expiration":3600,"heartBeatInterval":60,"heartBeatCount":3}`, ObservedAt: second},
		{DeviceID: 7, TargetCode: "C1", ConfigType: "VideoParamOpt", PayloadJSON: `{"downloadSpeed":"1/2/4","resolution":"1/2"}`, ObservedAt: first},
		{DeviceID: 7, TargetCode: "C2", ConfigType: "VideoParamOpt", PayloadJSON: `{"downloadSpeed":"2/8","resolution":"2/6"}`, ObservedAt: second},
	}).Error)

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("GET", "/", nil)
	controller := NewDeviceMgmtController()
	summary, err := controller.videoCapability(ctx, db, 7, 3)
	require.NoError(t, err)
	require.Equal(t, []int{1, 2, 4, 8}, summary.DownloadSpeeds)
	require.Equal(t, 8, summary.MaxDownloadSpeed)
	require.Equal(t, []string{"1", "2", "6"}, summary.Resolutions)
	require.EqualValues(t, 3, summary.ChannelCount)
	require.EqualValues(t, 2, summary.ObservedChannelCount)
	require.NotNil(t, summary.ObservedAt)
	require.Equal(t, second, *summary.ObservedAt)

	basic, err := controller.basicParam(ctx, db, 7, "D1")
	require.NoError(t, err)
	require.Equal(t, "门口设备", basic.Name)
	require.Equal(t, 3600, *basic.Expiration)
}

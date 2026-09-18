package controllers_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/devicecapture"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	globalapp "uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/global/consts"
	basemodels "uvplatform.cn/uvp-gb28181/app/models"
)

func TestCreateSnapshotSessionDispatchesCapabilityUploadURL(t *testing.T) {
	controller, db, channel, sender := newPTZResourceController(t)
	require.NoError(t, db.AutoMigrate(&basemodels.SysDepartment{}, &basemodels.SysRole{}, &basemodels.SysUserRole{}, &basemodels.User{}))
	seedDeptScopedUser(t, db, 12, 10)
	require.NoError(t, db.Model(&gbmodels.GbDevice{}).Where("device_id = ?", channel.DeviceID).Updates(map[string]interface{}{"effective_version": gbmodels.ProtocolVersion2022, "owner_dept_id": 10}).Error)
	require.NoError(t, db.Model(channel).Update("owner_dept_id", 10).Error)
	controller.SetCaptureRuntime(devicecapture.NewRegistry(t.TempDir()))
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(func(c *gin.Context) {
		c.Set(consts.BindContextKeyName, &globalapp.Claims{ClaimsUser: globalapp.ClaimsUser{UserID: 12}})
		c.Next()
	})
	router.POST("/channel/:id/snapshot-sessions", controller.CreateSnapshotSession)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/channel/"+uintStr(channel.ID)+"/snapshot-sessions", strings.NewReader(`{"snapNum":2,"interval":3}`))
	request.Host = "127.0.0.1:8080"
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Len(t, sender.bodies, 1)
	require.Contains(t, sender.bodies[0], "<SnapShotConfig>")
	require.Contains(t, sender.bodies[0], "<SnapNum>2</SnapNum><Interval>3</Interval>")
	require.Contains(t, sender.bodies[0], "http://127.0.0.1:8080/api/gb28181/device-snapshots/uploads/")
}

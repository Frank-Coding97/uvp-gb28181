package controllers_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/catalogprogress"
	gbcontrollers "uvplatform.cn/uvp-gb28181/app/gb28181/controllers"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/global/consts"
)

type catalogRefreshTrigger struct{ calls int }

func (t *catalogRefreshTrigger) Trigger(context.Context, string, string, string) { t.calls++ }

func catalogRefreshContext(t *testing.T, id uint) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("GET", "/", nil)
	c.Set(consts.BindContextKeyName, &app.Claims{ClaimsUser: app.ClaimsUser{UserID: id}})
	return c, recorder
}

func TestRefreshDeviceCatalogReusesActiveOperation(t *testing.T) {
	db := newScopedDeviceDB(t)
	seedDeptScopedUser(t, db, 100, 10)
	device := &gbmodels.GbDevice{DeviceID: "34020000002000000101", Status: gbmodels.DeviceStatusOnline, OwnerDeptID: 10, IP: "192.0.2.10", Port: 5060, Transport: "UDP"}
	require.NoError(t, db.Create(device).Error)

	trigger := &catalogRefreshTrigger{}
	controller := gbcontrollers.NewDeviceMgmtController()
	controller.SetDB(func() *gorm.DB { return db })
	controller.SetCatalogTrigger(trigger)

	call := func() map[string]any {
		c, recorder := catalogRefreshContext(t, 100)
		c.Params = gin.Params{{Key: "id", Value: uintStr(device.ID)}}
		controller.RefreshDeviceCatalog(c)
		return unmarshal(t, recorder)
	}
	first := call()
	second := call()
	firstData := first["data"].(map[string]any)
	secondData := second["data"].(map[string]any)
	require.Equal(t, firstData["operationId"], secondData["operationId"])
	require.Equal(t, false, firstData["deduplicated"])
	require.Equal(t, true, secondData["deduplicated"])
	require.Equal(t, 1, trigger.calls)
	catalogprogress.Default.Fail(firstData["operationId"].(string), errors.New("test cleanup"))
}

func TestGetDeviceCatalogRefreshProgressChecksDeviceOwnership(t *testing.T) {
	db := newScopedDeviceDB(t)
	seedDeptScopedUser(t, db, 100, 10)
	device := &gbmodels.GbDevice{DeviceID: "34020000002000000102", Status: gbmodels.DeviceStatusOnline, OwnerDeptID: 10}
	other := &gbmodels.GbDevice{DeviceID: "34020000002000000103", Status: gbmodels.DeviceStatusOnline, OwnerDeptID: 10}
	require.NoError(t, db.Create(device).Error)
	require.NoError(t, db.Create(other).Error)
	controller := gbcontrollers.NewDeviceMgmtController()
	controller.SetDB(func() *gorm.DB { return db })
	started := catalogprogress.Default.Start(device.DeviceID)
	defer catalogprogress.Default.Fail(started.Snapshot.OperationID, errors.New("test cleanup"))
	router := gin.New()
	router.Use(gin.Recovery(), func(c *gin.Context) {
		c.Set(consts.BindContextKeyName, &app.Claims{ClaimsUser: app.ClaimsUser{UserID: 100}})
		c.Next()
	})
	router.GET("/device/:id/catalog/refresh/:operationId", controller.GetDeviceCatalogRefreshProgress)

	request := func(id uint) map[string]any {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest("GET", "/device/"+uintStr(id)+"/catalog/refresh/"+started.Snapshot.OperationID, nil))
		return unmarshal(t, recorder)
	}
	require.EqualValues(t, 0, request(device.ID)["code"])
	require.EqualValues(t, 1, request(other.ID)["code"])
}

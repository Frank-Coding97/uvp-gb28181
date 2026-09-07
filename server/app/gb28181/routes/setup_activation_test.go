package routes

import (
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	gbcontrollers "uvplatform.cn/uvp-gb28181/app/gb28181/controllers"
)

func TestSetupControllerPublicationDuringActivation(t *testing.T) {
	previous := setupController.Load()
	t.Cleanup(func() { SetSetupController(previous) })
	controller := gbcontrollers.NewSetupController(nil, nil, nil, nil)
	r := gin.New()
	r.GET("/setup", setupRoute(func(current *gbcontrollers.SetupController, c *gin.Context) {
		if current == nil {
			t.Error("admitted setup lost its controller")
		}
		c.Status(204)
	}))
	var workers sync.WaitGroup
	workers.Add(1)
	go func() {
		defer workers.Done()
		for i := 0; i < 500; i++ {
			SetSetupController(controller)
			SetSetupController(nil)
		}
	}()
	for i := 0; i < 500; i++ {
		out := httptest.NewRecorder()
		r.ServeHTTP(out, httptest.NewRequest("GET", "/setup", nil))
		if out.Code != 204 && out.Code != 503 {
			t.Fatalf("status=%d", out.Code)
		}
	}
	workers.Wait()
}

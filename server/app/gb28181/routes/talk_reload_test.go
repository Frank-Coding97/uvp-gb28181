package routes

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestTalkControllerReloadIsConcurrentSafe(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	RegisterRoutes(engine.Group("/api"))
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			SetTalkService(nil, nil)
		}()
		go func() {
			defer wg.Done()
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/gb28181/device-mgmt/channel/1/talk-sessions", nil)
			engine.ServeHTTP(response, request)
			if response.Code != http.StatusServiceUnavailable {
				t.Errorf("status=%d", response.Code)
			}
		}()
	}
	wg.Wait()
	SetTalkService(nil, nil)
}

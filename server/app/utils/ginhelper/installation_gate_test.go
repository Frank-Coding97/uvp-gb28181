package ginhelper

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestInstallationGatePhasesAndExactRoutes(t *testing.T) {
	for _, tc := range []struct {
		phase, method, path string
		want                int
	}{
		{"pending_admin", "GET", "/", 204},
		{"pending_admin", "GET", "/static/main.js", 204},
		{"pending_admin", "GET", "/static/../uploads/private", 503},
		{"pending_admin", "POST", "/api/standalone/setup/admin", 204},
		{"pending_admin", "GET", "/api/standalone/setup/admin", 503},
		{"pending_admin", "POST", "/api/login", 503},
		{"pending_admin", "POST", "/index/hook/on_record_mp4", 503},
		{"pending_sip", "POST", "/api/standalone/setup/admin", 204},
		{"pending_sip", "POST", "/api/login", 204},
		{"pending_sip", "GET", "/api/users/profile", 204},
		{"pending_sip", "PUT", "/api/gb28181/sip/setup/config", 204},
		{"pending_sip", "POST", "/api/gb28181/sip/setup/skip", 503},
		{"pending_sip", "POST", "/api/users/add", 503},
		{"pending_sip", "GET", "/api/users/list", 503},
		{"pending_sip", "POST", "/index/hook/on_play", 503},
		{"pending_sip", "GET", "/uploads/private", 503},
		{"complete", "GET", "/api/users/list", 204},
		{"broken", "GET", "/", 503},
	} {
		t.Run(tc.phase+tc.method+tc.path, func(t *testing.T) {
			r := gin.New()
			r.Use(InstallationGate(func() string { return tc.phase }))
			r.NoRoute(func(c *gin.Context) { c.Status(204) })
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
			if w.Code != tc.want {
				t.Fatalf("status=%d want=%d", w.Code, tc.want)
			}
			if tc.want == 503 && w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("rejection must not cache")
			}
		})
	}
}

func TestInstallationGateRechecksActivationAndKeepsRouteAuth(t *testing.T) {
	phase := "pending_admin"
	r := gin.New()
	r.Use(InstallationGate(func() string { return phase }))
	r.GET("/api/users/profile", func(c *gin.Context) { c.AbortWithStatus(401) })
	for _, next := range []string{"pending_admin", "pending_sip", "complete"} {
		phase = next
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/api/users/profile", nil))
		want := 401
		if next == "pending_admin" {
			want = 503
		}
		if w.Code != want {
			t.Fatalf("%s: got=%d want=%d", next, w.Code, want)
		}
	}
}

package routes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	appmodels "uvplatform.cn/uvp-gb28181/app/models"
	openapimodels "uvplatform.cn/uvp-gb28181/app/openapi/models"
	appservice "uvplatform.cn/uvp-gb28181/app/service"
	"uvplatform.cn/uvp-gb28181/app/utils/response"
)

func newOpenAPIAdminBrowserFixture(t *testing.T) *openAPIAdminHTTPFixture {
	t.Helper()
	oldResponse := app.Response
	app.Response = response.NewResponseHandler()
	t.Cleanup(func() { app.Response = oldResponse })
	f := newOpenAPIAdminHTTPFixture(t)
	require.NoError(t, f.db.AutoMigrate(&appmodels.SysMenu{}, &appmodels.SysRoleMenu{}))
	menu := appmodels.SysMenu{Path: "/gb28181/openapi-client", Name: "gb28181-openapi-client", Component: "gb28181/openapi-client/index", Title: "OpenAPI 客户端", Type: 2}
	require.NoError(t, f.db.Create(&menu).Error)
	require.NoError(t, f.db.Create(&appmodels.SysRoleMenu{RoleID: 1, MenuID: menu.ID}).Error)
	for _, action := range []string{"read", "create", "grant", "rotate", "status", "audit"} {
		button := appmodels.SysMenu{ParentID: menu.ID, Name: "Permission_gb28181_openapi_client_" + action, Type: 3, Hide: 1, Permission: "gb28181:openapi:client:" + action}
		require.NoError(t, f.db.Create(&button).Error)
		require.NoError(t, f.db.Create(&appmodels.SysRoleMenu{RoleID: 1, MenuID: button.ID}).Error)
	}
	for _, path := range []string{"/api/users/profile", "/api/sysMenu/getRouters"} {
		require.NoError(t, f.helper.AddPolicyForRole(1, path, http.MethodGet))
		response := f.do(t, f.adminToken, http.MethodGet, path, "")
		f.requestCount.Add(-1) // The existing cleanup counter tracks OpenAPI logs only.
		require.Equal(t, http.StatusOK, response.status)
		var envelope struct {
			Code int             `json:"code"`
			Data json.RawMessage `json:"data"`
		}
		require.NoError(t, json.Unmarshal(response.body, &envelope))
		require.Zero(t, envelope.Code, string(response.body))
		if strings.HasSuffix(path, "profile") {
			require.Contains(t, string(envelope.Data), "gb28181:openapi:client:create")
			require.NotContains(t, string(envelope.Data), "*:*:*")
		} else {
			require.Contains(t, string(envelope.Data), "gb28181/openapi-client/index")
		}
	}
	waitForBrowserSessionLogs(t, f, 2)
	return f
}

func waitForBrowserSessionLogs(t *testing.T, f *openAPIAdminHTTPFixture, minimum int64) {
	t.Helper()
	require.Eventually(t, func() bool {
		var count int64
		return f.db.Model(&appmodels.SysOperationLog{}).Where("path IN ?", []string{
			"/api/users/profile", "/api/sysMenu/getRouters",
		}).Count(&count).Error == nil && count >= minimum
	}, 3*time.Second, 10*time.Millisecond, "wait for asynchronous fixture session logs before restoring global DB")
}

func TestOpenAPIAdminBrowserProfileAndMenu(t *testing.T) {
	newOpenAPIAdminBrowserFixture(t)
}

func recordBrowserFixtureRequest(f *openAPIAdminHTTPFixture, sessionRequests *atomic.Int64, path string) {
	if strings.HasPrefix(path, openAPIAdminHTTPPathPrefix) {
		f.requestCount.Add(1)
	} else if path == "/api/users/profile" || path == "/api/sysMenu/getRouters" {
		sessionRequests.Add(1)
	}
}

func TestOpenAPIAdminBrowserHeartbeatDoesNotAwaitAudit(t *testing.T) {
	f := &openAPIAdminHTTPFixture{}
	var sessionRequests atomic.Int64
	recordBrowserFixtureRequest(f, &sessionRequests, "/api/users/profile")
	recordBrowserFixtureRequest(f, &sessionRequests, "/api/sysMenu/getRouters")
	recordBrowserFixtureRequest(f, &sessionRequests, "/api/users/session/heartbeat")
	recordBrowserFixtureRequest(f, &sessionRequests, openAPIAdminHTTPPathPrefix)
	require.Equal(t, int64(2), sessionRequests.Load(), "heartbeat is intentionally excluded by OperationLogMiddleware")
	require.Equal(t, int64(1), f.requestCount.Load())
}

// To drive the built SPA, explicitly set UVP_OPENAPI_BROWSER_DIST to web/dist
// and UVP_OPENAPI_BROWSER_SERVE=1. Only the temporary SQLite fixture is used;
// the outer server binds loopback and proxies to the real, pinned-TLS root.
// This is not a login-form, native-database or production-TLS acceptance test.
func TestOpenAPIAdminBrowserFixture(t *testing.T) {
	if os.Getenv("UVP_OPENAPI_BROWSER_SERVE") != "1" {
		t.Skip("real browser run requires UVP_OPENAPI_BROWSER_SERVE=1 and a built SPA")
	}
	f := newOpenAPIAdminBrowserFixture(t)
	var sessionRequests atomic.Int64
	sessionRequests.Store(2)
	t.Cleanup(func() {
		f.server.Close()
		waitForBrowserSessionLogs(t, f, sessionRequests.Load())
	})
	dist, err := filepath.Abs(os.Getenv("UVP_OPENAPI_BROWSER_DIST"))
	require.NoError(t, err)
	require.NotEmpty(t, os.Getenv("UVP_OPENAPI_BROWSER_DIST"))
	_, err = os.Stat(filepath.Join(dist, "index.html"))
	require.NoError(t, err)
	var user appmodels.User
	require.NoError(t, f.db.First(&user, 7).Error)
	pair, err := f.sessions.CreateLogin(context.Background(), &user, appservice.LoginMetadata{ClientIP: "127.0.0.1", UserAgent: "openapi-browser-test"}, app.TokenService, time.Hour)
	require.NoError(t, err)
	rootURL, err := url.Parse(f.server.URL)
	require.NoError(t, err)
	proxy := httputil.NewSingleHostReverseProxy(rootURL)
	proxy.Transport = f.client.Transport
	files := http.FileServer(http.Dir(dist))
	finished := make(chan struct{})
	var once sync.Once
	mux := http.NewServeMux()
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		// No fixture request can reach a business database, device or network
		// endpoint outside this explicit management/session allowlist.
		allowed := strings.HasPrefix(r.URL.Path, openAPIAdminHTTPPathPrefix) ||
			r.URL.Path == "/api/users/profile" || r.URL.Path == "/api/sysMenu/getRouters" ||
			r.URL.Path == "/api/users/session/heartbeat"
		if !allowed {
			http.Error(w, "outside browser fixture scope", http.StatusNotFound)
			return
		}
		recordBrowserFixtureRequest(f, &sessionRequests, r.URL.Path)
		proxy.ServeHTTP(w, r)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		switch r.URL.Path {
		case "/__fixture/start":
			if r.Method != http.MethodGet {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			for name, value := range map[string]map[string]any{
				"gin-fast-access-token":  {"accessToken": pair.AccessToken, "accessTokenExpires": time.Now().Add(time.Hour).Unix()},
				"gin-fast-refresh-token": {"refreshToken": pair.RefreshToken, "refreshTokenExpires": time.Now().Add(time.Hour).Unix()},
			} {
				encoded, _ := json.Marshal(value)
				http.SetCookie(w, &http.Cookie{Name: name, Value: url.QueryEscape(string(encoded)), Path: "/", SameSite: http.SameSiteStrictMode})
			}
			http.Redirect(w, r, "/#/gb28181/openapi-client", http.StatusSeeOther)
		case "/__fixture/finish":
			if r.Method != http.MethodPost || r.Header.Get("Origin") != "http://"+r.Host {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			once.Do(func() { close(finished) })
		case "/__fixture/missing-permission":
			if r.Method != http.MethodPost || r.Header.Get("Origin") != "http://"+r.Host {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			if err := setBrowserMissingPermission(f, r.URL.Query().Get("action")); err != nil {
				http.Error(w, "invalid fixture permission change", http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			files.ServeHTTP(w, r)
		}
	})
	outer := httptest.NewServer(mux)
	defer outer.Close()
	t.Logf("BROWSER_FIXTURE_URL=%s/__fixture/start (temporary SQLite; 20 minute limit)", outer.URL)
	select {
	case <-finished:
		var clients []openapimodels.Client
		require.NoError(t, f.db.Find(&clients).Error)
		require.NotEmpty(t, clients, "browser must create at least one client")
		for _, c := range clients {
			require.Equal(t, uint(10), c.OwnerDeptID)
			require.Equal(t, openapimodels.StatusRevoked, c.Status, "browser lifecycle must end in revoked")
			require.GreaterOrEqual(t, c.SecretVersion, int64(2), "browser must rotate the secret")
		}
		t.Logf("BROWSER_DB_VERIFIED clients=%d: exact department, rotated secret, revoked state", len(clients))
	case <-time.After(20 * time.Minute):
		t.Fatal("browser acceptance was not finished; no pass claimed")
	}
}

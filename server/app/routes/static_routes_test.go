package routes

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestStaticRoutesServeProductionWebAndScopedPublicFiles(t *testing.T) {
	gin.SetMode(gin.TestMode)
	root := t.TempDir()
	webRoot := filepath.Join(root, "web")
	publicRoot := filepath.Join(root, "resource", "public")
	uploadRoot := filepath.Join(root, "data", "uploads")
	if err := os.MkdirAll(filepath.Join(webRoot, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(publicRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(publicRoot, "uploads"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(uploadRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	writeStaticTestFile(t, filepath.Join(webRoot, "index.html"), "release-web-marker")
	writeStaticTestFile(t, filepath.Join(webRoot, "assets", "app.js"), "window.__release = true")
	writeStaticTestFile(t, filepath.Join(publicRoot, "area.json"), "resource-public-file")
	writeStaticTestFile(t, filepath.Join(publicRoot, "uploads", "resource-upload.txt"), "resource-upload-must-not-be-served")
	writeStaticTestFile(t, filepath.Join(uploadRoot, "avatar.txt"), "data-upload-file")
	writeStaticTestFile(t, filepath.Join(root, "config-secret.yml"), "config-secret")

	engine := gin.New()
	registerStaticRoutes(engine, staticRouteConfig{
		WebRoot:    webRoot,
		PublicPath: "/public",
		PublicRoot: publicRoot,
		UploadRoot: uploadRoot,
	})
	engine.GET("/api/known", func(c *gin.Context) { c.String(http.StatusOK, "api-handler") })

	server := httptest.NewServer(engine)
	defer server.Close()
	client := server.Client()

	tests := []struct {
		name       string
		path       string
		accept     string
		wantStatus int
		wantBody   string
		notBody    string
	}{
		{name: "home page", path: "/", accept: "text/html", wantStatus: http.StatusOK, wantBody: "release-web-marker"},
		{name: "deep SPA page refresh", path: "/media/overview", accept: "text/html", wantStatus: http.StatusOK, wantBody: "release-web-marker"},
		{name: "production asset", path: "/assets/app.js", wantStatus: http.StatusOK, wantBody: "window.__release = true"},
		{name: "resource public file", path: "/public/area.json", wantStatus: http.StatusOK, wantBody: "resource-public-file"},
		{name: "data uploads file", path: "/public/uploads/avatar.txt", wantStatus: http.StatusOK, wantBody: "data-upload-file"},
		{name: "resource uploads directory is not an alternate root", path: "/public/uploads/resource-upload.txt", wantStatus: http.StatusNotFound, notBody: "resource-upload-must-not-be-served"},
		{name: "known API remains API", path: "/api/known", accept: "text/html", wantStatus: http.StatusOK, wantBody: "api-handler"},
		{name: "missing asset is not SPA", path: "/assets/missing.js", accept: "text/html", wantStatus: http.StatusNotFound, notBody: "release-web-marker"},
		{name: "Windows normalized trailing space is not SPA", path: "/assets/missing.js%20", accept: "text/html", wantStatus: http.StatusNotFound, notBody: "release-web-marker"},
		{name: "Windows normalized trailing dot is not SPA", path: "/media/overview.%2e", accept: "text/html", wantStatus: http.StatusNotFound, notBody: "release-web-marker"},
		{name: "missing API is real 404", path: "/api/missing", accept: "text/html", wantStatus: http.StatusNotFound, notBody: "release-web-marker"},
		{name: "missing hook is real 404", path: "/index/hook/missing", accept: "text/html", wantStatus: http.StatusNotFound, notBody: "release-web-marker"},
		{name: "missing media service path is real 404", path: "/index/api/missing", accept: "text/html", wantStatus: http.StatusNotFound, notBody: "release-web-marker"},
		{name: "missing upload is real 404", path: "/public/uploads/missing.txt", accept: "text/html", wantStatus: http.StatusNotFound, notBody: "release-web-marker"},
		{name: "public root does not list files", path: "/public", accept: "text/html", wantStatus: http.StatusNotFound, notBody: "release-web-marker"},
		{name: "data root is not mounted", path: "/data/uvp.db", accept: "text/html", wantStatus: http.StatusNotFound, notBody: "release-web-marker"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			status, body := requestStaticTest(t, client, server.URL+test.path, test.accept)
			if status != test.wantStatus {
				t.Fatalf("status = %d, want %d; body=%q", status, test.wantStatus, body)
			}
			if test.wantBody != "" && !strings.Contains(body, test.wantBody) {
				t.Fatalf("body %q does not contain %q", body, test.wantBody)
			}
			if test.notBody != "" && strings.Contains(body, test.notBody) {
				t.Fatalf("body %q unexpectedly contains %q", body, test.notBody)
			}
		})
	}

	status, body := requestStaticTest(t, client, server.URL+"/public/%2e%2e/config-secret.yml", "text/html")
	if status != http.StatusNotFound || strings.Contains(body, "config-secret") || strings.Contains(body, "release-web-marker") {
		t.Fatalf("encoded traversal status=%d body=%q; expected a blank 404 without file or SPA content", status, body)
	}
	status, body = requestStaticTest(t, client, server.URL+"/public/%2e%2e%20/config-secret.yml", "text/html")
	if status != http.StatusNotFound || strings.Contains(body, "config-secret") || strings.Contains(body, "release-web-marker") {
		t.Fatalf("encoded Windows trailing-space traversal status=%d body=%q; expected a blank 404 without file or SPA content", status, body)
	}
	status, body = requestStaticTest(t, client, server.URL+"/public/uploads/%2e%2e/%2e%2e/config-secret.yml", "text/html")
	if status != http.StatusNotFound || strings.Contains(body, "config-secret") || strings.Contains(body, "release-web-marker") {
		t.Fatalf("upload traversal status=%d body=%q; expected a blank 404 without file or SPA content", status, body)
	}

	headRequest, err := http.NewRequest(http.MethodHead, server.URL+"/assets/app.js", nil)
	if err != nil {
		t.Fatal(err)
	}
	headResponse, err := client.Do(headRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer headResponse.Body.Close()
	if headResponse.StatusCode != http.StatusOK {
		t.Fatalf("HEAD status = %d, want %d", headResponse.StatusCode, http.StatusOK)
	}
	headBody, err := io.ReadAll(headResponse.Body)
	if err != nil {
		t.Fatal(err)
	}
	if len(headBody) != 0 {
		t.Fatalf("HEAD returned %d body bytes", len(headBody))
	}
}

func TestStaticRoutesDoNotTurnWebSocketUpgradeIntoSPAHTML(t *testing.T) {
	gin.SetMode(gin.TestMode)
	root := t.TempDir()
	writeStaticTestFile(t, filepath.Join(root, "index.html"), "release-web-marker")

	engine := gin.New()
	registerStaticRoutes(engine, staticRouteConfig{WebRoot: root, PublicPath: "/public"})
	engine.GET("/ws/registered", func(c *gin.Context) { c.String(http.StatusNoContent, "") })
	engine.GET("/control/forbidden", func(c *gin.Context) { c.AbortWithStatus(http.StatusForbidden) })
	controlStarted := make(chan struct{})
	controlCanceled := make(chan struct{})
	engine.GET("/control/cancel", func(c *gin.Context) {
		close(controlStarted)
		<-c.Request.Context().Done()
		close(controlCanceled)
	})
	server := httptest.NewServer(engine)
	defer server.Close()
	client := server.Client()

	request, err := http.NewRequest(http.MethodGet, server.URL+"/socket/missing", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Accept", "text/html")
	request.Header.Set("Connection", "Upgrade")
	request.Header.Set("Upgrade", "websocket")
	request.Header.Set("Sec-WebSocket-Version", "13")
	request.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusNotFound || strings.Contains(string(body), "release-web-marker") {
		t.Fatalf("missing websocket upgrade status=%d body=%q; expected a non-SPA 404", response.StatusCode, body)
	}

	pageStatus, pageBody := requestStaticTest(t, client, server.URL+"/dashboard/overview", "text/html")
	if pageStatus != http.StatusOK || !strings.Contains(pageBody, "release-web-marker") {
		t.Fatalf("ordinary HTML route status=%d body=%q; expected SPA index", pageStatus, pageBody)
	}

	registeredRequest, err := http.NewRequest(http.MethodGet, server.URL+"/ws/registered", nil)
	if err != nil {
		t.Fatal(err)
	}
	registeredRequest.Header.Set("Accept", "text/html")
	registeredRequest.Header.Set("Connection", "Upgrade")
	registeredRequest.Header.Set("Upgrade", "websocket")
	registeredResponse, err := client.Do(registeredRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer registeredResponse.Body.Close()
	if registeredResponse.StatusCode != http.StatusNoContent {
		t.Fatalf("registered websocket control route status=%d, want %d", registeredResponse.StatusCode, http.StatusNoContent)
	}

	permissionResponse, err := client.Get(server.URL + "/control/forbidden")
	if err != nil {
		t.Fatal(err)
	}
	defer permissionResponse.Body.Close()
	permissionBody, err := io.ReadAll(permissionResponse.Body)
	if err != nil {
		t.Fatal(err)
	}
	if permissionResponse.StatusCode != http.StatusForbidden || strings.Contains(string(permissionBody), "release-web-marker") {
		t.Fatalf("registered permission route status=%d body=%q; expected middleware response without SPA fallback", permissionResponse.StatusCode, permissionBody)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cancelRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/control/cancel", nil)
	if err != nil {
		t.Fatal(err)
	}
	requestDone := make(chan error, 1)
	go func() {
		response, requestErr := client.Do(cancelRequest)
		if response != nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
		}
		requestDone <- requestErr
	}()
	select {
	case <-controlStarted:
	case <-time.After(time.Second):
		t.Fatal("registered control route did not start")
	}
	cancel()
	select {
	case <-controlCanceled:
	case <-time.After(time.Second):
		t.Fatal("registered control route did not observe request cancellation")
	}
	select {
	case <-requestDone:
	case <-time.After(time.Second):
		t.Fatal("canceled HTTP request did not return")
	}
}

func TestStaticRoutesPreserveRangeRequestsForScopedFiles(t *testing.T) {
	gin.SetMode(gin.TestMode)
	root := t.TempDir()
	publicRoot := filepath.Join(root, "public")
	if err := os.MkdirAll(publicRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	writeStaticTestFile(t, filepath.Join(publicRoot, "recording.bin"), "0123456789")

	engine := gin.New()
	registerStaticRoutes(engine, staticRouteConfig{PublicPath: "/public", PublicRoot: publicRoot})
	server := httptest.NewServer(engine)
	defer server.Close()
	request, err := http.NewRequest(http.MethodGet, server.URL+"/public/recording.bin", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Range", "bytes=2-5")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusPartialContent || string(body) != "2345" {
		t.Fatalf("range status=%d body=%q; expected 206 with 2345", response.StatusCode, body)
	}
}

func TestStaticRoutesDoNotRegisterAnOpenRootProxy(t *testing.T) {
	engine := gin.New()
	registerStaticRoutes(engine, staticRouteConfig{PublicPath: "/public"})
	for _, route := range engine.Routes() {
		if route.Path == "/*filepath" || route.Path == "/*any" {
			t.Fatalf("unexpected open root wildcard route: %s %s", route.Method, route.Path)
		}
	}
}

func writeStaticTestFile(t *testing.T, name, content string) {
	t.Helper()
	if err := os.WriteFile(name, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func requestStaticTest(t *testing.T, client *http.Client, url, accept string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, string(body)
}

package routes

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

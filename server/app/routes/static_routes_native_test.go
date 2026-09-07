package routes

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// This fixture is started by the Windows browser script. Normal unit runs
// never open a persistent listener or require a built frontend.
func TestServeNativeStandaloneWeb(t *testing.T) {
	root := os.Getenv("UVP_NATIVE_WEB_WORK_ROOT")
	if root == "" {
		t.Skip("requires the isolated Windows browser fixture")
	}
	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()
	registerStaticRoutes(engine, staticRouteConfig{
		WebRoot: filepath.Join(root, "web"), PublicPath: "/public",
		PublicRoot: filepath.Join(root, "public"), UploadRoot: filepath.Join(root, "uploads"),
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := &http.Server{Handler: engine, ReadHeaderTimeout: 5 * time.Second}
	stopped := make(chan error, 1)
	go func() { stopped <- server.Serve(listener) }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, server.Shutdown(ctx))
		require.ErrorIs(t, <-stopped, http.ErrServerClosed)
	}()
	require.NoError(t, os.WriteFile(filepath.Join(root, "ready.txt"), []byte("http://"+listener.Addr().String()), 0600))
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(90 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case <-ticker.C:
			if _, err := os.Stat(filepath.Join(root, "stop.txt")); err == nil {
				return
			}
		case <-deadline.C:
			t.Fatal("native browser fixture did not stop within 90 seconds")
		}
	}
}

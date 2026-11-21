package videoshareprobe

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestNginxRelayRevocation(t *testing.T) {
	if os.Getenv("VIDEO_SHARE_PROBE_NGINX") == "" {
		t.Skip("set VIDEO_SHARE_PROBE_NGINX to the packaged nginx binary")
	}
	for _, slow := range []bool{false, true} {
		t.Run(fmt.Sprintf("slow=%t", slow), func(t *testing.T) {
			source := continuousSource(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _ = relay(ctx, w, source.URL) }))
			defer backend.Close()
			endpoint := startNginx(t, backend.URL)
			client := &http.Client{Timeout: 8 * time.Second}
			resp, err := client.Get(endpoint)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			b := make([]byte, 1024)
			if _, err = io.ReadFull(resp.Body, b); err != nil {
				t.Fatal(err)
			}
			if slow {
				time.Sleep(300 * time.Millisecond)
			}
			revoked := time.Now()
			cancel()
			var total int
			last := revoked
			for {
				n, e := resp.Body.Read(b)
				if n > 0 {
					total += n
					last = time.Now()
					if slow {
						time.Sleep(20 * time.Millisecond)
					}
				}
				if e != nil {
					break
				}
			}
			lag := last.Sub(revoked)
			if lag > 3*time.Second {
				t.Fatalf("last-byte lag %s exceeds fixture budget", lag)
			}
			t.Logf("nginx receive last-byte lag=%s bytes=%d slow=%t", lag, total, slow)
		})
	}
}

func startNginx(t *testing.T, upstream string) string {
	t.Helper()
	binary := os.Getenv("VIDEO_SHARE_PROBE_NGINX")
	if binary == "" {
		t.Skip("nginx binary not configured")
	}
	root := t.TempDir()
	port := freePort(t)
	config := fmt.Sprintf(`daemon off;
master_process off;
pid %s/nginx.pid;
error_log %s/error.log warn;
events { worker_connections 64; }
http {
 access_log off;
 client_body_temp_path %s/body;
 proxy_temp_path %s/proxy;
 server { listen 127.0.0.1:%d;
 location / { proxy_pass %s; proxy_http_version 1.1;
 proxy_buffering off; proxy_cache off; gzip off;
 proxy_read_timeout 10s; send_timeout 3s;
 }
 }
}`, root, root, root, root, port, upstream)
	name := filepath.Join(root, "nginx.conf")
	if err := os.WriteFile(name, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-p", root, "-c", name)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stopFixture(cmd) })
	waitPort(t, port)
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	return port
}

func waitPort(t *testing.T, port int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("isolated fixture port %d not ready", port)
}

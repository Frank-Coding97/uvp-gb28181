package videoshare

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

// TestProductionRelayBehindNginx is opt-in because it starts an isolated nginx
// process. It exercises Service.RelayHTTPFLV, not the diagnostic probe relay.
func TestProductionRelayBehindNginx(t *testing.T) {
	nginxBinary := os.Getenv("VIDEO_SHARE_TEST_NGINX")
	if nginxBinary == "" {
		t.Skip("set VIDEO_SHARE_TEST_NGINX to an nginx executable")
	}
	for _, tc := range []struct {
		name          string
		slow, expires bool
	}{
		{name: "disable_normal"},
		{name: "disable_slow_reader", slow: true},
		{name: "natural_expiry", expires: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Eight seconds covers the production five-second authorization poll plus
			// its two-second I/O budget and a second of fixture scheduling slack.
			// This is deliberately stricter than the product's thirty-second limit.
			const cutoffBudget = 8 * time.Second
			sourceStopped := make(chan struct{})
			var stopOnce sync.Once
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer stopOnce.Do(func() { close(sourceStopped) })
				w.Header().Set("Content-Type", "video/x-flv")
				flusher := w.(http.Flusher)
				if _, err := w.Write([]byte{'F', 'L', 'V', 1, 1, 0, 0, 0, 9, 0, 0, 0, 0}); err != nil {
					return
				}
				flusher.Flush()
				ticker := time.NewTicker(25 * time.Millisecond)
				defer ticker.Stop()
				frame := bytes.Repeat([]byte{0x56}, 512)
				var sequence uint64
				for {
					select {
					case <-r.Context().Done():
						return
					case <-ticker.C:
						sequence++
						binary.BigEndian.PutUint64(frame, sequence)
						_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(2 * time.Second))
						if _, err := w.Write(frame); err != nil {
							return
						}
						flusher.Flush()
					}
				}
			}))
			t.Cleanup(func() { source.CloseClientConnections(); source.Close() })
			service, channelPK, _ := newServiceFixture(t)
			service.now = time.Now
			link, err := service.SetEnabled(context.Background(), channelPK, 9, true, 0)
			require.NoError(t, err)
			var cutoff time.Time
			if tc.expires {
				if os.Getenv("VIDEO_SHARE_TEST_REAL_EXPIRY") == "1" {
					cutoff = *link.ExpiresAt // Exercise the issued five-minute lifetime without shortening it.
				} else {
					cutoff = time.Now().Add(3 * time.Second).UTC()
					require.NoError(t, service.db.Model(&gbmodels.GbChannelVideoShare{}).Where("channel_pk = ?", channelPK).Update("expires_at", cutoff).Error)
					link.Signature, err = service.signer.Sign(Claims{PublicID: link.PublicID, Generation: link.Generation, ExpiresAt: cutoff.Unix(), KeyID: KeyID})
					require.NoError(t, err)
				}
			}
			service.SetPlaybackPreparer(playbackPreparerFunc(func(context.Context, *gbmodels.GbChannelVideoShare) (PlaybackSource, error) {
				return PlaybackSource{StreamID: "proxy-fixture", Generation: 1, HTTPFLVURL: source.URL}, nil
			}))
			viewer, err := service.CreateViewer(context.Background(), link.PublicID, link.Signature)
			require.NoError(t, err)
			t.Cleanup(service.DrainViewers)
			relayStopped := make(chan struct{})
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(relayStopped)
				service.RelayHTTPFLV(r.Context(), w, viewer.Token)
			}))
			t.Cleanup(func() { backend.CloseClientConnections(); backend.Close() })
			endpoint := startShareProxyNginx(t, nginxBinary, backend.URL)
			requestTimeout := 30 * time.Second
			if tc.expires {
				requestTimeout += time.Until(cutoff)
			}
			requestCtx, cancel := context.WithTimeout(context.Background(), requestTimeout)
			defer cancel()
			request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint+"/live.flv", nil)
			require.NoError(t, err)
			transport := &http.Transport{Proxy: nil, DisableKeepAlives: true}
			defer transport.CloseIdleConnections()
			response, err := (&http.Client{Transport: transport}).Do(request)
			require.NoError(t, err)
			defer response.Body.Close()
			require.Equal(t, http.StatusOK, response.StatusCode)
			require.Equal(t, "video/x-flv", response.Header.Get("Content-Type"))
			prefix := make([]byte, 13)
			_, err = io.ReadFull(response.Body, prefix)
			require.NoError(t, err)
			require.Equal(t, "FLV", string(prefix[:3]))
			// Establish media delivery before revocation; slow readers also leave a
			// bounded backlog in the real nginx/socket path before beginning to drain.
			firstFrame := make([]byte, 512)
			_, err = io.ReadFull(response.Body, firstFrame)
			require.NoError(t, err)
			if tc.slow {
				time.Sleep(500 * time.Millisecond)
			}
			type observedRead struct {
				last  time.Time
				bytes int
				err   error
			}
			reads := make(chan observedRead, 1)
			var latestMedia atomic.Int64
			go func() {
				result := observedRead{last: time.Now()}
				buffer := make([]byte, 1024)
				for {
					n, err := response.Body.Read(buffer)
					if n > 0 {
						result.last = time.Now()
						latestMedia.Store(result.last.UnixNano())
						result.bytes += n
					}
					if err != nil {
						result.err = err
						reads <- result
						return
					}
					if tc.slow {
						select {
						case <-requestCtx.Done():
							result.err = requestCtx.Err()
							reads <- result
							return
						case <-time.After(40 * time.Millisecond):
						}
					}
				}
			}()
			if tc.expires {
				select {
				case <-reads:
					t.Fatal("media ended before link expiry")
				case <-time.After(time.Until(cutoff.Add(time.Second))):
				}
				require.Greater(t, latestMedia.Load(), cutoff.UnixNano(), "proxy must deliver new media after natural link expiry")
				_, err = service.Resolve(context.Background(), link.PublicID, link.Signature)
				require.ErrorIs(t, err, ErrExpired)
				_, err = service.LookupViewer(context.Background(), viewer.Token)
				require.NoError(t, err)
				_, err = service.ScanExpired(context.Background(), 100)
				require.NoError(t, err)
			}
			state, err := service.Status(context.Background(), channelPK, 9)
			require.NoError(t, err)
			_, err = service.SetEnabled(context.Background(), channelPK, 9, false, state.Revision)
			require.NoError(t, err)
			cutoff = time.Now() // Explicit close committed, including an already-expired link.
			timeout := time.NewTimer(time.Until(cutoff.Add(cutoffBudget)))
			defer timeout.Stop()
			select {
			case result := <-reads:
				require.True(t, errors.Is(result.err, io.EOF) || errors.Is(result.err, io.ErrUnexpectedEOF), "the proxy must end the media response, not wait for the client timeout")
				lag := result.last.Sub(cutoff)
				require.LessOrEqual(t, lag, cutoffBudget)
				require.LessOrEqual(t, lag, 30*time.Second)
				t.Logf("last media byte after cutoff=%s received=%d slow=%t expiry=%t", lag, result.bytes, tc.slow, tc.expires)
			case <-timeout.C:
				cancel()
				_ = response.Body.Close()
				<-reads
				t.Fatalf("production relay did not close within %s of cutoff (product limit 30s)", cutoffBudget)
			}
			select {
			case <-relayStopped:
			case <-time.After(time.Second):
				t.Fatal("production relay handler did not finish")
			}
			select {
			case <-sourceStopped:
			case <-time.After(3 * time.Second):
				t.Fatal("upstream media source was not released")
			}
		})
	}
}

func startShareProxyNginx(t *testing.T, binaryPath, backend string) string {
	t.Helper()
	root := t.TempDir()
	for _, directory := range []string{"body", "proxy"} {
		require.NoError(t, os.Mkdir(filepath.Join(root, directory), 0700))
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())
	config := fmt.Sprintf(`daemon off;
master_process off;
pid "%s/nginx.pid";
error_log "%s/error.log" warn;
events { worker_connections 32; }
http {
 access_log off;
 client_body_temp_path "%s/body";
 proxy_temp_path "%s/proxy";
 server {
  listen 127.0.0.1:%d;
  location /live.flv {
   proxy_pass %s;
   proxy_http_version 1.1;
   proxy_set_header Connection "";
   proxy_buffering off;
   proxy_request_buffering off;
   proxy_cache off;
   gzip off;
   proxy_read_timeout 30s;
   send_timeout 30s;
  }
 }
}
`, root, root, root, root, port, backend)
	configPath := filepath.Join(root, "nginx.conf")
	require.NoError(t, os.WriteFile(configPath, []byte(config), 0600))
	cmd := exec.Command(binaryPath, "-p", root+string(os.PathSeparator), "-c", configPath)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	require.NoError(t, cmd.Start())
	exited := make(chan struct{})
	var exitErr error
	go func() { exitErr = cmd.Wait(); close(exited) }()
	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)
		select {
		case <-exited:
		case <-time.After(2 * time.Second):
			_ = cmd.Process.Kill()
			<-exited
		}
	})
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	address := fmt.Sprintf("127.0.0.1:%d", port)
	for {
		select {
		case <-exited:
			t.Fatalf("isolated nginx exited before listening: %v; %s", exitErr, output.String())
		case <-deadline.C:
			t.Fatal("isolated nginx did not listen on its random loopback port")
		case <-ticker.C:
			connection, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
			if err == nil {
				_ = connection.Close()
				return "http://" + address
			}
		}
	}
}

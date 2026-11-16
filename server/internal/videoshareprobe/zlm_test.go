package videoshareprobe

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Opt-in: use packaged executables plus synthetic media, never an existing
// device's stream or a running ZLM instance. All listeners are loopback-only.
func TestPackagedZLMRelay(t *testing.T) {
	binary := os.Getenv("VIDEO_SHARE_PROBE_ZLM")
	ffmpeg := os.Getenv("VIDEO_SHARE_PROBE_FFMPEG")
	sample := os.Getenv("VIDEO_SHARE_PROBE_SAMPLE")
	if binary == "" || ffmpeg == "" || sample == "" {
		t.Skip("set packaged ZLM, ffmpeg and synthetic sample paths")
	}
	base := startZLM(t, binary, ffmpeg, sample, "")
	t.Run("FLV", func(t *testing.T) {
		for _, mode := range []string{"revoke", "deadline"} {
			t.Run(mode, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				var expiry time.Time
				if mode == "deadline" {
					cancel()
					expiry = time.Now().Add(2 * time.Second)
					ctx, cancel = context.WithDeadline(context.Background(), expiry)
					defer cancel()
				}
				backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if ctx.Err() != nil {
						http.Error(w, "expired", 410)
						return
					}
					_ = relay(ctx, w, base+"/probe/live.live.flv")
				}))
				defer backend.Close()
				endpoint := startNginx(t, backend.URL)
				client := &http.Client{Timeout: 6 * time.Second}
				resp, err := client.Get(endpoint)
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				header := make([]byte, 3)
				if _, err = io.ReadFull(resp.Body, header); err != nil || string(header) != "FLV" {
					t.Fatalf("invalid FLV: %q %v", header, err)
				}
				if mode == "revoke" {
					time.Sleep(300 * time.Millisecond)
				}
				invalidated := expiry
				if mode == "revoke" {
					invalidated = time.Now()
					cancel()
				}
				last := time.Now()
				var count int
				b := make([]byte, 8192)
				for {
					n, e := resp.Body.Read(b)
					if n > 0 {
						last = time.Now()
						count += n
					}
					if e != nil {
						break
					}
				}
				lag := last.Sub(invalidated)
				if lag > 3*time.Second {
					t.Fatalf("receiver last-byte lag=%s", lag)
				}
				t.Logf("ZLM H264/AAC FLV through nginx: %d bytes, last-byte lag=%s", count, lag)
				again, err := client.Get(endpoint)
				if err != nil {
					t.Fatal(err)
				}
				again.Body.Close()
				if again.StatusCode != 410 {
					t.Fatalf("new access status=%d", again.StatusCode)
				}
			})
		}
	})
	t.Run("HLS", func(t *testing.T) {
		playlistURL := base + "/probe/live/hls.m3u8"
		var playlist string
		client := &http.Client{Timeout: 3 * time.Second}
		deadline := time.Now().Add(12 * time.Second)
		for time.Now().Before(deadline) {
			resp, err := client.Get(playlistURL)
			if err == nil {
				data, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
				resp.Body.Close()
				if resp.StatusCode == 200 && strings.Contains(string(data), "#EXTINF:") {
					playlist = string(data)
					break
				}
			}
			time.Sleep(100 * time.Millisecond)
		}
		if playlist == "" {
			t.Fatal("ZLM HLS playlist unavailable")
		}
		// This fixture maps exactly the observed plain MPEG-TS references. General
		// child/key/map URI parsing and per-resource authorization remain T13.
		var segments []string
		for _, line := range strings.Split(playlist, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if strings.Contains(line, ":") || strings.HasPrefix(line, "/") || strings.Contains(line, "..") {
				t.Fatalf("unexpected fixture resource %q", line)
			}
			segments = append(segments, base+"/probe/live/"+line)
		}
		if len(segments) == 0 {
			t.Fatal("no HLS segments")
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if ctx.Err() != nil {
				http.Error(w, "expired", 410)
				return
			}
			if r.URL.Path == "/segment/0" {
				_ = relay(ctx, w, segments[0])
				return
			}
			http.NotFound(w, r)
		}))
		defer backend.Close()
		endpoint := startNginx(t, backend.URL)
		resp, err := client.Get(endpoint + "/segment/0")
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if len(data) < 188 || data[0] != 0x47 {
			t.Fatalf("invalid MPEG-TS segment: %d bytes", len(data))
		}
		cancel()
		resp, err = client.Get(endpoint + "/segment/0")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 410 {
			t.Fatalf("expired segment status=%d", resp.StatusCode)
		}
		t.Logf("ZLM HLS playlist=%d bytes references=%d, proxied TS=%d bytes; revoked segment rejected", len(playlist), len(segments), len(data))
	})
}

func startZLM(t *testing.T, binary, ffmpeg, sample, hookURL string) string {
	t.Helper()
	root := t.TempDir()
	httpPort, rtmpPort := freePort(t), freePort(t)
	config := fmt.Sprintf(`[general]
listen_ip=127.0.0.1
mediaServerId=video-share-probe
[http]
port=%d
sslport=0
rootPath=%s/www
dirMenu=0
[rtmp]
port=%d
sslport=0
[rtsp]
port=0
sslport=0
[rtc]
port=0
tcpPort=0
signalingPort=0
signalingSslPort=0
icePort=0
iceTcpPort=0
[srt]
port=0
[shell]
port=0
[rtp_proxy]
port=0
[hook]
enable=1
on_play=%s
[protocol]
enable_hls=1
hls_demand=0
hls_save_path=%s/www
enable_mp4=0
enable_rtsp=0
enable_rtmp=1
[hls]
segDur=1
segNum=3
segDelay=0
`, httpPort, root, rtmpPort, hookURL, root)
	name := filepath.Join(root, "zlm.ini")
	if err := os.WriteFile(name, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	launchFixture(t, exec.Command(binary, "-c", name, "-l", "3", "-t", "1", "--log-dir", filepath.Join(root, "logs")))
	waitPort(t, httpPort)
	waitPort(t, rtmpPort)
	launchFixture(t, exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-re", "-stream_loop", "-1", "-i", sample, "-c", "copy", "-f", "flv", fmt.Sprintf("rtmp://127.0.0.1:%d/probe/live", rtmpPort)))
	// Allow RTMP publication to finish before opening the FLV response.
	time.Sleep(750 * time.Millisecond)
	return fmt.Sprintf("http://127.0.0.1:%d", httpPort)
}

func launchFixture(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	logFile, err := os.CreateTemp(t.TempDir(), "process-*.log")
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err = cmd.Start(); err != nil {
		logFile.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { stopFixture(cmd); logFile.Close() })
}

// Do not leave child media processes alive when a failed probe is torn down.
func stopFixture(cmd *exec.Cmd) {
	_ = cmd.Process.Signal(os.Interrupt)
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		_ = cmd.Process.Kill()
		<-done
	}
}

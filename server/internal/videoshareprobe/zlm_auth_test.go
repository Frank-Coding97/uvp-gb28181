package videoshareprobe

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"uvplatform.com/uvp-gb28181/app/gb28181/playauth"
)

// This checks the real node's observed origin against the existing signer.
// The production HookController/database permission adapter is not installed by
// this transport probe, so this does not replace VS01-04's application test.
func TestPackagedZLMSignerOrigin(t *testing.T) {
	binary, ffmpeg, sample := os.Getenv("VIDEO_SHARE_PROBE_ZLM"), os.Getenv("VIDEO_SHARE_PROBE_FFMPEG"), os.Getenv("VIDEO_SHARE_PROBE_SAMPLE")
	if binary == "" || ffmpeg == "" || sample == "" {
		t.Skip("packaged media fixture not configured")
	}
	for _, mode := range []string{"off", "token", "ip"} {
		t.Run(mode, func(t *testing.T) {
			signer, err := playauth.NewSigner(bytes.Repeat([]byte{0x73}, 32))
			if err != nil {
				t.Fatal(err)
			}
			binding := playauth.Binding{DeviceID: "34020000002000000001", ChannelID: "34020000001320000001", DeviceEpoch: 1, App: "probe", Stream: "live", MediaServerID: "video-share-probe", MediaGeneration: 1, BindClientIP: mode == "ip", ClientIP: "127.0.0.1"}
			var calls atomic.Int32
			hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var event struct {
					IP     string `json:"ip"`
					Params string `json:"params"`
				}
				if err := json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&event); err != nil {
					http.Error(w, "invalid", 400)
					return
				}
				calls.Add(1)
				code := 0
				if mode != "off" {
					expected := binding
					expected.ClientIP = event.IP
					values, _ := url.ParseQuery(event.Params)
					if _, err := signer.Verify(values.Get(playauth.QueryParameter), expected); err != nil {
						code = -1
					}
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "msg": "probe", "enable_hls": true})
			}))
			defer hook.Close()
			base := startZLM(t, binary, ffmpeg, sample, hook.URL)
			client := &http.Client{Timeout: 3 * time.Second}
			check := func(token string, allowed bool) {
				t.Helper()
				endpoint := base + "/probe/live.live.flv"
				if token != "" {
					endpoint += "?play_token=" + url.QueryEscape(token)
				}
				resp, err := client.Get(endpoint)
				if err != nil {
					t.Fatalf("probe media request failed (allowed=%t)", allowed)
				}
				defer resp.Body.Close()
				header := make([]byte, 3)
				_, readErr := io.ReadFull(resp.Body, header)
				isFLV := resp.StatusCode == 200 && readErr == nil && string(header) == "FLV"
				if isFLV != allowed {
					t.Fatalf("allow=%t status=%d media=%t", allowed, resp.StatusCode, isFLV)
				}
			}
			check("", mode == "off")
			if mode != "off" {
				grant, err := signer.IssueDirect(binding)
				if err != nil {
					t.Fatal(err)
				}
				check(grant.Token, true)
				if mode == "ip" {
					wrong := binding
					wrong.ClientIP = "192.0.2.44"
					badGrant, err := signer.IssueDirect(wrong)
					if err != nil {
						t.Fatal(err)
					}
					check(badGrant.Token, false)
				}
			}
			if calls.Load() == 0 {
				t.Fatal("ZLM did not call on_play; authentication probe invalid")
			}
			t.Logf("real ZLM on_play + production signer: mode=%s callbacks=%d; IP binding uses node-observed loopback origin", mode, calls.Load())
		})
	}
}

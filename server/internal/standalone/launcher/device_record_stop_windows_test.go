//go:build windows

package launcher

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestWindowsStandaloneRecordingStop(t *testing.T) {
	if os.Getenv("UVP_CORE_RECORD_STOP") != "1" {
		t.Skip("set UVP_CORE_RECORD_STOP=1 for isolated real recording shutdown")
	}
	runWindowsStandaloneDeviceCorePath(t, true)
}

func coreEnableRecording(t *testing.T, client *t18HTTPClient, id uint, evidence *coreEvidence) {
	t.Helper()
	if id == 0 {
		coreFail(t, evidence, "recording_enable", "channel_row_id_missing")
	}
	status, _, body := client.request(t, http.MethodPatch, fmt.Sprintf("/api/gb28181/device-mgmt/channel/%d/cloud-recording", id), "", []byte(`{"enabled":true}`))
	var channel struct {
		Enabled bool `json:"cloudRecordingEnabled"`
	}
	if status != http.StatusOK || !coreResponseData(body, &channel) || !channel.Enabled {
		t.Logf("recording enable HTTP status: %d", status)
		coreFail(t, evidence, "recording_enable", "recording_setting_not_confirmed")
	}
	t.Log("RECORDING_ENABLED")
}

func coreVerifyRecording(t *testing.T, client *t18HTTPClient, id uint, evidence *coreEvidence) {
	t.Helper()
	status, _, body := client.request(t, http.MethodGet, fmt.Sprintf("/api/gb28181/cloud-recordings/files?page=1&pageSize=50&channelId=%d", id), "", nil)
	var result struct {
		List []struct {
			ID            string `json:"id"`
			Source        string `json:"source"`
			MetadataState string `json:"metadataState"`
			Availability  string `json:"availability"`
			FileSize      int64  `json:"fileSize"`
		} `json:"list"`
	}
	if status != http.StatusOK || !coreResponseData(body, &result) || len(result.List) == 0 {
		t.Logf("recording list HTTP status: %d", status)
		coreFail(t, evidence, "recording_restart", "recording_index_missing")
	}
	for _, file := range result.List {
		if file.Source == "hook" && file.MetadataState == "complete" && file.Availability == "available" && file.FileSize > 0 {
			status, _, body := client.request(t, http.MethodPost, "/api/gb28181/cloud-recordings/files/"+url.PathEscape(file.ID)+"/access", "", []byte(`{"mode":"play"}`))
			var grant struct {
				Capability string `json:"capability"`
			}
			if status != http.StatusOK || !coreResponseData(body, &grant) || grant.Capability == "" {
				coreFail(t, evidence, "recording_read", "access_grant_rejected")
			}
			response, err := client.client.Get(strings.TrimRight(client.baseURL, "/") + "/api/gb28181/cloud-recordings/content/" + url.PathEscape(file.ID) + "?cap=" + url.QueryEscape(grant.Capability))
			if err != nil {
				coreFail(t, evidence, "recording_read", "content_request_failed")
			}
			content, readErr := io.ReadAll(io.LimitReader(response.Body, 32*1024*1024+1))
			response.Body.Close()
			if readErr != nil || response.StatusCode != http.StatusOK || int64(len(content)) != file.FileSize || len(content) < 12 || !bytes.Equal(content[4:8], []byte("ftyp")) {
				t.Logf("recording content HTTP=%d received=%d indexed=%d contentType=%s", response.StatusCode, len(content), file.FileSize, response.Header.Get("Content-Type"))
				coreFail(t, evidence, "recording_read", "mp4_content_or_index_size_mismatch")
			}
			t.Logf("RECORDING_INDEX_READY id=%s bytes=%d", file.ID, file.FileSize)
			return
		}
	}
	coreFail(t, evidence, "recording_restart", "complete_hook_recording_missing")
}

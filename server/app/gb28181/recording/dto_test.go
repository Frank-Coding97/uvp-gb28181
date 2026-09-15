package recording

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func TestNewFileDTOExposesPublicCatalogFieldsOnly(t *testing.T) {
	start := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	duration := 60.5
	size := uint64(1024)
	file := models.GbRecordingFile{
		ID: 9007199254740993, FileKey: "a35d8d0f5d2146c8a37be5c88202bccfc86421806df200b22f272fba6fe08f3b",
		ChannelID: 10, ChannelCode: "34020000001320000001", ChannelName: "东门", DeviceID: "34020000002000000001", DeviceName: "一号 NVR",
		NodeID: 2, FileName: "a.mp4", FilePath: "/private/a.mp4", Folder: "/private", URL: "http://internal/a.mp4",
		StartTime: &start, TimeLen: &duration, FileSize: &size, Source: models.RecordingFileSourceHook, MetadataState: models.RecordingMetadataComplete,
	}

	dto := NewFileDTO(file, NodeDTO{ID: "2", Name: "node-a", State: "active"}, "available")
	require.Equal(t, "9007199254740993", dto.ID)
	require.Equal(t, start.Add(60500*time.Millisecond), *dto.EndTime)
	require.Equal(t, "available", dto.Availability)

	body, err := json.Marshal(dto)
	require.NoError(t, err)
	jsonText := string(body)
	for _, internal := range []string{"filePath", "folder", "url", "apiSecret", "host"} {
		require.NotContains(t, jsonText, internal)
	}
	require.Contains(t, jsonText, `"id":"9007199254740993"`)
}

func TestNewFileDTOPreservesUnknownPartialMetadata(t *testing.T) {
	dto := NewFileDTO(models.GbRecordingFile{ID: 1, MetadataState: models.RecordingMetadataPartial}, NodeDTO{ID: "2"}, "available")
	require.Nil(t, dto.StartTime)
	require.Nil(t, dto.EndTime)
	require.Nil(t, dto.TimeLen)
	require.Nil(t, dto.FileSize)
}

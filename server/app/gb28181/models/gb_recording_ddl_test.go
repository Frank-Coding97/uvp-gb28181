package models

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRecordingFileDDLStaysWithinInnoDBKeyLimit(t *testing.T) {
	_, sourceFile, _, _ := runtime.Caller(0)
	serverRoot := filepath.Join(filepath.Dir(sourceFile), "..", "..", "..")
	files := []string{
		filepath.Join(serverRoot, "resource/database/gb28181/gb_recording_file.sql"),
		filepath.Join(serverRoot, "resource/database/gb28181/migrations/2026-07-22-channel-cloud-recording.sql"),
	}
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("读取 %s: %v", file, err)
		}
		if !strings.Contains(string(content), "`file_path`(766)") {
			t.Errorf("%s 的复合唯一索引必须使用 766 字符前缀,确保 utf8mb4 key 不超过 3072 bytes", file)
		}
	}
}

package snapshot

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSanitize_KeepsAlnum(t *testing.T) {
	got := sanitize("34020000001180000001")
	if got != "34020000001180000001" {
		t.Errorf("正常数字编码应保留,got=%s", got)
	}
}

func TestSanitize_ReplacesSpecialChars(t *testing.T) {
	got := sanitize("34020../etc")
	// . . / 三个非法字符各替成 _,长度不变
	if got != "34020___etc" {
		t.Errorf("../ 应替成 _,got=%s", got)
	}
}

func TestSnapshotPath_LayoutIsStable(t *testing.T) {
	ts := time.Date(2026, 7, 20, 12, 34, 56, 0, time.UTC)
	abs, url := snapshotPath("/tmp/uploads", "/uploads", "did1", "cid1", ts)
	wantAbs := filepath.Join("/tmp/uploads", "gb-channel-snapshot", "2026-07", "did1_cid1.jpg")
	if abs != wantAbs {
		t.Errorf("abs 期望 %s,实际 %s", wantAbs, abs)
	}
	if url != "/uploads/gb-channel-snapshot/2026-07/did1_cid1.jpg" {
		t.Errorf("URL 期望 /uploads/gb-channel-snapshot/2026-07/did1_cid1.jpg,实际 %s", url)
	}
}

func TestWriteSnapshotFile_CreatesParentDir(t *testing.T) {
	tmp := t.TempDir()
	abs := filepath.Join(tmp, "gb-channel-snapshot", "2026-07", "did1_cid1.jpg")
	fakeJPEG := []byte{0xFF, 0xD8, 0xFF, 'X', 'Y'}
	if err := writeSnapshotFile(abs, fakeJPEG); err != nil {
		t.Fatalf("落盘失败: %v", err)
	}
	got, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	if string(got) != string(fakeJPEG) {
		t.Errorf("内容不一致")
	}
}

func TestWriteSnapshotFile_Overwrites(t *testing.T) {
	tmp := t.TempDir()
	abs := filepath.Join(tmp, "x.jpg")
	if err := writeSnapshotFile(abs, []byte("v1")); err != nil {
		t.Fatal(err)
	}
	if err := writeSnapshotFile(abs, []byte("v2")); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(abs)
	if string(got) != "v2" {
		t.Errorf("应覆盖写入,got=%s", got)
	}
}

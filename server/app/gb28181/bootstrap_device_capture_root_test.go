package gb28181

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// captureImageDirFor 还原**图片真正落盘的那一级目录**：
// `devicecapture.Registry.Upload` 里是 `filepath.Join(root, "gb-device-snapshots", sessionID)`，
// 所以断言必须打在拼完之后的路径上 —— 只测 `deviceCaptureBaseDirFor` 的返回值会漏掉
// "目录名重复一层"这类错（写这段时就真的先写错过一次）。
func captureImageDirFor(serverroot string) string {
	return filepath.Join(deviceCaptureBaseDirFor(serverroot), "gb-device-snapshots")
}

// TestDeviceCaptureDirIsOutsideStaticRoot 钉住 2026-09-20 修的那个洞：
// 设备抓拍图**不能**落在 `httpserver.serverroot`（默认 `./resource/public`）里 ——
// 那个目录被 `engine.Static(serverrootpath, serverroot)`（默认 `/public`）**免鉴权**挂载，
// 实测 `curl /public/gb-channel-snapshot/…jpg` → 200 + 真图。
//
// 落回去的后果不是"多暴露一个文件"，而是 `snapshotUploadURL` 下发的 token **整条失效**：
// 图片路径 = 静态根 + 固定两段，谁拿到路径谁就能取图，不需要 token。
func TestDeviceCaptureDirIsOutsideStaticRoot(t *testing.T) {
	cases := []struct {
		name       string
		serverroot string
	}{
		{"默认相对路径", ""},
		{"显式相对路径", "./resource/public"},
		{"带尾斜杠", "resource/public/"},
		{"绝对路径", "/var/www/uvp/resource/public"},
		{"静态根就在当前目录", "public"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			staticRoot := tc.serverroot
			if strings.TrimSpace(staticRoot) == "" {
				staticRoot = "./resource/public"
			}
			imageDir := captureImageDirFor(tc.serverroot)

			rel, err := filepath.Rel(filepath.Clean(staticRoot), imageDir)
			require.NoError(t, err)
			require.True(t, strings.HasPrefix(rel, ".."),
				"抓拍图片目录必须在静态根之外：static=%q imageDir=%q rel=%q", staticRoot, imageDir, rel)

			// 反向锚点：目录名不许重复出现 `gb-device-snapshots`
			// （`Upload` 已经拼了一层，baseDir 再拼一层就是 `…/gb-device-snapshots/gb-device-snapshots/…`）。
			require.Equal(t, "gb-device-snapshots", filepath.Base(imageDir))
			require.NotContains(t, filepath.Base(filepath.Dir(imageDir)), "gb-device-snapshots")
		})
	}
}

// 具体取值锚点：默认配置下图片必须落在 `resource/gb-device-snapshots`
// （`./resource/public` 的兄弟目录，不是它的子目录）。
func TestDeviceCaptureDirDefaultValue(t *testing.T) {
	require.Equal(t, filepath.Join("resource", "gb-device-snapshots"), captureImageDirFor(""))
	require.Equal(t, filepath.Join("resource", "gb-device-snapshots"), captureImageDirFor("./resource/public"))
	require.Equal(t, filepath.Join("/var/www/uvp/resource", "gb-device-snapshots"),
		captureImageDirFor("/var/www/uvp/resource/public"))
}

package snapshot

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"time"
)

// snapshotPath 根据设备 / 通道编码生成落盘绝对路径 + 前端可访问的相对 URL。
//
// root:     upload 根目录(如 ./resource/public/uploads)
// urlPrefix: URL 前缀(如 /uploads)
// 布局:   <root>/gb-channel-snapshot/<yyyy-MM>/<safeDid>_<safeCid>.jpg
// URL:    <urlPrefix>/gb-channel-snapshot/<yyyy-MM>/<safeDid>_<safeCid>.jpg
func snapshotPath(root, urlPrefix, deviceID, channelID string, ts time.Time) (absPath, relURL string) {
	month := ts.Format("2006-01")
	fname := fmt.Sprintf("%s_%s.jpg", sanitize(deviceID), sanitize(channelID))
	absPath = filepath.Join(root, "gb-channel-snapshot", month, fname)
	relURL = path.Join(urlPrefix, "gb-channel-snapshot", month, fname)
	return
}

// sanitize 保留 [A-Za-z0-9_-],其余替成 _;防路径遍历(../)与非法字符污染。
// GB28181 通道编码正常是 20 位数字,兜底一下不会误伤。
func sanitize(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z':
			b[i] = c
		case c >= 'a' && c <= 'z':
			b[i] = c
		case c >= '0' && c <= '9':
			b[i] = c
		case c == '_' || c == '-':
			b[i] = c
		default:
			b[i] = '_'
		}
	}
	return string(b)
}

// writeSnapshotFile 覆盖式写入 JPEG bytes。父目录不存在自动创建。
func writeSnapshotFile(absPath string, bytes []byte) error {
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("创建快照目录失败 %s: %w", dir, err)
	}
	if err := os.WriteFile(absPath, bytes, 0o644); err != nil {
		return fmt.Errorf("写快照文件失败 %s: %w", absPath, err)
	}
	return nil
}

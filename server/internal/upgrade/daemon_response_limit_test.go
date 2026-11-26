//go:build !windows

package upgrade

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// startOversizedResponder 起一个只回一条写死响应的假升级助手，用来验证
// 客户端读取响应时的上限与请求上限解耦。
func startOversizedResponder(t *testing.T, payload func() ProtocolResponse) string {
	t.Helper()
	// AF_UNIX 路径上限很短，别用 t.TempDir()（测试名较长时容易超限）。
	dir, err := os.MkdirTemp("", "uvpsock")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socketPath := filepath.Join(dir, "u.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		if _, err := reader.ReadBytes('\n'); err != nil {
			return
		}
		_ = json.NewEncoder(conn).Encode(payload())
	}()
	return socketPath
}

// 回归：status 响应会带上全部历史任务与中文更新说明，单条就上 KB 级，
// 一旦整体超过请求上限（64KB）就会被截断成半截 JSON，现场表现为升级页恒 503。
func TestCallUnixAcceptsStatusResponseLargerThanRequestLimit(t *testing.T) {
	notes := strings.Repeat("升级说明", 20000) // 中文 UTF-8 4 字/4 字节口径下 ~240KB
	socketPath := startOversizedResponder(t, func() ProtocolResponse {
		return ProtocolResponse{
			Protocol: UpdaterProtocolVersion,
			OK:       true,
			Task:     &TaskRecord{TaskID: "big", ReleaseNotes: notes},
		}
	})
	response, err := CallUnix(context.Background(), socketPath, ProtocolRequest{Protocol: UpdaterProtocolVersion, Action: ActionStatus})
	if err != nil {
		t.Fatalf("CallUnix error = %v (响应超过请求上限不能再被截断)", err)
	}
	if !response.OK || response.Task == nil || response.Task.ReleaseNotes != notes {
		t.Fatalf("response = %#v", response)
	}
	if len(notes) <= maxProtocolRequestBytes {
		t.Fatalf("fixture 太小，没覆盖到请求上限: %d", len(notes))
	}
}

// 反向门禁：响应上限仍然存在，别为了修截断就变成无界读取。
func TestCallUnixStillBoundsOversizedResponse(t *testing.T) {
	huge := strings.Repeat("x", maxProtocolResponseBytes+1024)
	socketPath := startOversizedResponder(t, func() ProtocolResponse {
		return ProtocolResponse{
			Protocol: UpdaterProtocolVersion,
			OK:       true,
			Task:     &TaskRecord{TaskID: "huge", ReleaseNotes: huge},
		}
	})
	if _, err := CallUnix(context.Background(), socketPath, ProtocolRequest{Protocol: UpdaterProtocolVersion, Action: ActionStatus}); err == nil {
		t.Fatal("超过响应上限的报文必须报错，不能无界读取")
	}
}

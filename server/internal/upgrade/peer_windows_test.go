//go:build windows

package upgrade

// Windows 上本地通信的信任边界不在对端身份。升级助手与业务进程跑在同一个
// 登录账号下，socket 只存在于安装根目录里，能连上的进程本来就已经拥有该目录
// 的 ACL 权限 —— 再比对 UID 既无意义也无 API（Windows 没有 getpeereid 等价物）。
// 所以这里刻意断言与 Unix 相反的方向，避免有人把 Unix 的 fail-closed 语义照搬。

import (
	"context"
	"path/filepath"
	"testing"
)

func TestWindowsPeerAuthorizerAcceptsAnyLocalPeer(t *testing.T) {
	if err := NewUnixPeerAuthorizer(nil)(nil); err != nil {
		t.Fatalf("empty allowlist must be accepted on windows: %v", err)
	}
	if err := NewUnixPeerAuthorizer([]uint32{1234})(nil); err != nil {
		t.Fatalf("allowlist must not be consulted on windows: %v", err)
	}
}

func TestWindowsPeerAuthorizerServesStatusOverLocalSocket(t *testing.T) {
	root := t.TempDir()
	store, err := OpenTaskStore(filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	server := &UnixServer{
		SocketPath:     daemonSocketPath(),
		Store:          store,
		PeerAuthorizer: NewUnixPeerAuthorizer(nil),
	}
	cancel, done := startUnixServer(t, server)
	defer func() { cancel(); <-done }()
	response, err := CallUnix(context.Background(), server.SocketPath, ProtocolRequest{Protocol: UpdaterProtocolVersion, Action: ActionStatus})
	if err != nil || !response.OK {
		t.Fatalf("status over local socket = %#v, err = %v", response, err)
	}
}

package security

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// stubAgent 记录调用次数，用来证明"代理不存在时一次都不该拨号"。
type stubAgent struct {
	banCalls       int
	unbanCalls     int
	reconcileCalls int
	statusCalls    int
}

func (*stubAgent) Capability() AgentCapabilityState { return AgentCapabilitySupported }
func (a *stubAgent) Ban(BanDecision) error {
	a.banCalls++
	return nil
}
func (a *stubAgent) Unban(string) error {
	a.unbanCalls++
	return nil
}
func (a *stubAgent) Reconcile([]BanDecision) error {
	a.reconcileCalls++
	return nil
}
func (a *stubAgent) Status() AgentStatus {
	a.statusCalls++
	return AgentStatus{Connected: true, Capability: AgentCapabilitySupported}
}

func TestSocketPresenceClientReportsUnsupportedWhenSocketMissing(t *testing.T) {
	base := &stubAgent{}
	client := NewSocketPresenceClient(base, filepath.Join(t.TempDir(), "absent.sock"))

	require.Equal(t, AgentCapabilityUnsupported, client.Capability())
	require.False(t, client.Status().Connected)
	require.Equal(t, AbsentReason, client.Status().LastError)

	// 三个写操作都必须给出**可被 errors.Is 识别**的 unsupported 错误：
	// runtime 靠它把封禁标 agent_state=unsupported（应用层仍拦截），
	// 而不是标成"同步失败"并把 400 抛给界面。
	require.ErrorIs(t, client.Ban(BanDecision{SourceIP: "203.0.113.9"}), ErrFirewallUnsupported)
	require.ErrorIs(t, client.Unban("203.0.113.9"), ErrFirewallUnsupported)
	require.ErrorIs(t, client.Reconcile(nil), ErrFirewallUnsupported)

	// 关键：不存在就不要去拨号（否则每次快照都要等 2 秒超时）。
	require.Zero(t, base.banCalls+base.unbanCalls+base.reconcileCalls+base.statusCalls)
}

func TestSocketPresenceClientDelegatesWhenSocketExists(t *testing.T) {
	dir := t.TempDir()
	socket := filepath.Join(dir, "firewall-agent.sock")
	// 用普通文件代替 socket：探测只关心"路径存在且不是目录"。
	require.NoError(t, os.WriteFile(socket, nil, 0o600))

	base := &stubAgent{}
	client := NewSocketPresenceClient(base, socket)
	require.Equal(t, AgentCapabilitySupported, client.Capability())
	require.True(t, client.Status().Connected)

	require.NoError(t, client.Ban(BanDecision{SourceIP: "203.0.113.9"}))
	require.NoError(t, client.Reconcile(nil))
	require.Equal(t, 1, base.banCalls)
	require.Equal(t, 1, base.reconcileCalls)
}

// ⛔ 回归锁：runtime.UpdatePolicy 在"代理不在"时**必须能保存策略**。
// 这正是老板 2026-10-20 报的故障：绿色包没有代理，切保护模式直接 400 英文 dial 错误。
func TestRuntimeUpdatePolicySucceedsWhenAgentSocketIsMissing(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	client := NewSocketPresenceClient(&stubAgent{}, filepath.Join(t.TempDir(), "absent.sock"))
	runtime := NewRuntime(DefaultPolicy(), clock, client, []byte("test"))

	next := DefaultPolicy()
	next.Mode = ModeStrict
	require.NoError(t, runtime.UpdatePolicy(next, "operator"))
	require.Equal(t, ModeStrict, runtime.Policy().Mode)
	require.Equal(t, AgentCapabilityUnsupported, runtime.AgentStatus().Capability)
}

func TestNewSocketPresenceClientToleratesNilBaseOrEmptyPath(t *testing.T) {
	require.Equal(t, AgentCapabilityUnsupported, NewSocketPresenceClient(nil, "/tmp/x.sock").Capability())
	require.Equal(t, AgentCapabilityUnsupported, NewSocketPresenceClient(&stubAgent{}, "").Capability())
	require.True(t, errors.Is(NewSocketPresenceClient(nil, "/tmp/x.sock").Ban(BanDecision{}), ErrFirewallUnsupported))
}

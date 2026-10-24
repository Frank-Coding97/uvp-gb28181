package firewall

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/gb28181/security"
)

var (
	errTableMissing = errors.New("table missing")
	errNftFailed    = errors.New("nft failed")
)

// reconfigurableMemoryBackend 模拟 NftBackend：记下端口、记录重建次数，
// 重建时**清空**集合（与 nft delete table 的真实语义一致），用来验证补规则。
type reconfigurableMemoryBackend struct {
	mu       sync.Mutex
	rules    map[string]struct{}
	port     uint16
	rebuilds int
}

func newReconfigurableMemoryBackend(port uint16) *reconfigurableMemoryBackend {
	return &reconfigurableMemoryBackend{rules: map[string]struct{}{}, port: port}
}

func (b *reconfigurableMemoryBackend) Add(sourceIP string, _ time.Time) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rules[sourceIP] = struct{}{}
	return nil
}

func (b *reconfigurableMemoryBackend) Remove(sourceIP string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.rules, sourceIP)
	return nil
}

func (b *reconfigurableMemoryBackend) List() ([]string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]string, 0, len(b.rules))
	for ip := range b.rules {
		out = append(out, ip)
	}
	return out, nil
}

func (b *reconfigurableMemoryBackend) SetSIPPort(_ context.Context, port uint16) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.port = port
	b.rebuilds++
	// 重建 = 内核集合清空
	b.rules = map[string]struct{}{}
	return nil
}

func (b *reconfigurableMemoryBackend) snapshot() (uint16, int, int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.port, b.rebuilds, len(b.rules)
}

func TestAgentSetSIPPortRebuildsOnceAndRestoresRules(t *testing.T) {
	backend := newReconfigurableMemoryBackend(51064)
	agent := New(backend, nil, nil)
	decision := security.BanDecision{
		DecisionID: "d1", SourceIP: "203.0.113.9", Score: 6,
		CreatedAt: time.Unix(1000, 0), Permanent: true,
	}
	require.NoError(t, agent.Ban(decision))

	// 第一次下发 = 建立基线（代理启动时用 --sip-port 建过表，但 Agent 自己还不知道这个值）。
	require.NoError(t, agent.SetSIPPort(context.Background(), 51064))
	port, rebuilds, rules := backend.snapshot()
	require.Equal(t, uint16(51064), port)
	require.Equal(t, 1, rebuilds)
	require.Equal(t, 1, rules)

	// 同端口重复下发：不能重建（这条路径挂在每次 socket 请求上）。
	require.NoError(t, agent.SetSIPPort(context.Background(), 51064))
	port, rebuilds, rules = backend.snapshot()
	require.Equal(t, uint16(51064), port)
	require.Equal(t, 1, rebuilds)
	require.Equal(t, 1, rules)

	// 端口变了：再重建一次，且规则必须被补回去（否则内核封禁静默失效）。
	require.NoError(t, agent.SetSIPPort(context.Background(), 5060))
	port, rebuilds, rules = backend.snapshot()
	require.Equal(t, uint16(5060), port)
	require.Equal(t, 2, rebuilds)
	require.Equal(t, 1, rules, "重建后必须把已跟踪规则重新写回内核集合")
}

func TestAgentSetSIPPortIgnoresZeroAndNonReconfigurableBackend(t *testing.T) {
	// 0 = 平台还不知道端口（首次启动、引导页未填），必须保持代理原样。
	agent := New(NewMemoryBackend(), nil, nil)
	require.NoError(t, agent.SetSIPPort(context.Background(), 0))
	require.NoError(t, agent.SetSIPPort(context.Background(), 5060))
	status := agent.Status()
	require.True(t, status.Connected)
}

func TestNftBackendSetSIPPortRewritesTableAndRollsBackOnFailure(t *testing.T) {
	fail := false
	runner := &fakeRunner{fn: func(name string, args []string, input string) ([]byte, error) {
		if name == "nft" && strings.Join(args, " ") == "-j list table inet uvp_sip_guard" {
			return nil, errTableMissing
		}
		if fail && strings.Contains(input, "add rule") {
			return nil, errNftFailed
		}
		return nil, nil
	}}
	backend, err := NewNftBackend(context.Background(), runner, NftConfig{
		Interface: "eth0", Destination: mustIP(t, "192.168.168.101"), Port: 51064,
	})
	require.NoError(t, err)
	require.Equal(t, uint16(51064), backend.Port())

	require.NoError(t, backend.SetSIPPort(context.Background(), 5060))
	require.Equal(t, uint16(5060), backend.Port())
	last := runner.calls[len(runner.calls)-1]
	require.Contains(t, last.input, "tcp dport 5060")
	require.Contains(t, last.input, "udp dport 5060")

	// 失败必须回滚端口值，否则后续 ban 会往"没建成的新表"里写。
	fail = true
	require.Error(t, backend.SetSIPPort(context.Background(), 5061))
	require.Equal(t, uint16(5060), backend.Port())
}

func mustPort(t *testing.T, backend *reconfigurableMemoryBackend) uint16 {
	t.Helper()
	port, _, _ := backend.snapshot()
	return port
}

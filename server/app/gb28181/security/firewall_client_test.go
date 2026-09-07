//go:build !windows

package security_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/security"
	"uvplatform.cn/uvp-gb28181/app/gb28181/security/firewall"
)

func TestUnixFirewallClientBanStatusUnbanAndReconcile(t *testing.T) {
	socket := filepath.Join(os.TempDir(), fmt.Sprintf("uvp-agent-%d.sock", os.Getpid()))
	_ = os.Remove(socket)
	defer os.Remove(socket)
	stop := make(chan struct{})
	agent := firewall.New(firewall.NewMemoryBackend(), security.RealClock(), nil)
	errCh := make(chan error, 1)
	go func() { errCh <- agent.ServeUnix(socket, stop) }()
	deadline := time.Now().Add(time.Second)
	for {
		if _, err := os.Stat(socket); err == nil {
			break
		}
		select {
		case err := <-errCh:
			require.NoError(t, err)
		default:
		}
		require.True(t, time.Now().Before(deadline), "agent socket was not created")
		time.Sleep(5 * time.Millisecond)
	}

	client := security.NewUnixFirewallClient(socket, time.Second)
	decision := security.BanDecision{DecisionID: "d1", SourceIP: "203.0.113.20", CreatedAt: time.Now(), TTL: time.Minute}
	require.NoError(t, client.Ban(decision))
	require.True(t, client.Status().Connected)
	require.Equal(t, 1, client.Status().AppliedRules)
	require.NoError(t, client.Reconcile([]security.BanDecision{{DecisionID: "d2", SourceIP: "203.0.113.21", CreatedAt: time.Now(), TTL: time.Minute}}))
	require.Equal(t, 1, client.Status().AppliedRules)
	require.NoError(t, client.Unban("203.0.113.21"))
	require.Zero(t, client.Status().AppliedRules)

	close(stop)
	require.NoError(t, <-errCh)
}

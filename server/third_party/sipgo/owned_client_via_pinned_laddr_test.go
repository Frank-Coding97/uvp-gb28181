package sipgo

import (
	"net"
	"testing"

	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
)

// TestClientCreateViaPrefersPinnedLocalAddr pins the UVP fork behaviour: a
// request that pinned its local socket must generate a Via that names that
// address, never the transport's wildcard local address (see UVP_PATCH.md,
// "Sent-by comes from the pinned local address").
func TestClientCreateViaPrefersPinnedLocalAddr(t *testing.T) {
	ua, err := NewUA(WithUserAgent("uvp-via-test"))
	require.NoError(t, err)
	defer func() { _ = ua.Close() }()

	client, err := NewClient(ua)
	require.NoError(t, err)

	request := sip.NewRequest(sip.REGISTER, sip.Uri{Scheme: "sip", User: "34020000002000000001", Host: "3402000000"})
	request.SetTransport("UDP")
	request.Laddr = sip.Addr{IP: net.ParseIP("192.0.2.20"), Port: 5061, Hostname: "192.0.2.20"}

	via := clientRequestCreateVia(client, request)
	require.Equal(t, "192.0.2.20", via.Host)
	require.Equal(t, 5061, via.Port)
	require.NotEmpty(t, via.Params.GetOr("branch", ""))

	// 未钉本地地址时保持上游行为:交给传输层按真实连接补齐。
	unpinned := sip.NewRequest(sip.REGISTER, request.Recipient)
	unpinned.SetTransport("UDP")
	via = clientRequestCreateVia(client, unpinned)
	require.Empty(t, via.Host)
	require.Zero(t, via.Port)
}

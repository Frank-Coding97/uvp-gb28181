package security

import (
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A platform without system firewall support must still enforce the actual
// pre-parser packet boundary, including after an application ban is removed.
func TestUnsupportedFirewallRegisterAdmissionBoundary(t *testing.T) {
	for _, transport := range []string{"UDP", "TCP"} {
		t.Run(transport, func(t *testing.T) {
			runtime := NewRuntime(unsupportedBanPolicy(), &fakeClock{now: time.Unix(100, 0)}, NewUnsupportedFirewallClient(), []byte("test"))
			props := readProps()
			props.Transport = transport
			packet := admissionSIPFrame(transport, "REGISTER", "34020000001320000088", "register", "")
			out, err := runtime.Admission().Filter(props, packet)
			require.NoError(t, err)
			require.Equal(t, packet, out)

			require.NoError(t, runtime.Record(Event{Transport: transport, SourceIP: "198.51.100.10", SourceVerified: true, Method: "INVITE", Reason: ReasonUnknownMethod}))
			out, err = runtime.Admission().Filter(props, packet)
			require.NoError(t, err)
			require.Empty(t, out)

			other := props
			other.RemoteAddr = &net.UDPAddr{IP: net.ParseIP("198.51.100.11"), Port: 5060}
			out, err = runtime.Admission().Filter(other, packet)
			require.NoError(t, err)
			require.Equal(t, packet, out)

			require.NoError(t, runtime.Unban("198.51.100.10", "operator"))
			out, err = runtime.Admission().Filter(props, packet)
			require.NoError(t, err)
			require.Equal(t, packet, out)
		})
	}
}

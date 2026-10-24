package uac

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/emiago/sipgo/siptest"
	"github.com/stretchr/testify/require"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

func TestLiveByeFollowsValidatedPortWithoutChangingDialog(t *testing.T) {
	for _, route := range []string{"", "<sip:proxy.example.com;lr>"} {
		t.Run(route, func(t *testing.T) {
			ua, err := sipgo.NewUA()
			require.NoError(t, err)
			t.Cleanup(func() { _ = ua.Close() })
			u, err := New(ua, testPlatformID, "3402000000", "192.0.2.1", 5061, false)
			require.NoError(t, err)
			expires := time.Now().Add(time.Hour)
			device := &gbmodels.GbDevice{DeviceID: testDeviceID, IP: "192.0.2.20", Port: 5060, Transport: "UDP", RegisterExpireAt: &expires}
			u.SetDeviceEndpointLookup(func(context.Context, string) (*gbmodels.GbDevice, error) { return device, nil })
			var sentInvite, sentBye *sip.Request
			u.client.TxRequester = &siptest.ClientTxRequester{OnRequest: func(req *sip.Request) *sip.Response {
				response := sip.NewResponseFromRequest(req, sip.StatusOK, "OK", nil)
				if req.Method == sip.INVITE {
					sentInvite = req.Clone()
					response.To().Params.Add("tag", "device-tag")
					response.AppendHeader(sip.NewHeader("Contact", "<sip:channel@3402000000>"))
					response.SetSource("192.0.2.20:5060")
					if route != "" {
						response.AppendHeader(sip.NewHeader("Record-Route", route))
					}
				}
				if req.Method == sip.BYE {
					sentBye = req.Clone()
				}
				return response
			}}
			s := validInviteOutcomeSession()
			manager := NewSessionManager()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_, err = u.InviteTracked(ctx, manager, s, "v=0\r\n")
			require.NoError(t, err)
			device.Port = 12230 // The model accepts a later validated heartbeat.
			_, err = u.ByeIfCurrent(ctx, manager, s.Ref())
			require.NoError(t, err)
			require.NotNil(t, sentBye)
			require.Equal(t, sentInvite.CallID().Value(), sentBye.CallID().Value())
			require.Equal(t, "sip:channel@3402000000", sentBye.Recipient.String())
			localTag, _ := sentInvite.From().Params.Get("tag")
			byeTag, _ := sentBye.From().Params.Get("tag")
			remoteTag, _ := sentBye.To().Params.Get("tag")
			require.Equal(t, localTag, byeTag)
			require.Equal(t, "device-tag", remoteTag)
			require.Greater(t, sentBye.CSeq().SeqNo, sentInvite.CSeq().SeqNo)
			if route == "" {
				require.Equal(t, "192.0.2.20:12230", sentBye.Destination())
			} else {
				require.Equal(t, "proxy.example.com:5060", sentBye.Destination())
			}
		})
	}
}

func TestLiveByeEndpointTrustBoundary(t *testing.T) {
	expires := time.Now().Add(time.Hour)
	for _, tc := range []struct {
		name      string
		mutate    func(*gbmodels.GbDevice)
		lookupErr error
		wantErr   bool
	}{
		{"same IP new port", func(d *gbmodels.GbDevice) { d.Port = 12230 }, nil, false},
		{"other IP", func(d *gbmodels.GbDevice) { d.IP = "192.0.2.99" }, nil, false},
		{"other transport", func(d *gbmodels.GbDevice) { d.Transport = "TCP" }, nil, false},
		{"other identity", func(d *gbmodels.GbDevice) { d.DeviceID = "other" }, nil, false},
		{"expired", func(d *gbmodels.GbDevice) { expired := time.Now().Add(-time.Second); d.RegisterExpireAt = &expired }, nil, false},
		{"invalid port", func(d *gbmodels.GbDevice) { d.Port = 0 }, nil, false},
		{"lookup error", func(*gbmodels.GbDevice) {}, errors.New("database unavailable"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := &gbmodels.GbDevice{DeviceID: testDeviceID, IP: "192.0.2.20", Port: 5060, Transport: "UDP", RegisterExpireAt: &expires}
			tc.mutate(d)
			u := &UAC{deviceEndpoint: func(context.Context, string) (*gbmodels.GbDevice, error) { return d, tc.lookupErr }}
			req := sip.NewRequest(sip.BYE, sip.Uri{Host: "3402000000"})
			req.SetDestination("192.0.2.20:5060")
			dest, err := u.liveDialogDestination(testDeviceID, "192.0.2.20:5060", "UDP")(context.Background(), req)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tc.name == "same IP new port" {
				require.Equal(t, "192.0.2.20:12230", dest)
			} else {
				require.Empty(t, dest)
			}
		})
	}
}

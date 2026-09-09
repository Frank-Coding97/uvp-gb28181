package gb28181

import (
	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
	"testing"
	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/model"
)

func TestCascadeCatalogPeerSeparatesSameIPByPort(t *testing.T) {
	p := model.GbCascadePlatform{UpstreamServerID: "34020000002000000001", LocalDeviceID: "34020000002000000002", Host: "192.168.10.220", Port: 15060, Transport: "UDP"}
	req := sip.NewRequest(sip.MESSAGE, sip.Uri{User: p.LocalDeviceID, Host: "192.168.10.106"})
	req.AppendHeader(&sip.FromHeader{Address: sip.Uri{User: p.UpstreamServerID}})
	req.AppendHeader(&sip.ToHeader{Address: sip.Uri{User: p.LocalDeviceID}})
	req.SetSource("192.168.10.220:15060")
	req.SetTransport("UDP")
	require.True(t, matchCascadeCatalogPeer(req, p))
	p.Port = 16060
	require.False(t, matchCascadeCatalogPeer(req, p))
	req.SetSource("192.168.10.220:16060")
	require.True(t, matchCascadeCatalogPeer(req, p))
	req.From().Address.User = "other"
	require.False(t, matchCascadeCatalogPeer(req, p))
}

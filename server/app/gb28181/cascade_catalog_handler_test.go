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

func TestCascadeControlPeerUsesPublishedChannelAsTarget(t *testing.T) {
	p := model.GbCascadePlatform{UpstreamServerID: "34020000002000000001", LocalDeviceID: "34020000002000000002", Host: "192.168.10.220", Port: 15060, Transport: "UDP"}
	const publishedChannelID = "34020000001320000010"
	req := sip.NewRequest(sip.MESSAGE, sip.Uri{User: publishedChannelID, Host: "3402000000"})
	req.AppendHeader(&sip.FromHeader{Address: sip.Uri{User: p.UpstreamServerID}})
	req.AppendHeader(&sip.ToHeader{Address: sip.Uri{User: publishedChannelID}})
	req.SetSource("192.168.10.220:15060")
	req.SetTransport("UDP")

	require.True(t, matchCascadeControlPeer(req, p))
	p.Port = 16060
	require.False(t, matchCascadeControlPeer(req, p))
	req.SetSource("192.168.10.220:16060")
	require.True(t, matchCascadeControlPeer(req, p))
	req.From().Address.User = "other"
	require.False(t, matchCascadeControlPeer(req, p))
}

func TestFindCascadeControlPeerLeavesDeviceControlResponsesForDeviceHandler(t *testing.T) {
	platforms := []model.GbCascadePlatform{
		{ID: 3, UpstreamServerID: "34020000002000000001", Host: "192.168.10.220", Port: 15060, Transport: "UDP"},
	}
	req := sip.NewRequest(sip.MESSAGE, sip.Uri{User: "34020000001320000010", Host: "3402000000"})
	req.AppendHeader(&sip.FromHeader{Address: sip.Uri{User: platforms[0].UpstreamServerID}})
	req.SetSource("192.168.10.220:5060")
	req.SetTransport("UDP")

	matched, ambiguous := findCascadeControlPeer(req, platforms)
	require.Nil(t, matched)
	require.False(t, ambiguous)

	req.SetSource("192.168.10.220:15060")
	req.From().Address.User = "unexpected"
	matched, ambiguous = findCascadeControlPeer(req, platforms)
	require.Nil(t, matched)
	require.False(t, ambiguous)
}

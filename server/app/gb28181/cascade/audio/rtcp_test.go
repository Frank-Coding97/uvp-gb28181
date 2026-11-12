package audio

import (
	"testing"

	"github.com/pion/rtcp"
	"github.com/stretchr/testify/require"
)

func TestRewriteCompoundRTCPMapsNegotiatedSSRCs(t *testing.T) {
	const sourceSSRC, receiverSSRC = uint32(100000001), uint32(100000002)
	input, err := rtcp.Marshal([]rtcp.Packet{
		&rtcp.SenderReport{SSRC: sourceSSRC, PacketCount: 12, Reports: []rtcp.ReceptionReport{{SSRC: receiverSSRC, Jitter: 4}}},
		rtcp.NewCNAMESourceDescription(sourceSSRC, "cascade-source"),
		&rtcp.Goodbye{Sources: []uint32{sourceSSRC}, Reason: "stopped"},
	})
	require.NoError(t, err)

	got, err := rewriteRTCP(input, sourceSSRC, receiverSSRC)
	require.NoError(t, err)
	packets, err := rtcp.Unmarshal(got)
	require.NoError(t, err)
	require.Len(t, packets, 3)
	sr := packets[0].(*rtcp.SenderReport)
	require.Equal(t, receiverSSRC, sr.SSRC)
	require.Equal(t, sourceSSRC, sr.Reports[0].SSRC, "reports about the far leg must map back to its negotiated SSRC")
	sdes := packets[1].(*rtcp.SourceDescription)
	require.Equal(t, receiverSSRC, sdes.Chunks[0].Source)
	bye := packets[2].(*rtcp.Goodbye)
	require.Equal(t, []uint32{receiverSSRC}, bye.Sources)
}

func TestRewriteCompoundRTCPMapsReceiverReportsBackToSource(t *testing.T) {
	const sourceSSRC, receiverSSRC = uint32(100000001), uint32(100000002)
	input, err := rtcp.Marshal([]rtcp.Packet{
		&rtcp.ReceiverReport{SSRC: receiverSSRC, Reports: []rtcp.ReceptionReport{{SSRC: sourceSSRC, FractionLost: 1}}},
	})
	require.NoError(t, err)

	got, err := rewriteRTCP(input, receiverSSRC, sourceSSRC)
	require.NoError(t, err)
	packets, err := rtcp.Unmarshal(got)
	require.NoError(t, err)
	rr := packets[0].(*rtcp.ReceiverReport)
	require.Equal(t, sourceSSRC, rr.SSRC)
	require.Equal(t, receiverSSRC, rr.Reports[0].SSRC)
}

func TestRewriteCompoundRTCPRejectsUnnegotiatedSenderAndMalformedPacket(t *testing.T) {
	packet, err := rtcp.Marshal([]rtcp.Packet{&rtcp.ReceiverReport{SSRC: 100000003}})
	require.NoError(t, err)
	_, err = rewriteRTCP(packet, 100000001, 100000002)
	require.Error(t, err)
	_, err = rewriteRTCP([]byte{0x80, 201, 0, 1}, 100000001, 100000002)
	require.Error(t, err)
}

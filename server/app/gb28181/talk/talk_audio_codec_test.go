package talk

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/sdp"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/node"
)

// 会话编码决定「源流里必须出现哪种音轨」：PCMU 会话不能被 PCMA 源顶替，
// 否则 ZLM 会按 PT 0 打包 A-law 数据，设备喇叭里全是噪声。
func TestHasReadyAudioMatchesSessionCodec(t *testing.T) {
	pcmuTracks := []zlm.MediaTrack{{CodecType: 1, CodecIDName: "PCMU", Ready: true, SampleRate: 8000}}
	pcmaTracks := []zlm.MediaTrack{{CodecType: 1, CodecIDName: "PCMA", Ready: true, SampleRate: 8000}}

	require.True(t, hasReadyAudio(pcmuTracks, sdp.AudioCodecPCMU))
	require.False(t, hasReadyAudio(pcmuTracks, sdp.AudioCodecPCMA))
	require.True(t, hasReadyAudio(pcmaTracks, sdp.AudioCodecPCMA))
	require.False(t, hasReadyAudio(pcmaTracks, sdp.AudioCodecPCMU))

	// 16k 的 PCMA 不算：G.711 固定 8kHz，放过它等于把另一种载体当 G.711 发。
	wideband := []zlm.MediaTrack{{CodecType: 1, CodecIDName: "PCMA", Ready: true, SampleRate: 16000}}
	require.False(t, hasReadyAudio(wideband, sdp.AudioCodecPCMA))
}

const pcmuBroadcastOffer = "v=0\r\no=device 0 0 IN IP4 192.0.2.20\r\ns=Play\r\n" +
	"c=IN IP4 192.0.2.20\r\nt=0 0\r\nm=audio 30000 RTP/AVP 0\r\n" +
	"a=recvonly\r\na=rtpmap:0 PCMU/8000\r\ny=0100000001\r\n"

// 设备只收 G.711U：平台 answer 与 ZLM 发送都要用 PCMU（PT 0）。
func TestPrepareBroadcastInvitePCMU(t *testing.T) {
	ctx := context.Background()
	base := &fakeActivationMedia{info: &zlm.MediaInfo{Online: true, Tracks: []zlm.MediaTrack{{CodecType: 1, CodecIDName: "PCMU", Ready: true, SampleRate: 8000}}}, localPort: 31000}
	service, repo, session, _, _ := newBroadcastActivationService(t, base)
	media := &recordingBroadcastMedia{fakeActivationMedia: base}
	service.activation.deps.ClientFor = func(*node.Node) TalkMediaClient { return media }
	require.NoError(t, repo.db.Model(&models.GbTalkSession{}).Where("session_id = ?", session.SessionID).
		Updates(map[string]any{"audio_codec": "pcmu"}).Error)

	prepared, err := service.PrepareBroadcastInvite(ctx, BroadcastInvite{
		PeerID: "device-1", TargetID: "channel-1", CallID: "pcmu-call", CSeq: 1, SDP: pcmuBroadcastOffer,
	})
	require.NoError(t, err)
	require.Contains(t, prepared.AnswerSDP, "m=audio 31000 RTP/AVP 0\r\n")
	require.Contains(t, prepared.AnswerSDP, "a=rtpmap:0 PCMU/8000")
	require.Contains(t, prepared.AnswerSDP, "f=v/////a/2/8/1")
	require.NotNil(t, media.request.PayloadType)
	require.Equal(t, 0, *media.request.PayloadType)
}

// 会话是 G.711U、设备 offer 只有 G.711A：必须回 488 并在 reason 里写清差在哪，
// 不能把 A-law 数据贴上 PT 0 发出去（设备听到的是噪声，排查成本极高）。
func TestPrepareBroadcastInviteRejectsCodecMismatch(t *testing.T) {
	ctx := context.Background()
	base := &fakeActivationMedia{info: pcmaTalkInfo(), localPort: 31000}
	service, repo, session, _, _ := newBroadcastActivationService(t, base)
	media := &recordingBroadcastMedia{fakeActivationMedia: base}
	service.activation.deps.ClientFor = func(*node.Node) TalkMediaClient { return media }
	require.NoError(t, repo.db.Model(&models.GbTalkSession{}).Where("session_id = ?", session.SessionID).
		Updates(map[string]any{"audio_codec": "pcmu"}).Error)

	offer := "v=0\r\nc=IN IP4 192.0.2.20\r\nm=audio 30000 RTP/AVP 8\r\n" +
		"a=recvonly\r\na=rtpmap:8 PCMA/8000\r\ny=0100000001\r\n"
	_, err := service.PrepareBroadcastInvite(ctx, BroadcastInvite{
		PeerID: "device-1", TargetID: "channel-1", CallID: "mismatch-call", CSeq: 1, SDP: offer,
	})
	var sipErr *BroadcastSIPError
	require.ErrorAs(t, err, &sipErr)
	require.Equal(t, 488, sipErr.Status)
	require.Contains(t, sipErr.Reason, "G.711U")
	require.Contains(t, sipErr.Reason, "G.711A")
	require.Nil(t, media.request.PayloadType, "编码不匹配时不能启动 ZLM 发送")
}

package trace

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func TestClassifySIPBusinessUsesProtocolEvidence(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want BusinessCode
	}{
		{"register", "REGISTER sip:34020000001320000001@3402000000 SIP/2.0\r\nCSeq: 1 REGISTER\r\n\r\n", BusinessRegister},
		{"keepalive", "MESSAGE sip:34020000001320000001@3402000000 SIP/2.0\r\nContent-Type: Application/MANSCDP+xml\r\n\r\n<Notify><CmdType>Keepalive</CmdType></Notify>", BusinessKeepalive},
		{"catalog", "MESSAGE sip:x SIP/2.0\r\nContent-Type: Application/MANSCDP+xml\r\n\r\n<Query><CmdType>Catalog</CmdType></Query>", BusinessCatalog},
		{"device info", "MESSAGE sip:x SIP/2.0\r\nContent-Type: Application/MANSCDP+xml\r\n\r\n<Response><CmdType>DeviceInfo</CmdType></Response>", BusinessDeviceInfo},
		{"alarm", "MESSAGE sip:x SIP/2.0\r\nContent-Type: Application/MANSCDP+xml\r\n\r\n<Notify><CmdType>Alarm</CmdType></Notify>", BusinessAlarm},
		{"ptz", "MESSAGE sip:x SIP/2.0\r\nContent-Type: Application/MANSCDP+xml\r\n\r\n<Control><CmdType>DeviceControl</CmdType><PTZCmd>1</PTZCmd></Control>", BusinessPTZ},
		{"record control", "MESSAGE sip:x SIP/2.0\r\nContent-Type: Application/MANSCDP+xml\r\n\r\n<Control><CmdType>DeviceControl</CmdType><RecordCmd>Record</RecordCmd></Control>", BusinessDeviceControl},
		{"record query", "MESSAGE sip:x SIP/2.0\r\nContent-Type: Application/MANSCDP+xml\r\n\r\n<Query><CmdType>RecordInfo</CmdType></Query>", BusinessRecordQuery},
		{"broadcast", "MESSAGE sip:x SIP/2.0\r\nContent-Type: Application/MANSCDP+xml\r\n\r\n<Notify><CmdType>Broadcast</CmdType></Notify>", BusinessBroadcast},
		{"mobile position", "MESSAGE sip:x SIP/2.0\r\nContent-Type: Application/MANSCDP+xml\r\n\r\n<Notify><CmdType>MobilePosition</CmdType></Notify>", BusinessMobilePosition},
		{"realtime", "INVITE sip:x SIP/2.0\r\nSubject: channel:ssrc,platform:0\r\nContent-Type: application/sdp\r\n\r\ns=Play\r\n", BusinessRealtimePlay},
		{"playback", "INVITE sip:x SIP/2.0\r\nContent-Type: application/sdp\r\n\r\ns=Playback\r\n", BusinessPlayback},
		{"download", "INVITE sip:x SIP/2.0\r\nContent-Type: application/sdp\r\n\r\ns=Download\r\n", BusinessDownload},
		{"talk", "INVITE sip:x SIP/2.0\r\nContent-Type: application/sdp\r\n\r\ns=Talk\r\n", BusinessTalk},
		{"broadcast invite", "INVITE sip:x SIP/2.0\r\nContent-Type: application/sdp\r\n\r\ns=Broadcast\r\n", BusinessBroadcast},
		{"realtime subject fallback", "INVITE sip:x SIP/2.0\r\nSubject: 34020000001320000001:00001,34020000002000000001:1\r\n\r\n", BusinessRealtimePlay},
		{"historical subject is not realtime", "INVITE sip:x SIP/2.0\r\nSubject: 34020000001320000001:10001,34020000002000000001:1\r\n\r\n", BusinessUnknown},
		{"playback control", "INFO sip:x SIP/2.0\r\nContent-Type: Application/MANSRTSP\r\n\r\nPAUSE RTSP/1.0\r\nCSeq: 2\r\n", BusinessPlaybackControl},
		{"subscribe", "SUBSCRIBE sip:x SIP/2.0\r\nCSeq: 2 SUBSCRIBE\r\n\r\n", BusinessSubscription},
		{"ack", "ACK sip:x SIP/2.0\r\nCSeq: 3 ACK\r\n\r\n", BusinessAck},
		{"bye", "BYE sip:x SIP/2.0\r\nCSeq: 3 BYE\r\n\r\n", BusinessHangup},
		{"unknown message", "MESSAGE sip:x SIP/2.0\r\nContent-Type: text/plain\r\n\r\nhello", BusinessUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifySIPBusiness([]byte(tt.raw))
			require.Equal(t, tt.want, got.Code)
		})
	}
}

func TestExtractNationalID(t *testing.T) {
	require.Equal(t, "34020000001320000001", extractNationalID("sip:34020000001320000001@3402000000"))
	require.Equal(t, "34020000001320000001", extractNationalID("34020000001320000001@3402000000"))
	require.Empty(t, extractNationalID(""))
}

func TestExtractSIPMetadataIncludesBusinessAndNationalIDs(t *testing.T) {
	raw := []byte("MESSAGE sip:34020000002000000001@3402000000 SIP/2.0\r\n" +
		"From: <sip:34020000001320000001@3402000000>;tag=1\r\n" +
		"To: <sip:34020000002000000001@3402000000>\r\n" +
		"Call-ID: call-1\r\nCSeq: 1 MESSAGE\r\nContent-Type: Application/MANSCDP+xml\r\n\r\n" +
		"<Notify><CmdType>Keepalive</CmdType><DeviceID>34020000001320000001</DeviceID></Notify>")

	metadata := extractSIPMetadata(raw, DirectionInbound)
	require.Equal(t, "34020000001320000001", metadata.FromID)
	require.Equal(t, "34020000002000000001", metadata.ToID)
	require.Equal(t, BusinessKeepalive, metadata.BusinessCode)
	require.Equal(t, "心跳保持", metadata.BusinessType)
}

func TestResolveDisplayAddrReplacesWildcardOnly(t *testing.T) {
	require.Equal(t, "192.0.2.10:5062", resolveDisplayAddr("[::]:5062", "192.0.2.10:5062"))
	require.Equal(t, "192.0.2.10:5062", resolveDisplayAddr("0.0.0.0:5062", "192.0.2.10:5062"))
	require.Equal(t, "198.51.100.2:5060", resolveDisplayAddr("198.51.100.2:5060", "192.0.2.10:5062"))
}

func TestNormalizeStoredAddrKeepsHistoricalWildcardReadable(t *testing.T) {
	require.Equal(t, "本机 SIP:5062", normalizeStoredAddr("[::]:5062"))
	require.Equal(t, "本机 SIP:5062", normalizeStoredAddr("0.0.0.0:5062"))
}

func TestMigratedLegacyUnknownRowsUseConservativeMethodFallback(t *testing.T) {
	row := gbmodels.GbSipTraceMessage{
		Method: "REGISTER", BusinessCode: "unknown", BusinessType: "未知业务", BusinessConfidence: "none",
	}

	message := messageSummary(row)
	require.Equal(t, BusinessRegister, message.BusinessCode)
	require.Equal(t, "注册", message.BusinessType)
	require.Equal(t, "medium", message.BusinessConfidence)
}

func TestSummarizeRowsKeepsMediaBusinessOverHangupAndNormalizesLegacyAddress(t *testing.T) {
	at := time.Date(2026, 8, 24, 6, 0, 0, 0, time.UTC)
	rows := []gbmodels.GbSipTraceMessage{
		{
			EventID: "1", OccurredAt: at, Direction: string(DirectionOutbound), DeviceID: "device", CallID: "call-1",
			Method: "INVITE", FromURI: "sip:platform@example.com", ToURI: "sip:device@example.com",
			BusinessCode: string(BusinessPlayback), BusinessType: "回放", BusinessConfidence: "high",
			LocalAddr: "[::]:5062", RemoteAddr: "192.0.2.10:5060",
		},
		{
			EventID: "2", OccurredAt: at.Add(time.Second), Direction: string(DirectionOutbound), DeviceID: "device", CallID: "call-1",
			Method: "BYE", BusinessCode: string(BusinessHangup), BusinessType: "挂断", BusinessConfidence: "high",
			LocalAddr: "[::]:5062", RemoteAddr: "192.0.2.10:5060",
		},
	}

	sessions := summarizeRows(rows, time.Hour)
	require.Len(t, sessions, 1)
	require.Equal(t, BusinessPlayback, sessions[0].BusinessCode)
	require.Equal(t, "回放", sessions[0].BusinessType)
	require.Equal(t, "platform", sessions[0].FromID)
	require.Equal(t, "device", sessions[0].ToID)
	require.Equal(t, "本机 SIP:5062", sessions[0].SourceAddr)
	require.Equal(t, "192.0.2.10:5060", sessions[0].DestinationAddr)
}

func TestSummarizeRowsCorrelatesAckAndByeAcrossCollectorDeviceIDs(t *testing.T) {
	at := time.Date(2026, 8, 24, 6, 0, 0, 0, time.UTC)
	rows := []gbmodels.GbSipTraceMessage{
		{
			EventID: "1", OccurredAt: at, Direction: string(DirectionOutbound), DeviceID: "device-media", CallID: "call-cross-device",
			Method: "INVITE", FromID: "platform", ToID: "device", BusinessCode: string(BusinessRealtimePlay), BusinessType: "实时点播", BusinessConfidence: "high",
		},
		{
			EventID: "2", OccurredAt: at.Add(time.Millisecond), Direction: string(DirectionOutbound), DeviceID: "device-signaling", CallID: "call-cross-device",
			Method: "ACK", FromID: "platform", ToID: "device", BusinessCode: string(BusinessUnknown), BusinessType: "未知业务", BusinessConfidence: "none",
		},
		{
			EventID: "3", OccurredAt: at.Add(time.Second), Direction: string(DirectionOutbound), DeviceID: "device-signaling", CallID: "call-cross-device",
			Method: "BYE", FromID: "platform", ToID: "device", BusinessCode: string(BusinessHangup), BusinessType: "挂断", BusinessConfidence: "high",
		},
		{
			EventID: "4", OccurredAt: at.Add(2 * time.Second), Direction: string(DirectionInbound), DeviceID: "device-media", CallID: "call-cross-device",
			Method: "BYE", StatusCode: 200, FromID: "platform", ToID: "device", BusinessCode: string(BusinessHangup), BusinessType: "挂断", BusinessConfidence: "high",
		},
	}

	sessions := summarizeRows(rows, time.Hour)
	require.Len(t, sessions, 1)
	require.Equal(t, "device-media", sessions[0].DeviceID)
	require.Equal(t, BusinessRealtimePlay, sessions[0].BusinessCode)
	require.Equal(t, uint16(200), sessions[0].FinalStatus)
	require.Equal(t, uint64(4), sessions[0].MessageCount)
}

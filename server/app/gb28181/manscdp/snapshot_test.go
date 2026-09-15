package manscdp

import (
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
)

func TestBuildSnapshotConfigUses2022WireShape(t *testing.T) {
	body, err := BuildSnapshotConfigWithProfile(protocol.ProfileFor(protocol.Version2022), "34020000001320000001", 7, SnapshotConfig{
		SessionID: "session-1", UploadURL: "http://127.0.0.1/api/gb28181/device-snapshots/uploads/token/", SnapNum: 2, Interval: 3,
	})
	require.NoError(t, err)
	wire := string(body)
	require.Contains(t, wire, "encoding=\"GB18030\"")
	require.Contains(t, wire, "<SnapShotConfig><SessionID>session-1</SessionID>")
	require.Contains(t, wire, "<SnapNum>2</SnapNum><Interval>3</Interval>")
}

func TestBuildSnapshotConfigRejectsLegacyProfile(t *testing.T) {
	_, err := BuildSnapshotConfigWithProfile(protocol.ProfileFor(protocol.Version2016), "channel", 1, SnapshotConfig{
		SessionID: "s", UploadURL: "http://127.0.0.1/upload/", SnapNum: 1, Interval: 1,
	})
	require.Error(t, err)
}

func TestParseSnapshotNotify(t *testing.T) {
	notify, err := ParseSnapshotNotify([]byte(`<?xml version="1.0" encoding="UTF-8"?><Notify><CmdType>Notify</CmdType><SubCmd>SnapShot</SubCmd><SN>9</SN><DeviceID>34020000001320000001</DeviceID><SessionID>session-1</SessionID><SnapShotID>shot-1</SnapShotID><Time>2026-08-30T23:30:00+08:00</Time><StoragePath>http://localhost/shot-1.jpg</StoragePath></Notify>`))
	require.NoError(t, err)
	require.Equal(t, "session-1", notify.SessionID)
	require.Equal(t, "shot-1", notify.SnapshotID)
}

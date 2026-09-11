package handler

import (
	"context"
	"testing"

	"github.com/emiago/sipgo/siptest"
	"github.com/stretchr/testify/require"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
)

type snapshotMessageSink struct {
	device string
	body   []byte
}

func (s *snapshotMessageSink) OnSnapshotNotify(_ context.Context, device string, body []byte) error {
	s.device = device
	s.body = append([]byte(nil), body...)
	return nil
}

func TestMessageHandlerDispatchesSnapshotNotify(t *testing.T) {
	body := []byte(`<?xml version="1.0" encoding="UTF-8"?><Notify><CmdType>Notify</CmdType><SubCmd>SnapShot</SubCmd><SN>9</SN><DeviceID>34020000001320000001</DeviceID><SessionID>session-1</SessionID><SnapShotID>shot-1</SnapShotID><Time>2026-08-30T23:30:00+08:00</Time><StoragePath>http://localhost/shot-1.jpg</StoragePath></Notify>`)
	req := recordInfoRequest(body)
	tx := siptest.NewServerTxRecorder(req)
	sink := &snapshotMessageSink{}
	handler := NewMessageHandler(gbconfig.Config{})
	handler.SetSnapshotSink(sink)
	handler.Handle(req, tx)

	require.Len(t, tx.Result(), 1)
	require.EqualValues(t, 200, tx.Result()[0].StatusCode)
	require.Equal(t, "34020000001320000001", sink.device)
	require.Equal(t, body, sink.body)
}

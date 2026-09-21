package handler

import (
	"context"
	"testing"

	"github.com/emiago/sipgo/sip"
	"github.com/emiago/sipgo/siptest"
	"github.com/stretchr/testify/require"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
)

// snapshotMessageRequest 复用 `recordInfoRequest` 的报文骨架，只把 `From` 换成真实来源设备。
// ⛔ 必须换：`ptzDeviceCode` 优先取 SIP `From`（取不到才回落到报文里的 `DeviceID`），
// 而 `recordInfoRequest` 的 `From` 写死成 `34020000001320000001`。不改的话，测试断言到的
// "来源设备"是夹具的默认值而不是被测报文里的值，看着绿其实没验到东西。
func snapshotMessageRequest(body []byte, fromUser string) *sip.Request {
	req := recordInfoRequest(body)
	req.RemoveHeader("From")
	req.AppendHeader(&sip.FromHeader{Address: sip.Uri{User: fromUser, Host: fromUser}})
	return req
}

type snapshotMessageSink struct {
	device     string
	body       []byte
	notifySeen bool
	finished   bool
}

func (s *snapshotMessageSink) OnSnapshotNotify(_ context.Context, device string, body []byte) error {
	s.device = device
	s.body = append([]byte(nil), body...)
	s.notifySeen = true
	return nil
}

func (s *snapshotMessageSink) OnUploadSnapShotFinished(_ context.Context, device string, body []byte) error {
	s.device = device
	s.body = append([]byte(nil), body...)
	s.finished = true
	return nil
}

// hikUploadSnapShotFinishedBody 是海康 `37010301021320000002` 的真机原文
// （2026-09-20 13:31:04，`SN=9402` 那次 `DeviceConfig` 下发的 3 张抓拍完成后主动上报）。
// ⛔ 两处照抄：根元素是 `<Notify>`；`<SnapShotList>` 是**一张一个并列元素**。
const hikUploadSnapShotFinishedBody = `<?xml version="1.0" encoding="GB18030"?><Notify><CmdType>UploadSnapShotFinished</CmdType><SN>9402</SN><DeviceID>37010301021320000002</DeviceID><SessionID>probe-b-000000000000000000000000000000</SessionID><SnapShotList><SnapShotFileID>37010301021320000002022026092013305816101</SnapShotFileID></SnapShotList><SnapShotList><SnapShotFileID>3701030102132000000202202609201331014002</SnapShotFileID></SnapShotList><SnapShotList><SnapShotFileID>3701030102132000000202202609201331044003</SnapShotFileID></SnapShotList></Notify>`

// TestMessageHandlerDispatchesUploadSnapShotFinished 锚住 **A.2.5.7 标准形态**的入站门禁。
// 2026-09-20 之前这条报文被 `head.CmdType == "Notify"` 的门禁挡在门外，连分支都进不去 ⇒
// 抓拍会话永远完不成（`NotifiedCount` 恒 0）。
func TestMessageHandlerDispatchesUploadSnapShotFinished(t *testing.T) {
	req := snapshotMessageRequest([]byte(hikUploadSnapShotFinishedBody), "37010301021320000002")
	tx := siptest.NewServerTxRecorder(req)
	sink := &snapshotMessageSink{}
	handler := NewMessageHandler(gbconfig.Config{})
	handler.SetSnapshotSink(sink)
	handler.Handle(req, tx)

	require.Len(t, tx.Result(), 1, "设备在上报结果，必须回 SIP 200，否则它会重发")
	require.EqualValues(t, 200, tx.Result()[0].StatusCode)
	require.True(t, sink.finished, "标准形态必须走 OnUploadSnapShotFinished")
	// ⛔ 反向锚点：两种形态**各走各的方法**。若有人把两条分支合并成一个"内部再猜"的入口，
	// 这条会红 —— 而合并的代价是"哪些标识算完成"的语义被拆成两套。
	require.False(t, sink.notifySeen, "标准形态不得走私有形态的方法")
	require.Equal(t, "37010301021320000002", sink.device)
	require.Equal(t, []byte(hikUploadSnapShotFinishedBody), sink.body)
}

// TestMessageHandlerKeepsPrivateSnapshotNotify 反向锚点：私有形态（模拟器当前口径）不能被
// 这次改动顺手删掉 —— 它是本仓唯一的端到端联调对象。
func TestMessageHandlerKeepsPrivateSnapshotNotify(t *testing.T) {
	body := []byte(`<?xml version="1.0" encoding="UTF-8"?><Notify><CmdType>Notify</CmdType><SubCmd>SnapShot</SubCmd><SN>9</SN><DeviceID>34020000001320000001</DeviceID><SessionID>session-1</SessionID><SnapShotID>shot-1</SnapShotID><Time>2026-08-30T23:30:00+08:00</Time><StoragePath>http://localhost/shot-1.jpg</StoragePath></Notify>`)
	req := recordInfoRequest(body)
	tx := siptest.NewServerTxRecorder(req)
	sink := &snapshotMessageSink{}
	handler := NewMessageHandler(gbconfig.Config{})
	handler.SetSnapshotSink(sink)
	handler.Handle(req, tx)

	require.Len(t, tx.Result(), 1)
	require.EqualValues(t, 200, tx.Result()[0].StatusCode)
	require.True(t, sink.notifySeen, "私有形态必须仍走 OnSnapshotNotify")
	require.False(t, sink.finished, "私有形态不得走标准形态的方法")
	require.Equal(t, "34020000001320000001", sink.device)
	require.Equal(t, body, sink.body)
}

// TestMessageHandlerDoesNotTreatPlainNotifyAsSnapshotFinished 反向锚点：普通订阅 Notify
// （`CmdType=Notify` 但没有 `SubCmd=SnapShot`）既不能走标准形态、也不能被私有形态误收。
func TestMessageHandlerDoesNotTreatPlainNotifyAsSnapshotFinished(t *testing.T) {
	body := []byte(`<?xml version="1.0" encoding="UTF-8"?><Notify><CmdType>Notify</CmdType><SN>9</SN><DeviceID>34020000001320000001</DeviceID><SessionID>session-1</SessionID></Notify>`)
	req := recordInfoRequest(body)
	tx := siptest.NewServerTxRecorder(req)
	sink := &snapshotMessageSink{}
	handler := NewMessageHandler(gbconfig.Config{})
	handler.SetSnapshotSink(sink)
	handler.Handle(req, tx)

	require.False(t, sink.finished)
	require.False(t, sink.notifySeen)
}

package handler

import (
	"context"
	"errors"
	"testing"

	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
)

type upgradeHandlerProbe struct {
	tx             *captureServerTransaction
	ordinarySeen   bool
	finalSeen      bool
	finalCalled    bool
	finalStatus    int
	finalErr       error
	finalConsumed  bool
	ordinaryCalled bool
}

func (p *upgradeHandlerProbe) OnUpgradeMessage(context.Context, string, string, string, []byte) (bool, error) {
	p.ordinaryCalled = true
	p.ordinarySeen = p.tx != nil && p.tx.response != nil
	return true, nil
}

func (p *upgradeHandlerProbe) OnUpgradeResultMessage(context.Context, string, string, string, []byte) (bool, int, error) {
	p.finalCalled = true
	p.finalSeen = p.tx != nil && p.tx.response != nil
	return p.finalConsumed, p.finalStatus, p.finalErr
}

func upgradeResultMessageBody() []byte {
	return []byte(`<Notify><CmdType>DeviceUpgradeResult</CmdType><SN>17</SN><DeviceID>34020000001320000001</DeviceID><SessionID>0123456789abcdef0123456789abcdef</SessionID><UpgradeResult>OK</UpgradeResult><Firmware>v2.3.4</Firmware></Notify>`)
}

func deviceControlMessageBody() []byte {
	return []byte(`<Response><CmdType>DeviceControl</CmdType><SN>17</SN><DeviceID>34020000001320000001</DeviceID><Result>OK</Result></Response>`)
}

func newUpgradeHandlerRequest(body []byte) *sip.Request {
	req := sip.NewRequest(sip.MESSAGE, sip.Uri{User: "platform", Host: "3402000000"})
	prepareNotifyRequest(req)
	req.SetBody(body)
	callID := sip.CallIDHeader("upgrade-handler-test")
	req.AppendHeader(&callID)
	req.AppendHeader(&sip.CSeqHeader{SeqNo: 3, MethodName: sip.MESSAGE})
	return req
}

func TestMessageHandlerAcksFinalUpgradeResultAfterProcessor(t *testing.T) {
	tx := &captureServerTransaction{}
	probe := &upgradeHandlerProbe{tx: tx, finalConsumed: true, finalStatus: 200}
	h := NewMessageHandler(gbconfig.Config{})
	h.SetUpgradeProcessor(probe)

	h.Handle(newUpgradeHandlerRequest(upgradeResultMessageBody()), tx)

	require.True(t, probe.finalCalled)
	require.False(t, probe.finalSeen, "final DeviceUpgradeResult must be persisted before SIP 200")
	require.NotNil(t, tx.response)
	require.Equal(t, 200, tx.response.StatusCode)
}

func TestMessageHandlerReturns503WhenFinalUpgradePersistenceFails(t *testing.T) {
	tx := &captureServerTransaction{}
	probe := &upgradeHandlerProbe{tx: tx, finalConsumed: true, finalStatus: 503, finalErr: errors.New("database unavailable")}
	h := NewMessageHandler(gbconfig.Config{})
	h.SetUpgradeProcessor(probe)

	h.Handle(newUpgradeHandlerRequest(upgradeResultMessageBody()), tx)

	require.True(t, probe.finalCalled)
	require.False(t, probe.finalSeen)
	require.NotNil(t, tx.response)
	require.Equal(t, 503, tx.response.StatusCode)
}

func TestMessageHandlerReturns400ForMalformedFinalUpgradeXML(t *testing.T) {
	tx := &captureServerTransaction{}
	h := NewMessageHandler(gbconfig.Config{})
	req := newUpgradeHandlerRequest([]byte(`<Notify><CmdType>DeviceUpgradeResult</CmdType>`))

	h.Handle(req, tx)

	require.NotNil(t, tx.response)
	require.Equal(t, 400, tx.response.StatusCode)
}

func TestMessageHandlerReturns503WhenFinalUpgradeProcessorIsDetached(t *testing.T) {
	tx := &captureServerTransaction{}
	h := NewMessageHandler(gbconfig.Config{})

	h.Handle(newUpgradeHandlerRequest(upgradeResultMessageBody()), tx)

	require.NotNil(t, tx.response)
	require.Equal(t, 503, tx.response.StatusCode)
}

func TestMessageHandlerAcksDeviceControlBeforeUpgradeProcessor(t *testing.T) {
	tx := &captureServerTransaction{}
	probe := &upgradeHandlerProbe{tx: tx}
	h := NewMessageHandler(gbconfig.Config{})
	h.SetUpgradeProcessor(probe)

	h.Handle(newUpgradeHandlerRequest(deviceControlMessageBody()), tx)

	require.True(t, probe.ordinaryCalled)
	require.True(t, probe.ordinarySeen, "ordinary DeviceControl keeps the fast transport ACK")
	require.NotNil(t, tx.response)
	require.Equal(t, 200, tx.response.StatusCode)
}

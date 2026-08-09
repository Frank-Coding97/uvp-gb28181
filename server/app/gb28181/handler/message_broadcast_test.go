package handler

import (
	"context"
	"testing"

	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
)

type captureBroadcastProcessor struct {
	peer string
	body []byte
}

func (p *captureBroadcastProcessor) OnBroadcastMessage(_ context.Context, peer string, body []byte) error {
	p.peer = peer
	p.body = append([]byte(nil), body...)
	return nil
}

func TestMessageHandlerAcknowledgesAndForwardsBroadcastResponse(t *testing.T) {
	h := NewMessageHandler(gbconfig.Config{})
	processor := &captureBroadcastProcessor{}
	h.SetBroadcastProcessor(processor)
	req := sip.NewRequest(sip.MESSAGE, sip.Uri{User: "platform"})
	req.AppendHeader(&sip.FromHeader{Address: sip.Uri{User: "device-1"}, Params: sip.NewParams()})
	req.SetBody([]byte(`<?xml version="1.0"?><Response><CmdType>Broadcast</CmdType><SN>9</SN><DeviceID>device-1</DeviceID><TargetID>channel-1</TargetID><Result>OK</Result></Response>`))
	tx := &captureServerTransaction{}

	h.Handle(req, tx)

	require.NotNil(t, tx.response)
	require.Equal(t, 200, tx.response.StatusCode)
	require.Equal(t, "device-1", processor.peer)
	require.Contains(t, string(processor.body), "<CmdType>Broadcast</CmdType>")
}

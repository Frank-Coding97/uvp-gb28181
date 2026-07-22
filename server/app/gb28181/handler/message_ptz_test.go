package handler

import (
	"testing"

	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.cn/uvp-gb28181/app/gb28181/metrics"
)

func TestTxKindFromCmd_PTZ2022(t *testing.T) {
	for _, cmd := range []string{manscdp.CmdDeviceControl, manscdp.CmdPTZPreciseCtrl, manscdp.CmdPresetQuery, manscdp.CmdHomePositionQuery, manscdp.CmdCruiseTrackListQuery, manscdp.CmdCruiseTrackQuery, manscdp.CmdPTZPreciseStatusQuery} {
		require.Equal(t, metrics.TxPTZ, txKindFromCmd(cmd), cmd)
	}
}

func TestPTZDeviceCodeUsesSIPFromInsteadOfResponseChannel(t *testing.T) {
	req := sip.NewRequest(sip.MESSAGE, sip.Uri{User: "platform", Host: "127.0.0.1"})
	req.AppendHeader(&sip.FromHeader{Address: sip.Uri{User: "34020000001320000001", Host: "3402000000"}})
	require.Equal(t, "34020000001320000001", ptzDeviceCode(req, "34020000001310000001"))
}

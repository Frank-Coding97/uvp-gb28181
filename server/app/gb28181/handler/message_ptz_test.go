package handler

import (
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.cn/uvp-gb28181/app/gb28181/metrics"
)

func TestTxKindFromCmd_PTZ2022(t *testing.T) {
	for _, cmd := range []string{manscdp.CmdDeviceControl, manscdp.CmdPTZPreciseCtrl, manscdp.CmdHomePositionQuery, manscdp.CmdCruiseTrackListQuery, manscdp.CmdCruiseTrackQuery, manscdp.CmdPTZPreciseStatusQuery} {
		require.Equal(t, metrics.TxPTZ, txKindFromCmd(cmd), cmd)
	}
}

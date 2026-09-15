package ptz

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
)

func TestHandlerIgnoresUnexpectedBusinessResponseForOneWayDeviceControl(t *testing.T) {
	for _, result := range []string{"OK", "ERROR"} {
		t.Run(result, func(t *testing.T) {
			service, db, _, _ := newPTZHandlerTestService(t, nil)
			profile := protocol.ProfileFor(protocol.Version2016)
			operation, err := service.Execute(context.Background(), testTarget(), Command{
				CmdType: manscdp.CmdDeviceControl, Action: "iframe", IdempotencyKey: "one-way-" + result,
				Profile: profile, TargetScope: gbmodels.ControlTargetScopeChannel, TargetCode: "C",
				Payload: map[string]interface{}{"action": "iframe"},
				Build: func(sn int) ([]byte, error) {
					return manscdp.BuildIFrameControlWithProfile(profile, "C", sn)
				},
			})
			require.NoError(t, err)
			require.False(t, operation.ResponseRequired)
			require.Equal(t, gbmodels.PTZOperationSent, operation.Status)

			require.NoError(t, service.OnPTZMessage(
				context.Background(), "D", "unexpected-response", "9",
				deviceControlResponse(operation.SN, result),
			))
			stored := storedOperation(t, db, operation.OperationID)
			require.Equal(t, gbmodels.PTZOperationSent, stored.Status)
			require.Empty(t, stored.DeviceResult)
			require.Nil(t, stored.ResponseCallID)
		})
	}
}

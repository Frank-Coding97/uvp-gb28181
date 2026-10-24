package gb28181

import (
	"context"
	"fmt"

	"go.uber.org/zap"
	"uvplatform.com/uvp-gb28181/app/global/app"
)

type cascadePTZNotificationBridge struct{}

func (cascadePTZNotificationBridge) RelayPTZNotify(ctx context.Context, deviceCode, callID, cseq string, body []byte) {
	broadcaster, ok := cascadeRuntimeManager.(interface {
		BroadcastPTZ(context.Context, string, string, string, []byte) error
	})
	if !ok {
		return
	}
	if err := broadcaster.BroadcastPTZ(ctx, deviceCode, callID, cseq, body); err != nil {
		app.Log(ctx).Warn("PTZ 精准通知向上级级联转发失败", zap.String("event", "cascade.event.ptz_relay_failed"), zap.String("call_id", callID), zap.String("cseq", cseq), zap.Error(err))
	}
}

type cascadeAlarmNotificationBridge struct{}

func (cascadeAlarmNotificationBridge) RelayAlarmNotify(ctx context.Context, deviceCode, callID, cseq string, body []byte) error {
	broadcaster, ok := cascadeRuntimeManager.(interface {
		EnqueueAlarm(context.Context, string, string, string, []byte) error
	})
	if !ok {
		return fmt.Errorf("cascade Alarm acceptance runtime is unavailable")
	}
	if err := broadcaster.EnqueueAlarm(ctx, deviceCode, callID, cseq, body); err != nil {
		app.Log(ctx).Warn("告警通知向上级级联转发失败", zap.String("event", "cascade.event.alarm_relay_failed"), zap.String("call_id", callID), zap.String("cseq", cseq), zap.Error(err))
		return err
	}
	return nil
}

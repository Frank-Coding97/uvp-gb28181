package subscribe

import (
	"context"
	"fmt"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

type PTZNotificationSink interface {
	OnPTZNotify(context.Context, string, string, string, []byte) error
}

// PTZProcessor forwards a validated subscription notification with the
// subscription's main device identity and the XML target left in the body.
type PTZProcessor struct {
	sink PTZNotificationSink
}

func NewPTZProcessor(sink PTZNotificationSink) *PTZProcessor {
	return &PTZProcessor{sink: sink}
}

func (p *PTZProcessor) Process(ctx context.Context, device *gbmodels.GbDevice, notification Notification) error {
	if p == nil || p.sink == nil || device == nil {
		return fmt.Errorf("PTZ 精准通知处理器未就绪")
	}
	return p.sink.OnPTZNotify(ctx, device.DeviceID, notification.CallID, notification.CSeq, notification.Body)
}

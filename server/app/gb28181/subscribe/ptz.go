package subscribe

import (
	"context"
	"fmt"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

type PTZNotificationSink interface {
	OnPTZNotify(context.Context, string, string, string, []byte) error
}

type PTZNotificationRelay interface {
	RelayPTZNotify(context.Context, string, string, string, []byte)
}

// PTZProcessor forwards a validated subscription notification with the
// subscription's main device identity and the XML target left in the body.
type PTZProcessor struct {
	sink  PTZNotificationSink
	relay PTZNotificationRelay
}

func NewPTZProcessor(sink PTZNotificationSink) *PTZProcessor {
	return &PTZProcessor{sink: sink}
}

func (p *PTZProcessor) SetRelay(relay PTZNotificationRelay) {
	if p != nil {
		p.relay = relay
	}
}

func (p *PTZProcessor) Process(ctx context.Context, device *gbmodels.GbDevice, notification Notification) error {
	if p == nil || p.sink == nil || device == nil {
		return fmt.Errorf("PTZ 精准通知处理器未就绪")
	}
	if err := p.sink.OnPTZNotify(ctx, device.DeviceID, notification.CallID, notification.CSeq, notification.Body); err != nil {
		return err
	}
	if p.relay != nil {
		p.relay.RelayPTZNotify(ctx, device.DeviceID, notification.CallID, notification.CSeq, notification.Body)
	}
	return nil
}

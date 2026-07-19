package trace

import (
	"context"
	"sync/atomic"

	"github.com/emiago/sipgo/sip"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
)

// Runtime is the lifecycle boundary between the SIP transport and trace pipeline.
type Runtime interface {
	ReadFilter(sip.TransportReadProps, []byte) ([]byte, error)
	WriteObserver(sip.TransportWriteProps, []byte)
	ConnectionClosed(sip.TransportReadProps)
	Shutdown(context.Context) error
}

// Module establishes transport hooks. Queueing and persistence are added by later tasks.
type Module struct {
	closed  atomic.Bool
	framer  *FrameAssembler
	onFrame func(Frame)
}

func NewRuntime(gbconfig.TraceConfig) Runtime {
	return &Module{framer: NewFrameAssembler(DefaultMaxFrameBytes)}
}

func (m *Module) ReadFilter(props sip.TransportReadProps, data []byte) ([]byte, error) {
	if m.closed.Load() || m.framer == nil {
		return data, nil
	}
	frames := m.framer.Push(props, data)
	for _, frame := range frames {
		m.emitFrame(frame)
	}
	return data, nil
}

func (m *Module) WriteObserver(sip.TransportWriteProps, []byte) {}

func (m *Module) ConnectionClosed(props sip.TransportReadProps) {
	if m.framer != nil {
		m.framer.Forget(props)
	}
}

func (m *Module) Shutdown(context.Context) error {
	m.closed.Store(true)
	return nil
}

func (m *Module) emitFrame(frame Frame) {
	if m.onFrame == nil {
		return
	}
	func() {
		defer func() { _ = recover() }()
		m.onFrame(frame)
	}()
}

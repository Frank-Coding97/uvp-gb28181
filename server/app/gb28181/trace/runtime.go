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
	Shutdown(context.Context) error
}

// Module establishes transport hooks. Queueing and persistence are added by later tasks.
type Module struct {
	closed atomic.Bool
}

func NewRuntime(gbconfig.TraceConfig) Runtime {
	return &Module{}
}

func (m *Module) ReadFilter(_ sip.TransportReadProps, data []byte) ([]byte, error) {
	return data, nil
}

func (m *Module) WriteObserver(sip.TransportWriteProps, []byte) {}

func (m *Module) Shutdown(context.Context) error {
	m.closed.Store(true)
	return nil
}

package diagnosis

import "context"

type DiagnosticSink interface {
	Emit(context.Context, Event) error
}

type NoopSink struct{}

func (NoopSink) Emit(context.Context, Event) error {
	return nil
}

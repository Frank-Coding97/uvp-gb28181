package sip

import (
	"context"

	"github.com/emiago/sipgo"
	siplib "github.com/emiago/sipgo/sip"
)

// drainHandler registers each accepted handler while holding the same lock
// that closes admission. Closing listeners alone does not join transaction
// handlers already dispatched by sipgo.
func (s *Server) drainHandler(next sipgo.RequestHandler) sipgo.RequestHandler {
	return func(req *siplib.Request, tx siplib.ServerTransaction) {
		s.handlerMu.Lock()
		if s.handlersClosing {
			s.handlerMu.Unlock()
			if req != nil && tx != nil && req.Method != siplib.ACK {
				_ = tx.Respond(siplib.NewResponseFromRequest(req, siplib.StatusServiceUnavailable, "Server Shutting Down", nil))
			}
			return
		}
		s.activeHandlers++
		s.handlerMu.Unlock()
		defer func() {
			s.handlerMu.Lock()
			defer s.handlerMu.Unlock()
			s.activeHandlers--
			if s.handlersClosing && s.activeHandlers == 0 {
				close(s.handlersDrained)
			}
		}()
		next(req, tx)
	}
}

// DrainRequests closes business request admission but preserves the UA and
// response transactions needed by outgoing session-cleanup BYEs.
func (s *Server) DrainRequests(ctx context.Context) error {
	s.handlerMu.Lock()
	if !s.handlersClosing {
		s.handlersClosing = true
		s.handlersDrained = make(chan struct{})
		if s.activeHandlers == 0 {
			close(s.handlersDrained)
		}
	}
	done := s.handlersDrained
	s.handlerMu.Unlock()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

package sip

import "context"

// BeginQuiesce rejects new Broadcast INVITEs and waits for handlers that were
// admitted before the gate closed. ACK/BYE and the outgoing UAC transport stay
// available until the session cleanup phase subsequently calls Shutdown.
func (s *Server) BeginQuiesce(ctx context.Context) error {
	s.inviteMu.Lock()
	if !s.quiescing {
		s.quiescing = true
		s.invitesDrained = make(chan struct{})
		if s.activeInvites == 0 {
			close(s.invitesDrained)
		}
	}
	done := s.invitesDrained
	s.inviteMu.Unlock()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Server) beginInvite() bool {
	s.inviteMu.Lock()
	defer s.inviteMu.Unlock()
	if s.quiescing {
		return false
	}
	s.activeInvites++
	return true
}

func (s *Server) endInvite() {
	s.inviteMu.Lock()
	defer s.inviteMu.Unlock()
	s.activeInvites--
	if s.quiescing && s.activeInvites == 0 {
		close(s.invitesDrained)
	}
}

package talk

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

const defaultCleanupTimeout = 15 * time.Second

type cleanupCall struct {
	done chan struct{}
	err  error
}

type cleanupRuntime struct {
	mu      sync.Mutex
	running map[string]*cleanupCall
}

func newCleanupRuntime() *cleanupRuntime {
	return &cleanupRuntime{running: make(map[string]*cleanupCall)}
}

func (s *Service) Cleanup(ctx context.Context, sessionID string, terminal models.TalkSessionState, reason string) error {
	if s == nil || s.repo == nil {
		return ErrTalkActivationUnavailable
	}
	if !terminal.IsTerminal() {
		return fmt.Errorf("%w: %s", ErrInvalidTerminalState, terminal)
	}
	if s.cleanup == nil {
		s.cleanup = newCleanupRuntime()
	}
	call, owner := s.cleanup.begin(sessionID)
	if owner {
		go func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), defaultCleanupTimeout)
			defer cancel()
			err := s.executeCleanup(cleanupCtx, sessionID, terminal, reason)
			s.cleanup.complete(sessionID, call, err)
		}()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-call.done:
		return call.err
	}
}

func (s *Service) executeCleanup(ctx context.Context, sessionID string, terminal models.TalkSessionState, reason string) error {
	session, err := s.repo.FindBySession(ctx, sessionID)
	if err != nil || session == nil {
		return err
	}
	if session.State.IsTerminal() {
		return nil
	}
	var firstErr error
	record := func(step string, stepErr error) {
		if stepErr != nil && firstErr == nil {
			firstErr = fmt.Errorf("%s: %w", step, stepErr)
		}
	}
	if session.State != models.TalkSessionStopping {
		changed, transitionErr := s.repo.Transition(ctx, sessionID, session.State, models.TalkSessionStopping, TransitionPatch{})
		record("mark stopping", transitionErr)
		if transitionErr == nil && !changed {
			reloaded, reloadErr := s.repo.FindBySession(ctx, sessionID)
			record("reload cleanup state", reloadErr)
			if reloaded == nil {
				record("reload cleanup state", ErrTalkSessionNotFound)
			} else if reloaded.State.IsTerminal() {
				return firstErr
			} else {
				session = reloaded
				if session.State != models.TalkSessionStopping {
					record("cleanup state conflict", fmt.Errorf("state=%s", session.State))
				}
			}
		}
	}
	if session.CallID != "" && s.activation != nil && s.activation.deps.Inviter != nil {
		record("TALK BYE", s.activation.deps.Inviter.ByeTalk(ctx, session.CallID))
	}
	var client TalkMediaClient
	if s.activation == nil || s.activation.deps.ClientFor == nil || s.nodes == nil {
		record("ZLM client", ErrTalkActivationUnavailable)
	} else if mediaNode, ok := s.nodes.Get(session.NodeID); !ok || mediaNode == nil {
		record("ZLM node", ErrTalkNodeUnavailable)
	} else {
		client = s.activation.deps.ClientFor(mediaNode)
		if client == nil {
			record("ZLM client", ErrTalkActivationUnavailable)
		}
	}
	if client != nil && session.SourceStream != "" && session.SSRC != "" {
		record("stopSendRtp", client.StopSendRtp(ctx, defaultTalkVHost, session.App, session.SourceStream, session.SSRC))
	}
	if client != nil && session.SourceStream != "" {
		record("close source", client.CloseTalkSource(ctx, defaultTalkVHost, session.App, session.SourceStream))
	}
	message := strings.TrimSpace(reason)
	if firstErr != nil {
		if message != "" {
			message += "; "
		}
		message += firstErr.Error()
	}
	if len(message) > 500 {
		message = message[:500]
	}
	finishCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, finishErr := s.repo.FinishAndReleaseLease(finishCtx, sessionID, terminal, message, s.now().UTC()); finishErr != nil {
		return errors.Join(firstErr, finishErr)
	}
	return firstErr
}

func (s *Service) SweepExpired(ctx context.Context) error {
	if s == nil || s.repo == nil {
		return ErrTalkActivationUnavailable
	}
	sessions, err := s.repo.ListExpired(ctx, s.now().UTC())
	if err != nil {
		return err
	}
	return s.cleanupSessions(ctx, sessions, models.TalkSessionExpired, "talk lease expired")
}

func (s *Service) Recover(ctx context.Context) error {
	if s == nil || s.repo == nil {
		return ErrTalkActivationUnavailable
	}
	sessions, err := s.repo.ListNonterminal(ctx)
	if err != nil {
		return err
	}
	return s.cleanupSessions(ctx, sessions, models.TalkSessionExpired, "service restart recovery")
}

func (s *Service) Shutdown(ctx context.Context) error {
	if s == nil || s.repo == nil {
		return nil
	}
	sessions, err := s.repo.ListNonterminal(ctx)
	if err != nil {
		return err
	}
	return s.cleanupSessions(ctx, sessions, models.TalkSessionEnded, "service shutdown")
}

func (s *Service) cleanupSessions(ctx context.Context, sessions []models.GbTalkSession, terminal models.TalkSessionState, reason string) error {
	var wg sync.WaitGroup
	errs := make(chan error, len(sessions))
	for i := range sessions {
		wg.Add(1)
		go func(sessionID string) {
			defer wg.Done()
			if err := s.Cleanup(ctx, sessionID, terminal, reason); err != nil {
				errs <- err
			}
		}(sessions[i].SessionID)
	}
	wg.Wait()
	close(errs)
	var result error
	for err := range errs {
		result = errors.Join(result, err)
	}
	return result
}

type CleanupWorker struct {
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
	mu     sync.RWMutex
	err    error
}

func (s *Service) StartCleanupWorker(interval time.Duration) *CleanupWorker {
	if interval <= 0 {
		interval = time.Second
	}
	ctx, cancel := context.WithCancel(context.Background())
	worker := &CleanupWorker{cancel: cancel, done: make(chan struct{})}
	ready := make(chan struct{})
	go func() {
		defer close(worker.done)
		worker.setError(s.SweepExpired(ctx))
		close(ready)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				worker.setError(s.SweepExpired(ctx))
			}
		}
	}()
	<-ready
	return worker
}

func (w *CleanupWorker) Err() error {
	if w == nil {
		return nil
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.err
}

func (w *CleanupWorker) setError(err error) {
	if err == nil {
		return
	}
	w.mu.Lock()
	w.err = err
	w.mu.Unlock()
}

func (w *CleanupWorker) Stop() {
	if w == nil {
		return
	}
	w.once.Do(w.cancel)
	<-w.done
}

func (r *cleanupRuntime) begin(sessionID string) (*cleanupCall, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if call := r.running[sessionID]; call != nil {
		return call, false
	}
	call := &cleanupCall{done: make(chan struct{})}
	r.running[sessionID] = call
	return call, true
}

func (r *cleanupRuntime) complete(sessionID string, call *cleanupCall, err error) {
	r.mu.Lock()
	call.err = err
	delete(r.running, sessionID)
	close(call.done)
	r.mu.Unlock()
}

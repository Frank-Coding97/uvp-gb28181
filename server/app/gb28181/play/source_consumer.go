package play

import "context"

// SourceLeaseChecker is a local, bounded ownership check. Implementations must
// not call the coordinator or perform I/O; stop invokes it under coordinator.mu.
type SourceLeaseChecker interface {
	HasLease(string) bool
}

type sourceLeaseClaim struct {
	ctx       context.Context
	registry  *SourceLeaseRegistry
	consumer  string
	lease     *SourceLease
	cancelled bool
}

func (c *Coordinator) registerConsumerLocked(entry *coordinatorEntry, claim *sourceLeaseClaim) {
	if claim == nil {
		return
	}
	if entry.state == LiveStateReady {
		c.bindConsumerLocked(entry.result, claim)
		return
	}
	if entry.consumerClaims == nil {
		entry.consumerClaims = make(map[*sourceLeaseClaim]struct{})
	}
	entry.consumerClaims[claim] = struct{}{}
}

func (c *Coordinator) bindConsumerLocked(result *Result, claim *sourceLeaseClaim) {
	if claim.cancelled || claim.ctx.Err() != nil || claim.lease != nil || result == nil || result.Generation == 0 {
		return
	}
	claim.lease = claim.registry.Acquire(result.StreamID, result.Generation, claim.consumer)
}

func (c *Coordinator) releaseConsumer(claim *sourceLeaseClaim) {
	if claim == nil {
		return
	}
	c.mu.Lock()
	claim.cancelled = true
	lease := claim.lease
	c.mu.Unlock()
	if lease != nil {
		_ = lease.Release()
	}
}

// BackendOriginResolver is an internal dependency, never populated from an HTTP
// request. It proves the media server's observed backend address after selection.
type BackendOriginResolver func(context.Context, *Result) (string, error)

func (s *Service) StartAuthorizedWithBackendOrigin(ctx context.Context, req AuthorizedRequest, registry *SourceLeaseRegistry, consumer string, origin BackendOriginResolver) (*Result, *SourceLease, error) {
	if origin == nil {
		return nil, nil, ErrPlayAuthorizationUnavailable
	}
	return s.startAuthorizedWithSourceLease(ctx, req, registry, consumer, origin)
}

// StartAuthorizedWithSourceLease atomically joins live ownership before Ready
// becomes visible to none-reader cleanup. The caller releases only this lease.
func (s *Service) StartAuthorizedWithSourceLease(ctx context.Context, req AuthorizedRequest, registry *SourceLeaseRegistry, consumer string) (result *Result, lease *SourceLease, err error) {
	return s.startAuthorizedWithSourceLease(ctx, req, registry, consumer, nil)
}

func (s *Service) startAuthorizedWithSourceLease(ctx context.Context, req AuthorizedRequest, registry *SourceLeaseRegistry, consumer string, origin BackendOriginResolver) (result *Result, lease *SourceLease, err error) {
	if registry == nil || consumer == "" {
		return nil, nil, ErrPlayAuthorizationUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	claim := &sourceLeaseClaim{ctx: ctx, registry: registry, consumer: consumer}
	defer func() {
		if err != nil {
			s.coordinator().releaseConsumer(claim)
		}
	}()
	result, err = s.startAuthorizedWithOrigin(ctx, req, claim, origin)
	if err != nil {
		return nil, nil, err
	}
	c := s.coordinator()
	c.mu.Lock()
	lease = claim.lease
	c.mu.Unlock()
	if err = ctx.Err(); err != nil {
		return nil, nil, err
	}
	if lease == nil {
		return nil, nil, ErrPlayAuthorizationUnavailable
	}
	return result, lease, nil
}

func (s *Service) SetSourceLeaseChecker(checker SourceLeaseChecker) {
	s.sourceLeaseMu.Lock()
	s.sourceLeaseChecker = checker
	s.sourceLeaseMu.Unlock()
}

func (s *Service) sourceHasLease(streamID string) bool {
	s.sourceLeaseMu.RLock()
	checker := s.sourceLeaseChecker
	s.sourceLeaseMu.RUnlock()
	return checker != nil && checker.HasLease(streamID)
}

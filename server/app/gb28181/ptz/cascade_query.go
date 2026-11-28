package ptz

import (
	"context"
	"fmt"
	"time"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

type cascadeQueryWaiter struct {
	pages     [][]byte
	bytes     int
	complete  bool
	err       error
	changed   chan struct{}
	refs      int
	expiresAt time.Time
	identity  string
}

// QueryForCascade returns only responses accepted for this exact live operation.
// Publication callers must recheck their own upstream and device authorization
// before forwarding. No historical UI cache is used as a response substitute.
func (s *Service) QueryForCascade(ctx context.Context, target Target, kind QueryKind, trackID int, key string) ([][]byte, error) {
	return s.queryForCascade(ctx, target, fmt.Sprintf("ptz:%s:%d", kind, trackID), key, func() (gbmodels.GbPTZOperation, error) {
		return s.Refresh(ctx, target, kind, trackID, key)
	})
}

// StorageCardsForCascade observes this storage read operation, independently of
// the PTZ query kinds. No previous UI cache can satisfy this request.
func (s *Service) StorageCardsForCascade(ctx context.Context, target Target, key string) ([]byte, error) {
	pages, err := s.queryForCascade(ctx, target, "storage-cards", key, func() (gbmodels.GbPTZOperation, error) {
		return s.RefreshStorageCards(ctx, target, 0, 0, key)
	})
	if err != nil {
		return nil, err
	}
	if len(pages) != 1 {
		return nil, fmt.Errorf("storage query requires one complete response")
	}
	return pages[0], nil
}

func (s *Service) queryForCascade(ctx context.Context, target Target, identity, key string, start func() (gbmodels.GbPTZOperation, error)) ([][]byte, error) {
	if s == nil || key == "" {
		return nil, fmt.Errorf("cascade query service or identity unavailable")
	}
	mapKey := cascadeQueryWaiterKey(target.DeviceID, target.ChannelID, target.DeviceCode, target.ChannelCode, target.DeviceEpoch, key)
	s.cascadeQueryMu.Lock()
	if s.cascadeQueries == nil {
		s.cascadeQueries = make(map[string]*cascadeQueryWaiter)
	}
	now := time.Now()
	for existingKey, existing := range s.cascadeQueries {
		if existing.refs == 0 && existing.complete && !existing.expiresAt.After(now) {
			delete(s.cascadeQueries, existingKey)
		}
	}
	waiter := s.cascadeQueries[mapKey]
	if waiter == nil {
		if len(s.cascadeQueries) >= 32 {
			oldestKey := ""
			var oldest time.Time
			for existingKey, existing := range s.cascadeQueries {
				if existing.refs == 0 && existing.complete && (oldestKey == "" || existing.expiresAt.Before(oldest)) {
					oldestKey, oldest = existingKey, existing.expiresAt
				}
			}
			if oldestKey != "" {
				delete(s.cascadeQueries, oldestKey)
			}
		}
		if len(s.cascadeQueries) >= 32 {
			s.cascadeQueryMu.Unlock()
			return nil, fmt.Errorf("cascade query capacity exceeded")
		}
		waiter = &cascadeQueryWaiter{changed: make(chan struct{}, 1), identity: identity}
		s.cascadeQueries[mapKey] = waiter
	} else if waiter.identity != identity {
		s.cascadeQueryMu.Unlock()
		return nil, fmt.Errorf("cascade query identity conflict")
	}
	waiter.refs++
	s.cascadeQueryMu.Unlock()
	defer func() {
		s.cascadeQueryMu.Lock()
		waiter.refs--
		if waiter.refs == 0 && !waiter.complete && s.cascadeQueries[mapKey] == waiter {
			delete(s.cascadeQueries, mapKey)
		}
		s.cascadeQueryMu.Unlock()
	}()

	operation, err := start()
	if err != nil {
		return nil, err
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		s.cascadeQueryMu.Lock()
		complete, resultErr := waiter.complete, waiter.err
		var pages [][]byte
		if complete && resultErr == nil {
			for _, body := range waiter.pages {
				pages = append(pages, append([]byte(nil), body...))
			}
		}
		s.cascadeQueryMu.Unlock()
		if resultErr != nil {
			return nil, resultErr
		}
		if complete {
			return pages, nil
		}
		current, err := s.GetOperation(ctx, operation.OperationID)
		if err != nil {
			return nil, err
		}
		switch current.Status {
		case gbmodels.PTZOperationRejected, gbmodels.PTZOperationTimeout, gbmodels.PTZOperationCancelled, gbmodels.PTZOperationUnknown:
			return nil, fmt.Errorf("cascade query ended with %s", current.Status)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-waiter.changed:
		case <-ticker.C:
		}
	}
}

// Called only after a matching response has passed validation and its state
// transaction has committed. The query-stage lock preserves page ordering.
func (s *Service) observeCascadeQuery(operation gbmodels.GbPTZOperation, body []byte, complete bool) {
	s.cascadeQueryMu.Lock()
	defer s.cascadeQueryMu.Unlock()
	epoch := int64(0)
	if operation.DeviceEpoch != nil {
		epoch = *operation.DeviceEpoch
	}
	waiter := s.cascadeQueries[cascadeQueryWaiterKey(operation.DeviceID, operation.ChannelID, operation.DeviceCode, operation.ChannelCode, epoch, operation.IdempotencyKey)]
	if waiter == nil || waiter.complete || waiter.err != nil {
		return
	}
	if len(waiter.pages) >= 10000 || waiter.bytes+len(body) > 4<<20 {
		waiter.pages = nil
		waiter.err = fmt.Errorf("cascade query response limit exceeded")
	} else {
		waiter.pages = append(waiter.pages, append([]byte(nil), body...))
		waiter.bytes += len(body)
		waiter.complete = complete
		if complete {
			waiter.expiresAt = time.Now().Add(30 * time.Second)
		}
	}
	select {
	case waiter.changed <- struct{}{}:
	default:
	}
}

func cascadeQueryWaiterKey(deviceID, channelID uint, deviceCode, channelCode string, epoch int64, key string) string {
	return fmt.Sprintf("%d:%s:%d:%s:%d:%s", deviceID, deviceCode, channelID, channelCode, epoch, key)
}

func (s *Service) clearCascadeQueries() {
	s.cascadeQueryMu.Lock()
	defer s.cascadeQueryMu.Unlock()
	for _, waiter := range s.cascadeQueries {
		waiter.pages = nil
		waiter.err = fmt.Errorf("PTZ query service retired")
		select {
		case waiter.changed <- struct{}{}:
		default:
		}
	}
	s.cascadeQueries = nil
}

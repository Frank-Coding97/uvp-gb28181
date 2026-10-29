package security

import "time"

const (
	registerScanWindow      = 10 * time.Minute
	registerScanThreshold   = 10
	registerScanDistinctIDs = 3
	maxRegisterObservations = 128
)

type registerObservation struct {
	at          time.Time
	transaction string
	deviceID    string
}

// ValidDeviceID matches the platform's device creation contract.
func ValidDeviceID(id string) bool {
	if len(id) != 20 {
		return false
	}
	for i := range id {
		if id[i] < '0' || id[i] > '9' {
			return false
		}
	}
	return true
}

func verifiedEventSource(event Event) bool {
	return event.SourceVerified || isStreamTransport(event.Transport)
}

func scoringBucketKey(event Event) string {
	key := riskBucketKey(event)
	if event.RiskScope == ScopeSource && verifiedEventSource(event) {
		return "verified|" + key
	}
	return key
}

// Caller holds s.mu. A claimed ID never establishes an authenticated source.
func (s *Scorer) protectedSource(source string) bool {
	for _, endpoint := range s.endpoints {
		if endpoint.Address == source {
			return true
		}
	}
	return false
}

// SetAutoBanEnabled disables new IP bans when authenticated-source history is
// unavailable. Packet rejection and existing bans remain effective.
func (s *Scorer) SetAutoBanEnabled(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.autoBanDisabled = !enabled
}

func (s *Scorer) observeInvalidRegister(event Event) (bool, bool) {
	if event.TransactionID == "" || ValidDeviceID(event.DeviceID) {
		return false, false
	}
	for key, entries := range s.registrations {
		n := 0
		for n < len(entries) && !entries[n].at.Add(registerScanWindow).After(event.Occurred) {
			n++
		}
		if n == len(entries) {
			delete(s.registrations, key)
		} else {
			s.registrations[key] = entries[n:]
		}
	}
	detected := s.addRegisterObservation(event.SourceIP, event)
	verified := false
	if verifiedEventSource(event) {
		verified = s.addRegisterObservation("verified|"+event.SourceIP, event)
	}
	return detected, verified
}

func (s *Scorer) addRegisterObservation(key string, event Event) bool {
	entries, exists := s.registrations[key]
	if !exists && len(s.registrations) >= 2*s.policy.MaxEventKeys {
		var oldest string
		var at time.Time
		for k, v := range s.registrations {
			if at.IsZero() || v[len(v)-1].at.Before(at) {
				oldest, at = k, v[len(v)-1].at
			}
		}
		delete(s.registrations, oldest)
	}
	duplicate := false
	for _, e := range entries {
		if e.transaction == event.TransactionID {
			duplicate = true
			break
		}
	}
	if !duplicate {
		entries = append(entries, registerObservation{at: event.Occurred, transaction: event.TransactionID, deviceID: event.DeviceID})
		if len(entries) > maxRegisterObservations {
			entries = entries[len(entries)-maxRegisterObservations:]
		}
		s.registrations[key] = entries
	}
	if len(entries) < registerScanThreshold {
		return false
	}
	ids := make(map[string]struct{}, registerScanDistinctIDs)
	for _, e := range entries {
		ids[e.deviceID] = struct{}{}
		if len(ids) >= registerScanDistinctIDs {
			return true
		}
	}
	return false
}

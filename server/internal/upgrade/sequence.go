package upgrade

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
)

type acceptedSequence struct {
	sequence uint64
	digest   string
}

// SequenceGate prevents replaying an older release-channel manifest. A
// repeated sequence is idempotent only when its verified payload is identical.
// The caller must persist the accepted value before replacing this process if
// replay protection is required across restarts.
type SequenceGate struct {
	mu       sync.Mutex
	accepted map[string]acceptedSequence
}

// AcceptEnvelope binds a channel sequence to the exact signed envelope that
// was fetched.  The envelope includes the signed payload and key identifiers,
// so a repeated sequence with different bytes is rejected as a replay/conflict.
func (g *SequenceGate) AcceptEnvelope(channel string, sequence uint64, envelope []byte) error {
	return g.Accept(channel, sequence, envelope)
}

func NewSequenceGate() *SequenceGate {
	return &SequenceGate{accepted: make(map[string]acceptedSequence)}
}

// Accept records a verified manifest payload for channel. It must be called
// only after signature verification; the payload digest binds the sequence to
// the exact signed content.
func (g *SequenceGate) Accept(channel string, sequence uint64, payload []byte) error {
	if g == nil {
		return errors.New("sequence gate is nil")
	}
	if channel == "" || sequence == 0 || len(payload) == 0 {
		return errors.New("channel, positive sequence and payload are required")
	}
	digestBytes := sha256.Sum256(payload)
	digest := hex.EncodeToString(digestBytes[:])

	g.mu.Lock()
	defer g.mu.Unlock()
	if g.accepted == nil {
		g.accepted = make(map[string]acceptedSequence)
	}
	previous, exists := g.accepted[channel]
	if exists {
		switch {
		case sequence < previous.sequence:
			return fmt.Errorf("manifest sequence %d is older than accepted sequence %d", sequence, previous.sequence)
		case sequence == previous.sequence && digest != previous.digest:
			return errors.New("manifest sequence points to different payload")
		case sequence == previous.sequence:
			return nil
		}
	}
	g.accepted[channel] = acceptedSequence{sequence: sequence, digest: digest}
	return nil
}

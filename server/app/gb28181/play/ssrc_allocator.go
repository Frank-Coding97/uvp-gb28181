package play

import (
	"errors"
	"fmt"
	"strconv"
	"sync"

	"uvplatform.cn/uvp-gb28181/app/gb28181/sdp"
)

const realtimeSSRCSequenceSpace = 10000

var (
	ErrInvalidSSRCDomain = errors.New("非法实时SSRC域")
	ErrInvalidSSRC       = errors.New("非法实时SSRC")
	ErrSSRCInUse         = errors.New("实时SSRC已占用")
	ErrSSRCExhausted     = errors.New("实时SSRC序号已耗尽")
)

// RealtimeSSRCAllocator owns the active four-digit sequence leases for one
// GB28181 monitoring domain.
type RealtimeSSRCAllocator struct {
	mu            sync.Mutex
	domain        string
	prefix        string
	sequenceSpace int
	next          int
	leased        map[int]struct{}
}

func NewRealtimeSSRCAllocator(domain string) (*RealtimeSSRCAllocator, error) {
	return newRealtimeSSRCAllocator(domain, realtimeSSRCSequenceSpace)
}

func newRealtimeSSRCAllocator(domain string, sequenceSpace int) (*RealtimeSSRCAllocator, error) {
	if sequenceSpace <= 0 || sequenceSpace > realtimeSSRCSequenceSpace {
		return nil, fmt.Errorf("%w: 非法序号空间", ErrInvalidSSRCDomain)
	}
	first, err := sdp.FormatRealtimeSSRC(domain, 0)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidSSRCDomain, err)
	}
	return &RealtimeSSRCAllocator{
		domain:        domain,
		prefix:        first[:6],
		sequenceSpace: sequenceSpace,
		leased:        make(map[int]struct{}, sequenceSpace),
	}, nil
}

func (a *RealtimeSSRCAllocator) Acquire() (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	for offset := 0; offset < a.sequenceSpace; offset++ {
		sequence := (a.next + offset) % a.sequenceSpace
		if _, occupied := a.leased[sequence]; occupied {
			continue
		}
		a.leased[sequence] = struct{}{}
		a.next = (sequence + 1) % a.sequenceSpace
		ssrc, _ := sdp.FormatRealtimeSSRC(a.domain, uint16(sequence))
		return ssrc, nil
	}
	return "", ErrSSRCExhausted
}

func (a *RealtimeSSRCAllocator) Reserve(ssrc string) error {
	sequence, err := a.sequenceOf(ssrc)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, occupied := a.leased[sequence]; occupied {
		return ErrSSRCInUse
	}
	a.leased[sequence] = struct{}{}
	return nil
}

func (a *RealtimeSSRCAllocator) Release(ssrc string) {
	sequence, err := a.sequenceOf(ssrc)
	if err != nil {
		return
	}
	a.mu.Lock()
	delete(a.leased, sequence)
	a.mu.Unlock()
}

func (a *RealtimeSSRCAllocator) sequenceOf(ssrc string) (int, error) {
	if len(ssrc) != 10 || ssrc[:6] != a.prefix {
		return 0, ErrInvalidSSRC
	}
	sequence, err := strconv.Atoi(ssrc[6:])
	if err != nil || sequence < 0 || sequence >= a.sequenceSpace {
		return 0, ErrInvalidSSRC
	}
	return sequence, nil
}

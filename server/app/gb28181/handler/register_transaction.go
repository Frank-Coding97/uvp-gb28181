package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/emiago/sipgo/sip"
)

const (
	defaultRegisterTransactionTTL  = 60 * time.Second
	defaultMaxRegisterTransactions = 4096
)

type registerTransactionResult struct {
	status  int
	reason  string
	body    []byte
	headers []registerResponseHeader
}

type registerResponseHeader struct {
	name  string
	value string
}

func normalizeRegisterResponse(response *sip.Response) (registerTransactionResult, bool) {
	if response == nil {
		return registerTransactionResult{}, false
	}
	result := registerTransactionResult{
		status: response.StatusCode,
		reason: response.Reason,
		body:   append([]byte(nil), response.Body()...),
	}
	for _, header := range response.Headers() {
		name := header.Name()
		switch strings.ToLower(name) {
		case "via", "from", "to", "call-id", "cseq", "content-length", "x-gb-ver":
			continue
		}
		result.headers = append(result.headers, registerResponseHeader{name: name, value: header.Value()})
	}
	return result, true
}

func (result registerTransactionResult) response(req *sip.Request, platformVersion string) *sip.Response {
	response := newRegisterResponse(req, result.status, result.reason, append([]byte(nil), result.body...), platformVersion)
	for _, header := range result.headers {
		response.AppendHeader(sip.NewHeader(header.name, header.value))
	}
	return response
}

type registerTransactionEntry struct {
	createdAt time.Time
	expiresAt time.Time
	ready     chan struct{}
	result    registerTransactionResult
	complete  bool
}

type registerTransactionLedger struct {
	mu      sync.Mutex
	entries map[string]*registerTransactionEntry
	ttl     time.Duration
	max     int
	now     func() time.Time
}

func newRegisterTransactionLedger(ttl time.Duration, max int, now func() time.Time) *registerTransactionLedger {
	if ttl <= 0 {
		ttl = defaultRegisterTransactionTTL
	}
	if max <= 0 {
		max = defaultMaxRegisterTransactions
	}
	if now == nil {
		now = time.Now
	}
	return &registerTransactionLedger{entries: make(map[string]*registerTransactionEntry), ttl: ttl, max: max, now: now}
}

// begin elects one request to execute the transaction. Replays either return
// the cached result or wait for the elected request to publish it.
func (ledger *registerTransactionLedger) begin(key string) (registerTransactionResult, func(registerTransactionResult, bool), bool) {
	for {
		now := ledger.now()
		ledger.mu.Lock()
		ledger.removeExpiredLocked(now)
		if entry := ledger.entries[key]; entry != nil {
			if entry.complete {
				result := entry.result
				ledger.mu.Unlock()
				return result, nil, false
			}
			ready := entry.ready
			ledger.mu.Unlock()
			<-ready
			continue
		}
		if len(ledger.entries) >= ledger.max {
			ledger.evictOldestLocked()
		}
		entry := &registerTransactionEntry{createdAt: now, expiresAt: now.Add(ledger.ttl), ready: make(chan struct{})}
		ledger.entries[key] = entry
		ledger.mu.Unlock()

		var once sync.Once
		finish := func(result registerTransactionResult, cache bool) {
			once.Do(func() {
				ledger.mu.Lock()
				if current := ledger.entries[key]; current == entry {
					if cache {
						entry.result = result
						entry.complete = true
						entry.expiresAt = ledger.now().Add(ledger.ttl)
					} else {
						delete(ledger.entries, key)
					}
					close(entry.ready)
				}
				ledger.mu.Unlock()
			})
		}
		return registerTransactionResult{}, finish, true
	}
}

func (ledger *registerTransactionLedger) removeExpiredLocked(now time.Time) {
	for key, entry := range ledger.entries {
		if entry.complete && !entry.expiresAt.After(now) {
			delete(ledger.entries, key)
		}
	}
}

func (ledger *registerTransactionLedger) evictOldestLocked() {
	var oldestKey string
	var oldest *registerTransactionEntry
	for key, entry := range ledger.entries {
		if !entry.complete {
			continue
		}
		if oldest == nil || entry.createdAt.Before(oldest.createdAt) {
			oldestKey, oldest = key, entry
		}
	}
	if oldest != nil {
		delete(ledger.entries, oldestKey)
	}
}

func registerTransactionKey(req *sip.Request, deviceID string) string {
	callID := registerCallID(req)
	cseq := registerCSeq(req)
	branch := ""
	if via := req.Via(); via != nil {
		branch, _ = via.Params.Get("branch")
	}
	authorization := ""
	if header := req.GetHeader("Authorization"); header != nil {
		sum := sha256.Sum256([]byte(strings.TrimSpace(header.Value())))
		authorization = hex.EncodeToString(sum[:])
	}
	return fmt.Sprintf("%s|%s|%d|%s|%s", deviceID, callID, cseq, branch, authorization)
}

type capturingRegisterTransaction struct {
	sip.ServerTransaction
	responseResult registerTransactionResult
	captured       bool
}

func (transaction *capturingRegisterTransaction) Respond(response *sip.Response) error {
	transaction.responseResult, transaction.captured = normalizeRegisterResponse(response)
	return transaction.ServerTransaction.Respond(response)
}

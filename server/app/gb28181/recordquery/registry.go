package recordquery

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"sync"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
)

type correlationKey struct {
	senderDeviceCode string
	sn               int
}

type queryEntry struct {
	mu              sync.Mutex
	channelCode     string
	maxRecords      int
	updates         chan struct{}
	sawResponse     bool
	explicitEmpty   bool
	declaredTotal   int
	capacityReached bool
	records         map[string]manscdp.RecordInfoItem
	rejectedCount   int
	warningCount    int
	warningCodes    []string
}

type entrySnapshot struct {
	sawResponse     bool
	explicitEmpty   bool
	declaredTotal   int
	capacityReached bool
	records         []manscdp.RecordInfoItem
	// 协议诊断计数:聚合边界不再把无效项/警告静默丢弃,
	// 结果里可区分协议数据错误与真正的设备超时
	rejectedCount int
	warningCount  int
	warningCodes  []string
}

type Registry struct {
	mu      sync.RWMutex
	entries map[correlationKey]*queryEntry
	max     int
	closed  bool
	done    chan struct{}
}

func NewRegistry(maxActive int) (*Registry, error) {
	if maxActive <= 0 {
		return nil, queryError(ErrorCodeInvalidArgument, ErrInvalidArgument)
	}
	return &Registry{entries: make(map[correlationKey]*queryEntry), max: maxActive, done: make(chan struct{})}, nil
}

func (r *Registry) register(senderDeviceCode string, sn, maxRecords int, channelCode string) (*queryEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, queryError(ErrorCodeUnavailable, ErrUnavailable)
	}
	if len(r.entries) >= r.max {
		return nil, queryError(ErrorCodeBusy, ErrBusy)
	}
	key := correlationKey{senderDeviceCode: senderDeviceCode, sn: sn}
	if _, exists := r.entries[key]; exists {
		return nil, queryError(ErrorCodeBusy, ErrBusy)
	}
	entry := &queryEntry{
		channelCode: channelCode,
		maxRecords:  maxRecords,
		updates:     make(chan struct{}, 1),
		records:     make(map[string]manscdp.RecordInfoItem),
	}
	r.entries[key] = entry
	return entry, nil
}

func (r *Registry) remove(senderDeviceCode string, sn int) {
	r.mu.Lock()
	delete(r.entries, correlationKey{senderDeviceCode: senderDeviceCode, sn: sn})
	r.mu.Unlock()
}

func (r *Registry) Accept(senderDeviceCode string, response *manscdp.RecordInfoResponse) bool {
	if r == nil || response == nil {
		return false
	}
	r.mu.RLock()
	entry := r.entries[correlationKey{senderDeviceCode: senderDeviceCode, sn: response.SN}]
	r.mu.RUnlock()
	if entry == nil || response.DeviceID != entry.channelCode {
		return false
	}
	entry.add(response)
	return true
}

func (r *Registry) Active() int {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.entries)
}

func (r *Registry) Done() <-chan struct{} {
	if r == nil {
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	return r.done
}

func (r *Registry) Close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	if !r.closed {
		r.closed = true
		clear(r.entries)
		close(r.done)
	}
	r.mu.Unlock()
}

func (e *queryEntry) add(response *manscdp.RecordInfoResponse) {
	e.mu.Lock()
	e.sawResponse = true
	if response.SumNum > e.declaredTotal {
		e.declaredTotal = response.SumNum
	}
	if response.SumNum == 0 && len(response.Items) == 0 && len(response.ItemResults) == 0 && response.RecordListNum == 0 {
		e.explicitEmpty = true
	}
	for _, item := range response.Items {
		if item.DeviceID != e.channelCode {
			e.rejectedCount++
			continue
		}
		key := recordDigest(item)
		if _, duplicate := e.records[key]; duplicate {
			continue
		}
		if len(e.records) >= e.maxRecords {
			e.capacityReached = true
			break
		}
		e.records[key] = item
	}
	for _, result := range response.ItemResults {
		if result.Valid {
			continue
		}
		e.rejectedCount++
		if result.Error != nil {
			e.warningCount++
			if code := strings.TrimSpace(string(result.Error.Code)); code != "" && len(e.warningCodes) < 8 {
				e.warningCodes = append(e.warningCodes, code)
			}
		}
	}
	if len(e.records) >= e.maxRecords && (e.declaredTotal > len(e.records) || len(response.Items) > e.maxRecords) {
		e.capacityReached = true
	}
	e.mu.Unlock()
	select {
	case e.updates <- struct{}{}:
	default:
	}
}

func (e *queryEntry) snapshot() entrySnapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	records := make([]manscdp.RecordInfoItem, 0, len(e.records))
	for _, item := range e.records {
		records = append(records, item)
	}
	return entrySnapshot{
		sawResponse: e.sawResponse, explicitEmpty: e.explicitEmpty,
		declaredTotal: e.declaredTotal, capacityReached: e.capacityReached,
		records:       records,
		rejectedCount: e.rejectedCount, warningCount: e.warningCount,
		warningCodes: append([]string(nil), e.warningCodes...),
	}
}

func recordDigest(item manscdp.RecordInfoItem) string {
	hash := sha256.New()
	fields := []string{
		item.DeviceID, item.Name, item.FilePath, item.Address, item.StartTime,
		item.EndTime, strconv.Itoa(item.Secrecy), string(item.Type), item.RecorderID,
		item.RecordLocation,
	}
	for _, field := range fields {
		hash.Write([]byte(strconv.Itoa(len(field))))
		hash.Write([]byte{':'})
		hash.Write([]byte(field))
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func normalizeIdentity(value string) string { return strings.TrimSpace(value) }

package recording

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

const maxRecordQueryRange = 24 * time.Hour

var (
	ErrRecordDeviceNotFound  = errors.New("设备不存在")
	ErrRecordDeviceOffline   = errors.New("设备离线")
	ErrRecordChannelNotFound = errors.New("通道不存在")
	ErrRecordQueryRange      = errors.New("录像查询时间范围非法")
)

type MessageSender interface {
	SendMessage(context.Context, string, string, string, []byte) error
}

type RecordDeviceRepo interface {
	FindByDeviceID(context.Context, string) (*gbmodels.GbDevice, error)
}

type RecordChannelRepo interface {
	FindChannel(context.Context, string, string) (*gbmodels.GbChannel, error)
}

type QueryRequest struct {
	DeviceID   string
	ChannelID  string
	StartTime  time.Time
	EndTime    time.Time
	Type       string
	Secrecy    int
	RecorderID string
}

type QueryResult struct {
	Records    []manscdp.RecordInfoItem
	Total      int
	Incomplete bool
}

type QueryService struct {
	sender   MessageSender
	devices  RecordDeviceRepo
	channels RecordChannelRepo

	nextSN  atomic.Uint64
	mu      sync.Mutex
	waiters map[string]*queryWaiter
}

type queryWaiter struct {
	mu      sync.Mutex
	done    chan struct{}
	closed  bool
	total   int
	records []manscdp.RecordInfoItem
	seen    map[string]struct{}
}

func NewQueryService(sender MessageSender, devices RecordDeviceRepo, channels RecordChannelRepo) *QueryService {
	return &QueryService{sender: sender, devices: devices, channels: channels, waiters: make(map[string]*queryWaiter)}
}

func (s *QueryService) Query(ctx context.Context, request QueryRequest) (QueryResult, error) {
	if s == nil || s.sender == nil || s.devices == nil || s.channels == nil {
		return QueryResult{}, fmt.Errorf("录像查询服务未装配")
	}
	if err := validateQueryRequest(request); err != nil {
		return QueryResult{}, err
	}
	device, err := s.devices.FindByDeviceID(ctx, request.DeviceID)
	if err != nil {
		return QueryResult{}, fmt.Errorf("查询设备失败: %w", err)
	}
	if device == nil {
		return QueryResult{}, ErrRecordDeviceNotFound
	}
	if device.Status != gbmodels.DeviceStatusOnline {
		return QueryResult{}, ErrRecordDeviceOffline
	}
	channel, err := s.channels.FindChannel(ctx, request.DeviceID, request.ChannelID)
	if err != nil {
		return QueryResult{}, fmt.Errorf("查询通道失败: %w", err)
	}
	if channel == nil {
		return QueryResult{}, ErrRecordChannelNotFound
	}

	sn := int(s.nextSN.Add(1))
	key := queryKey(device.DeviceID, sn)
	waiter := &queryWaiter{done: make(chan struct{}), seen: make(map[string]struct{})}
	s.mu.Lock()
	s.waiters[key] = waiter
	s.mu.Unlock()
	defer s.removeWaiter(key)

	body, err := manscdp.BuildRecordInfoQuery(manscdp.RecordInfoQuery{
		SN: sn, DeviceID: channel.ChannelID, StartTime: request.StartTime, EndTime: request.EndTime,
		Type: request.Type, Secrecy: request.Secrecy, RecorderID: request.RecorderID,
	})
	if err != nil {
		return QueryResult{}, err
	}
	destination := fmt.Sprintf("%s:%d", device.IP, device.Port)
	if err := s.sender.SendMessage(ctx, device.DeviceID, destination, device.Transport, body); err != nil {
		return QueryResult{}, fmt.Errorf("发送录像查询失败: %w", err)
	}

	select {
	case <-waiter.done:
		return waiter.result(false), nil
	case <-ctx.Done():
		return waiter.result(true), nil
	}
}

// OnRecordInfo receives one asynchronously delivered device response. Unknown SNs are ignored.
func (s *QueryService) OnRecordInfo(_ context.Context, response *manscdp.RecordInfoResponse) error {
	if s == nil || response == nil {
		return nil
	}
	s.mu.Lock()
	waiter := s.waiters[queryKey(response.DeviceID, response.SN)]
	s.mu.Unlock()
	if waiter == nil {
		return nil
	}
	waiter.add(response)
	return nil
}

func (s *QueryService) removeWaiter(key string) {
	s.mu.Lock()
	delete(s.waiters, key)
	s.mu.Unlock()
}

func (w *queryWaiter) add(response *manscdp.RecordInfoResponse) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	if response.Sum > w.total {
		w.total = response.Sum
	}
	for _, record := range response.Records {
		key := recordKey(record)
		if _, exists := w.seen[key]; exists {
			continue
		}
		w.seen[key] = struct{}{}
		w.records = append(w.records, record)
	}
	if w.total > 0 && len(w.records) >= w.total {
		w.closed = true
		close(w.done)
	}
}

func (w *queryWaiter) result(incomplete bool) QueryResult {
	w.mu.Lock()
	defer w.mu.Unlock()
	records := append([]manscdp.RecordInfoItem(nil), w.records...)
	return QueryResult{Records: records, Total: w.total, Incomplete: incomplete || (w.total > len(records))}
}

func validateQueryRequest(request QueryRequest) error {
	if strings.TrimSpace(request.DeviceID) == "" || strings.TrimSpace(request.ChannelID) == "" {
		return ErrRecordChannelNotFound
	}
	if request.StartTime.IsZero() || request.EndTime.IsZero() || !request.EndTime.After(request.StartTime) || request.EndTime.Sub(request.StartTime) > maxRecordQueryRange {
		return ErrRecordQueryRange
	}
	return nil
}

func queryKey(deviceID string, sn int) string {
	return deviceID + ":" + fmt.Sprintf("%d", sn)
}

func recordKey(record manscdp.RecordInfoItem) string {
	return strings.Join([]string{record.DeviceID, record.FilePath, record.StartTime, record.EndTime}, "\x00")
}

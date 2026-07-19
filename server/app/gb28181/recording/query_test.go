package recording

import (
	"context"
	"sync"
	"testing"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

type querySenderStub struct {
	mu   sync.Mutex
	body []byte
	err  error
}

func (s *querySenderStub) SendMessage(_ context.Context, _, _, _ string, body []byte) error {
	s.mu.Lock()
	s.body = append([]byte(nil), body...)
	s.mu.Unlock()
	return s.err
}

func (s *querySenderStub) sent() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.body) > 0
}

type queryDeviceStub struct{ device *gbmodels.GbDevice }

func (s queryDeviceStub) FindByDeviceID(context.Context, string) (*gbmodels.GbDevice, error) {
	return s.device, nil
}

type queryChannelStub struct{ channel *gbmodels.GbChannel }

func (s queryChannelStub) FindChannel(context.Context, string, string) (*gbmodels.GbChannel, error) {
	return s.channel, nil
}

func queryTime(t *testing.T, text string) time.Time {
	t.Helper()
	value, err := time.ParseInLocation("2006-01-02T15:04:05", text, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestQueryServiceAggregatesRecordInfoSegments(t *testing.T) {
	sender := &querySenderStub{}
	service := NewQueryService(sender,
		queryDeviceStub{device: &gbmodels.GbDevice{DeviceID: "device", Status: gbmodels.DeviceStatusOnline, IP: "10.0.0.8", Port: 5060}},
		queryChannelStub{channel: &gbmodels.GbChannel{DeviceID: "device", ChannelID: "channel"}},
	)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	resultCh := make(chan QueryResult, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := service.Query(ctx, QueryRequest{DeviceID: "device", ChannelID: "channel", StartTime: queryTime(t, "2026-07-19T08:00:00"), EndTime: queryTime(t, "2026-07-19T09:00:00")})
		resultCh <- result
		errCh <- err
	}()

	deadline := time.Now().Add(time.Second)
	for !sender.sent() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	response, err := manscdp.ParseRecordInfoResponse([]byte(`<Response><CmdType>RecordInfo</CmdType><SN>1</SN><DeviceID>device</DeviceID><SumNum>2</SumNum><RecordList Num="1"><Item><DeviceID>channel</DeviceID><FilePath>/1.mp4</FilePath><StartTime>2026-07-19T08:00:00</StartTime><EndTime>2026-07-19T08:10:00</EndTime></Item></RecordList></Response>`))
	if err != nil {
		t.Fatal(err)
	}
	if err := service.OnRecordInfo(context.Background(), response); err != nil {
		t.Fatal(err)
	}
	response.Records[0].FilePath = "/2.mp4"
	response.Num = 1
	if err := service.OnRecordInfo(context.Background(), response); err != nil {
		t.Fatal(err)
	}

	if err := <-errCh; err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	result := <-resultCh
	if result.Total != 2 || result.Incomplete || len(result.Records) != 2 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestQueryServiceReturnsPartialOnContextDeadline(t *testing.T) {
	sender := &querySenderStub{}
	service := NewQueryService(sender,
		queryDeviceStub{device: &gbmodels.GbDevice{DeviceID: "device", Status: gbmodels.DeviceStatusOnline, IP: "10.0.0.8", Port: 5060}},
		queryChannelStub{channel: &gbmodels.GbChannel{DeviceID: "device", ChannelID: "channel"}},
	)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	result, err := service.Query(ctx, QueryRequest{DeviceID: "device", ChannelID: "channel", StartTime: queryTime(t, "2026-07-19T08:00:00"), EndTime: queryTime(t, "2026-07-19T09:00:00")})
	if err != nil {
		t.Fatalf("partial query should not return error: %v", err)
	}
	if !result.Incomplete || result.Total != 0 {
		t.Fatalf("unexpected partial result: %+v", result)
	}
}

func TestQueryServiceRejectsOfflineDevice(t *testing.T) {
	sender := &querySenderStub{}
	service := NewQueryService(sender,
		queryDeviceStub{device: &gbmodels.GbDevice{DeviceID: "device", Status: gbmodels.DeviceStatusOffline}},
		queryChannelStub{channel: &gbmodels.GbChannel{DeviceID: "device", ChannelID: "channel"}},
	)
	_, err := service.Query(context.Background(), QueryRequest{DeviceID: "device", ChannelID: "channel", StartTime: queryTime(t, "2026-07-19T08:00:00"), EndTime: queryTime(t, "2026-07-19T09:00:00")})
	if err == nil {
		t.Fatal("offline device should be rejected")
	}
	if sender.sent() {
		t.Fatal("offline query should not send MESSAGE")
	}
}

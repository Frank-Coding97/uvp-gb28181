package recordquery

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
)

type senderFunc func(context.Context, string, string, string, []byte) (uac.TrackedMessageResult, error)

func (f senderFunc) SendMessageTracked(ctx context.Context, deviceCode, destination, transport string, body []byte) (uac.TrackedMessageResult, error) {
	return f(ctx, deviceCode, destination, transport, body)
}

func queryFixture() QueryRequest {
	location := time.FixedZone("CST", 8*60*60)
	return QueryRequest{
		OwnerUserID: 7,
		ChannelID:   12,
		DeviceCode:  "device-a",
		ChannelCode: "channel-a",
		Destination: "192.0.2.1:5060",
		Transport:   "UDP",
		StartTime:   time.Date(2026, 8, 2, 8, 0, 0, 0, location),
		EndTime:     time.Date(2026, 8, 2, 9, 0, 0, 0, location),
		Type:        manscdp.RecordInfoQueryTypeAll,
	}
}

func record(name, path, start, end string) manscdp.RecordInfoItem {
	return manscdp.RecordInfoItem{
		DeviceID: "channel-a", Name: name, FilePath: path, Address: "addr",
		StartTime: start, EndTime: end, Secrecy: 0, Type: "time",
		RecorderID: "recorder", RecordLocation: "local",
	}
}

func sentQuery(t *testing.T, body []byte) (int, string) {
	t.Helper()
	head, err := manscdp.ParseHead(body)
	require.NoError(t, err)
	sn, err := strconv.Atoi(head.SN)
	require.NoError(t, err)
	return sn, head.DeviceID
}

func newTestService(t *testing.T, sender TrackedSender, mutate func(*Options)) *Service {
	t.Helper()
	options := Options{
		Timeout:            120 * time.Millisecond,
		CompletionQuiet:    15 * time.Millisecond,
		MaxActiveQueries:   4,
		MaxRecordsPerQuery: 20,
		ResultTTL:          time.Minute,
		Location:           time.FixedZone("CST", 8*60*60),
	}
	if mutate != nil {
		mutate(&options)
	}
	service, err := NewService(sender, options)
	require.NoError(t, err)
	t.Cleanup(service.Close)
	return service
}

func TestServiceRegistersBeforeSendAndAggregatesOutOfOrder(t *testing.T) {
	var service *Service
	sender := senderFunc(func(_ context.Context, deviceCode, _, _ string, body []byte) (uac.TrackedMessageResult, error) {
		sn, channelCode := sentQuery(t, body)
		require.True(t, service.Accept(deviceCode, &manscdp.RecordInfoResponse{
			SN: sn, DeviceID: channelCode, SumNum: 3,
			Items: []manscdp.RecordInfoItem{
				record("later", "/b", "2026-08-02T08:20:00", "2026-08-02T08:30:00"),
				record("first", "/a", "2026-08-02T08:00:00", "2026-08-02T08:10:00"),
			},
		}))
		require.True(t, service.Accept(deviceCode, &manscdp.RecordInfoResponse{
			SN: sn, DeviceID: channelCode, SumNum: 3,
			Items: []manscdp.RecordInfoItem{record("middle", "/c", "2026-08-02T08:10:00", "2026-08-02T08:20:00")},
		}))
		return uac.TrackedMessageResult{StatusCode: 202, Attempted: true}, nil
	})
	service = newTestService(t, sender, nil)

	result, err := service.Query(context.Background(), queryFixture())
	require.NoError(t, err)
	require.Equal(t, QueryStatusComplete, result.Status)
	require.Equal(t, 3, result.DeclaredTotal)
	require.Equal(t, []string{"first", "middle", "later"}, []string{result.Records[0].Name, result.Records[1].Name, result.Records[2].Name})
	for _, item := range result.Records {
		require.NotEmpty(t, item.RecordKey)
	}
	require.Equal(t, 0, service.Active())
}

func TestServiceCorrelationDeviceValidationDedupeAndSumDrift(t *testing.T) {
	responses := make(chan int, 1)
	service := newTestService(t, senderFunc(func(_ context.Context, _, _, _ string, body []byte) (uac.TrackedMessageResult, error) {
		sn, _ := sentQuery(t, body)
		responses <- sn
		return uac.TrackedMessageResult{StatusCode: 200}, nil
	}), nil)

	done := make(chan QueryResult, 1)
	go func() {
		result, _ := service.Query(context.Background(), queryFixture())
		done <- result
	}()
	sn := <-responses
	one := record("same", "/a", "2026-08-02T08:00:00", "2026-08-02T08:10:00")
	two := record("same", "/b", "2026-08-02T08:10:00", "2026-08-02T08:20:00")
	three := record("third", "/c", "2026-08-02T08:20:00", "2026-08-02T08:30:00")

	require.False(t, service.Accept("device-b", &manscdp.RecordInfoResponse{SN: sn, DeviceID: "channel-a", SumNum: 2, Items: []manscdp.RecordInfoItem{one}}))
	require.False(t, service.Accept("device-a", &manscdp.RecordInfoResponse{SN: sn + 1, DeviceID: "channel-a", SumNum: 2, Items: []manscdp.RecordInfoItem{one}}))
	require.False(t, service.Accept("device-a", &manscdp.RecordInfoResponse{SN: sn, DeviceID: "wrong-channel", SumNum: 2, Items: []manscdp.RecordInfoItem{one}}))
	require.True(t, service.Accept("device-a", &manscdp.RecordInfoResponse{SN: sn, DeviceID: "channel-a", SumNum: 2, Items: []manscdp.RecordInfoItem{one, two}}))

	select {
	case <-done:
		t.Fatal("query completed before a drifting SumNum could settle")
	case <-time.After(5 * time.Millisecond):
	}
	require.True(t, service.Accept("device-a", &manscdp.RecordInfoResponse{SN: sn, DeviceID: "channel-a", SumNum: 3, Items: []manscdp.RecordInfoItem{one, three}}))
	require.True(t, service.Accept("device-a", &manscdp.RecordInfoResponse{SN: sn, DeviceID: "channel-a", SumNum: 2, Items: []manscdp.RecordInfoItem{three}}))

	result := <-done
	require.Equal(t, QueryStatusComplete, result.Status)
	require.Equal(t, 3, result.DeclaredTotal)
	require.Len(t, result.Records, 3, "full-field duplicate should be removed while distinct paths remain")
}

func TestSortRecordItemsStableForEqualAndUnknownTimes(t *testing.T) {
	location := time.FixedZone("CST", 8*60*60)
	items := []manscdp.RecordInfoItem{
		record("unknown", "/z", "vendor-time", "vendor-end"),
		record("long", "/b", "2026-08-02T08:00:00", "2026-08-02T08:20:00"),
		record("short-b", "/c", "2026-08-02T08:00:00", "2026-08-02T08:10:00"),
		record("short-a", "/a", "2026-08-02T08:00:00", "2026-08-02T08:10:00"),
	}
	sortRecordItems(items, location)
	require.Equal(t, []string{"short-a", "short-b", "long", "unknown"}, []string{items[0].Name, items[1].Name, items[2].Name, items[3].Name})
}

func TestServiceTerminalStatesAndCapacity(t *testing.T) {
	tests := []struct {
		name       string
		respond    func(*Service, string, int)
		mutate     func(*Options)
		wantStatus QueryStatus
		wantErr    ErrorCode
		wantCount  int
	}{
		{name: "empty", respond: func(s *Service, sender string, sn int) {
			s.Accept(sender, &manscdp.RecordInfoResponse{SN: sn, DeviceID: "channel-a", SumNum: 0, Empty: true})
		}, wantStatus: QueryStatusEmpty},
		{name: "partial deadline", respond: func(s *Service, sender string, sn int) {
			s.Accept(sender, &manscdp.RecordInfoResponse{SN: sn, DeviceID: "channel-a", SumNum: 2, Items: []manscdp.RecordInfoItem{record("one", "/a", "2026-08-02T08:00:00", "2026-08-02T08:10:00")}})
		}, wantStatus: QueryStatusPartial, wantCount: 1},
		{name: "timeout", wantStatus: QueryStatusTimeout, wantErr: ErrorCodeTimeout},
		{name: "capacity", respond: func(s *Service, sender string, sn int) {
			s.Accept(sender, &manscdp.RecordInfoResponse{SN: sn, DeviceID: "channel-a", SumNum: 3, Items: []manscdp.RecordInfoItem{
				record("one", "/a", "2026-08-02T08:00:00", "2026-08-02T08:10:00"),
				record("two", "/b", "2026-08-02T08:10:00", "2026-08-02T08:20:00"),
			}})
		}, mutate: func(o *Options) { o.MaxRecordsPerQuery = 1 }, wantStatus: QueryStatusPartial, wantCount: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var service *Service
			service = newTestService(t, senderFunc(func(_ context.Context, sender, _, _ string, body []byte) (uac.TrackedMessageResult, error) {
				sn, _ := sentQuery(t, body)
				if test.respond != nil {
					test.respond(service, sender, sn)
				}
				return uac.TrackedMessageResult{StatusCode: 200}, nil
			}), test.mutate)
			result, err := service.Query(context.Background(), queryFixture())
			require.Equal(t, test.wantStatus, result.Status)
			require.Len(t, result.Records, test.wantCount)
			if test.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, sentinelFor(test.wantErr))
			}
			require.Equal(t, 0, service.Active())
		})
	}
}

func TestServiceSendFailuresBusyCancelAndClose(t *testing.T) {
	t.Run("send errors and non-2xx", func(t *testing.T) {
		for _, test := range []struct {
			name   string
			result uac.TrackedMessageResult
			err    error
		}{
			{name: "transport", err: errors.New("network down")},
			{name: "sip reject", result: uac.TrackedMessageResult{StatusCode: 486}},
		} {
			t.Run(test.name, func(t *testing.T) {
				service := newTestService(t, senderFunc(func(context.Context, string, string, string, []byte) (uac.TrackedMessageResult, error) {
					return test.result, test.err
				}), nil)
				result, err := service.Query(context.Background(), queryFixture())
				require.Equal(t, QueryStatusSendFailed, result.Status)
				require.ErrorIs(t, err, ErrSendFailed)
				require.Equal(t, 0, service.Active())
			})
		}
	})

	started := make(chan struct{}, 1)
	service := newTestService(t, senderFunc(func(context.Context, string, string, string, []byte) (uac.TrackedMessageResult, error) {
		started <- struct{}{}
		return uac.TrackedMessageResult{StatusCode: 200}, nil
	}), func(o *Options) { o.MaxActiveQueries = 1; o.Timeout = time.Second })
	firstDone := make(chan error, 1)
	go func() { _, err := service.Query(context.Background(), queryFixture()); firstDone <- err }()
	<-started

	_, err := service.Query(context.Background(), queryFixture())
	require.ErrorIs(t, err, ErrBusy)

	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = service.Query(cancelCtx, queryFixture())
	require.ErrorIs(t, err, ErrBusy, "capacity is checked before an inactive canceled query can register")

	service.Close()
	require.ErrorIs(t, <-firstDone, ErrUnavailable)
	require.Equal(t, 0, service.Active())
	_, err = service.Query(context.Background(), queryFixture())
	require.ErrorIs(t, err, ErrUnavailable)

	service2 := newTestService(t, senderFunc(func(context.Context, string, string, string, []byte) (uac.TrackedMessageResult, error) {
		return uac.TrackedMessageResult{StatusCode: 200}, nil
	}), nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := service2.Query(ctx, queryFixture())
	require.Equal(t, QueryStatusCanceled, result.Status)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 0, service2.Active())
}

func TestServiceConcurrentQueriesRaceClean(t *testing.T) {
	var service *Service
	service = newTestService(t, senderFunc(func(_ context.Context, sender, _, _ string, body []byte) (uac.TrackedMessageResult, error) {
		sn, channelCode := sentQuery(t, body)
		go service.Accept(sender, &manscdp.RecordInfoResponse{SN: sn, DeviceID: channelCode, SumNum: 0, Empty: true})
		return uac.TrackedMessageResult{StatusCode: 200}, nil
	}), func(o *Options) { o.MaxActiveQueries = 64 })

	var wg sync.WaitGroup
	for range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := service.Query(context.Background(), queryFixture())
			require.NoError(t, err)
			require.Equal(t, QueryStatusEmpty, result.Status)
		}()
	}
	wg.Wait()
	require.Equal(t, 0, service.Active())
}

func TestServiceCloseCancelsSendStage(t *testing.T) {
	started := make(chan struct{})
	service := newTestService(t, senderFunc(func(ctx context.Context, _, _, _ string, _ []byte) (uac.TrackedMessageResult, error) {
		close(started)
		<-ctx.Done()
		return uac.TrackedMessageResult{}, ctx.Err()
	}), nil)
	done := make(chan error, 1)
	go func() {
		_, err := service.Query(context.Background(), queryFixture())
		done <- err
	}()
	<-started
	service.Close()
	require.ErrorIs(t, <-done, ErrUnavailable)
	require.Equal(t, 0, service.Active())
}

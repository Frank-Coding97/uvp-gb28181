package gb28181

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/emiago/sipgo/siptest"
	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/sipclient"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.com/uvp-gb28181/app/gb28181/recordquery"
	"uvplatform.com/uvp-gb28181/app/gb28181/uac"
)

func TestCascadeRecordInfoPlanRewritesPublishedIdentityAndBatches(t *testing.T) {
	query := cascadeRecordQuery{SN: 73, DeviceID: "34020000001320000010"}
	items := []recordquery.Record{
		{RecordInfoItem: manscdp.RecordInfoItem{DeviceID: "source-channel", Name: "first", FilePath: "http://37010301021180000007/record/one.ps", StartTime: "2026-08-02T08:00:00", EndTime: "2026-08-02T08:01:00", Type: "time", RecorderID: "internal-device"}},
		{RecordInfoItem: manscdp.RecordInfoItem{DeviceID: "source-channel", Name: "second", StartTime: "2026-08-02T08:01:00", EndTime: "2026-08-02T08:02:00", Type: "time"}},
	}
	bodies, err := planCascadeRecordResponses(protocol.ProfileFor(protocol.Version2022), query, items, "34020000002000000002", 1, nil)
	require.NoError(t, err)
	require.Len(t, bodies, 2)
	for _, body := range bodies {
		response, err := manscdp.ParseRecordInfoResponse(body)
		require.NoError(t, err)
		require.Equal(t, 73, response.SN)
		require.Equal(t, 2, response.SumNum)
		require.Len(t, response.Items, 1)
		require.Equal(t, query.DeviceID, response.Items[0].DeviceID)
		require.NotContains(t, string(body), "source-channel")
		require.NotContains(t, string(body), "internal-device")
		require.NotContains(t, string(body), "37010301021180000007")
		if response.Items[0].Name == "first" {
			require.Equal(t, "http://"+query.DeviceID+"/record/one.ps", response.Items[0].FilePath)
		}
	}
	bodies, err = planCascadeRecordResponses(protocol.ProfileFor(protocol.Version2016), query, nil, "local", 1, nil)
	require.NoError(t, err)
	require.Len(t, bodies, 1)
	require.Contains(t, string(bodies[0]), "<SumNum>0</SumNum>")
	require.NotContains(t, string(bodies[0]), "RecordList")
}

type cascadeRecordSenderFunc func(context.Context, string, string, string, []byte) (uac.TrackedMessageResult, error)

func (f cascadeRecordSenderFunc) SendMessageTracked(ctx context.Context, device, destination, transport string, body []byte) (uac.TrackedMessageResult, error) {
	return f(ctx, device, destination, transport, body)
}

type cascadeRecordTransport struct {
	requests  []*sip.Request
	onRequest func(int)
}

func (f *cascadeRecordTransport) Do(_ context.Context, req *sip.Request) (*sip.Response, error) {
	f.requests = append(f.requests, req.Clone())
	if f.onRequest != nil {
		f.onRequest(len(f.requests))
	}
	return sip.NewResponseFromRequest(req, 200, "OK", nil), nil
}
func (f *cascadeRecordTransport) DoDigestAuth(ctx context.Context, req *sip.Request, _ *sip.Response, _ sipclient.Credentials) (*sip.Response, error) {
	return f.Do(ctx, req)
}

func TestCascadeRecordQueryForwardsDeviceResultWithOriginalSN(t *testing.T) {
	testCascadeRecordQueryBatches(t, "")
}

func TestCascadeRecordQueryStopsLaterBatchesWhenAuthorizationChanges(t *testing.T) {
	for _, cause := range []string{"recording-permission", "sharing", "source-epoch"} {
		t.Run(cause, func(t *testing.T) { testCascadeRecordQueryBatches(t, cause) })
	}
}

func testCascadeRecordQueryBatches(t *testing.T, revoke string) {
	t.Helper()
	h, db, _ := newCascadeVideoTestRuntime(t)
	require.NoError(t, db.Model(&model.GbCascadePlatform{}).Where("id = 1").Updates(map[string]interface{}{
		"recording_enabled": true, "catalog_batch_size": 1, "upstream_domain": "3402000000",
	}).Error)
	require.NoError(t, db.Table("gb_device").Where("id = 1").Updates(map[string]interface{}{"ip": "192.0.2.10", "port": 5060, "transport": "UDP"}).Error)
	previous := recordQueryService
	t.Cleanup(func() { recordQueryService = previous })
	var service *recordquery.Service
	sender := cascadeRecordSenderFunc(func(_ context.Context, device, destination, transport string, body []byte) (uac.TrackedMessageResult, error) {
		require.Equal(t, "192.0.2.10:5060", destination)
		require.Equal(t, "UDP", transport)
		head, err := manscdp.ParseHead(body)
		require.NoError(t, err)
		sn, err := strconv.Atoi(head.SN)
		require.NoError(t, err)
		require.NotEqual(t, 73, sn)
		service.Accept(device, &manscdp.RecordInfoResponse{SN: sn, DeviceID: head.DeviceID, SumNum: 2,
			Items: []manscdp.RecordInfoItem{
				{DeviceID: head.DeviceID, Name: "first", StartTime: "2026-08-02T08:00:00", EndTime: "2026-08-02T08:01:00", Type: "time"},
				{DeviceID: head.DeviceID, Name: "second", StartTime: "2026-08-02T08:01:00", EndTime: "2026-08-02T08:02:00", Type: "time"},
			}})
		return uac.TrackedMessageResult{StatusCode: 200}, nil
	})
	var err error
	service, err = recordquery.NewService(sender, recordquery.Options{Timeout: time.Second, CompletionQuiet: time.Millisecond, MaxActiveQueries: 4, MaxRecordsPerQuery: 10, ResultTTL: time.Minute, Location: time.FixedZone("CST", 8*60*60)})
	require.NoError(t, err)
	t.Cleanup(service.Close)
	recordQueryService = service
	transport := &cascadeRecordTransport{onRequest: func(index int) {
		if index != 1 {
			return
		}
		switch revoke {
		case "recording-permission":
			require.NoError(t, db.Model(&model.GbCascadePlatform{}).Where("id = 1").Update("recording_enabled", false).Error)
		case "sharing":
			require.NoError(t, db.Model(&model.GbCascadeChannelProjection{}).Where("platform_id = 1").Update("active", false).Error)
		case "source-epoch":
			require.NoError(t, db.Table("gb_device").Where("id = 1").Update("access_epoch", 2).Error)
		}
	}}
	callback := newCascadeMessageHandler(h.store, newCascadePlatformClientFactory(transport, nil, time.Second))
	req := cascadeTestRequest(15060, "0200000001")
	req.Method = sip.MESSAGE
	req.CSeq().MethodName = sip.MESSAGE
	req.SetBody([]byte("<Query><CmdType>RecordInfo</CmdType><SN>73</SN><DeviceID>34020000001320000010</DeviceID>" +
		"<StartTime>2026-08-02T08:00:00</StartTime><EndTime>2026-08-02T09:00:00</EndTime><Type>all</Type><Secrecy>0</Secrecy></Query>"))
	tx := siptest.NewServerTxRecorder(req)
	defer tx.Terminate()
	require.True(t, callback(req, tx))
	require.Equal(t, 200, tx.Result()[0].StatusCode)
	wantBatches := 2
	if revoke != "" {
		wantBatches = 1
	}
	require.Len(t, transport.requests, wantBatches)
	for _, sent := range transport.requests {
		response, err := manscdp.ParseRecordInfoResponse(sent.Body())
		require.NoError(t, err)
		require.Equal(t, 73, response.SN)
		require.Equal(t, 2, response.SumNum)
		require.Len(t, response.Items, 1)
	}
}

// 设备声明的 SumNum 与实际推来的条目数不一致时（实测大华 199 vs 386），
// 查询会落到 Partial/Incomplete。此时不能把已经拿到的条目整批丢掉：
// 上级在录像多的日子会看到「没有录像」。仍然要按实际条目回响应。
func TestCascadeRecordQueryReturnsPartialResultWhenDeviceUnderDelivers(t *testing.T) {
	h, db, _ := newCascadeVideoTestRuntime(t)
	require.NoError(t, db.Model(&model.GbCascadePlatform{}).Where("id = 1").Updates(map[string]interface{}{
		"recording_enabled": true, "catalog_batch_size": 1, "upstream_domain": "3402000000",
	}).Error)
	require.NoError(t, db.Table("gb_device").Where("id = 1").Updates(map[string]interface{}{"ip": "192.0.2.10", "port": 5060, "transport": "UDP"}).Error)
	previous := recordQueryService
	t.Cleanup(func() { recordQueryService = previous })
	var service *recordquery.Service
	sender := cascadeRecordSenderFunc(func(_ context.Context, device, _, _ string, body []byte) (uac.TrackedMessageResult, error) {
		head, err := manscdp.ParseHead(body)
		if err != nil {
			return uac.TrackedMessageResult{}, err
		}
		sn, err := strconv.Atoi(head.SN)
		if err != nil {
			return uac.TrackedMessageResult{}, err
		}
		// 声明 3 条，只推 2 条：永远达不到完成判据。
		service.Accept(device, &manscdp.RecordInfoResponse{SN: sn, DeviceID: head.DeviceID, SumNum: 3,
			Items: []manscdp.RecordInfoItem{
				{DeviceID: head.DeviceID, Name: "first", StartTime: "2026-08-02T08:00:00", EndTime: "2026-08-02T08:01:00", Type: "time"},
				{DeviceID: head.DeviceID, Name: "second", StartTime: "2026-08-02T08:01:00", EndTime: "2026-08-02T08:02:00", Type: "time"},
			}})
		return uac.TrackedMessageResult{StatusCode: 200}, nil
	})
	var err error
	service, err = recordquery.NewService(sender, recordquery.Options{Timeout: 200 * time.Millisecond, CompletionQuiet: time.Millisecond, MaxActiveQueries: 4, MaxRecordsPerQuery: 10, ResultTTL: time.Minute, Location: time.FixedZone("CST", 8*60*60)})
	require.NoError(t, err)
	t.Cleanup(service.Close)
	recordQueryService = service
	transport := &cascadeRecordTransport{}
	callback := newCascadeMessageHandler(h.store, newCascadePlatformClientFactory(transport, nil, time.Second))
	req := cascadeTestRequest(15060, "0200000001")
	req.Method = sip.MESSAGE
	req.CSeq().MethodName = sip.MESSAGE
	req.SetBody([]byte("<Query><CmdType>RecordInfo</CmdType><SN>73</SN><DeviceID>34020000001320000010</DeviceID>" +
		"<StartTime>2026-08-02T08:00:00</StartTime><EndTime>2026-08-02T09:00:00</EndTime><Type>all</Type><Secrecy>0</Secrecy></Query>"))
	tx := siptest.NewServerTxRecorder(req)
	defer tx.Terminate()
	require.True(t, callback(req, tx))
	require.Equal(t, 200, tx.Result()[0].StatusCode)
	require.Len(t, transport.requests, 2, "partial result must still be forwarded as one response per record")
	for _, sent := range transport.requests {
		response, err := manscdp.ParseRecordInfoResponse(sent.Body())
		require.NoError(t, err)
		require.Equal(t, 73, response.SN)
		require.Equal(t, 2, response.SumNum, "SumNum must describe what was actually returned")
		require.Len(t, response.Items, 1)
	}
}

// 上级等应答有耐心上限（LiveGBS 15 秒即超时）。即使设备一直凑不齐 SumNum，
// 我方也必须在预算内把已收到的条目回出去，不能拖到对端超时。
func TestCascadeRecordQueryAnswersWithinBudgetWhenDeviceNeverCompletes(t *testing.T) {
	h, db, _ := newCascadeVideoTestRuntime(t)
	require.NoError(t, db.Model(&model.GbCascadePlatform{}).Where("id = 1").Updates(map[string]interface{}{
		"recording_enabled": true, "catalog_batch_size": 3, "upstream_domain": "3402000000",
	}).Error)
	require.NoError(t, db.Table("gb_device").Where("id = 1").Updates(map[string]interface{}{"ip": "192.0.2.10", "port": 5060, "transport": "UDP"}).Error)
	previousBudget := cascadeRecordQueryBudget
	cascadeRecordQueryBudget = 100 * time.Millisecond
	t.Cleanup(func() { cascadeRecordQueryBudget = previousBudget })
	previous := recordQueryService
	t.Cleanup(func() { recordQueryService = previous })
	var service *recordquery.Service
	sender := cascadeRecordSenderFunc(func(_ context.Context, device, _, _ string, body []byte) (uac.TrackedMessageResult, error) {
		head, err := manscdp.ParseHead(body)
		if err != nil {
			return uac.TrackedMessageResult{}, err
		}
		sn, err := strconv.Atoi(head.SN)
		if err != nil {
			return uac.TrackedMessageResult{}, err
		}
		service.Accept(device, &manscdp.RecordInfoResponse{SN: sn, DeviceID: head.DeviceID, SumNum: 5,
			Items: []manscdp.RecordInfoItem{
				{DeviceID: head.DeviceID, Name: "a", StartTime: "2026-08-02T08:00:00", EndTime: "2026-08-02T08:01:00", Type: "time"},
				{DeviceID: head.DeviceID, Name: "b", StartTime: "2026-08-02T08:01:00", EndTime: "2026-08-02T08:02:00", Type: "time"},
				{DeviceID: head.DeviceID, Name: "c", StartTime: "2026-08-02T08:02:00", EndTime: "2026-08-02T08:03:00", Type: "time"},
			}})
		return uac.TrackedMessageResult{StatusCode: 200}, nil
	})
	var err error
	// 聚合超时故意设得很长：能按时回来说明是被预算兜住的，不是被超时兜住的。
	service, err = recordquery.NewService(sender, recordquery.Options{Timeout: 30 * time.Second, CompletionQuiet: time.Millisecond, MaxActiveQueries: 4, MaxRecordsPerQuery: 10, ResultTTL: time.Minute, Location: time.FixedZone("CST", 8*60*60)})
	require.NoError(t, err)
	t.Cleanup(service.Close)
	recordQueryService = service
	transport := &cascadeRecordTransport{}
	callback := newCascadeMessageHandler(h.store, newCascadePlatformClientFactory(transport, nil, time.Second))
	req := cascadeTestRequest(15060, "0200000001")
	req.Method = sip.MESSAGE
	req.CSeq().MethodName = sip.MESSAGE
	req.SetBody([]byte("<Query><CmdType>RecordInfo</CmdType><SN>73</SN><DeviceID>34020000001320000010</DeviceID>" +
		"<StartTime>2026-08-02T08:00:00</StartTime><EndTime>2026-08-02T09:00:00</EndTime><Type>all</Type><Secrecy>0</Secrecy></Query>"))
	tx := siptest.NewServerTxRecorder(req)
	defer tx.Terminate()
	done := make(chan struct{})
	go func() {
		callback(req, tx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("handler exceeded the cascade record query budget")
	}
	require.NotEmpty(t, transport.requests)
	total := 0
	for _, sent := range transport.requests {
		response, err := manscdp.ParseRecordInfoResponse(sent.Body())
		require.NoError(t, err)
		require.Equal(t, 3, response.SumNum, "SumNum 应等于实际返回的总条数")
		total += len(response.Items)
	}
	require.Equal(t, 3, total)
}

func TestCascadeRecordQueryRejectsDisabledPermissionAndUnsharedChannel(t *testing.T) {
	h, db, _ := newCascadeVideoTestRuntime(t)
	request := cascadeTestRequest(15060, "0200000001")
	request.Method = sip.MESSAGE
	request.CSeq().MethodName = sip.MESSAGE
	request.SetBody([]byte("<Query><CmdType>RecordInfo</CmdType><SN>73</SN><DeviceID>34020000001320000010</DeviceID>" +
		"<StartTime>2026-08-02T08:00:00</StartTime><EndTime>2026-08-02T09:00:00</EndTime><Type>all</Type><Secrecy>0</Secrecy></Query>"))
	callback := newCascadeRecordInfoHandler(h.store, nil)
	tx := siptest.NewServerTxRecorder(request)
	defer tx.Terminate()
	require.True(t, callback(request, tx))
	require.Equal(t, 403, tx.Result()[0].StatusCode)
	require.NoError(t, db.Model(&model.GbCascadePlatform{}).Where("id = 1").Update("recording_enabled", true).Error)
	request.SetBody([]byte(strings.Replace(string(request.Body()), "34020000001320000010", "34020000001320009999", 1)))
	tx2 := siptest.NewServerTxRecorder(request)
	defer tx2.Terminate()
	require.True(t, callback(request, tx2))
	require.Equal(t, 404, tx2.Result()[0].StatusCode)
}

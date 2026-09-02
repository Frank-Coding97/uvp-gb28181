package uac

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/emiago/sipgo/sip"
	"uvplatform.cn/uvp-gb28181/app/gb28181/metrics"
)

const (
	testPlatformID = "34020000002000000001"
	testDeviceID   = "34020000001320000001"
)

type trackedMessageRecorder struct {
	endStatus  int
	endSuccess bool
}

func (*trackedMessageRecorder) Begin(metrics.Transaction) {}

func (r *trackedMessageRecorder) End(_, _ string, statusCode int, success bool) {
	r.endStatus = statusCode
	r.endSuccess = success
}

func trackedMessageTestUAC(doMessage func(context.Context, *sip.Request) (*sip.Response, error)) *UAC {
	return &UAC{
		serverID:    testPlatformID,
		domain:      "3402000000",
		sipPort:     5061,
		advertiseIP: "192.0.2.1",
		doMessage:   doMessage,
	}
}

func TestPlatformContactUsesAdvertiseIP(t *testing.T) {
	contact := platformContact("34020000002000000001", "192.168.10.20", 5061)
	want := sip.Uri{User: "34020000002000000001", Host: "192.168.10.20", Port: 5061}
	if contact.Address.String() != want.String() {
		t.Fatalf("Contact=%q, want %q", contact.Address.String(), want.String())
	}
}

// T1.6-U1~U3: detectMessageKind 三类常见 MANSCDP body 识别
func TestDetectMessageKind(t *testing.T) {
	cases := []struct {
		name string
		body []byte
		want metrics.TxKind
	}{
		{
			name: "Catalog Query",
			body: []byte(`<?xml version="1.0"?><Query><CmdType>Catalog</CmdType><SN>1</SN><DeviceID>340200</DeviceID></Query>`),
			want: metrics.TxCatalog,
		},
		{
			name: "RecordInfo Query",
			body: []byte(`<?xml version="1.0"?><Query><CmdType>RecordInfo</CmdType><SN>1</SN></Query>`),
			want: metrics.TxRecord,
		},
		{
			name: "DeviceControl PTZ",
			body: []byte(`<?xml version="1.0"?><Control><CmdType>DeviceControl</CmdType><SN>1</SN><PTZCmd>A50F4D08FF</PTZCmd></Control>`),
			want: metrics.TxPTZ,
		},
		{
			name: "DeviceControl no PTZCmd field",
			body: []byte(`<?xml version="1.0"?><Control><CmdType>DeviceControl</CmdType></Control>`),
			want: metrics.TxPTZ,
		},
		{
			name: "PTZPosition query",
			body: []byte("<Query><CmdType>PTZPosition</CmdType><SN>1</SN><DeviceID>C</DeviceID></Query>"),
			want: metrics.TxPTZ,
		},
		{
			name: "Unknown body",
			body: []byte(`<random/>`),
			want: metrics.TxUnknown,
		},
		{
			name: "Empty body",
			body: []byte{},
			want: metrics.TxUnknown,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := detectMessageKind(c.body); got != c.want {
				t.Errorf("detectMessageKind(%s)=%v, want %v", c.name, got, c.want)
			}
		})
	}
}

// 验证 nextCSeq 单调递增
func TestUAC_NextCSeq(t *testing.T) {
	u := &UAC{}
	a := u.nextCSeq()
	b := u.nextCSeq()
	c := u.nextCSeq()
	if a == b || b == c {
		t.Errorf("nextCSeq not monotonic: %s %s %s", a, b, c)
	}
}

// platformFromHeader:GB28181 § 9.1.1 要求 From userpart = 平台 serverID
// 之前不显式设置 From 时,sipgo 用 UserAgent 名字兜底,合规设备/模拟器会丢弃这类请求
func TestPlatformFromHeader(t *testing.T) {
	u := &UAC{serverID: "34020000002000000001", domain: "3402000000", sipPort: 5061, advertiseIP: "192.0.2.1"}
	h := u.platformFromHeader()
	if h == nil {
		t.Fatal("platformFromHeader returned nil")
	}
	if h.Address.User != "34020000002000000001" {
		t.Errorf("From User = %q, want %q", h.Address.User, "34020000002000000001")
	}
	if h.Address.Host != "3402000000" {
		t.Errorf("From Host = %q, want %q", h.Address.Host, "3402000000")
	}
	// 必须带 tag,否则 SIP dialog 层校验会失败
	if _, ok := h.Params.Get("tag"); !ok {
		t.Error("From header missing tag param")
	}
}

// normalizeTransport: 大小写混用 / 空值 / 非法值都应兜底到合法 SIP transport(UDP/TCP)
func TestNormalizeTransport(t *testing.T) {
	cases := map[string]string{
		"":        "UDP",
		"UDP":     "UDP",
		"udp":     "UDP",
		"Udp":     "UDP",
		"  UDP ":  "UDP",
		"TCP":     "TCP",
		"tcp":     "TCP",
		"Tcp":     "TCP",
		"  tcp  ": "TCP",
		"WS":      "UDP", // 未支持的传输一律兜底
		"sctp":    "UDP",
		"garbage": "UDP",
	}
	for in, want := range cases {
		if got := normalizeTransport(in); got != want {
			t.Errorf("normalizeTransport(%q)=%q, want %q", in, got, want)
		}
	}
}

func TestBuildInviteRequestTargetsChannel(t *testing.T) {
	u := &UAC{
		serverID:         testPlatformID,
		domain:           "3402000000",
		sipPort:          5061,
		dynamicAdvertise: true,
		resolveLocalIP: func(destination string) (string, error) {
			if destination != "192.168.10.108:5060" {
				t.Fatalf("unexpected destination %q", destination)
			}
			return "192.168.126.126", nil
		},
	}
	s := &Session{
		DeviceID:  "34020000001320000001",
		ChannelID: "34020000001320000020",
		SSRC:      "0200000001",
		Dest:      "192.168.10.108:5060",
		Transport: "udp",
	}

	req, err := u.buildInviteRequest(s, "v=0\r\n")
	if err != nil {
		t.Fatal(err)
	}
	if req.Recipient.User != s.ChannelID {
		t.Fatalf("INVITE Request-URI user = %q, want channel %q", req.Recipient.User, s.ChannelID)
	}
	if req.Recipient.User == s.DeviceID {
		t.Fatalf("INVITE Request-URI must not target device root %q", s.DeviceID)
	}
	if req.Destination() != s.Dest {
		t.Errorf("INVITE destination = %q, want %q", req.Destination(), s.Dest)
	}
	if req.Transport() != "UDP" {
		t.Errorf("INVITE transport = %q, want UDP", req.Transport())
	}
	if req.Via() == nil || req.Via().Host != "192.168.126.126" || req.Via().Port != 5061 {
		t.Fatalf("INVITE Via=%v, want 192.168.126.126:5061", req.Via())
	}
	if req.Contact() == nil || req.Contact().Address.Host != "192.168.126.126" {
		t.Fatalf("INVITE Contact=%v, want dynamic local IP", req.Contact())
	}
}

func TestDynamicAdvertiseUsesRoutePerDestination(t *testing.T) {
	routes := map[string]string{
		"192.168.126.10:5060": "192.168.126.126",
		"10.8.0.8:5060":       "10.8.0.3",
	}
	u := &UAC{
		serverID:         testPlatformID,
		domain:           "3402000000",
		sipPort:          5061,
		dynamicAdvertise: true,
		resolveLocalIP: func(destination string) (string, error) {
			return routes[destination], nil
		},
	}

	for destination, want := range routes {
		req, _, err := u.buildTrackedMessageRequest(TrackedMessageRequest{
			DeviceID:    testDeviceID,
			Destination: destination,
			Transport:   "udp",
			Body:        []byte("<Query/>"),
		})
		if err != nil {
			t.Fatal(err)
		}
		if req.Via() == nil || req.Via().Host != want {
			t.Fatalf("destination %s Via=%v, want %s", destination, req.Via(), want)
		}
	}
}

func TestResolveRouteLocalIPReturnsLoopbackForLoopbackDestination(t *testing.T) {
	got, err := resolveRouteLocalIP("127.0.0.1:51406")
	if err != nil {
		t.Fatalf("resolve loopback route: %v", err)
	}
	if got != "127.0.0.1" {
		t.Fatalf("local IP=%q, want 127.0.0.1", got)
	}
}

func TestDynamicAdvertiseAllowsLoopbackOnlyForLoopbackDestination(t *testing.T) {
	u := &UAC{
		serverID:         testPlatformID,
		domain:           "3402000000",
		sipPort:          5061,
		dynamicAdvertise: true,
		resolveLocalIP: func(string) (string, error) {
			return "127.0.0.1", nil
		},
	}

	req, _, err := u.buildTrackedMessageRequest(TrackedMessageRequest{
		DeviceID:    testDeviceID,
		Destination: "127.0.0.1:51406",
		Transport:   "udp",
		Body:        []byte("<Query/>"),
	})
	if err != nil {
		t.Fatalf("build loopback MESSAGE: %v", err)
	}
	if req.Via() == nil || req.Via().Host != "127.0.0.1" {
		t.Fatalf("Via=%v, want loopback host", req.Via())
	}

	if _, err := u.outboundIP("192.0.2.10:5060"); err == nil {
		t.Fatal("non-loopback destination must reject a loopback route")
	}
}

func TestDynamicAdvertiseDoesNotFallbackToStaleAddress(t *testing.T) {
	u := &UAC{
		serverID:         testPlatformID,
		domain:           "3402000000",
		sipPort:          5061,
		advertiseIP:      "192.168.10.106",
		dynamicAdvertise: true,
		resolveLocalIP: func(string) (string, error) {
			return "", errors.New("no route")
		},
	}

	_, _, err := u.buildTrackedMessageRequest(TrackedMessageRequest{
		DeviceID:    testDeviceID,
		Destination: "192.0.2.10:5060",
		Transport:   "udp",
		Body:        []byte("<Query/>"),
	})
	if err == nil || !strings.Contains(err.Error(), "no route") {
		t.Fatalf("error=%v, want route error without stale fallback", err)
	}
}

func TestBuildSubscribeRequest_ReusesDialogMetadata(t *testing.T) {
	u := &UAC{serverID: "34020000002000000001", domain: "3402000000", sipPort: 5061, advertiseIP: "192.0.2.1"}
	req, err := u.buildSubscribeRequest(SubscriptionRequest{
		DeviceID:    "34020000001320000001",
		Destination: "192.168.10.108:5060",
		Transport:   "tcp",
		Event:       "presence",
		Body:        []byte("<Query/>"),
		Expires:     0,
		CallID:      "subscription-call-id",
		LocalTag:    "local-tag",
		RemoteTag:   "remote-tag",
		CSeq:        8,
	})
	if err != nil {
		t.Fatal(err)
	}
	if req.Method != sip.SUBSCRIBE || req.Recipient.User != "34020000001320000001" {
		t.Fatalf("unexpected SUBSCRIBE target: %s %s", req.Method, req.Recipient.User)
	}
	if req.Destination() != "192.168.10.108:5060" || req.Transport() != "TCP" {
		t.Fatalf("unexpected transport target: %s %s", req.Destination(), req.Transport())
	}
	if req.GetHeaders("Event")[0].Value() != "presence" || req.GetHeaders("Expires")[0].Value() != "0" {
		t.Fatal("missing Event or cancellation Expires headers")
	}
	if req.CallID() == nil || string(*req.CallID()) != "subscription-call-id" {
		t.Fatal("Call-ID must be reused for renewal/cancel")
	}
	if tag, _ := req.From().Params.Get("tag"); tag != "local-tag" {
		t.Fatalf("From tag=%q", tag)
	}
	if tag, _ := req.To().Params.Get("tag"); tag != "remote-tag" {
		t.Fatalf("To tag=%q", tag)
	}
	if req.CSeq() == nil || req.CSeq().SeqNo != 8 || req.CSeq().MethodName != sip.SUBSCRIBE {
		t.Fatal("CSeq must be preserved as SUBSCRIBE dialog metadata")
	}
}

func TestBuildTrackedMessageRequest(t *testing.T) {
	u := &UAC{serverID: "34020000002000000001", domain: "3402000000", sipPort: 5061, advertiseIP: "192.0.2.1"}
	req, meta, err := u.buildTrackedMessageRequest(TrackedMessageRequest{
		DeviceID: "34020000001320000001", Destination: "192.0.2.10:5060", Transport: "tcp", Body: []byte("<Control/>"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if req.Method != sip.MESSAGE || req.Recipient.User != "34020000001320000001" || req.Destination() != "192.0.2.10:5060" || req.Transport() != "TCP" {
		t.Fatalf("unexpected request: method=%s target=%s dest=%s transport=%s", req.Method, req.Recipient.User, req.Destination(), req.Transport())
	}
	if meta.CallID == "" || meta.CSeq == "" || req.CallID() == nil || req.CSeq() == nil {
		t.Fatalf("tracked request missing key: meta=%+v", meta)
	}
	if string(*req.CallID()) != meta.CallID || req.CSeq().MethodName != sip.MESSAGE {
		t.Fatalf("request metadata mismatch: meta=%+v", meta)
	}
}

func TestSendMessageTrackedAcceptsOnlyFinal2xx(t *testing.T) {
	cases := []struct {
		status  int
		success bool
	}{
		{status: 199},
		{status: 200, success: true},
		{status: 202, success: true},
		{status: 299, success: true},
		{status: 300},
		{status: 400},
		{status: 403},
		{status: 500},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("status_%d", tc.status), func(t *testing.T) {
			responseBody := []byte(fmt.Sprintf("<Response><Status>%d</Status></Response>", tc.status))
			var sentRequest *sip.Request
			recorder := &trackedMessageRecorder{}
			u := trackedMessageTestUAC(func(_ context.Context, req *sip.Request) (*sip.Response, error) {
				sentRequest = req
				return sip.NewResponseFromRequest(req, tc.status, "test response", responseBody), nil
			})
			u.SetRecorder(recorder)

			result, err := u.SendMessageTracked(context.Background(), testDeviceID, "192.0.2.10:5060", "udp", []byte("<Query/>"))

			if tc.success && err != nil {
				t.Fatalf("status %d should succeed: %v", tc.status, err)
			}
			if !tc.success && err == nil {
				t.Fatalf("status %d should fail", tc.status)
			}
			if !result.Attempted {
				t.Fatal("transport call should be marked attempted")
			}
			if result.CallID == "" || result.CSeq == "" || result.StatusCode != tc.status {
				t.Fatalf("correlation metadata not preserved: %+v", result)
			}
			if sentRequest == nil || sentRequest.CallID() == nil || string(*sentRequest.CallID()) != result.CallID || sentRequest.CSeq() == nil || fmt.Sprint(sentRequest.CSeq().SeqNo) != result.CSeq {
				t.Fatalf("request/result correlation mismatch: request=%v result=%+v", sentRequest, result)
			}
			if string(result.ResponseBody) != string(responseBody) {
				t.Fatalf("response summary=%q, want %q", result.ResponseBody, responseBody)
			}
			if tc.success && result.ErrorSummary != "" {
				t.Fatalf("successful result has error summary %q", result.ErrorSummary)
			}
			if !tc.success && result.ErrorSummary == "" {
				t.Fatal("failed result should retain an error summary")
			}
			if recorder.endStatus != tc.status || recorder.endSuccess != tc.success {
				t.Fatalf("recorder end=(%d,%v), want (%d,%v)", recorder.endStatus, recorder.endSuccess, tc.status, tc.success)
			}
		})
	}
}

func TestSendMessageTrackedTransportErrorRetainsCorrelation(t *testing.T) {
	transportErr := errors.New("UDP transaction timed out")
	u := trackedMessageTestUAC(func(context.Context, *sip.Request) (*sip.Response, error) {
		return nil, transportErr
	})

	result, err := u.SendMessageTracked(context.Background(), testDeviceID, "192.0.2.10:5060", "udp", []byte("<Query/>"))

	if !errors.Is(err, transportErr) {
		t.Fatalf("error=%v, want transport error", err)
	}
	if !result.Attempted || result.CallID == "" || result.CSeq == "" {
		t.Fatalf("transport uncertainty lost attempt correlation: %+v", result)
	}
	if result.StatusCode != 0 || len(result.ResponseBody) != 0 || !strings.Contains(result.ErrorSummary, transportErr.Error()) {
		t.Fatalf("unexpected transport result: %+v", result)
	}
}

func TestSendMessageTrackedConstructionFailureIsNotAttempted(t *testing.T) {
	called := false
	u := trackedMessageTestUAC(func(context.Context, *sip.Request) (*sip.Response, error) {
		called = true
		return nil, nil
	})

	result, err := u.SendMessageTracked(context.Background(), testDeviceID, "", "udp", []byte("<Query/>"))

	if err == nil {
		t.Fatal("missing destination should fail before transport")
	}
	if called || result.Attempted || result.CallID != "" || result.CSeq != "" || result.StatusCode != 0 {
		t.Fatalf("construction failure must be explicitly unsent: called=%v result=%+v", called, result)
	}
	if result.ErrorSummary == "" {
		t.Fatal("construction failure should retain an error summary")
	}
}

func TestSendMessageTrackedResponseSummaryIsRedactedAndBounded(t *testing.T) {
	secret := "do-not-persist-this"
	body := []byte("<Response><Password>" + secret + "</Password><Data>" + strings.Repeat("x", 8192) + "</Data></Response>")
	u := trackedMessageTestUAC(func(_ context.Context, req *sip.Request) (*sip.Response, error) {
		return sip.NewResponseFromRequest(req, 403, "Forbidden", body), nil
	})

	result, err := u.SendMessageTracked(context.Background(), testDeviceID, "192.0.2.10:5060", "udp", []byte("<Query/>"))

	if err == nil {
		t.Fatal("403 should fail")
	}
	if len(result.ResponseBody) > 4*1024 {
		t.Fatalf("response summary length=%d, want <=4096", len(result.ResponseBody))
	}
	if strings.Contains(string(result.ResponseBody), secret) || !strings.Contains(string(result.ResponseBody), "[REDACTED]") {
		t.Fatalf("response summary was not redacted: %q", result.ResponseBody)
	}
}

func TestSendMessageCompatibilityInherits2xxContract(t *testing.T) {
	status := 202
	u := trackedMessageTestUAC(func(_ context.Context, req *sip.Request) (*sip.Response, error) {
		return sip.NewResponseFromRequest(req, status, "test response", nil), nil
	})

	if err := u.SendMessage(context.Background(), testDeviceID, "192.0.2.10:5060", "tcp", []byte("<Query/>")); err != nil {
		t.Fatalf("legacy SendMessage should accept 202: %v", err)
	}
	status = 300
	if err := u.SendMessage(context.Background(), testDeviceID, "192.0.2.10:5060", "tcp", []byte("<Query/>")); err == nil {
		t.Fatal("legacy SendMessage should keep rejecting non-2xx")
	}
}

package uac

import (
	"testing"

	"github.com/emiago/sipgo/sip"
	"uvplatform.cn/uvp-gb28181/app/gb28181/metrics"
)

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
	u := &UAC{serverID: "34020000002000000001", domain: "3402000000"}
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
	u := &UAC{serverID: "34020000002000000001", domain: "3402000000"}
	s := &Session{
		DeviceID:  "34020000001320000001",
		ChannelID: "34020000001320000020",
		SSRC:      "0200000001",
		Dest:      "192.168.10.108:5060",
		Transport: "udp",
	}

	req := u.buildInviteRequest(s, "v=0\r\n")
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
}

func TestBuildSubscribeRequest_ReusesDialogMetadata(t *testing.T) {
	u := &UAC{serverID: "34020000002000000001", domain: "3402000000"}
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

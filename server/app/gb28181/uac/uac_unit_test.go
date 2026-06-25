package uac

import (
	"testing"

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

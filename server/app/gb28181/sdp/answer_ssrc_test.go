package sdp

import "testing"

func TestParseAnswerSSRC(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "WVP 改用自定义 SSRC 时回告 y=",
			body: "v=0\r\no=34020000012000000001 0 0 IN IP4 192.168.10.220\r\ns=Play\r\nc=IN IP4 192.168.10.220\r\nt=0 0\r\nm=video 52056 RTP/AVP 96\r\na=recvonly\r\ny=0200000007\r\n",
			want: "0200000007",
		},
		{
			name: "没有 y= 时返回空",
			body: "v=0\r\ns=Play\r\nm=video 52056 RTP/AVP 96\r\n",
			want: "",
		},
		{
			name: "空 y= 视为未声明",
			body: "v=0\r\ny=\r\n",
			want: "",
		},
		{
			name: "空报文",
			body: "",
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseAnswerSSRC([]byte(tc.body)); got != tc.want {
				t.Fatalf("ParseAnswerSSRC()=%q, want %q", got, tc.want)
			}
		})
	}
}

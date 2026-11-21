package sdp

import "testing"

func TestNormalizeAudioCodec(t *testing.T) {
	for _, tt := range []struct {
		raw   string
		want  AudioCodec
		valid bool
	}{
		{"", AudioCodecPCMA, true},
		{"pcma", AudioCodecPCMA, true},
		{" PCMA ", AudioCodecPCMA, true},
		{"g711a", AudioCodecPCMA, true},
		{"pcmu", AudioCodecPCMU, true},
		{"G711U", AudioCodecPCMU, true},
		{"aac", "", false},
	} {
		got, ok := NormalizeAudioCodec(tt.raw)
		if got != tt.want || ok != tt.valid {
			t.Fatalf("NormalizeAudioCodec(%q) = (%q,%v), want (%q,%v)", tt.raw, got, ok, tt.want, tt.valid)
		}
	}
}

func TestAudioCodecWireFormat(t *testing.T) {
	// 国标静态载荷类型：PCMA=8 / PCMU=0；f 行第二段 1=G.711A、2=G.711U。
	pcma := AudioCodecPCMA
	if pcma.PayloadType() != 8 || pcma.RTPMap() != "PCMA/8000" || pcma.FLine() != "v/////a/1/8/1" {
		t.Fatalf("PCMA wire format drifted: pt=%d rtpmap=%s f=%s", pcma.PayloadType(), pcma.RTPMap(), pcma.FLine())
	}
	pcmu := AudioCodecPCMU
	if pcmu.PayloadType() != 0 || pcmu.RTPMap() != "PCMU/8000" || pcmu.FLine() != "v/////a/2/8/1" {
		t.Fatalf("PCMU wire format drifted: pt=%d rtpmap=%s f=%s", pcmu.PayloadType(), pcmu.RTPMap(), pcmu.FLine())
	}
}

func TestParseG711RTPMap(t *testing.T) {
	for _, tt := range []struct {
		payload  string
		encoding string
		want     AudioCodec
		ok       bool
	}{
		{"a=rtpmap:8", "PCMA/8000", AudioCodecPCMA, true},
		{"a=rtpmap:8", "pcma/8000/1", AudioCodecPCMA, true},
		{"a=rtpmap:0", "PCMU/8000", AudioCodecPCMU, true},
		{"a=rtpmap:0", "PCMU/8000/1", AudioCodecPCMU, true},
		// 动态载荷类型不接受：国标对声音频只有这两个静态值。
		{"a=rtpmap:96", "PCMA/8000", "", false},
		{"a=rtpmap:8", "PCMU/8000", "", false},
		{"a=rtpmap:0", "PCMA/8000", "", false},
		{"a=rtpmap:8", "PCMA/16000", "", false},
		{"a=rtpmap:8", "PCMA/8000/2", "", false},
		{"a=rtpmap:8", "PCMA/8000/1/1", "", false},
	} {
		got, ok := ParseG711RTPMap(tt.payload, tt.encoding)
		if got != tt.want || ok != tt.ok {
			t.Fatalf("ParseG711RTPMap(%q,%q) = (%q,%v), want (%q,%v)", tt.payload, tt.encoding, got, ok, tt.want, tt.ok)
		}
	}
}

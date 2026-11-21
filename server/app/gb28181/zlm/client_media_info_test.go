package zlm

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestGetMediaInfoParsesFullResponse(t *testing.T) {
	c, server := newMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("schema"); got != "rtsp" {
			t.Fatalf("schema=%q, want rtsp", got)
		}
		if got := r.URL.Query().Get("vhost"); got != "__defaultVhost__" {
			t.Fatalf("vhost=%q", got)
		}
		_, _ = w.Write([]byte(`{
			"code":0,"schema":"rtsp","vhost":"__defaultVhost__","app":"rtp","stream":"stream-1",
			"createStamp":1700000000,"currentStamp":1234,"aliveSecond":42,"bytesSpeed":250000,
			"totalBytes":9000000,"readerCount":3,"totalReaderCount":7,"originType":3,
			"originTypeStr":"rtp_push","originUrl":"","isRecordingMP4":true,"isRecordingHLS":false,
			"tracks":[
				{"codec_id":0,"codec_id_name":"H264","ready":true,"codec_type":0,"frames":100,"duration":4.0,"width":1920,"height":1080,"fps":25.0,"key_frames":4,"gop_size":25,"gop_interval_ms":1000,"loss":0.0125},
				{"codec_id":3,"codec_id_name":"PCMA","ready":true,"codec_type":1,"frames":200,"duration":4.0,"sample_rate":8000,"channels":1,"sample_bit":16,"loss":-1}
			]
		}`))
	})
	defer server.Close()

	info, err := c.GetMediaInfo(context.Background(), "rtsp", "__defaultVhost__", "rtp", "stream-1")
	if err != nil {
		t.Fatal(err)
	}
	if !info.Online || info.AliveSecond != 42 || info.BytesSpeed != 250000 || info.ReaderCount != 3 {
		t.Fatalf("unexpected media info: %+v", info)
	}
	if len(info.Tracks) != 2 || info.Tracks[0].Width != 1920 || info.Tracks[0].FPS != 25 {
		t.Fatalf("unexpected tracks: %+v", info.Tracks)
	}
	if info.Tracks[0].Loss == nil || *info.Tracks[0].Loss != 0.0125 {
		t.Fatalf("video loss=%v", info.Tracks[0].Loss)
	}
	if info.Tracks[1].Loss != nil {
		t.Fatalf("negative loss must be unknown, got %v", *info.Tracks[1].Loss)
	}
}

func TestGetMediaInfoNotFoundIsOffline(t *testing.T) {
	c, server := newMockClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":-500,"msg":"can not find the stream"}`))
	})
	defer server.Close()

	info, err := c.GetMediaInfo(context.Background(), "rtsp", "__defaultVhost__", "rtp", "missing")
	if err != nil {
		t.Fatal(err)
	}
	if info.Online || info.App != "rtp" || info.Stream != "missing" {
		t.Fatalf("unexpected missing result: %+v", info)
	}
}

func TestGetMediaInfoStrictPreservesNotFound(t *testing.T) {
	c, server := newMockClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":-500,"msg":"can not find the stream"}`))
	})
	defer server.Close()

	info, err := c.GetMediaInfoStrict(context.Background(), "rtsp", "__defaultVhost__", "rtp", "missing")
	if info != nil {
		t.Fatalf("strict missing info=%+v want nil", info)
	}
	if !errors.Is(err, ErrMediaNotFound) {
		t.Fatalf("strict error=%v want ErrMediaNotFound", err)
	}
}

func TestGetMediaInfoStrictDoesNotConvertOtherFailureToOffline(t *testing.T) {
	c, server := newMockClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":-1,"msg":"backend failed"}`))
	})
	defer server.Close()

	info, err := c.GetMediaInfoStrict(context.Background(), "rtsp", "__defaultVhost__", "rtp", "broken")
	if info != nil {
		t.Fatalf("strict failure info=%+v want nil", info)
	}
	if err == nil || errors.Is(err, ErrMediaNotFound) {
		t.Fatalf("strict failure error=%v", err)
	}
}

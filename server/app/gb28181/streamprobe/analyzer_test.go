package streamprobe

import (
	"math"
	"testing"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
)

func TestAnalyzeMixedTracks(t *testing.T) {
	frames := []zlm.ProbeFrame{
		{Codec: "H264", TrackType: "video", DTS: 0, PTS: 0, RecvStamp: 0, FrameSize: 100, KeyFrame: true},
		{Codec: "PCMA", TrackType: "audio", DTS: 0, PTS: 0, RecvStamp: 10, FrameSize: 20},
		{Codec: "PCMA", TrackType: "audio", DTS: 20, PTS: 20, RecvStamp: 30, FrameSize: 20},
		{Codec: "H264", TrackType: "video", DTS: 40, PTS: 50, RecvStamp: 40, FrameSize: 100},
		{Codec: "PCMA", TrackType: "audio", DTS: 40, PTS: 40, RecvStamp: 50, FrameSize: 20},
		{Codec: "H264", TrackType: "video", DTS: 80, PTS: 80, RecvStamp: 80, FrameSize: 100, KeyFrame: true},
	}

	result := Analyze(frames)
	if result.Summary.SampleDurationMS != 80 || result.Summary.FrameCount != 6 || result.Summary.TotalBytes != 360 {
		t.Fatalf("unexpected summary: %+v", result.Summary)
	}
	closeEnough(t, result.Summary.AverageBitrateKbps, 36)
	if result.Video == nil || result.Video.FrameCount != 3 || result.Video.KeyFrameCount != 2 || result.Video.FPS == nil || result.Video.GOP == nil {
		t.Fatalf("unexpected video: %+v", result.Video)
	}
	closeEnough(t, *result.Video.FPS, 25)
	closeEnough(t, *result.Video.GOP, 2)
	if result.Audio == nil || result.Audio.AverageIntervalMS == nil {
		t.Fatalf("unexpected audio: %+v", result.Audio)
	}
	closeEnough(t, *result.Audio.AverageIntervalMS, 20)
	closeEnough(t, deref(result.Timestamps.VideoDTSIntervalMeanMS), 40)
	closeEnough(t, deref(result.Timestamps.ArrivalJitterMS), 0)
	closeEnough(t, deref(result.Timestamps.PTSDTSMaxMS), 10)
	closeEnough(t, deref(result.Timestamps.AVArrivalSkewMaxMS), 30)
	if result.Health.Status != HealthOK || len(result.Timeline) != 6 {
		t.Fatalf("unexpected health/timeline: %+v %+v", result.Health, result.Timeline)
	}
}

func TestAnalyzeEmptyAndSingleTrack(t *testing.T) {
	empty := Analyze(nil)
	if empty.Health.Status != HealthError || empty.Summary.AverageBitrateKbps != 0 {
		t.Fatalf("unexpected empty result: %+v", empty)
	}

	audio := Analyze([]zlm.ProbeFrame{
		{Codec: "PCMA", TrackType: "audio", DTS: 0, RecvStamp: 0, FrameSize: 160},
		{Codec: "PCMA", TrackType: "audio", DTS: 20, RecvStamp: 20, FrameSize: 160},
	})
	if audio.Health.Status != HealthOK || audio.Video != nil || audio.Audio == nil {
		t.Fatalf("single audio must not be abnormal: %+v", audio)
	}
}

func TestAnalyzeWarningsAndLast32TimelineFrames(t *testing.T) {
	frames := make([]zlm.ProbeFrame, 40)
	for i := range frames {
		frames[i] = zlm.ProbeFrame{Codec: "H264", TrackType: "video", DTS: int64(i * 40), PTS: int64(i * 40), RecvStamp: int64(i * 40), FrameSize: 100}
	}
	frames[20].DTS = 10
	frames[30].RecvStamp = frames[29].RecvStamp + 600
	result := Analyze(frames)
	if result.Health.Status != HealthWarning || len(result.Health.Issues) < 2 {
		t.Fatalf("expected traceable warnings: %+v", result.Health)
	}
	if len(result.Timeline) != 32 || result.Timeline[0].Sequence != 8 || result.Timeline[31].Sequence != 39 {
		t.Fatalf("unexpected timeline: %+v", result.Timeline)
	}
}

func closeEnough(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.0001 {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func deref(value *float64) float64 {
	if value == nil {
		return math.NaN()
	}
	return *value
}

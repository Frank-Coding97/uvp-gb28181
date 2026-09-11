package streamprobe

import (
	"math"
	"sort"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
)

const (
	HealthOK      = "ok"
	HealthWarning = "warning"
	HealthError   = "error"

	largeArrivalGapMS int64 = 500
	keyFrameWindowMS  int64 = 3000
)

type Result struct {
	Summary    Summary          `json:"summary"`
	Video      *TrackStats      `json:"video"`
	Audio      *TrackStats      `json:"audio"`
	Timestamps TimestampMetrics `json:"timestamps"`
	Timeline   []TimelineFrame  `json:"timeline"`
	Health     Health           `json:"health"`
}

type Summary struct {
	SampleDurationMS   int64   `json:"sampleDurationMs"`
	FrameCount         int     `json:"frameCount"`
	TotalBytes         int64   `json:"totalBytes"`
	AverageBitrateKbps float64 `json:"averageBitrateKbps"`
}

type TrackStats struct {
	Codec             string   `json:"codec"`
	FrameCount        int      `json:"frameCount"`
	KeyFrameCount     int      `json:"keyFrameCount"`
	FPS               *float64 `json:"fps"`
	GOP               *float64 `json:"gop"`
	AverageIntervalMS *float64 `json:"averageIntervalMs"`
}

type TimestampMetrics struct {
	VideoDTSIntervalMeanMS *float64 `json:"videoDtsIntervalMeanMs"`
	ArrivalJitterMS        *float64 `json:"arrivalJitterMs"`
	PTSDTSMaxMS            *float64 `json:"ptsDtsMaxMs"`
	AVArrivalSkewMaxMS     *float64 `json:"avArrivalSkewMaxMs"`
}

type TimelineFrame struct {
	Sequence       int    `json:"sequence"`
	TrackType      string `json:"trackType"`
	Codec          string `json:"codec"`
	KeyFrame       bool   `json:"keyFrame"`
	ConfigFrame    bool   `json:"configFrame"`
	RelativeTimeMS int64  `json:"relativeTimeMs"`
	FrameSize      int64  `json:"frameSize"`
}

type Health struct {
	Status     string     `json:"status"`
	Issues     []Issue    `json:"issues"`
	Thresholds Thresholds `json:"thresholds"`
}

type Issue struct {
	Code        string `json:"code"`
	Message     string `json:"message"`
	ThresholdMS *int64 `json:"thresholdMs"`
	ObservedMS  *int64 `json:"observedMs"`
}

type Thresholds struct {
	LargeArrivalGapMS int64 `json:"largeArrivalGapMs"`
	KeyFrameWindowMS  int64 `json:"keyFrameWindowMs"`
}

func Analyze(frames []zlm.ProbeFrame) Result {
	result := Result{Health: Health{
		Status:     HealthOK,
		Issues:     []Issue{},
		Thresholds: Thresholds{LargeArrivalGapMS: largeArrivalGapMS, KeyFrameWindowMS: keyFrameWindowMS},
	}}
	result.Summary.FrameCount = len(frames)
	if len(frames) == 0 {
		result.Health.Status = HealthError
		result.Health.Issues = append(result.Health.Issues, Issue{Code: "no_frames", Message: "采样期间未收到媒体帧"})
		result.Timeline = []TimelineFrame{}
		return result
	}

	minRecv, maxRecv := frames[0].RecvStamp, frames[0].RecvStamp
	var video, audio []zlm.ProbeFrame
	for i, frame := range frames {
		if frame.RecvStamp < minRecv {
			minRecv = frame.RecvStamp
		}
		if frame.RecvStamp > maxRecv {
			maxRecv = frame.RecvStamp
		}
		if frame.FrameSize > 0 {
			result.Summary.TotalBytes += frame.FrameSize
		}
		if !frame.ConfigFrame {
			switch frame.TrackType {
			case "video":
				video = append(video, frame)
			case "audio":
				audio = append(audio, frame)
			}
		}
		if i > 0 {
			gap := frame.RecvStamp - frames[i-1].RecvStamp
			if gap > largeArrivalGapMS {
				threshold, observed := largeArrivalGapMS, gap
				result.warn(Issue{Code: "large_arrival_gap", Message: "帧到达出现连续大间隔", ThresholdMS: &threshold, ObservedMS: &observed})
			}
		}
	}
	result.Summary.SampleDurationMS = maxRecv - minRecv
	if result.Summary.SampleDurationMS > 0 {
		result.Summary.AverageBitrateKbps = float64(result.Summary.TotalBytes*8) / float64(result.Summary.SampleDurationMS)
	}

	result.Video = analyzeTrack(video, true)
	result.Audio = analyzeTrack(audio, false)
	result.Timestamps = analyzeTimestamps(video, audio, frames)
	result.detectTimestampRegression(frames)
	if len(video) > 0 && result.Summary.SampleDurationMS >= keyFrameWindowMS && result.Video.KeyFrameCount == 0 {
		threshold, observed := keyFrameWindowMS, result.Summary.SampleDurationMS
		result.warn(Issue{Code: "missing_keyframe", Message: "三秒视频采样窗口内没有关键帧", ThresholdMS: &threshold, ObservedMS: &observed})
	}
	result.Timeline = buildTimeline(frames, minRecv)
	return result
}

func analyzeTrack(frames []zlm.ProbeFrame, video bool) *TrackStats {
	if len(frames) == 0 {
		return nil
	}
	stats := &TrackStats{Codec: frames[0].Codec, FrameCount: len(frames)}
	intervals := positiveDTSIntervals(frames)
	if mean := average(intervals); mean != nil {
		stats.AverageIntervalMS = mean
	}
	if video {
		for _, frame := range frames {
			if frame.KeyFrame {
				stats.KeyFrameCount++
			}
		}
		if len(frames) > 1 {
			duration := frames[len(frames)-1].DTS - frames[0].DTS
			if duration > 0 {
				value := float64(len(frames)-1) * 1000 / float64(duration)
				stats.FPS = &value
			}
		}
		var keyPositions []int
		for i, frame := range frames {
			if frame.KeyFrame {
				keyPositions = append(keyPositions, i)
			}
		}
		if len(keyPositions) > 1 {
			gops := make([]float64, 0, len(keyPositions)-1)
			for i := 1; i < len(keyPositions); i++ {
				gops = append(gops, float64(keyPositions[i]-keyPositions[i-1]))
			}
			value := median(gops)
			stats.GOP = &value
		}
	}
	return stats
}

func analyzeTimestamps(video, audio, all []zlm.ProbeFrame) TimestampMetrics {
	metrics := TimestampMetrics{}
	metrics.VideoDTSIntervalMeanMS = average(positiveDTSIntervals(video))
	jitterFrames := video
	if len(jitterFrames) < 2 {
		jitterFrames = audio
	}
	if intervals := arrivalIntervals(jitterFrames); len(intervals) > 0 {
		value := standardDeviation(intervals)
		metrics.ArrivalJitterMS = &value
	}
	if len(all) > 0 {
		maximum := float64(0)
		for _, frame := range all {
			delta := math.Abs(float64(frame.PTS - frame.DTS))
			if delta > maximum {
				maximum = delta
			}
		}
		metrics.PTSDTSMaxMS = &maximum
	}
	if len(video) > 0 && len(audio) > 0 {
		maximum := float64(0)
		for _, source := range [][]zlm.ProbeFrame{video, audio} {
			other := audio
			if source[0].TrackType == "audio" {
				other = video
			}
			for _, frame := range source {
				nearest := math.MaxFloat64
				for _, candidate := range other {
					delta := math.Abs(float64(frame.RecvStamp - candidate.RecvStamp))
					if delta < nearest {
						nearest = delta
					}
				}
				if nearest > maximum {
					maximum = nearest
				}
			}
		}
		metrics.AVArrivalSkewMaxMS = &maximum
	}
	return metrics
}

func (r *Result) detectTimestampRegression(frames []zlm.ProbeFrame) {
	last := map[string]int64{}
	seen := map[string]bool{}
	for _, frame := range frames {
		if frame.ConfigFrame {
			continue
		}
		if seen[frame.TrackType] && frame.DTS < last[frame.TrackType] {
			observed := last[frame.TrackType] - frame.DTS
			r.warn(Issue{Code: "timestamp_regression", Message: "媒体 DTS 出现倒退", ObservedMS: &observed})
			return
		}
		last[frame.TrackType] = frame.DTS
		seen[frame.TrackType] = true
	}
}

func (r *Result) warn(issue Issue) {
	if r.Health.Status != HealthError {
		r.Health.Status = HealthWarning
	}
	r.Health.Issues = append(r.Health.Issues, issue)
}

func buildTimeline(frames []zlm.ProbeFrame, start int64) []TimelineFrame {
	from := 0
	if len(frames) > 32 {
		from = len(frames) - 32
	}
	result := make([]TimelineFrame, 0, len(frames)-from)
	for i := from; i < len(frames); i++ {
		frame := frames[i]
		result = append(result, TimelineFrame{
			Sequence: i, TrackType: frame.TrackType, Codec: frame.Codec,
			KeyFrame: frame.KeyFrame, ConfigFrame: frame.ConfigFrame,
			RelativeTimeMS: frame.RecvStamp - start, FrameSize: frame.FrameSize,
		})
	}
	return result
}

func positiveDTSIntervals(frames []zlm.ProbeFrame) []float64 {
	if len(frames) < 2 {
		return nil
	}
	result := make([]float64, 0, len(frames)-1)
	for i := 1; i < len(frames); i++ {
		if delta := frames[i].DTS - frames[i-1].DTS; delta > 0 {
			result = append(result, float64(delta))
		}
	}
	return result
}

func arrivalIntervals(frames []zlm.ProbeFrame) []float64 {
	if len(frames) < 2 {
		return nil
	}
	result := make([]float64, 0, len(frames)-1)
	for i := 1; i < len(frames); i++ {
		if delta := frames[i].RecvStamp - frames[i-1].RecvStamp; delta >= 0 {
			result = append(result, float64(delta))
		}
	}
	return result
}

func average(values []float64) *float64 {
	if len(values) == 0 {
		return nil
	}
	var total float64
	for _, value := range values {
		total += value
	}
	result := total / float64(len(values))
	return &result
}

func standardDeviation(values []float64) float64 {
	mean := *average(values)
	var sum float64
	for _, value := range values {
		delta := value - mean
		sum += delta * delta
	}
	return math.Sqrt(sum / float64(len(values)))
}

func median(values []float64) float64 {
	sort.Float64s(values)
	middle := len(values) / 2
	if len(values)%2 == 1 {
		return values[middle]
	}
	return (values[middle-1] + values[middle]) / 2
}

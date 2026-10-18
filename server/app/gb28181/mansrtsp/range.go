package mansrtsp

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// PlaybackRange retains the time system on the wire. SMPTE uses the default
// RFC 2326 time base of 30 frames/second, with optional hundredth subframes.
// Converting its frame count to seconds is for bounds checks only.
type PlaybackRange struct {
	Unit         string
	Value        string
	StartSeconds float64
	EndSeconds   float64
	HasEnd       bool
}

var nptSeconds = regexp.MustCompile(`^[0-9]+(\.[0-9]*)?$`)
var smpteTime = regexp.MustCompile(`^[0-9]{1,2}:[0-9]{2}:[0-9]{2}(:[0-9]{2}(\.[0-9]{1,2})?)?$`)

func ParseRange(value string) (PlaybackRange, error) {
	unit, times, ok := strings.Cut(value, "=")
	if !ok || (unit != "npt" && unit != "smpte") {
		return PlaybackRange{}, protocolError(value, "unsupported Range time system")
	}
	first, last, ok := strings.Cut(times, "-")
	if !ok || first == "" || strings.Contains(last, "-") {
		return PlaybackRange{}, protocolError(value, "invalid Range")
	}
	parse := parseNPT
	if unit == "smpte" {
		parse = parseSMPTE
	}
	start, err := parse(first)
	if err != nil {
		return PlaybackRange{}, err
	}
	r := PlaybackRange{Unit: unit, Value: value, StartSeconds: start}
	if last != "" {
		r.EndSeconds, err = parse(last)
		r.HasEnd = true
		if err != nil {
			return PlaybackRange{}, err
		}
		if r.EndSeconds <= start {
			return PlaybackRange{}, protocolError(value, "Range end must follow start")
		}
	}
	return r, nil
}

func parseNPT(value string) (float64, error) {
	if strings.Contains(value, ":") {
		parts := strings.Split(value, ":")
		if len(parts) != 3 || len(parts[1]) != 2 || len(parts[2]) < 2 || !allDigits(parts[0]) || !allDigits(parts[1]) || !nptSeconds.MatchString(parts[2]) {
			return 0, protocolError(value, "invalid npt time")
		}
		hours, _ := strconv.ParseFloat(parts[0], 64)
		minutes, _ := strconv.Atoi(parts[1])
		seconds, _ := strconv.ParseFloat(parts[2], 64)
		if minutes >= 60 || seconds >= 60 {
			return 0, protocolError(value, "invalid npt time")
		}
		return checkedRangeSeconds(value, hours*3600+float64(minutes)*60+seconds)
	}
	if !nptSeconds.MatchString(value) {
		return 0, protocolError(value, "invalid npt time")
	}
	seconds, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, protocolError(value, "invalid npt time")
	}
	return checkedRangeSeconds(value, seconds)
}

func parseSMPTE(value string) (float64, error) {
	if !smpteTime.MatchString(value) {
		return 0, protocolError(value, "invalid smpte time")
	}
	parts := strings.Split(value, ":")
	hours, _ := strconv.Atoi(parts[0])
	minutes, _ := strconv.Atoi(parts[1])
	seconds, _ := strconv.Atoi(parts[2])
	frames := 0.0
	if len(parts) == 4 {
		frames, _ = strconv.ParseFloat(parts[3], 64)
	}
	if minutes >= 60 || seconds >= 60 || frames >= 30 {
		return 0, protocolError(value, "invalid smpte frame time")
	}
	return checkedRangeSeconds(value, float64(hours*3600+minutes*60+seconds)+frames/30)
}

func checkedRangeSeconds(value string, seconds float64) (float64, error) {
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 0 || seconds >= float64(math.MaxInt64)/float64(time.Second) {
		return 0, protocolError(value, "Range exceeds time limits")
	}
	return seconds, nil
}

func allDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func BuildRange(cseq uint32, value string, segmentDuration time.Duration) ([]byte, error) {
	r, err := ParseRange(value)
	if err != nil {
		return nil, err
	}
	if segmentDuration <= 0 || r.StartSeconds >= segmentDuration.Seconds() || (r.HasEnd && r.EndSeconds > segmentDuration.Seconds()) {
		return nil, invalidArgument("range", "must be inside segment duration")
	}
	return buildCommand("PLAY", cseq, "Range: "+r.Value)
}

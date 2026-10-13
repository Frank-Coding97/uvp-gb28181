package dashboard

import (
	"errors"
	"time"
)

type HistoryRange string

const (
	HistoryRange1H  HistoryRange = "1h"
	HistoryRange24H HistoryRange = "24h"
	HistoryRange7D  HistoryRange = "7d"
)

type HistoryWindow struct {
	Range     HistoryRange
	From      time.Time
	To        time.Time
	Bucket    time.Duration
	MaxPoints int
	Timezone  string
}

func ResolveHistoryWindow(raw string, now time.Time, location *time.Location) (HistoryWindow, error) {
	if location == nil {
		location = time.Local
	}
	to := now.In(location)
	rangeValue := HistoryRange(raw)
	if rangeValue == "" {
		rangeValue = HistoryRange24H
	}
	window := HistoryWindow{Range: rangeValue, To: to, Timezone: location.String()}
	switch rangeValue {
	case HistoryRange1H:
		window.From, window.Bucket, window.MaxPoints = to.Add(-time.Hour), time.Minute, 61
	case HistoryRange24H:
		window.From, window.Bucket, window.MaxPoints = to.Add(-24*time.Hour), 5*time.Minute, 289
	case HistoryRange7D:
		window.From, window.Bucket, window.MaxPoints = to.Add(-7*24*time.Hour), time.Hour, 169
	default:
		return HistoryWindow{}, errors.New("range 仅支持 1h、24h、7d")
	}
	return window, nil
}

func ResolveTrafficHistoryWindow(raw string, now time.Time, location *time.Location) (HistoryWindow, error) {
	window, err := ResolveHistoryWindow(raw, now, location)
	if err != nil {
		return HistoryWindow{}, err
	}
	if window.Range == HistoryRange1H {
		return HistoryWindow{}, errors.New("媒体流量仅支持 24h、7d")
	}
	if window.Range == HistoryRange24H {
		window.Bucket, window.MaxPoints = time.Hour, 24
	} else {
		localTo := window.To.In(location)
		dayStart := time.Date(localTo.Year(), localTo.Month(), localTo.Day(), 0, 0, 0, 0, location)
		window.From = dayStart.AddDate(0, 0, -6)
		window.Bucket, window.MaxPoints = 24*time.Hour, 7
	}
	return window, nil
}

func bucketStart(value time.Time, window HistoryWindow) time.Time {
	return value.In(window.To.Location()).Truncate(window.Bucket)
}

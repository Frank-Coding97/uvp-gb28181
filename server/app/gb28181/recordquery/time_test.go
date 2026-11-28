package recordquery

import (
	"errors"
	"testing"
	"time"
)

func TestWallClockShanghaiDoesNotShiftFromBrowserUTC(t *testing.T) {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("加载上海时区失败: %v", err)
	}

	parsed, err := ParseWallClock("2026-08-02T00:00:00", location)
	if err != nil {
		t.Fatalf("解析平台墙钟失败: %v", err)
	}
	if got := FormatMANSCDPWallClock(parsed, location); got != "2026-08-02T00:00:00" {
		t.Fatalf("MANSCDP 墙钟=%q,期望保持上海 00:00", got)
	}
	if _, offset := parsed.Zone(); offset != 8*60*60 {
		t.Fatalf("解析 offset=%d,期望 +08:00", offset)
	}
}

func TestWallClockRejectsDSTGap(t *testing.T) {
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("加载纽约时区失败: %v", err)
	}

	_, err = ParseWallClock("2026-03-08T02:30:00", location)
	if !errors.Is(err, ErrInvalidTime) {
		t.Fatalf("DST 缺口期望 invalid-time,实际 %T: %v", err, err)
	}
}

func TestWallClockServerNowIncludesPlatformOffset(t *testing.T) {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("加载上海时区失败: %v", err)
	}
	now := time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)

	if got := ServerNow(now, location); got != "2026-08-02T08:00:00+08:00" {
		t.Fatalf("serverNow=%q,期望带平台 offset", got)
	}
}

func TestWallClockRejectsOffsetAndNilLocation(t *testing.T) {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("加载上海时区失败: %v", err)
	}
	for _, tc := range []struct {
		name     string
		value    string
		location *time.Location
	}{
		{name: "禁止 offset", value: "2026-08-02T00:00:00Z", location: location},
		{name: "禁止 nil location", value: "2026-08-02T00:00:00", location: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseWallClock(tc.value, tc.location)
			if !errors.Is(err, ErrInvalidTime) {
				t.Fatalf("期望 invalid-time,实际 %T: %v", err, err)
			}
		})
	}
}

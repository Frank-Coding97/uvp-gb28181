package manscdp

import (
	"strings"
	"testing"
	"time"
)

func mustRecordTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.ParseInLocation("2006-01-02T15:04:05", value, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestBuildRecordInfoQuery(t *testing.T) {
	body, err := BuildRecordInfoQuery(RecordInfoQuery{
		SN:        8,
		DeviceID:  "34020000001320000001",
		StartTime: mustRecordTime(t, "2026-07-19T08:00:00"),
		EndTime:   mustRecordTime(t, "2026-07-19T09:00:00"),
		Type:      RecordTypeTime,
		Secrecy:   0,
	})
	if err != nil {
		t.Fatalf("BuildRecordInfoQuery() error = %v", err)
	}
	text := string(body)
	for _, want := range []string{
		`<?xml version="1.0" encoding="GB2312"?>`,
		`<CmdType>RecordInfo</CmdType>`,
		`<SN>8</SN>`,
		`<DeviceID>34020000001320000001</DeviceID>`,
		`<StartTime>2026-07-19T08:00:00</StartTime>`,
		`<EndTime>2026-07-19T09:00:00</EndTime>`,
		`<Type>time</Type>`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("query missing %q: %s", want, text)
		}
	}
	if _, err := BuildRecordInfoQuery(RecordInfoQuery{SN: 1, DeviceID: "c", StartTime: mustRecordTime(t, "2026-07-19T09:00:00"), EndTime: mustRecordTime(t, "2026-07-19T08:00:00")}); err == nil {
		t.Fatal("invalid time range should fail")
	}
}

func TestParseRecordInfoResponse(t *testing.T) {
	body := []byte(`<?xml version="1.0" encoding="GB2312"?>
<Response>
  <CmdType>RecordInfo</CmdType><SN>8</SN><DeviceID>34020000002000000001</DeviceID><SumNum>2</SumNum>
  <RecordList Num="1"><Item><DeviceID>34020000001320000001</DeviceID><Name>Front Door</Name><FilePath>/record/1.mp4</FilePath><Address>10.0.0.8</Address><StartTime>2026-07-19T08:00:00</StartTime><EndTime>2026-07-19T08:10:00</EndTime><Secrecy>0</Secrecy><Type>time</Type></Item></RecordList>
</Response>`)
	got, err := ParseRecordInfoResponse(body)
	if err != nil {
		t.Fatalf("ParseRecordInfoResponse() error = %v", err)
	}
	if got.SN != 8 || got.DeviceID != "34020000002000000001" || got.Sum != 2 || got.Num != 1 {
		t.Fatalf("unexpected response header: %+v", got)
	}
	if len(got.Records) != 1 {
		t.Fatalf("records=%d, want 1", len(got.Records))
	}
	item := got.Records[0]
	if item.FilePath != "/record/1.mp4" || item.StartTime != "2026-07-19T08:00:00" || item.EndTime != "2026-07-19T08:10:00" {
		t.Fatalf("unexpected item: %+v", item)
	}
	if _, err := ParseRecordInfoResponse([]byte(`<Response><CmdType>Catalog</CmdType></Response>`)); err == nil {
		t.Fatal("non-RecordInfo response should fail")
	}
}

func TestBuildPlaybackControl(t *testing.T) {
	start := mustRecordTime(t, "2026-07-19T08:00:00")
	end := mustRecordTime(t, "2026-07-19T09:00:00")
	tests := []struct {
		name string
		cmd  PlaybackCommand
		want []string
	}{
		{"pause", PlaybackCommand{Action: PlaybackActionPause}, []string{"<PlaybackCmd>Pause</PlaybackCmd>"}},
		{"resume", PlaybackCommand{Action: PlaybackActionPlay}, []string{"<PlaybackCmd>Play</PlaybackCmd>"}},
		{"speed", PlaybackCommand{Action: PlaybackActionFastForward, Scale: 2}, []string{"<PlaybackCmd>FastForward</PlaybackCmd>", "<Scale>2</Scale>"}},
		{"seek", PlaybackCommand{Action: PlaybackActionPlay, RangeStart: start, RangeEnd: end}, []string{"<Range>2026-07-19T08:00:00/2026-07-19T09:00:00</Range>"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, err := BuildPlaybackControl("34020000001320000001", 9, tt.cmd)
			if err != nil {
				t.Fatalf("BuildPlaybackControl() error = %v", err)
			}
			for _, want := range tt.want {
				if !strings.Contains(string(body), want) {
					t.Errorf("control missing %q: %s", want, body)
				}
			}
		})
	}
	if _, err := BuildPlaybackControl("channel", 0, PlaybackCommand{Action: PlaybackActionPause}); err == nil {
		t.Fatal("zero SN should fail")
	}
	if _, err := BuildPlaybackControl("channel", 1, PlaybackCommand{Action: PlaybackActionFastForward, Scale: 3}); err == nil {
		t.Fatal("unsupported scale should fail")
	}
}

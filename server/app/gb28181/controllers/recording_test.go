package controllers

import "testing"

func TestRecordQueryRequestToQueryRequest(t *testing.T) {
	got, err := (recordQueryRequest{
		DeviceID: "device", ChannelID: "channel",
		StartTime: "2026-07-19T08:00:00+08:00", EndTime: "2026-07-19T09:00:00+08:00",
	}).toQueryRequest()
	if err != nil {
		t.Fatal(err)
	}
	if got.DeviceID != "device" || got.ChannelID != "channel" || got.EndTime.Sub(got.StartTime).Hours() != 1 {
		t.Fatalf("unexpected query: %+v", got)
	}
	if _, err := (recordQueryRequest{DeviceID: "device", ChannelID: "channel", StartTime: "not-a-time", EndTime: "2026-07-19T09:00:00+08:00"}).toQueryRequest(); err == nil {
		t.Fatal("invalid time should be rejected")
	}
}

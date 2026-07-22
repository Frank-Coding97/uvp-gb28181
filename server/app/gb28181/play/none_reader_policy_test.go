package play

import (
	"context"
	"testing"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func TestShouldCloseOnNoneReaderHonorsCloudRecording(t *testing.T) {
	tests := []struct {
		name           string
		onDemand       bool
		cloudRecording bool
		want           bool
	}{
		{name: "按需且未录像", onDemand: true, cloudRecording: false, want: true},
		{name: "按需且录像中", onDemand: true, cloudRecording: true, want: false},
		{name: "常驻且未录像", onDemand: false, cloudRecording: false, want: false},
		{name: "常驻且录像中", onDemand: false, cloudRecording: true, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			channel := &gbmodels.GbChannel{
				DeviceID: "device", ChannelID: "channel", StreamID: tt.name,
				OnDemandLive: tt.onDemand, CloudRecordingEnabled: tt.cloudRecording,
			}
			service := &Service{channels: &fakeChannels{c: channel}}
			got, err := service.ShouldCloseOnNoneReader(context.Background(), tt.name)
			if err != nil {
				t.Fatalf("查询无人观看策略: %v", err)
			}
			if got != tt.want {
				t.Fatalf("ShouldCloseOnNoneReader=%v,期望 %v", got, tt.want)
			}
		})
	}
}

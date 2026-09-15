package play

import (
	"testing"

	"github.com/stretchr/testify/require"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func TestCurrentSSRCForChannelCompatibility(t *testing.T) {
	tests := []struct {
		name string
		ch   *gbmodels.GbChannel
		want string
	}{
		{name: "explicit current", ch: &gbmodels.GbChannel{StreamID: "fixed", CurrentSSRC: "0200000002"}, want: "0200000002"},
		{name: "legacy dynamic", ch: &gbmodels.GbChannel{StreamID: "0200000001"}, want: "0200000001"},
		{name: "fixed without current", ch: &gbmodels.GbChannel{StreamID: "34020000001320000001_34020000001310000001"}},
		{name: "invalid legacy", ch: &gbmodels.GbChannel{StreamID: "020000000x"}},
		{name: "nil"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, CurrentSSRCForChannel(tt.ch))
		})
	}
}

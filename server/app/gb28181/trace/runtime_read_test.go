package trace

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModuleReadFilterReturnsUnchangedBytesAndEmitsFrames(t *testing.T) {
	module := &Module{framer: NewFrameAssembler(4096)}
	var got []Frame
	module.onFrame = func(frame Frame) {
		got = append(got, frame)
	}
	props := testReadProps("TCP", 5060, 15060)
	raw := sipMessage("MESSAGE", "runtime", []byte("body"))

	first := append([]byte(nil), raw[:len(raw)/2]...)
	filtered, err := module.ReadFilter(props, first)
	require.NoError(t, err)
	require.Equal(t, first, filtered)
	require.Empty(t, got)

	second := append([]byte(nil), raw[len(raw)/2:]...)
	filtered, err = module.ReadFilter(props, second)
	require.NoError(t, err)
	require.Equal(t, second, filtered)
	require.Len(t, got, 1)
	require.Equal(t, raw, got[0].Data)
}

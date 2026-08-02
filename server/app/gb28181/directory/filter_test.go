package directory

import (
	"errors"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestParseFilter(t *testing.T) {
	for _, input := range [][3]string{{"national", "", ""}, {"", "national:unknown", ""}, {"national", "national:area:bad", ""}, {"custom", "custom:group:x", ""}, {"custom", "custom:ungrouped", "9"}} {
		_, err := ParseFilter(input[0], input[1], input[2])
		require.True(t, errors.Is(err, ErrDirectoryFilterInvalid))
	}
	f, err := ParseFilter("national", "national:area:370112", "")
	require.NoError(t, err)
	require.Equal(t, FilterNationalArea, f.Kind)
	f, err = ParseFilter("custom", "custom:group:12", "")
	require.NoError(t, err)
	require.Equal(t, uint(12), f.GroupID)
	f, err = ParseFilter("", "", "42")
	require.NoError(t, err)
	require.Equal(t, uint(42), f.NodeID)
}

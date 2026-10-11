package directory

import (
	"errors"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestParseFilter(t *testing.T) {
	// ⛔ business:unknown:* 已随「未归属业务组织」兜底节点一起下线(2026-10-11 产品决策)：
	// 旧选中态若还传这个 key 必须判非法，而不是静默返回全量设备。
	for _, input := range [][3]string{{"national", "", ""}, {"", "national:unknown", ""}, {"national", "national:area:bad", ""}, {"business", "business:unknown:4", ""}, {"custom", "custom:group:x", ""}, {"custom", "custom:ungrouped:x", ""}, {"custom", "custom:ungrouped", "9"}} {
		_, err := ParseFilter(input[0], input[1], input[2])
		require.True(t, errors.Is(err, ErrDirectoryFilterInvalid))
	}
	f, err := ParseFilter("national", "national:area:370112", "")
	require.NoError(t, err)
	require.Equal(t, FilterNationalArea, f.Kind)
	f, err = ParseFilter("custom", "custom:group:12", "")
	require.NoError(t, err)
	require.Equal(t, uint(12), f.GroupID)
	f, err = ParseFilter("custom", "custom:ungrouped:10", "")
	require.NoError(t, err)
	require.Equal(t, FilterCustomUngrouped, f.Kind)
	require.Equal(t, uint(10), f.OwnerDeptID)
	require.True(t, f.OwnerDeptScoped)
	f, err = ParseFilter("", "", "42")
	require.NoError(t, err)
	require.Equal(t, uint(42), f.NodeID)
}

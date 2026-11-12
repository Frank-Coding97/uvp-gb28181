package directory

import (
	"errors"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestParseFilter(t *testing.T) {
	// ⛔ business:unknown:* 已随「未归属业务组织」兜底节点一起下线(2026-10-11 产品决策)：
	// 旧选中态若还传这个 key 必须判非法，而不是静默返回全量设备。
	for _, input := range [][3]string{{"national", "", ""}, {"", "national:unknown", ""}, {"national", "national:area:bad", ""}, {"business", "business:unknown:4", ""}, {"custom", "custom:group:x", ""}, {"custom", "custom:ungrouped:x", ""}, {"custom", "custom:ungrouped", "9"}, {"department", "", ""}, {"department", "department:dept:0", ""}, {"department", "department:dept:nope", ""}, {"department", "national:area:370000", ""}} {
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
	f, err = ParseFilter("department", "department:dept:12", "")
	require.NoError(t, err)
	require.Equal(t, FilterDepartment, f.Kind)
	require.Equal(t, uint(12), f.DepartmentID)
	f, err = ParseFilter("department", "department:unassigned", "")
	require.NoError(t, err)
	require.Equal(t, FilterDepartmentEmpty, f.Kind)
	f, err = ParseFilter("", "", "42")
	require.NoError(t, err)
	require.Equal(t, uint(42), f.NodeID)
}

// 目录树里的「设备」叶子也能被选成筛选范围。全新安装时设备还没归到任何部门/
// 行政区，树里几乎只有这一层可选；把它判非法会让五个列表/地图接口一起 400。
func TestParseFilterDeviceNodeKeys(t *testing.T) {
	// 前半段是父节点 ID（只用于树定位），筛选一律取最后一段设备主键。
	for _, input := range []struct {
		view, key string
		deviceID  uint
	}{
		{"national", "national:device:7", 7},
		{"administrative", "national:device:7", 7},
		{"administrative", "administrative:device:7", 7},
		{"national", "national:device:12:7", 7},
		{"department", "department:device:1:7", 7},
		{"department", "department-unassigned:device:0:7", 7},
		{"business", "business:device:7", 7},
		{"business", "business:device:3:7", 7},
		{"custom", "custom:device:2:7", 7},
		{"custom", "custom-ungrouped:device:0:7", 7},
	} {
		f, err := ParseFilter(input.view, input.key, "")
		require.NoError(t, err, "%s / %s", input.view, input.key)
		require.Equal(t, FilterDevice, f.Kind, "%s / %s", input.view, input.key)
		require.Equal(t, input.deviceID, f.NodeID, "%s / %s", input.view, input.key)
	}

	// 白名单不能松成「含 :device: 就放行」：非法 id、错视图、野前缀仍然必须判非法。
	for _, input := range [][2]string{
		{"department", "department:device:1:"},
		{"department", "department:device:1:0"},
		{"department", "department:device:1:abc"},
		{"department", "business:device:7"},
		{"national", "whatever:device:7"},
		{"administrative", "administrative:unknown:device:7"},
		{"business", "business:unknown:4"},
	} {
		_, err := ParseFilter(input[0], input[1], "")
		require.True(t, errors.Is(err, ErrDirectoryFilterInvalid), "%s / %s", input[0], input[1])
	}
}

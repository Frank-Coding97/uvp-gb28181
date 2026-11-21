package directory

import (
	"errors"
	"strconv"
	"strings"
)

var ErrDirectoryFilterInvalid = errors.New("directory filter invalid")
var ErrDepartmentNotFound = errors.New("directory department not found")

type FilterKind string

const (
	FilterNationalArea    FilterKind = "national_area"
	FilterNationalUnknown FilterKind = "national_unknown"
	FilterNationalCatalog FilterKind = "national_catalog"
	FilterBusinessCatalog FilterKind = "business_catalog"
	FilterBusinessDevice  FilterKind = "business_device"
	// FilterDevice 是「把某台设备选成筛选范围」。目录树里设备是叶子、不是目录容器，
	// 但地图左栏「范围」下拉和列表页恢复的选中态都会把它的 key 带上来。
	FilterDevice          FilterKind = "device"
	FilterBusinessEmpty   FilterKind = "business_empty"
	FilterCustomGroup     FilterKind = "custom_group"
	FilterCustomUngrouped FilterKind = "custom_ungrouped"
	FilterDepartment      FilterKind = "department"
	FilterDepartmentEmpty FilterKind = "department_unassigned"
)

type Filter struct {
	View            string
	Key             string
	Kind            FilterKind
	Code            string
	GroupID         uint
	DepartmentID    uint
	NodeID          uint
	OwnerDeptID     uint
	OwnerDeptScoped bool
}

func ParseFilter(view, key, legacyNodeID string) (*Filter, error) {
	view, key, legacyNodeID = strings.TrimSpace(view), strings.TrimSpace(key), strings.TrimSpace(legacyNodeID)
	if legacyNodeID != "" {
		if view != "" || key != "" {
			return nil, ErrDirectoryFilterInvalid
		}
		id, err := strconv.ParseUint(legacyNodeID, 10, 64)
		if err != nil || id == 0 {
			return nil, ErrDirectoryFilterInvalid
		}
		return &Filter{NodeID: uint(id)}, nil
	}
	if view == "" && key == "" {
		return nil, nil
	}
	if view == "" || key == "" {
		if view == "business" && key == "" {
			return &Filter{View: view, Kind: FilterBusinessEmpty}, nil
		}
		return nil, ErrDirectoryFilterInvalid
	}
	f := &Filter{View: view, Key: key}
	switch {
	case (view == "national" || view == "administrative") && (key == "national:unknown" || key == "administrative:unknown"):
		f.Kind = FilterNationalUnknown
	case view == "national" && strings.HasPrefix(key, "national:area:"):
		f.Kind, f.Code = FilterNationalArea, strings.TrimPrefix(key, "national:area:")
		if !validDigits(f.Code) || (len(f.Code) != 6 && len(f.Code) != 8) {
			return nil, ErrDirectoryFilterInvalid
		}
	case view == "administrative" && strings.HasPrefix(key, "administrative:area:"):
		f.Kind, f.Code = FilterNationalArea, strings.TrimPrefix(key, "administrative:area:")
		if !validDigits(f.Code) || (len(f.Code) != 6 && len(f.Code) != 8) {
			return nil, ErrDirectoryFilterInvalid
		}
	case view == "national" && strings.HasPrefix(key, "national:catalog:"):
		f.Kind = FilterNationalCatalog
		id, err := strconv.ParseUint(strings.TrimPrefix(key, "national:catalog:"), 10, 64)
		if err != nil || id == 0 {
			return nil, ErrDirectoryFilterInvalid
		}
		f.NodeID = uint(id)
	case view == "business" && strings.HasPrefix(key, "business:catalog:"):
		f.Kind = FilterBusinessCatalog
		id, err := strconv.ParseUint(strings.TrimPrefix(key, "business:catalog:"), 10, 64)
		if err != nil || id == 0 {
			return nil, ErrDirectoryFilterInvalid
		}
		f.NodeID = uint(id)
	// 目录树里的「设备」叶子同样能当筛选范围（地图左栏「范围」下拉、列表页恢复的
	// 选中态）。它有两种形状：national:device:<设备ID> 与
	// <view>:device:<父节点ID>:<设备ID>，父节点 ID 只用于树定位，筛选时忽略。
	// ⛔ 旧实现只认 business:device:<设备ID>，其它视图的设备 key 一律判非法：
	//   全新安装时设备还挂在「未分配部门 / 未知行政区」下，用户选中这台设备，
	//   devices/channels/markers/clusters/channel-tree 五个接口就一起 400
	//   「目录筛选参数错误」，地图 2s 轮询再把报错弹窗堆满屏。
	case isDeviceNodeKey(view, key):
		f.Kind = FilterDevice
		id, err := strconv.ParseUint(key[strings.LastIndex(key, ":")+1:], 10, 64)
		if err != nil || id == 0 {
			return nil, ErrDirectoryFilterInvalid
		}
		f.NodeID = uint(id)
	case view == "department" && key == "department:unassigned":
		f.Kind = FilterDepartmentEmpty
	case view == "department" && strings.HasPrefix(key, "department:dept:"):
		f.Kind = FilterDepartment
		id, err := strconv.ParseUint(strings.TrimPrefix(key, "department:dept:"), 10, 64)
		if err != nil || id == 0 {
			return nil, ErrDirectoryFilterInvalid
		}
		f.DepartmentID = uint(id)
	case view == "custom" && key == "custom:ungrouped":
		f.Kind = FilterCustomUngrouped
	case view == "custom" && strings.HasPrefix(key, "custom:ungrouped:"):
		f.Kind = FilterCustomUngrouped
		id, err := strconv.ParseUint(strings.TrimPrefix(key, "custom:ungrouped:"), 10, 64)
		if err != nil {
			return nil, ErrDirectoryFilterInvalid
		}
		f.OwnerDeptID = uint(id)
		f.OwnerDeptScoped = true
	case view == "custom" && strings.HasPrefix(key, "custom:group:"):
		f.Kind = FilterCustomGroup
		id, err := strconv.ParseUint(strings.TrimPrefix(key, "custom:group:"), 10, 64)
		if err != nil || id == 0 {
			return nil, ErrDirectoryFilterInvalid
		}
		f.GroupID = uint(id)
	default:
		return nil, ErrDirectoryFilterInvalid
	}
	return f, nil
}

// isDeviceNodeKey 只放行目录树真正会生成的设备 key（见 directoryDeviceKey /
// national_tree.go）。⛔ 不要用 strings.Contains(key, ":device:") 兜底：那会把
// 任意字符串变成合法筛选条件，等于悄悄放开「未知 key 必须判非法」这道契约。
func isDeviceNodeKey(view, key string) bool {
	switch view {
	case "national", "administrative":
		return strings.HasPrefix(key, "national:device:") || strings.HasPrefix(key, "administrative:device:")
	case "department":
		return strings.HasPrefix(key, "department:device:") || strings.HasPrefix(key, "department-unassigned:device:")
	case "business":
		return strings.HasPrefix(key, "business:device:")
	case "custom":
		return strings.HasPrefix(key, "custom:device:") || strings.HasPrefix(key, "custom-ungrouped:device:")
	default:
		return false
	}
}

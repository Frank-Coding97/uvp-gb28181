package directory

type DirectoryDeviceVO struct {
	ID       uint   `json:"id"`
	DeviceID string `json:"deviceId"`
	Name     string `json:"name"`
	Alias    string `json:"alias"`
	Online   bool   `json:"online"`
}

type DirectoryNodeVO struct {
	Key         string             `json:"key"`
	Name        string             `json:"name"`
	Type        string             `json:"type"`
	Code        string             `json:"code,omitempty"`
	ReadOnly    bool               `json:"readOnly"`
	Count       int                `json:"count"`
	OnlineCount int                `json:"onlineCount"`
	Depth       int                `json:"depth"`
	Meta        map[string]string  `json:"meta,omitempty"`
	Device      *DirectoryDeviceVO `json:"device,omitempty"`
	Children    []DirectoryNodeVO  `json:"children,omitempty"`
}

// DirectoryView 是设备管理页的目录投影视图。
// national 保留为兼容值，实际展示为综合国标目录；administrative 只展示行政区划；
// business 展示 Catalog 中的业务分组/虚拟组织关系，custom 是兼容保留的平台自定义分组，
// department 复用平台部门组织结构。
type DirectoryView string

const (
	DirectoryViewNational       DirectoryView = "national"
	DirectoryViewAdministrative DirectoryView = "administrative"
	DirectoryViewDepartment     DirectoryView = "department"
	DirectoryViewBusiness       DirectoryView = "business"
	DirectoryViewCustom         DirectoryView = "custom"
)

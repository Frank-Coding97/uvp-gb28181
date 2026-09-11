package directory

type DirectoryNodeVO struct {
	Key         string            `json:"key"`
	Name        string            `json:"name"`
	Type        string            `json:"type"`
	Code        string            `json:"code,omitempty"`
	ReadOnly    bool              `json:"readOnly"`
	Count       int               `json:"count"`
	OnlineCount int               `json:"onlineCount"`
	Depth       int               `json:"depth"`
	Meta        map[string]string `json:"meta,omitempty"`
	Children    []DirectoryNodeVO `json:"children,omitempty"`
}

// DirectoryView 是设备管理页的目录投影视图。
// national 保留为兼容值，实际展示为综合国标目录；administrative 只展示行政区划；
// business 展示 Catalog 中的业务分组/虚拟组织关系，custom 是平台自定义分组。
type DirectoryView string

const (
	DirectoryViewNational       DirectoryView = "national"
	DirectoryViewAdministrative DirectoryView = "administrative"
	DirectoryViewBusiness       DirectoryView = "business"
	DirectoryViewCustom         DirectoryView = "custom"
)

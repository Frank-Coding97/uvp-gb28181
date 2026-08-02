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

package catalog

import (
	"strconv"
	"strings"

	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// BuildPath 计算物化路径
//
// 规则(plan §3.4):
//   根节点         path = "/"
//   parent.path="/", id=12         → "/12/"
//   parent.path="/12/", id=47       → "/12/47/"
//   parent.path="/12/47/", id=189   → "/12/47/189/"
//
// path 总是以 "/" 开头 + "/" 结尾;LIKE 'parentPath%' 查整棵子树
func BuildPath(parentPath string, id uint) string {
	if parentPath == "" {
		parentPath = "/"
	}
	if !strings.HasPrefix(parentPath, "/") {
		parentPath = "/" + parentPath
	}
	if !strings.HasSuffix(parentPath, "/") {
		parentPath += "/"
	}
	return parentPath + strconv.FormatUint(uint64(id), 10) + "/"
}

// DepthFromPath 从物化路径计算深度(根 = 0)
//
// "/"             → 0
// "/12/"          → 1
// "/12/47/189/"   → 3
func DepthFromPath(path string) uint8 {
	if path == "" || path == "/" {
		return 0
	}
	// 去掉首尾 /,然后按 / 切片计数
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return 0
	}
	return uint8(strings.Count(trimmed, "/") + 1)
}

// findOrCreateCivilCodeChain 行政区码逐级找节点;不存在则建
//
// 例:civilCode="370112" → 先找/建 "37"(省) → "3701"(市) → "370112"(区县)三级节点
// 返回最末端节点。注:当前简化实现按 6 位级别(2+2+2)拆分,实际 GB/T 2260
// 行政区码可能要按字典层级拆,Phase 2 可优化。
func findOrCreateCivilCodeChain(db *gorm.DB, tenantID uint, civilCode string) (*gbmodels.GbCatalogNode, error) {
	if civilCode == "" {
		return nil, nil
	}
	// 按 6 位前缀:2 / 4 / 6 三级
	codes := []string{}
	if len(civilCode) >= 2 {
		codes = append(codes, civilCode[:2])
	}
	if len(civilCode) >= 4 {
		codes = append(codes, civilCode[:4])
	}
	if len(civilCode) == 6 {
		codes = append(codes, civilCode)
	}

	var prev *gbmodels.GbCatalogNode
	for _, c := range codes {
		var parentID *uint
		parentPath := "/"
		if prev != nil {
			parentID = &prev.ID
			parentPath = prev.Path
		}
		node, err := findOrCreateNode(db, tenantID, gbmodels.NodeTypeCivilCode, c, parentID, parentPath, civilCodeDisplayName(c))
		if err != nil {
			return nil, err
		}
		prev = node
	}
	return prev, nil
}

// findOrCreateNode 通用 find-or-create
//
// 用 (tenant_id, node_type, code) 联合查找,避免重复创建;
// 不存在时:Create + 用新 ID 回填 path,二次 Update。
func findOrCreateNode(
	db *gorm.DB,
	tenantID uint,
	nodeType gbmodels.NodeType,
	code string,
	parentID *uint,
	parentPath string,
	name string,
) (*gbmodels.GbCatalogNode, error) {
	var existed gbmodels.GbCatalogNode
	q := db.Where("tenant_id = ? AND node_type = ? AND code = ?", tenantID, nodeType, code)
	if parentID != nil {
		q = q.Where("parent_id = ?", *parentID)
	} else {
		q = q.Where("parent_id IS NULL")
	}
	res := q.Limit(1).Find(&existed)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected > 0 {
		return &existed, nil
	}

	depth := DepthFromPath(parentPath)
	if parentID != nil {
		depth++
	}
	n := &gbmodels.GbCatalogNode{
		TenantID: tenantID,
		NodeType: nodeType,
		ParentID: parentID,
		Path:     "/", // 创建后用 ID 回填
		Depth:    depth,
		Name:     name,
		Code:     code,
		Source:   gbmodels.NodeSourceCatalog,
	}
	if nodeType == gbmodels.NodeTypeCivilCode {
		n.CivilCode = code
	}
	if err := db.Create(n).Error; err != nil {
		return nil, err
	}
	// 回填 path
	n.Path = BuildPath(parentPath, n.ID)
	if err := db.Model(n).Update("path", n.Path).Error; err != nil {
		return nil, err
	}
	return n, nil
}

// civilCodeDisplayName 行政区码占位显示名(无字典 lookup 时)
// 真实名走 civilcode.Service.Lookup,这里给个兜底
func civilCodeDisplayName(code string) string {
	return "行政区 " + code
}

package catalog

import (
	"strconv"
	"strings"

	"gorm.io/gorm"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

// BuildPath 计算物化路径
//
// 规则(plan §3.4):
//
//	根节点         path = "/"
//	parent.path="/", id=12         → "/12/"
//	parent.path="/12/", id=47       → "/12/47/"
//	parent.path="/12/47/", id=189   → "/12/47/189/"
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
func findOrCreateCivilCodeChain(
	db *gorm.DB,
	ownerDeptID uint,
	civilCode string,
	cache map[string]*gbmodels.GbCatalogNode,
) (*gbmodels.GbCatalogNode, error) {
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
	if len(civilCode) == 6 || len(civilCode) == 8 {
		codes = append(codes, civilCode)
	}

	var prev *gbmodels.GbCatalogNode
	for _, c := range codes {
		if node := cache[c]; node != nil {
			prev = node
			continue
		}
		var parentID *uint
		parentPath := "/"
		if prev != nil {
			parentID = &prev.ID
			parentPath = prev.Path
		}
		node, err := findOrCreateNode(db, ownerDeptID, gbmodels.NodeTypeCivilCode, c, parentID, parentPath, civilCodeDisplayName(c))
		if err != nil {
			return nil, err
		}
		cache[c] = node
		prev = node
	}
	return prev, nil
}

// findOrCreateNode 通用 find-or-create
//
// 用 (owner_dept_id, node_type, code, parent_id) 联合查找,避免重复创建;
// 不存在时:Create + 用新 ID 回填 path,二次 Update。
func findOrCreateNode(
	db *gorm.DB,
	ownerDeptID uint,
	nodeType gbmodels.NodeType,
	code string,
	parentID *uint,
	parentPath string,
	name string,
) (*gbmodels.GbCatalogNode, error) {
	return findOrCreateNodeWithSource(db, ownerDeptID, "", nodeType, code, parentID, parentPath, name, false)
}

// findOrCreateSourceNode 按“归属部门 + 上报来源 + 节点类型 + 编码”复用目录节点。
// ParentID 是可变关系，不能参与身份查找：父节点晚到或通道移动时应修正同一
// 节点的父关系和物化路径，而不是再造一个同编码节点。
func findOrCreateSourceNode(
	db *gorm.DB,
	ownerDeptID uint,
	sourceDeviceID string,
	nodeType gbmodels.NodeType,
	code string,
	parentID *uint,
	parentPath string,
	name string,
) (*gbmodels.GbCatalogNode, error) {
	return findOrCreateNodeWithSource(db, ownerDeptID, sourceDeviceID, nodeType, code, parentID, parentPath, name, true)
}

func findOrCreateNodeWithSource(
	db *gorm.DB,
	ownerDeptID uint,
	sourceDeviceID string,
	nodeType gbmodels.NodeType,
	code string,
	parentID *uint,
	parentPath string,
	name string,
	moveByIdentity bool,
) (*gbmodels.GbCatalogNode, error) {
	var existed gbmodels.GbCatalogNode
	q := db.Where("owner_dept_id = ? AND node_type = ? AND code = ?", ownerDeptID, nodeType, code).
		Where("(source = ? OR source = ?)", gbmodels.NodeSourceCatalog, "").
		Where("source_device_id = ?", strings.TrimSpace(sourceDeviceID))
	if !moveByIdentity {
		if parentID != nil {
			q = q.Where("parent_id = ?", *parentID)
		} else {
			q = q.Where("parent_id IS NULL")
		}
	}
	res := q.Order("id").Limit(1).Find(&existed)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected > 0 {
		if err := updateCatalogNodePlacement(db, &existed, parentID, parentPath, name, sourceDeviceID); err != nil {
			return nil, err
		}
		return &existed, nil
	}

	// depth 取自 parentPath 段数:
	//   parentPath="/"        → 新节点 depth=0(它就是根节点)
	//   parentPath="/17/"     → 新节点 depth=1(parent 自身在 depth 0,新节点在 1)
	//   parentPath="/17/18/"  → 新节点 depth=2
	// DepthFromPath 数 "/" 分段返回的就是 "path 末端节点在哪一层",
	// 所以对新节点(挂在 parentPath 末端下)来说,自己的 depth 就等于这个数。
	// (历史 bug:之前还多 +1,把行政区链算成 0→2→3 跳级。)
	depth := DepthFromPath(parentPath)
	n := &gbmodels.GbCatalogNode{
		OwnerDeptID:    ownerDeptID,
		NodeType:       nodeType,
		ParentID:       parentID,
		Path:           "/", // 创建后用 ID 回填
		Depth:          depth,
		Name:           fallbackName(name, code),
		Code:           code,
		SourceDeviceID: strings.TrimSpace(sourceDeviceID),
		Source:         gbmodels.NodeSourceCatalog,
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

// updateCatalogNodePlacement 更新目录项的可变父关系，并同步所有后代的物化路径。
// 这一步既覆盖父节点晚到，也覆盖通道/虚拟组织移动；不改变人工节点的 source。
func updateCatalogNodePlacement(
	db *gorm.DB,
	node *gbmodels.GbCatalogNode,
	parentID *uint,
	parentPath string,
	name string,
	sourceDeviceID string,
) error {
	if parentPath == "" {
		parentPath = "/"
	}
	oldPath := node.Path
	if oldPath == "" {
		oldPath = "/"
	}
	newPath := BuildPath(parentPath, node.ID)
	newDepth := nodeDepthFromPath(newPath)
	name = strings.TrimSpace(name)
	sourceDeviceID = strings.TrimSpace(sourceDeviceID)
	parentUnchanged := node.ParentID == nil && parentID == nil
	if node.ParentID != nil && parentID != nil {
		parentUnchanged = *node.ParentID == *parentID
	}
	placementUnchanged := oldPath == newPath && node.Depth == newDepth
	metadataUnchanged := node.Source == gbmodels.NodeSourceCatalog &&
		(sourceDeviceID == "" || node.SourceDeviceID == sourceDeviceID) &&
		(name == "" || node.Name == name)
	updates := map[string]any{
		"parent_id": parentID,
		"path":      newPath,
		"depth":     newDepth,
		"source":    gbmodels.NodeSourceCatalog,
	}
	if sourceDeviceID != "" {
		updates["source_device_id"] = strings.TrimSpace(sourceDeviceID)
	}
	if strings.TrimSpace(name) != "" {
		updates["name"] = strings.TrimSpace(name)
	}
	if node.OwnerDeptID == 0 {
		updates["owner_dept_id"] = node.OwnerDeptID
	}
	if !parentUnchanged || !placementUnchanged || !metadataUnchanged {
		if err := db.Model(node).Updates(updates).Error; err != nil {
			return err
		}
	}

	node.ParentID = parentID
	node.Path = newPath
	node.Depth = newDepth
	node.Source = gbmodels.NodeSourceCatalog
	if name != "" {
		node.Name = name
	}
	if sourceDeviceID != "" {
		node.SourceDeviceID = sourceDeviceID
	}
	if placementUnchanged {
		return nil
	}

	var descendants []gbmodels.GbCatalogNode
	if err := db.Where("owner_dept_id = ? AND id <> ? AND path LIKE ?", node.OwnerDeptID, node.ID, oldPath+"%").Find(&descendants).Error; err != nil {
		return err
	}
	for i := range descendants {
		descendant := &descendants[i]
		suffix := strings.TrimPrefix(descendant.Path, oldPath)
		path := newPath + suffix
		if err := db.Model(descendant).Updates(map[string]any{
			"path":  path,
			"depth": nodeDepthFromPath(path),
		}).Error; err != nil {
			return err
		}
	}
	return nil
}

// nodeDepthFromPath 返回目录节点的零基深度；DepthFromPath 返回路径段数，
// 两者相差一个节点自身段。根节点路径为 /，深度仍为 0。
func nodeDepthFromPath(path string) uint8 {
	segments := DepthFromPath(path)
	if segments == 0 {
		return 0
	}
	return segments - 1
}

// civilCodeDisplayName 行政区码占位显示名(无字典 lookup 时)
// 真实名走 civilcode.Service.Lookup,这里给个兜底
func civilCodeDisplayName(code string) string {
	return "行政区 " + code
}

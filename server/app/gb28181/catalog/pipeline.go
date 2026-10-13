// Package catalog pipeline 编排入库(全量 + 增量)
package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

// Pipeline 入库管道(plan §3 / A3 核心)
//
// 责任:
// - Ingest — 一批 CatalogItem 全量入库(manscdp 解析后调用)
// - IngestDelta — Subscribe NOTIFY 单条事件入库(Add / Update / Del / 状态变化)
//
// 实现策略:
// - 单事务处理一批(失败回滚)
// - 节点未找到时 find-or-create(物化路径回填)
// - 多挂载通过 gb_channel_mount 表
// - anomaly 编码 -> 兜底 virtual_org + 写审计
type Pipeline struct {
	db *gorm.DB
}

var ErrOwnerDeptRequired = errors.New("catalog: owner dept required")

// New 构造 Pipeline
func New(db *gorm.DB) *Pipeline {
	return &Pipeline{db: db}
}

// Ingest 全量入库一批 CatalogItem(manscdp 解析后调用)
//
// sender:来源元数据(归属部门 + 上报设备国标编码)
// items:本批通道/设备列表
//
// 错误返回:第一个失败的 item 错误;不全部停下(失败的跳过,继续后续)
// 这种设计让 anomaly 单条不阻塞整批入库
func (p *Pipeline) Ingest(ctx context.Context, sender Sender, items []CatalogItem) error {
	if len(items) == 0 {
		return nil
	}
	if sender.OwnerDeptID == 0 {
		sender.OwnerDeptID = p.resolveOwnerDeptID(ctx, sender)
	}
	if sender.OwnerDeptID == 0 {
		return fmt.Errorf("%w: sourceDeviceId=%s", ErrOwnerDeptRequired, sender.SourceDeviceID)
	}

	var firstErr error
	civilNodeCache := make(map[string]*gbmodels.GbCatalogNode)
	for _, it := range items {
		if it.DeviceID == "" {
			continue
		}
		if err := p.ingestOne(ctx, sender, it, civilNodeCache); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			// The failed item transaction may have rolled back nodes cached while
			// processing it. Clear the batch cache so later items resolve them again.
			for code := range civilNodeCache {
				delete(civilNodeCache, code)
			}
		}
	}
	return firstErr
}

func (p *Pipeline) ingestOne(ctx context.Context, sender Sender, it CatalogItem, civilNodeCache map[string]*gbmodels.GbCatalogNode) error {
	return p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		cls := Classify(it.DeviceID)

		// 1. 行政区链(优先 item.CivilCode,没有则回退 classify 结果)
		var civilNode *gbmodels.GbCatalogNode
		if it.CivilCode != "" {
			n, err := findOrCreateCivilCodeChain(tx, sender.OwnerDeptID, it.CivilCode, civilNodeCache)
			if err != nil {
				return err
			}
			civilNode = n
		} else if cls.CivilCode != "" {
			n, err := findOrCreateCivilCodeChain(tx, sender.OwnerDeptID, cls.CivilCode, civilNodeCache)
			if err != nil {
				return err
			}
			civilNode = n
		}

		parentNode, err := resolveBusinessParent(tx, sender, it, civilNode)
		if err != nil {
			return err
		}
		if err := ingestByType(ctx, tx, sender, it, cls, parentNode); err != nil {
			return err
		}
		return nil
	})
}

// resolveBusinessParent 选择业务父节点。2022 ParentID 可为 A/B;目录树只能
// 选择一个展示父节点,完整关系由资源关系表保留
func resolveBusinessParent(tx *gorm.DB, sender Sender, it CatalogItem, civilNode *gbmodels.GbCatalogNode) (*gbmodels.GbCatalogNode, error) {
	parentNode := civilNode
	classification := Classify(it.DeviceID)
	// NOTIFY 的状态 UPDATE 常常只带 DeviceID/Status。此时不能把已有通道
	// 从业务目录挪回行政区节点，先沿同一来源复用它当前的父节点。
	if classification.NodeType == gbmodels.NodeTypeChannel &&
		strings.TrimSpace(it.ParentID) == "" && strings.TrimSpace(it.BusinessGroupID) == "" {
		var current gbmodels.GbCatalogNode
		q := tx.Where("owner_dept_id = ? AND node_type = ? AND code = ?", sender.OwnerDeptID, classification.NodeType, it.DeviceID).
			Where("(source = ? OR source = ?)", gbmodels.NodeSourceCatalog, "").
			Where("source_device_id = ?", strings.TrimSpace(sender.SourceDeviceID)).
			Order("id").Limit(1)
		if result := q.Find(&current); result.Error != nil {
			return nil, result.Error
		} else if result.RowsAffected == 1 && current.ParentID != nil {
			var existingParent gbmodels.GbCatalogNode
			parentResult := tx.Where("id = ? AND owner_dept_id = ?", *current.ParentID, sender.OwnerDeptID).Limit(1).Find(&existingParent)
			if parentResult.Error != nil {
				return nil, parentResult.Error
			}
			if parentResult.RowsAffected == 1 {
				return &existingParent, nil
			}
		}
	}
	// 通道同时上报 ParentID(物理设备) 与 BusinessGroupID(业务组织) 时，
	// 若 ParentID 明确指向虚拟组织，保留该 216 层级；否则由 BusinessGroupID
	// 建业务挂载，而设备/组织节点仍优先沿 ParentID 建物理层级。
	parentCodes := SplitParentIDs(it.ParentID)
	if classification.NodeType == gbmodels.NodeTypeChannel {
		virtualParents := make([]string, 0, len(parentCodes))
		otherParents := make([]string, 0, len(parentCodes))
		for _, code := range parentCodes {
			if Classify(code).NodeType == gbmodels.NodeTypeVirtualOrg {
				virtualParents = append(virtualParents, code)
			} else {
				otherParents = append(otherParents, code)
			}
		}
		parentCodes = append(virtualParents, SplitParentIDs(it.BusinessGroupID)...)
		parentCodes = append(parentCodes, otherParents...)
	} else {
		parentCodes = append(parentCodes, SplitParentIDs(it.BusinessGroupID)...)
	}
	seenParents := map[string]struct{}{}
parentSearch:
	for _, parentCode := range parentCodes {
		if _, seen := seenParents[parentCode]; seen {
			continue
		}
		seenParents[parentCode] = struct{}{}
		if parentCode == it.DeviceID {
			continue
		}
		var existingParent *gbmodels.GbCatalogNode
		pCls := Classify(parentCode)
		if pCls.NodeType == gbmodels.NodeTypeCivilCode {
			var civilParent gbmodels.GbCatalogNode
			found := tx.Where("owner_dept_id = ? AND node_type = ? AND code = ?", sender.OwnerDeptID, gbmodels.NodeTypeCivilCode, parentCode).
				Where("(source = ? OR source = ?)", gbmodels.NodeSourceCatalog, "").
				Where("source_device_id = ?", "").Order("id").Limit(1).Find(&civilParent)
			if found.Error != nil {
				return nil, found.Error
			}
			if found.RowsAffected == 1 {
				existingParent = &civilParent
			}
		} else {
			parent, foundErr := findSourceNode(tx, sender, pCls.NodeType, parentCode)
			if foundErr != nil {
				return nil, foundErr
			}
			existingParent = parent
		}
		if existingParent != nil {
			parentNode = existingParent
			break
		}
		switch pCls.NodeType {
		case gbmodels.NodeTypeBizGroup, gbmodels.NodeTypeVirtualOrg:
			pn, err := findOrCreateSourceNode(
				tx, sender.OwnerDeptID, sender.SourceDeviceID, pCls.NodeType, parentCode,
				civilNodeID(civilNode), civilNodePath(civilNode), parentCode,
			)
			if err != nil {
				return nil, err
			}
			parentNode = pn
			// 业务容器是通道在业务树中的展示父节点。即使同一条
			// ParentID 还携带物理父设备，也不能让后者覆盖已选中的
			// 215/216；完整的物理关系仍由设备节点和通道自身字段保留。
			break parentSearch
		case gbmodels.NodeTypeDevice:
			pn, err := findOrCreateSourceNode(
				tx, sender.OwnerDeptID, sender.SourceDeviceID, pCls.NodeType, parentCode,
				civilNodeID(civilNode), civilNodePath(civilNode), parentCode,
			)
			if err != nil {
				return nil, err
			}
			parentNode = pn
			break parentSearch
		}
	}
	return parentNode, nil
}

// ingestByType 根据节点类型走对应的 upsert 路径
func ingestByType(ctx context.Context, tx *gorm.DB, sender Sender, it CatalogItem, cls Classification, parentNode *gbmodels.GbCatalogNode) error {
	switch cls.NodeType {
	case gbmodels.NodeTypeChannel:
		node, _, err := upsertChannel(ctx, tx, sender.OwnerDeptID, sender.SourceDeviceID, it, cls, parentNode)
		if err != nil {
			return err
		}
		if cls.Anomaly {
			return recordAnomaly(ctx, tx, sender.OwnerDeptID, node, cls, lookupSourceDeviceID(tx, sender))
		}
	case gbmodels.NodeTypeDevice:
		node, _, err := upsertDevice(ctx, tx, sender.OwnerDeptID, sender.SourceDeviceID, it, cls, parentNode)
		if err != nil {
			return err
		}
		if cls.Anomaly {
			return recordAnomaly(ctx, tx, sender.OwnerDeptID, node, cls, lookupSourceDeviceID(tx, sender))
		}
	case gbmodels.NodeTypeAlarmInput, gbmodels.NodeTypeAlarmOutput:
		node, _, err := upsertAlarmResource(ctx, tx, sender.OwnerDeptID, sender.SourceDeviceID, it, cls, parentNode)
		if err != nil {
			return err
		}
		if cls.Anomaly {
			return recordAnomaly(ctx, tx, sender.OwnerDeptID, node, cls, lookupSourceDeviceID(tx, sender))
		}
	case gbmodels.NodeTypeBizGroup, gbmodels.NodeTypeVirtualOrg:
		node, err := findOrCreateSourceNode(
			tx, sender.OwnerDeptID, sender.SourceDeviceID, cls.NodeType, it.DeviceID,
			civilNodeID(parentNode), civilNodePath(parentNode), fallbackName(it.Name, it.DeviceID),
		)
		if err != nil {
			return err
		}
		if cls.Anomaly {
			return recordAnomaly(ctx, tx, sender.OwnerDeptID, node, cls, lookupSourceDeviceID(tx, sender))
		}
	case gbmodels.NodeTypeCivilCode:
		// 已在第 1 步处理;跳过
	default:
		return errors.New("catalog: unknown node type")
	}
	return nil
}

// IngestDelta Subscribe NOTIFY 单条增量入库(G1 task 调用入口)
//
// action:add / update / del / on / off / vlost / defect
// del:软删(deleted_at),不真删
func (p *Pipeline) IngestDelta(ctx context.Context, sender Sender, action string, it CatalogItem) error {
	if sender.OwnerDeptID == 0 {
		sender.OwnerDeptID = p.resolveOwnerDeptID(ctx, sender)
	}
	if sender.OwnerDeptID == 0 {
		return fmt.Errorf("%w: sourceDeviceId=%s", ErrOwnerDeptRequired, sender.SourceDeviceID)
	}

	switch strings.ToUpper(strings.TrimSpace(action)) {
	case "ADD":
		return p.Ingest(ctx, sender, []CatalogItem{it})
	case "UPDATE":
		// 当前 ingestOne 自带 upsert 行为;UPDATE = ingest
		return p.Ingest(ctx, sender, []CatalogItem{it})
	case "DEL", "DELETE":
		return p.softDelete(ctx, sender, it.DeviceID)
	case "ON":
		return p.updateChannelStatus(ctx, sender, it.DeviceID, gbmodels.ChannelStatusOnline)
	case "OFF", "VLOST", "DEFECT":
		return p.updateChannelStatus(ctx, sender, it.DeviceID, gbmodels.ChannelStatusOffline)
	default:
		return errors.New("catalog: unknown delta action: " + action)
	}
}

func (p *Pipeline) updateChannelStatus(ctx context.Context, sender Sender, code string, status int8) error {
	if code == "" {
		return nil
	}
	return p.db.WithContext(ctx).Model(&gbmodels.GbChannel{}).
		Where("owner_dept_id = ? AND device_id = ? AND channel_id = ?", sender.OwnerDeptID, sender.SourceDeviceID, code).
		Update("status", status).Error
}

// softDelete 软删节点 + 关联(deleted_at)
func (p *Pipeline) softDelete(ctx context.Context, sender Sender, code string) error {
	if code == "" {
		return nil
	}
	if sender.OwnerDeptID == 0 {
		return ErrOwnerDeptRequired
	}
	return p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var nodes []gbmodels.GbCatalogNode
		q := tx.Where("code = ? AND owner_dept_id = ?", code, sender.OwnerDeptID).
			Where("(source = ? OR source = ?)", gbmodels.NodeSourceCatalog, "").
			Where("source_device_id = ?", strings.TrimSpace(sender.SourceDeviceID))
		if err := q.Find(&nodes).Error; err != nil {
			return err
		}
		for _, node := range nodes {
			if node.ChannelID != nil {
				mountQuery := tx.Where("channel_id = ? AND owner_dept_id = ?", *node.ChannelID, sender.OwnerDeptID)
				if node.ParentID != nil {
					mountQuery = mountQuery.Where("parent_node_id = ?", *node.ParentID)
				}
				if err := mountQuery.
					Where("mount_source = ? OR mount_source = ?", gbmodels.MountSourceCatalog, "").
					Delete(&gbmodels.GbChannelMount{}).Error; err != nil {
					return err
				}
				var remainingMounts int64
				if err := tx.Model(&gbmodels.GbChannelMount{}).
					Where("channel_id = ? AND owner_dept_id = ?", *node.ChannelID, sender.OwnerDeptID).
					Count(&remainingMounts).Error; err != nil {
					return err
				}
				if remainingMounts == 0 {
					if err := tx.Where("id = ? AND owner_dept_id = ?", *node.ChannelID, sender.OwnerDeptID).
						Delete(&gbmodels.GbChannel{}).Error; err != nil {
						return err
					}
				}
			}
			if node.AlarmResourceID != nil {
				if err := tx.Where("alarm_resource_id = ?", *node.AlarmResourceID).
					Delete(&gbmodels.GbAlarmResourceParent{}).Error; err != nil {
					return err
				}
				if err := tx.Where("alarm_resource_id = ?", *node.AlarmResourceID).
					Delete(&gbmodels.GbAlarmBinding{}).Error; err != nil {
					return err
				}
				if err := tx.Where("id = ?", *node.AlarmResourceID).
					Delete(&gbmodels.GbAlarmResource{}).Error; err != nil {
					return err
				}
			}
			if err := tx.Where("catalog_node_id = ? AND owner_dept_id = ?", node.ID, sender.OwnerDeptID).
				Delete(&gbmodels.GbAnomalyRecord{}).Error; err != nil {
				return err
			}
			if err := tx.Delete(&node).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func findSourceNode(tx *gorm.DB, sender Sender, nodeType gbmodels.NodeType, code string) (*gbmodels.GbCatalogNode, error) {
	var node gbmodels.GbCatalogNode
	result := tx.Where("owner_dept_id = ? AND node_type = ? AND code = ?", sender.OwnerDeptID, nodeType, code).
		Where("(source = ? OR source = ?)", gbmodels.NodeSourceCatalog, "").
		Where("source_device_id = ?", strings.TrimSpace(sender.SourceDeviceID)).
		Order("id").Limit(1).Find(&node)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return &node, nil
}

// civilNodeID 返回 *uint(handle nil)
func civilNodeID(n *gbmodels.GbCatalogNode) *uint {
	if n == nil {
		return nil
	}
	return &n.ID
}

// civilNodePath 返回 path(handle nil)
func civilNodePath(n *gbmodels.GbCatalogNode) string {
	if n == nil {
		return "/"
	}
	return n.Path
}

// lookupSourceDeviceID 解析 sender.SourceDeviceID(国标 20 位)对应的 gb_device.id(主键)
func lookupSourceDeviceID(tx *gorm.DB, sender Sender) *uint {
	if sender.SourceDeviceID == "" {
		return nil
	}
	var dev gbmodels.GbDevice
	res := tx.Where("device_id = ?", sender.SourceDeviceID).Limit(1).Find(&dev)
	if res.Error != nil || res.RowsAffected == 0 {
		return nil
	}
	id := dev.ID
	return &id
}

func (p *Pipeline) resolveOwnerDeptID(ctx context.Context, sender Sender) uint {
	if sender.SourceDeviceID == "" {
		return 0
	}
	var dev gbmodels.GbDevice
	res := p.db.WithContext(ctx).Where("device_id = ?", sender.SourceDeviceID).Limit(1).Find(&dev)
	if res.Error != nil || res.RowsAffected == 0 {
		return 0
	}
	return dev.OwnerDeptID
}

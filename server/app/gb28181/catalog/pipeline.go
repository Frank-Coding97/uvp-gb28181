// Package catalog pipeline 编排入库(全量 + 增量)
package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
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
	for _, it := range items {
		if it.DeviceID == "" {
			continue
		}
		if err := p.ingestOne(ctx, sender, it); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (p *Pipeline) ingestOne(ctx context.Context, sender Sender, it CatalogItem) error {
	return p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		cls := Classify(it.DeviceID)

		// 1. 行政区链(优先 item.CivilCode,没有则回退 classify 结果)
		var civilNode *gbmodels.GbCatalogNode
		if it.CivilCode != "" {
			n, err := findOrCreateCivilCodeChain(tx, sender.OwnerDeptID, it.CivilCode)
			if err != nil {
				return err
			}
			civilNode = n
		} else if cls.CivilCode != "" {
			n, err := findOrCreateCivilCodeChain(tx, sender.OwnerDeptID, cls.CivilCode)
			if err != nil {
				return err
			}
			civilNode = n
		}

		parentNode, err := resolveBusinessParent(tx, sender, it, civilNode)
		if err != nil {
			return err
		}
		return ingestByType(ctx, tx, sender, it, cls, parentNode)
	})
}

// resolveBusinessParent 选择业务父节点。2022 ParentID 可为 A/B;目录树只能
// 选择一个展示父节点,完整关系由资源关系表保留
func resolveBusinessParent(tx *gorm.DB, sender Sender, it CatalogItem, civilNode *gbmodels.GbCatalogNode) (*gbmodels.GbCatalogNode, error) {
	parentNode := civilNode
	for _, parentCode := range SplitParentIDs(it.ParentID) {
		if parentCode == it.DeviceID {
			continue
		}
		var existingParent gbmodels.GbCatalogNode
		found := tx.Where("owner_dept_id = ? AND code = ?", sender.OwnerDeptID, parentCode).
			Order("id").Limit(1).Find(&existingParent)
		if found.Error != nil {
			return nil, found.Error
		}
		if found.RowsAffected == 1 {
			parentNode = &existingParent
			break
		}
		pCls := Classify(parentCode)
		switch pCls.NodeType {
		case gbmodels.NodeTypeBizGroup, gbmodels.NodeTypeVirtualOrg:
			pn, err := findOrCreateNode(
				tx, sender.OwnerDeptID, pCls.NodeType, parentCode,
				civilNodeID(civilNode), civilNodePath(civilNode), parentCode,
			)
			if err != nil {
				return nil, err
			}
			parentNode = pn
		case gbmodels.NodeTypeDevice:
			// device 父:让通道挂在设备节点下(NVR 下的子通道)
			// 但本期为简化,通道直接挂行政区,设备节点单独建
			// device 节点的具体 upsert 由 channel 上报路径推断;此处跳过
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
		node, _, err := upsertDevice(ctx, tx, sender.OwnerDeptID, it, cls, parentNode)
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
		node, err := findOrCreateNode(
			tx, sender.OwnerDeptID, cls.NodeType, it.DeviceID,
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
		if err := tx.Where("code = ? AND owner_dept_id = ?", code, sender.OwnerDeptID).Find(&nodes).Error; err != nil {
			return err
		}
		for _, node := range nodes {
			if node.ChannelID != nil {
				parentNodeID := uint(0)
				if node.ParentID != nil {
					parentNodeID = *node.ParentID
				}
				if err := tx.Where("channel_id = ? AND parent_node_id = ? AND owner_dept_id = ?", *node.ChannelID, parentNodeID, sender.OwnerDeptID).
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

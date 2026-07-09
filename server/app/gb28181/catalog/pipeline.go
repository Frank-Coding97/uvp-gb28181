// Package catalog pipeline 编排入库(全量 + 增量)
package catalog

import (
	"context"
	"errors"

	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// Pipeline 入库管道(plan §3 / A3 核心)
//
// 责任:
//   - Ingest      — 一批 CatalogItem 全量入库(manscdp 解析后调用)
//   - IngestDelta — Subscribe NOTIFY 单条事件入库(Add / Update / Del)
//
// 实现策略:
//   - 单事务处理一批(失败回滚)
//   - 节点未找到时 find-or-create(物化路径回填)
//   - 多挂载通过 gb_channel_mount 表
//   - anomaly 编码 → 兜底 virtual_org + 写审计
type Pipeline struct {
	db *gorm.DB
}

// New 构造 Pipeline
func New(db *gorm.DB) *Pipeline {
	return &Pipeline{db: db}
}

// Ingest 全量入库一批 CatalogItem(manscdp 解析后调用)
//
// sender:来源元数据(tenant_id + 上报设备国标编码)
// items:本批通道/设备列表
//
// 错误返回:第一个失败的 item 错误;不全部停下(失败的跳过,继续后续)
// 这种设计让 anomaly 单条不阻塞整批入库
func (p *Pipeline) Ingest(ctx context.Context, sender Sender, items []CatalogItem) error {
	if len(items) == 0 {
		return nil
	}
	if sender.TenantID == 0 {
		// 兼容:本期未严格落多租户,默认 tenant 1
		sender.TenantID = 1
	}

	var firstErr error
	for _, it := range items {
		if it.DeviceID == "" {
			continue
		}
		if err := p.ingestOne(ctx, sender, it); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			// 继续,不停
		}
	}
	return firstErr
}

// ingestOne 单条入库(独立事务,失败不影响其他)
func (p *Pipeline) ingestOne(ctx context.Context, sender Sender, it CatalogItem) error {
	return p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		cls := Classify(it.DeviceID)

		// 1. 行政区码节点链(始终建,即便没有 device/channel)
		var civilNode *gbmodels.GbCatalogNode
		if it.CivilCode != "" {
			n, err := findOrCreateCivilCodeChain(tx, sender.TenantID, it.CivilCode)
			if err != nil {
				return err
			}
			civilNode = n
		} else if cls.CivilCode != "" {
			n, err := findOrCreateCivilCodeChain(tx, sender.TenantID, cls.CivilCode)
			if err != nil {
				return err
			}
			civilNode = n
		}

		// 2. 业务父节点(ParentID hint;若是 biz_group / virtual_org 走另外建)
		var parentNode *gbmodels.GbCatalogNode = civilNode
		if it.ParentID != "" && it.ParentID != it.DeviceID {
			pCls := Classify(it.ParentID)
			switch pCls.NodeType {
			case gbmodels.NodeTypeBizGroup, gbmodels.NodeTypeVirtualOrg:
				// 找/建 上级 biz_group / virtual_org 节点
				pn, err := findOrCreateNode(tx, sender.TenantID, pCls.NodeType, it.ParentID, civilNodeID(civilNode), civilNodePath(civilNode), it.ParentID)
				if err != nil {
					return err
				}
				parentNode = pn
			case gbmodels.NodeTypeDevice:
				// device 父:让通道挂在设备节点下(NVR 下的子通道)
				// 但本期为简化,通道直接挂行政区,设备节点单独建
				// device 节点的具体 upsert 由 channel 上报路径推断;此处跳过
			}
		}

		// 3. 根据当前 item 类型走不同路径
		switch cls.NodeType {
		case gbmodels.NodeTypeChannel:
			node, _, err := upsertChannel(ctx, tx, sender.TenantID, sender.SourceDeviceID, it, cls, parentNode)
			if err != nil {
				return err
			}
			if cls.Anomaly {
				return recordAnomaly(ctx, tx, sender.TenantID, node, cls, lookupSourceDeviceID(tx, sender))
			}
		case gbmodels.NodeTypeDevice:
			node, _, err := upsertDevice(ctx, tx, sender.TenantID, it, cls, parentNode)
			if err != nil {
				return err
			}
			if cls.Anomaly {
				return recordAnomaly(ctx, tx, sender.TenantID, node, cls, lookupSourceDeviceID(tx, sender))
			}
		case gbmodels.NodeTypeBizGroup, gbmodels.NodeTypeVirtualOrg:
			node, err := findOrCreateNode(tx, sender.TenantID, cls.NodeType, it.DeviceID, civilNodeID(parentNode), civilNodePath(parentNode), fallbackName(it.Name, it.DeviceID))
			if err != nil {
				return err
			}
			if cls.Anomaly {
				return recordAnomaly(ctx, tx, sender.TenantID, node, cls, lookupSourceDeviceID(tx, sender))
			}
		case gbmodels.NodeTypeCivilCode:
			// 已在第 1 步处理;跳过
		default:
			return errors.New("catalog: unknown node type")
		}
		return nil
	})
}

// IngestDelta Subscribe NOTIFY 单条增量入库(G1 task 调用入口)
//
// action:add / update / del(GB/T 28181 §11.5.3)
// del:软删(deleted_at),不真删
func (p *Pipeline) IngestDelta(ctx context.Context, sender Sender, action string, it CatalogItem) error {
	switch action {
	case "ADD", "add":
		return p.Ingest(ctx, sender, []CatalogItem{it})
	case "UPDATE", "update":
		// 当前 ingestOne 自带 upsert 行为;UPDATE = ingest
		return p.Ingest(ctx, sender, []CatalogItem{it})
	case "DEL", "del":
		return p.softDelete(ctx, sender, it.DeviceID)
	}
	return errors.New("catalog: unknown delta action: " + action)
}

// softDelete 软删节点 + 关联(deleted_at)
func (p *Pipeline) softDelete(ctx context.Context, sender Sender, code string) error {
	if code == "" {
		return nil
	}
	return p.db.WithContext(ctx).
		Where("tenant_id = ? AND code = ?", sender.TenantID, code).
		Delete(&gbmodels.GbCatalogNode{}).Error
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

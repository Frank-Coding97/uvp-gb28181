package catalog

import (
	"context"
	"time"

	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// recordAnomaly 标记节点为 anomaly + 写 gb_anomaly_record
//
// 调用方:Pipeline.Ingest 在 classify 结果 anomaly=true 时
func recordAnomaly(
	ctx context.Context,
	db *gorm.DB,
	tenantID uint,
	node *gbmodels.GbCatalogNode,
	cls Classification,
	sourceDevID *uint,
) error {
	// 1. 节点标记 anomaly + raw_code
	updates := map[string]any{
		"anomaly":        true,
		"anomaly_reason": cls.Reason,
		"raw_code":       cls.RawCode,
	}
	if err := db.WithContext(ctx).Model(node).Updates(updates).Error; err != nil {
		return err
	}
	// 同步内存对象,方便后续逻辑判断
	node.Anomaly = true
	node.AnomalyReason = cls.Reason
	node.RawCode = cls.RawCode

	// 2. 写审计记录(每次入库都追加一条;后续 resolve 走 anomaly handler)
	rec := &gbmodels.GbAnomalyRecord{
		TenantID:       tenantID,
		CatalogNodeID:  node.ID,
		RawCode:        cls.RawCode,
		FallbackType:   gbmodels.FallbackTypeVirtualOrg,
		Reason:         cls.Reason,
		Resolved:       false,
		SourceDeviceID: sourceDevID,
		CreatedAt:      time.Now(),
	}
	return db.WithContext(ctx).Create(rec).Error
}

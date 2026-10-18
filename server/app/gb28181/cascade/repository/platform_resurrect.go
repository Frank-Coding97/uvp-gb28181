package repository

import (
	"fmt"

	"gorm.io/gorm"

	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
)

// platformClaim 是"这一行该落在哪儿"的处理方案。
//
//	take  —— 接手的已删遗留行(复用它的主键与遗留投影);nil 表示直接 INSERT。
//	purge —— 必须真删的已删遗留行(它占着另一个键位);nil 表示没有。
type platformClaim struct {
	take  *model.GbCascadePlatform
	purge *model.GbCascadePlatform
}

// findRetiredPlatform 找出被软删、但仍然占着唯一键位的遗留平台行,并给出处理方案。
//
// gb_cascade_platform 的两个唯一索引都不含 deleted_at:
//
//	uk_cascade_platform_name       -> (name)
//	uk_cascade_platform_connection -> (local_device_id, local_domain,
//	                                   upstream_server_id, host, port, transport)
//
// 而 SoftDeletePlatform 走的是软删(行还在),于是删掉一个平台之后,重建同名、或
// 接入关系完全相同的平台,INSERT 会直接撞 1062,接口回 409
// 「平台名称或上级接入关系已存在」—— 用户明明刚刚亲手把它删掉。
//
// 这跟投影表 uk_cascade_*_published 那个坑是同一个反模式,处理方式也一致:
// 建之前先把遗留行认回来(见 resurrectPlatform),而不是让唯一索引把用户挡在门外。
// 投影表那边见 projection_retire.go,两处刻意保持同一套口径。
//
// 两个键位分别命中两条不同的遗留行时(用户在 A 行用过的名字 + B 行用过的接入关系,
// 两条都已删):接手接入关系那条,并把名称那条连残留子表一起真删 —— 键位腾不干净
// 就等于把用户关在门外,而这两条行在界面上都看不见,他没有任何入口可点。
//
// ⭐ 为什么取接入关系那条而不是名称那条:六元组才是"这个平台对接谁"的身份,复用它的
// 主键就保住了它遗留的投影行(上级看到的 published id 不会因为这次改动而抖动);
// 名称只是个标签,被它占着的那条已删行的映射反正已经 active=false,真删代价最小。
func findRetiredPlatform(tx *gorm.DB, row *model.GbCascadePlatform) (*platformClaim, error) {
	byName, err := findRetiredPlatformByName(tx, row.Name)
	if err != nil {
		return nil, err
	}
	byConnection, err := findRetiredPlatformByConnection(tx, row)
	if err != nil {
		return nil, err
	}
	switch {
	case byName == nil && byConnection == nil:
		return nil, nil
	case byName != nil && byConnection != nil && byName.ID != byConnection.ID:
		return &platformClaim{take: byConnection, purge: byName}, nil
	case byName != nil:
		return &platformClaim{take: byName}, nil
	default:
		return &platformClaim{take: byConnection}, nil
	}
}

// findRetiredPlatformByName 按名称键位找遗留行。
//
// 必须 Unscoped 才能看见软删行;判空看 RowsAffected,因为全局 gorm hook 会吞掉
// ErrRecordNotFound(与 FindPlatform 同一条约定,别用 First 的 err 判空)。
func findRetiredPlatformByName(tx *gorm.DB, name string) (*model.GbCascadePlatform, error) {
	if name == "" {
		return nil, nil
	}
	var row model.GbCascadePlatform
	result := tx.Unscoped().Where("name = ? AND deleted_at IS NOT NULL", name).Order("id").Limit(1).Find(&row)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return &row, nil
}

// findRetiredPlatformByConnection 按接入关系键位的六个列找遗留行。
// 列的顺序与 uk_cascade_platform_connection 的 priority 一致,便于对照。
func findRetiredPlatformByConnection(tx *gorm.DB, row *model.GbCascadePlatform) (*model.GbCascadePlatform, error) {
	var found model.GbCascadePlatform
	result := tx.Unscoped().
		Where("local_device_id = ? AND local_domain = ? AND upstream_server_id = ? AND host = ? AND port = ? AND transport = ? AND deleted_at IS NOT NULL",
			row.LocalDeviceID, row.LocalDomain, row.UpstreamServerID, row.Host, row.Port, row.Transport).
		Order("id").Limit(1).Find(&found)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return &found, nil
}

// resurrectPlatform 复用那条遗留行,把它的主键接手过来。
//
// 复用 id 而不是"硬删遗留行再 INSERT",是为了保住该平台已有的投影行
// (gb_cascade_*_projection.platform_id 指向它):SoftDeletePlatform 只把投影置
// active=false、并不删行,上级看到的 published id 因此保持不变,用户重新勾选
// 共享时走的是认领更新,而不是重新分配一遍号段。
//
// 运行时事实必须清空:注册/心跳/最近一次错误都属于那个已经被删掉的平台,
// 新平台还没有发生过任何一次交互,带着旧值会在界面上显示成"已注册"的假象。
//
// 创建时间用新建时的时间,不继承旧值 —— 用户是在新建,界面上的"创建时间"
// 应当是他这一次操作的时间,主键复用只是实现细节。
func resurrectPlatform(tx *gorm.DB, desired, retired *model.GbCascadePlatform) error {
	desired.ID = retired.ID
	desired.RegisterAt = nil
	desired.RegisterExpiresAt = nil
	desired.HeartbeatAt = nil
	desired.LastErrorCode = ""
	desired.LastErrorMessage = ""
	desired.LastErrorAt = nil
	// 投影修订号保留:SoftDeletePlatform 已经为它递增过一次(投影被置为 inactive),
	// 保留这个值才不会因为修订号回退,让更旧的共享快照意外匹配上。
	desired.ProjectionRevision = retired.ProjectionRevision
	// Save 对非零主键执行"更新所有字段"(含零值),配合 Unscoped 才能把
	// deleted_at 写回 NULL —— 少了 Unscoped,软删过滤会让这次 UPDATE 命中 0 行。
	return tx.Unscoped().Save(desired).Error
}

// purgeRetiredPlatform 物理删除一条"已删除"的平台遗留行,连同它名下的残留子表。
//
// 为什么必须真删,而不是留着:
//
//	两个唯一索引都不含 deleted_at,这行只要还在,它占着的名称/接入关系就永远腾不出来;
//	而它在界面上完全不可见 —— 用户没有任何入口可点,只能换名字或手工清库。
//	(2026-10-14 现场:dev 库攒到 6 条遗留行,把最后一个活着的平台删掉之后,新建必 409。)
//
// 它名下的投影/业务节点对当前系统已经没有对外效力:SoftDeletePlatform 在软删那一步
// 已经把投影置成 active=false、并删掉了业务节点与绑定,上级那边的目录也撤过了。
//
// 真删唯一的代价是释放它遗留的 published id 映射 —— 将来重建这条平台时会重新分配一遍
// 号段(而不是认领旧号位)。这是"让用户能建出他要的那条平台"必须付的代价,而且被释放的
// 映射属于一条已经被删掉的平台。
//
// ⛔ `deleted_at IS NOT NULL` 是硬约束:这个函数只碰软删行,永远不可能删掉活着的平台。
// 命中 0 行说明这行已经被别的请求认领/复活了,必须中止 —— 否则接下来必然撞唯一键。
//
// gb_cascade_media_session 不动:它是历史记录、不占任何平台相关的唯一键,
// 与 SoftDeletePlatform 保留它的口径一致。
func purgeRetiredPlatform(tx *gorm.DB, row *model.GbCascadePlatform) error {
	if row == nil || row.ID == 0 {
		return nil
	}
	if err := tx.Where("platform_id = ?", row.ID).Delete(&model.GbCascadeBusinessBinding{}).Error; err != nil {
		return err
	}
	if err := tx.Where("platform_id = ?", row.ID).Delete(&model.GbCascadeBusinessNode{}).Error; err != nil {
		return err
	}
	// 投影表自己也有 deleted_at(软删),必须 Unscoped 硬删:留着照样占死
	// uk_cascade_*_published(与 projection_retire.go 同一条理由)。
	// 顺序是"先通道后设备":通道行通过 device_projection_id 挂在设备行上,
	// 先删设备行不会报错,但会让这些通道行变成新的孤儿。
	if err := tx.Unscoped().Where("platform_id = ?", row.ID).Delete(&model.GbCascadeChannelProjection{}).Error; err != nil {
		return err
	}
	if err := tx.Unscoped().Where("platform_id = ?", row.ID).Delete(&model.GbCascadeDeviceProjection{}).Error; err != nil {
		return err
	}
	result := tx.Unscoped().Where("id = ? AND deleted_at IS NOT NULL", row.ID).Delete(&model.GbCascadePlatform{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: 待清理的平台遗留行 id=%d 已不是软删状态", ErrRevisionConflict, row.ID)
	}
	return nil
}

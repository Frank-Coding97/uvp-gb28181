package repository

import (
	"fmt"

	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/model"
)

// findRetiredPlatform 找出被软删、但仍然占着唯一键位的遗留平台行。
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
// 建之前先把遗留行认回来(见 Resurrect),而不是让唯一索引把用户挡在门外。
// 投影表那边见 projection_retire.go,两处刻意保持同一套口径。
//
// 两个键位分别命中两条不同的遗留行时无解:复活任意一条,另一个键位仍然被占着。
// 这种情况如实报 ErrRetiredPlatformConflict,而不是随便挑一条把冲突留到下一次。
func findRetiredPlatform(tx *gorm.DB, row *model.GbCascadePlatform) (*model.GbCascadePlatform, error) {
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
		return nil, fmt.Errorf("%w: name=%q 与接入关系分别被两条已删除的平台占用", ErrRetiredPlatformConflict, row.Name)
	case byName != nil:
		return byName, nil
	default:
		return byConnection, nil
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

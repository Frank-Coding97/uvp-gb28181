package models

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/models"
)

// 设备在线状态
const (
	DeviceStatusOffline int8 = 0 // 离线
	DeviceStatusOnline  int8 = 1 // 在线
)

// SubscribeCapability 订阅能力(Q4 决议:智能升降级状态机)
// unknown:首次注册,未尝试
// subscribed:SUBSCRIBE Catalog 成功,等 NOTIFY 推送
// fallback:SUBSCRIBE 不支持,降级到 30min 主动 Query 兜底
type SubscribeCapability string

const (
	SubscribeUnknown    SubscribeCapability = "unknown"
	SubscribeSubscribed SubscribeCapability = "subscribed"
	SubscribeFallback   SubscribeCapability = "fallback"
)

// ProtocolVersion is the normalized wire profile used by a new operation.
// The values intentionally match the public device API and the protocol
// resolver so database rows can be inspected without decoding XML.
const (
	ProtocolVersion2016           = "2016"
	ProtocolVersion2022           = "2022"
	ProtocolOverrideAuto          = "auto"
	ProtocolVersionSourceRegister = "register"
	ProtocolVersionSourceOverride = "override"
	ProtocolVersionSourceHistory  = "history"
	ProtocolVersionSourceDefault  = "default"
)

// GbDevice 国标设备模型(注册/心跳主体)
// 在线模型:keepalive_time + keepalive_interval 是事实真相,status 是物化缓存(由事实派生)
type GbDevice struct {
	models.BaseModel
	DeviceID          string     `gorm:"column:device_id;size:20;uniqueIndex;comment:20位国标编码" json:"deviceId"`
	Name              string     `gorm:"column:name;size:255;comment:设备名称(设备自上报)" json:"name"`
	Alias             string     `gorm:"column:alias;size:255;default:'';comment:用户自定义别名(不被上报覆盖)" json:"alias"`
	Password          string     `gorm:"column:password;size:255;comment:按设备独立密码(本期用统一密码,留空)" json:"-"`
	Transport         string     `gorm:"column:transport;size:8;comment:传输模式 UDP/TCP" json:"transport"`
	Manufacturer      string     `gorm:"column:manufacturer;size:255;comment:厂商" json:"manufacturer"`
	Model             string     `gorm:"column:model;size:255;comment:型号" json:"model"`
	Firmware          string     `gorm:"column:firmware;size:255;comment:固件版本" json:"firmware"`
	IP                string     `gorm:"column:ip;size:64;comment:设备来源IP" json:"ip"`
	Port              int        `gorm:"column:port;comment:设备来源端口" json:"port"`
	RegisterTime      *time.Time `gorm:"column:register_time;comment:最近注册成功时间" json:"registerTime"`
	RegisterExpireAt  *time.Time `gorm:"column:register_expire_at;comment:注册到期时刻" json:"registerExpireAt"`
	KeepaliveTime     *time.Time `gorm:"column:keepalive_time;comment:【事实】最后心跳时间" json:"keepaliveTime"`
	KeepaliveInterval int        `gorm:"column:keepalive_interval;default:60;comment:【事实】期望心跳周期(秒)" json:"keepaliveInterval"`
	Expires           int        `gorm:"column:expires;comment:注册有效期(秒)" json:"expires"`
	Status            int8       `gorm:"column:status;default:0;comment:【物化缓存】在线状态 0离线 1在线" json:"status"`
	OfflineAt         *time.Time `gorm:"column:offline_at;comment:最近被判离线的时刻" json:"offlineAt"`
	CreatedBy         uint       `gorm:"column:created_by;comment:创建人" json:"createdBy"`
	OwnerDeptID       uint       `gorm:"column:owner_dept_id;comment:归属部门ID" json:"ownerDeptId"`
	// Subscribe Catalog 智能升降级(Q4 决议) — A1 加,G1 状态机落地
	SubscribeCapability SubscribeCapability `gorm:"column:subscribe_capability;size:16;default:unknown;index:idx_subscribe_capability,priority:1;comment:订阅能力 unknown/subscribed/fallback" json:"subscribeCapability"`
	SubscribeLastTest   *time.Time          `gorm:"column:subscribe_last_test;index:idx_subscribe_capability,priority:2;comment:最近一次 SUBSCRIBE 尝试" json:"subscribeLastTest"`
	SubscribeExpiresAt  *time.Time          `gorm:"column:subscribe_expires_at;comment:订阅过期时刻(提前续订)" json:"subscribeExpiresAt"`
	// Dual-version profile archive. These fields are nullable/loosely typed so
	// legacy device rows can be upgraded without inventing a 2022 declaration.
	ReportedVersion        string     `gorm:"column:reported_version;size:8;not null;default:'';comment:最近一次 X-GB-Ver 原始版本" json:"reportedVersion"`
	ReportedVersionAt      *time.Time `gorm:"column:reported_version_at;comment:最近一次 X-GB-Ver 时间" json:"reportedVersionAt"`
	ProtocolOverride       string     `gorm:"column:protocol_override;size:8;not null;default:auto;comment:协议版本覆盖 auto/2016/2022" json:"protocolOverride"`
	EffectiveVersion       string     `gorm:"column:effective_version;size:8;not null;default:2016;comment:当前生效协议版本" json:"effectiveVersion"`
	EffectiveVersionSource string     `gorm:"column:effective_version_source;size:16;not null;default:default;comment:生效版本来源" json:"effectiveVersionSource"`
	EffectiveVersionAt     *time.Time `gorm:"column:effective_version_at;comment:生效版本更新时间" json:"effectiveVersionAt"`
}

// IsOnlineByFact 从事实(keepalive_time)派生在线状态,不依赖 status 缓存字段
// threshold = keepalive_interval × timeoutCount + grace(秒)
func (d *GbDevice) IsOnlineByFact(timeoutCount, graceSeconds int) bool {
	if d.KeepaliveTime == nil {
		return false
	}
	interval := d.KeepaliveInterval
	if interval <= 0 {
		interval = 60
	}
	threshold := time.Duration(interval*timeoutCount+graceSeconds) * time.Second
	return time.Since(*d.KeepaliveTime) <= threshold
}

// TableName 表名
func (GbDevice) TableName() string {
	return "gb_device"
}

func NewGbDevice() *GbDevice {
	return &GbDevice{}
}

type GbDeviceList []*GbDevice

// FindByDeviceID 按国标编码查询设备,未命中返回 (nil, nil)
func FindByDeviceID(c context.Context, deviceID string) (*GbDevice, error) {
	return FindByDeviceIDWithDB(c, app.DB().WithContext(c), deviceID)
}

// FindByDeviceIDWithDB 查询设备，允许调用方把查询放进现有事务。
func FindByDeviceIDWithDB(c context.Context, db *gorm.DB, deviceID string) (*GbDevice, error) {
	var d GbDevice
	// 注意:底座注册了全局 hook MaskNotDataError(RaiseErrorOnNotFound=false),
	// 查不到时不会返回 ErrRecordNotFound,故用 RowsAffected 判断是否命中,不依赖 error
	result := db.WithContext(c).Where("device_id = ?", deviceID).Limit(1).Find(&d)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return &d, nil
}

// Upsert 自动建档:存在则更新,不存在则插入(以 device_id 为唯一键)
func Upsert(c context.Context, d *GbDevice) error {
	return UpsertWithDB(c, app.DB().WithContext(c), d)
}

// UpsertWithDB 自动建档/更新，允许调用方把写入放进现有事务。
func UpsertWithDB(c context.Context, db *gorm.DB, d *GbDevice) error {
	existing, err := FindByDeviceIDWithDB(c, db, d.DeviceID)
	if err != nil {
		return err
	}
	if existing == nil {
		return db.WithContext(c).Create(d).Error
	}
	d.ID = existing.ID
	return db.WithContext(c).Model(&GbDevice{}).Where("id = ?", existing.ID).Updates(d).Error
}

// UpdateStatus 更新设备在线状态(物化缓存)
func UpdateStatus(c context.Context, deviceID string, status int8) error {
	return app.DB().WithContext(c).Model(&GbDevice{}).
		Where("device_id = ?", deviceID).
		Update("status", status).Error
}

// TouchKeepalive 记录一次心跳；onlineOnHeartbeat 控制是否同步刷新 status 为在线。
// 返回 true 表示本次心跳让设备从离线恢复,在线恢复方据此重新拉取 Catalog。
func TouchKeepalive(c context.Context, deviceID string, onlineOnHeartbeat bool) (bool, error) {
	now := time.Now()
	db := app.DB().WithContext(c)
	var restored bool
	err := db.Transaction(func(tx *gorm.DB) error {
		var d GbDevice
		result := tx.Set("gorm:query_option", "FOR UPDATE").Where("device_id = ?", deviceID).Limit(1).Find(&d)
		if result.Error != nil || result.RowsAffected == 0 {
			return result.Error
		}
		if !onlineOnHeartbeat {
			return tx.Model(&GbDevice{}).Where("id = ?", d.ID).Update("keepalive_time", now).Error
		}
		metadata := StatusEventMetadata{IP: d.IP, Port: d.Port, Transport: d.Transport, KeepaliveInterval: intPtr(d.KeepaliveInterval)}
		if d.Status != DeviceStatusOnline {
			from := d.Status
			if err := tx.Model(&GbDevice{}).Where("id = ?", d.ID).Updates(map[string]interface{}{"keepalive_time": now, "status": DeviceStatusOnline}).Error; err != nil {
				return err
			}
			if err := RecordStatusEvent(tx, &d, DeviceEventHeartbeatRecovered, DeviceEventSourceKeepalive, &from, DeviceStatusOnline, now, metadata); err != nil {
				return err
			}
			restored = true
			return nil
		}
		return tx.Model(&GbDevice{}).Where("id = ?", d.ID).Update("keepalive_time", now).Error
	})
	return restored, err
}

// MarkOffline 置离线:设备与所属通道必须原子翻转,避免列表出现设备离线但通道在线。
// 通道重新上线必须等待设备恢复后的 Catalog ON/OFF,不能凭设备上线直接推断。
func MarkOffline(c context.Context, deviceID string) error {
	return MarkOfflineWithReason(c, deviceID, DeviceEventHeartbeatTimeout, DeviceEventSourceOfflineScanner)
}

// MarkOfflineWithReason 置离线并记录具体离线原因。
func MarkOfflineWithReason(c context.Context, deviceID string, eventType DeviceStatusEventType, source DeviceStatusEventSource) error {
	now := time.Now()
	return app.DB().WithContext(c).Transaction(func(tx *gorm.DB) error {
		var d GbDevice
		deviceQuery := tx.Set("gorm:query_option", "FOR UPDATE").Where("device_id = ?", deviceID).Limit(1).Find(&d)
		if deviceQuery.Error != nil {
			return deviceQuery.Error
		}
		if deviceQuery.RowsAffected == 0 {
			return fmt.Errorf("设备 %s 不存在,注销未更新状态", deviceID)
		}
		if d.Status == DeviceStatusOffline {
			return nil
		}
		if err := tx.Model(&GbDevice{}).Where("id = ?", d.ID).Updates(map[string]interface{}{"status": DeviceStatusOffline, "offline_at": now}).Error; err != nil {
			return err
		}
		channelUpdate := tx.Model(&GbChannel{}).
			Where("device_id = ? AND status <> ?", deviceID, ChannelStatusOffline).
			Update("status", ChannelStatusOffline)
		if channelUpdate.Error != nil {
			return channelUpdate.Error
		}
		metadata := StatusEventMetadata{IP: d.IP, Port: d.Port, Transport: d.Transport, KeepaliveInterval: intPtr(d.KeepaliveInterval)}
		from := d.Status
		return RecordStatusEvent(tx, &d, eventType, source, &from, DeviceStatusOffline, now, metadata)
	})
}

func intPtr(value int) *int { return &value }

// ListStaleOnline 查询 status=1(缓存在线)但心跳已超时的设备(扫描器用)
// 注意:阈值按设备各自 keepalive_interval 计算,故在 SQL 里用字段表达式,不能用全局常量
// cutoffBase = timeoutCount, grace = 宽限秒数
func ListStaleOnline(c context.Context, timeoutCount, graceSeconds int) (GbDeviceList, error) {
	var list GbDeviceList
	// keepalive_time < now - (keepalive_interval * timeoutCount + grace) 秒
	err := app.DB().WithContext(c).
		Where("status = ?", DeviceStatusOnline).
		Where("keepalive_time IS NOT NULL").
		Where("keepalive_time < DATE_SUB(NOW(), INTERVAL (keepalive_interval * ? + ?) SECOND)", timeoutCount, graceSeconds).
		Find(&list).Error
	return list, err
}

// ListOnline 查询所有在线设备
func ListOnline(c context.Context) (GbDeviceList, error) {
	var list GbDeviceList
	err := app.DB().WithContext(c).Where("status = ?", DeviceStatusOnline).Find(&list).Error
	return list, err
}

// ListPaged 分页查询设备列表,返回当页数据与总数
func ListPaged(c context.Context, page, pageSize int, scopes ...func(*gorm.DB) *gorm.DB) (GbDeviceList, int64, error) {
	var list GbDeviceList
	var total int64
	q := app.DB().WithContext(c).Model(&GbDevice{}).Scopes(scopes...)
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.
		Order("status DESC, register_time DESC, name DESC, id DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&list).Error
	return list, total, err
}

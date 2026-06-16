package models

import (
	"context"
	"time"

	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/models"
)

// 设备在线状态
const (
	DeviceStatusOffline int8 = 0 // 离线
	DeviceStatusOnline  int8 = 1 // 在线
)

// GbDevice 国标设备模型(注册/心跳主体)
// 在线模型:keepalive_time + keepalive_interval 是事实真相,status 是物化缓存(由事实派生)
type GbDevice struct {
	models.BaseModel
	DeviceID          string     `gorm:"column:device_id;size:20;uniqueIndex;comment:20位国标编码" json:"deviceId"`
	Name              string     `gorm:"column:name;size:255;comment:设备名称" json:"name"`
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
	TenantID          uint       `gorm:"column:tenant_id;comment:租户ID" json:"tenantId"`
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
	var d GbDevice
	// 注意:底座注册了全局 hook MaskNotDataError(RaiseErrorOnNotFound=false),
	// 查不到时不会返回 ErrRecordNotFound,故用 RowsAffected 判断是否命中,不依赖 error
	result := app.DB().WithContext(c).Where("device_id = ?", deviceID).Limit(1).Find(&d)
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
	existing, err := FindByDeviceID(c, d.DeviceID)
	if err != nil {
		return err
	}
	if existing == nil {
		return app.DB().WithContext(c).Create(d).Error
	}
	d.ID = existing.ID
	return app.DB().WithContext(c).Model(&GbDevice{}).Where("id = ?", existing.ID).Updates(d).Error
}

// UpdateStatus 更新设备在线状态(物化缓存)
func UpdateStatus(c context.Context, deviceID string, status int8) error {
	return app.DB().WithContext(c).Model(&GbDevice{}).
		Where("device_id = ?", deviceID).
		Update("status", status).Error
}

// TouchKeepalive 记录一次心跳:更新 keepalive_time 事实 + 刷新 status 缓存为在线
func TouchKeepalive(c context.Context, deviceID string) error {
	now := time.Now()
	return app.DB().WithContext(c).Model(&GbDevice{}).
		Where("device_id = ?", deviceID).
		Updates(map[string]interface{}{
			"keepalive_time": now,
			"status":         DeviceStatusOnline,
		}).Error
}

// MarkOffline 置离线:翻转 status 缓存 + 记录 offline_at(为离线事件铺路)
func MarkOffline(c context.Context, deviceID string) error {
	now := time.Now()
	return app.DB().WithContext(c).Model(&GbDevice{}).
		Where("device_id = ?", deviceID).
		Updates(map[string]interface{}{
			"status":     DeviceStatusOffline,
			"offline_at": now,
		}).Error
}

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
func ListPaged(c context.Context, page, pageSize int) (GbDeviceList, int64, error) {
	var list GbDeviceList
	var total int64
	if err := app.DB().WithContext(c).Model(&GbDevice{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := app.DB().WithContext(c).
		Order("id DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&list).Error
	return list, total, err
}

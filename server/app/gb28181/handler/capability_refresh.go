package handler

import (
	"context"
	"fmt"
	"sync/atomic"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.com/uvp-gb28181/app/gb28181/ptz"
	"uvplatform.com/uvp-gb28181/app/global/app"
)

// CapabilityRefresher 在一批 Catalog 完整入库后刷新设备能力。
type CapabilityRefresher interface {
	Refresh(context.Context, string, int)
}

type deviceCapabilityRefresher struct {
	db       *gorm.DB
	ptz      *ptz.Service
	workers  chan struct{}
	sequence atomic.Uint64
}

// NewCapabilityRefresher 创建设备上线能力刷新器。workers 限制跨通道下行并发，避免大设备瞬间占满 SIP 发送队列。
func NewCapabilityRefresher(db *gorm.DB, service *ptz.Service, workers int) CapabilityRefresher {
	if workers <= 0 {
		workers = 4
	}
	return &deviceCapabilityRefresher{db: db, ptz: service, workers: make(chan struct{}, workers)}
}

func (r *deviceCapabilityRefresher) Refresh(ctx context.Context, deviceCode string, catalogSN int) {
	if r == nil || r.db == nil || r.ptz == nil || deviceCode == "" {
		return
	}
	// A device commonly resets/reuses Catalog SN. The completed Catalog batch
	// is the refresh boundary, so every invocation must get a fresh idempotency
	// token; SN remains only the protocol correlation number.
	batch := r.sequence.Add(1)
	key := fmt.Sprintf("%s:%d:%d", deviceCode, catalogSN, batch)
	if ctx == nil {
		ctx = context.Background()
	}
	scope := context.WithoutCancel(ctx)
	app.BackgroundWork.Go(func() {
		var device struct {
			gbmodels.GbDevice
			AccessEpoch int64 `gorm:"column:access_epoch"`
		}
		if result := r.db.WithContext(scope).Where("device_id = ?", deviceCode).Limit(1).Find(&device); result.Error != nil || result.RowsAffected != 1 {
			return
		}
		base := ptz.Target{DeviceEpoch: device.AccessEpoch, DeviceID: uint(device.ID), DeviceCode: device.DeviceID, IP: device.IP, Port: device.Port, Transport: device.Transport,
			DeviceOnline: device.Status == gbmodels.DeviceStatusOnline, Profile: protocol.ProfileFor(protocol.Version2016)}
		if device.EffectiveVersion == gbmodels.ProtocolVersion2022 {
			base.Profile = protocol.ProfileFor(protocol.Version2022)
		}
		if _, err := r.ptz.ReadDeviceConfigs(scope, base, []string{manscdp.ConfigTypeBasicParam}, 0, 0, "capability:basic:"+key); err != nil {
			app.Log(scope).Named("gb28181.capability").Warn("BasicParam 能力查询未排队", zap.String("device_id", deviceCode), zap.Error(err))
		}

		var channels []gbmodels.GbChannel
		if result := r.db.WithContext(scope).Where("device_id = ? AND deleted_at IS NULL", deviceCode).Order("id").Find(&channels); result.Error != nil {
			return
		}
		for _, channel := range channels {
			r.workers <- struct{}{}
			ch := channel
			app.BackgroundWork.Go(func() {
				defer func() { <-r.workers }()
				target := base
				target.ChannelID, target.ChannelCode = ch.ID, ch.ChannelID
				// 能力查询针对设备当前注册链路；Catalog 中的 OFF 只表示当前媒体状态，
				// 不应阻止读取该通道的 VideoParamOpt。
				target.ChannelOnline = true
				if _, err := r.ptz.ReadVideoParamOptions(scope, target, 0, 0, "capability:video-param-opt:"+key+":"+ch.ChannelID); err != nil {
					app.Log(scope).Named("gb28181.capability").Warn("VideoParamOpt 能力查询未排队", zap.String("device_id", deviceCode), zap.String("channel_id", ch.ChannelID), zap.Error(err))
				}
			})
		}
	})
}

package handler

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.com/uvp-gb28181/app/gb28181/ptz"
	"uvplatform.com/uvp-gb28181/app/global/app"
)

const (
	// capabilityFreshFor 决定"多久之内不再重复读同一条通道的能力"。
	//
	// ⛔ 这是去重的**唯一**依据，不能用"多久没触发过刷新"来代替：
	// Catalog 是分批下发的，一批完成就会触发一次全设备刷新，而 `VideoParamOpt`
	// 这种能力块十分钟内不会变。按时间窗粗暴合并会漏掉后几批才入目录的通道。
	capabilityFreshFor = 10 * time.Minute

	// capabilityEnqueueBatch / capabilityEnqueueInterval 控制对**同一台设备**的入队节奏。
	//
	// 67 通道的设备一次上线要读 68 条配置；SIP 发送槽位全局 16 个、每条还要等设备
	// 回 200 OK（实测约 2 秒）。一口气全排进去，设备侧会同时收到十几个
	// ConfigDownload，既拖长应答时间也把后面的指令堵在队列里。
	capabilityEnqueueBatch    = 4
	capabilityEnqueueInterval = 800 * time.Millisecond

	// capabilitySettleDelay 是"补齐一轮"之前的静默期，等 Catalog 分页发完再跑第二遍。
	capabilitySettleDelay = 3 * time.Second

	// capabilityMaxFollowUpRound 是一次触发最多跑几轮（首轮 + 补轮）。
	// 触发密集时靠它兜底，避免后台任务被反复续命。
	capabilityMaxFollowUpRound = 3
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

	// mu 保护 running/pending：同一台设备同时只跑一轮，轮内再来的触发只置一个
	// "待补一轮"标记，等这一轮结束后合并执行一次。⛔ 不能直接丢弃——
	// 分批 Catalog 的后几批通道是新入目录的，丢掉就永远读不到能力。
	mu      sync.Mutex
	running map[string]struct{}
	pending map[string]struct{}
}

// NewCapabilityRefresher 创建设备上线能力刷新器。workers 限制跨通道下行并发，避免大设备瞬间占满 SIP 发送队列。
func NewCapabilityRefresher(db *gorm.DB, service *ptz.Service, workers int) CapabilityRefresher {
	if workers <= 0 {
		workers = 4
	}
	return &deviceCapabilityRefresher{
		db: db, ptz: service, workers: make(chan struct{}, workers),
		running: make(map[string]struct{}), pending: make(map[string]struct{}),
	}
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

	if !r.begin(deviceCode) {
		return
	}
	app.BackgroundWork.Go(func() {
		defer r.finish(deviceCode)
		for round := 0; ; round++ {
			r.refreshDevice(scope, deviceCode, roundKey(key, round), round)
			if !r.consumePending(deviceCode) || round+1 >= capabilityMaxFollowUpRound {
				return
			}
			// 分页 Catalog 的最后几批可能刚落地，等一拍再补一轮；
			// 这一轮会被 FreshVideoParamChannels 滤成"只补没读到的通道"。
			if !sleepContext(scope, capabilitySettleDelay) {
				return
			}
		}
	})
}

func roundKey(key string, round int) string {
	if round == 0 {
		return key
	}
	return fmt.Sprintf("%s:follow-up-%d", key, round)
}

// begin 抢占设备级"正在刷新"标；已在跑则只登记一次待补。
func (r *deviceCapabilityRefresher) begin(deviceCode string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, running := r.running[deviceCode]; running {
		r.pending[deviceCode] = struct{}{}
		return false
	}
	r.running[deviceCode] = struct{}{}
	return true
}

// consumePending 在一轮结束后取走待补标记（有则说明期间又来了一次触发）。
func (r *deviceCapabilityRefresher) consumePending(deviceCode string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.pending[deviceCode]
	delete(r.pending, deviceCode)
	return ok
}

func (r *deviceCapabilityRefresher) finish(deviceCode string) {
	r.mu.Lock()
	delete(r.running, deviceCode)
	_, again := r.pending[deviceCode]
	delete(r.pending, deviceCode)
	r.mu.Unlock()
	// 收尾这一刻才到的触发不能被丢掉（多批 Catalog 的尾巴都在这里）：
	// 它已经错过本轮，直接重新排一轮；去重集合会让这一轮只补没读到的通道。
	if again {
		r.Refresh(context.Background(), deviceCode, 0)
	}
}

func (r *deviceCapabilityRefresher) refreshDevice(scope context.Context, deviceCode, key string, round int) {
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
	// 后台批量读取用放宽的排队窗口：设备级 BasicParam 只查一次，直接走后台窗口，
	// 免得它排在一串通道查询后面被 5 秒窗口判死。
	// 补轮（round>0）只为捞后几批新入目录的通道，设备级能力不再重复问。
	if round == 0 {
		if _, err := r.ptz.ReadDeviceConfigsBackground(scope, base, []string{manscdp.ConfigTypeBasicParam}, 0, 0, "capability:basic:"+key); err != nil {
			app.Log(scope).Named("gb28181.capability").Warn("BasicParam 能力查询未排队", zap.String("device_id", deviceCode), zap.Error(err))
		}
	}

	var channels []gbmodels.GbChannel
	if result := r.db.WithContext(scope).Where("device_id = ? AND deleted_at IS NULL", deviceCode).Order("id").Find(&channels); result.Error != nil {
		return
	}
	fresh, err := r.ptz.FreshVideoParamChannels(scope, deviceCode, time.Now().Add(-capabilityFreshFor))
	if err != nil {
		// 去重查询失败不影响"该读还是要读"，只是可能重复读一遍。
		app.Log(scope).Named("gb28181.capability").Warn("读取能力去重集合失败", zap.String("device_id", deviceCode), zap.Error(err))
		fresh = map[string]struct{}{}
	}
	pending := pendingCapabilityChannels(channels, fresh)
	if len(pending) == 0 {
		return
	}
	for start := 0; start < len(pending); start += capabilityEnqueueBatch {
		end := start + capabilityEnqueueBatch
		if end > len(pending) {
			end = len(pending)
		}
		var wg sync.WaitGroup
		for _, channel := range pending[start:end] {
			r.workers <- struct{}{}
			ch := channel
			wg.Add(1)
			app.BackgroundWork.Go(func() {
				defer func() { <-r.workers; wg.Done() }()
				target := base
				target.ChannelID, target.ChannelCode = ch.ID, ch.ChannelID
				// 能力查询针对设备当前注册链路；Catalog 中的 OFF 只表示当前媒体状态，
				// 不应阻止读取该通道的 VideoParamOpt。
				target.ChannelOnline = true
				if _, err := r.ptz.ReadDeviceConfigsBackground(scope, target, []string{manscdp.ConfigTypeVideoParamOpt}, 0, 0, "capability:video-param-opt:"+key+":"+ch.ChannelID); err != nil {
					app.Log(scope).Named("gb28181.capability").Warn("VideoParamOpt 能力查询未排队", zap.String("device_id", deviceCode), zap.String("channel_id", ch.ChannelID), zap.Error(err))
				}
			})
		}
		wg.Wait()
		if end < len(pending) && !sleepContext(scope, capabilityEnqueueInterval) {
			return
		}
	}
}

// pendingCapabilityChannels 过滤掉"刚读过能力"的通道，其余保持目录原顺序。
// 顺序必须稳定：同一批入队的报文顺序决定设备侧看到的下行顺序，便于比对抓包。
func pendingCapabilityChannels(channels []gbmodels.GbChannel, fresh map[string]struct{}) []gbmodels.GbChannel {
	pending := make([]gbmodels.GbChannel, 0, len(channels))
	for _, channel := range channels {
		if _, ok := fresh[channel.ChannelID]; ok {
			continue
		}
		pending = append(pending, channel)
	}
	return pending
}

// sleepContext 在 scope 被取消时立刻返回 false，避免后台任务取消后还空等。
func sleepContext(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

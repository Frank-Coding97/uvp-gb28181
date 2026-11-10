package handler

import (
	"context"
	"fmt"
	"sync"
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

	// capabilityWorkerWait 是"等一个下行并发槽位"的最长等待。
	//
	// ⛔ 原来是无超时的 `r.workers <- struct{}{}`：槽位里的活一旦卡住
	// （数据库慢、发送队列堵），4 个槽位占满后所有后续设备的刷新全部挂在这行上
	// 永久阻塞，而设备级 running 标又不会释放 ⇒ 后续触发全被判"已在刷新"直接丢弃。
	// 2026-11-09 万级压测的表现就是这样：10002 台里只有前 38 台真正读到了配置，
	// 剩下 99.6% 静默消失，一条告警都没有。
	capabilityWorkerWait = 30 * time.Second

	// capabilityRefreshBudget 是一次设备刷新（含补轮）的整体预算。
	//
	// 兜底作用：无论内部哪一步卡住，到点必须放手并释放 running 标，
	// 让下一批 Catalog 完成时还能重新触发。⛔ 不能设成"永久"。
	capabilityRefreshBudget = 5 * time.Minute

	// 万台 Catalog 同时完成时，先限制进入数据库和 PTZ 意图事务的设备数。
	// 等待时间不占用单台设备的执行预算。
	capabilityDeviceConcurrency   = 32
	capabilityDeviceAdmissionWait = 20 * time.Minute
)

// CapabilityRefresher 在一批 Catalog 完整入库后刷新设备能力。
type CapabilityRefresher interface {
	Refresh(context.Context, string, int)
}

type deviceCapabilityRefresher struct {
	db            *gorm.DB
	ptz           *ptz.Service
	workers       chan struct{}
	deviceWorkers chan struct{}

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
		deviceWorkers: make(chan struct{}, capabilityDeviceConcurrency),
		running:       make(map[string]struct{}), pending: make(map[string]struct{}),
	}
}

func (r *deviceCapabilityRefresher) Refresh(ctx context.Context, deviceCode string, catalogSN int) {
	if r == nil || r.db == nil || r.ptz == nil || deviceCode == "" {
		return
	}
	// ⛔⛔ 幂等键必须**跨进程重启**唯一。
	//
	// 设备侧的 Catalog SN 会重复（实测 62 上那台级联设备恒为 1），原来用
	// "进程内计数器"当批次号：计数器重启即归零 ⇒ 重启后与历史行撞键；撞上的旧行
	// 若目标身份已变（目录重建导致通道主键变了、access_epoch 变了），Execute 就判
	// 「幂等键已用于不同 PTZ 请求」，**整批能力读取一条都发不出去**，而且只留一条 WARN。
	// ⇒ 批次标识用纳秒时间戳（唯一且不依赖内存状态），SN 只作协议关联号。
	key := fmt.Sprintf("%s:%d:%d", deviceCode, catalogSN, time.Now().UnixNano())
	if ctx == nil {
		ctx = context.Background()
	}
	scope := context.WithoutCancel(ctx)

	if !r.begin(deviceCode) {
		return
	}
	admitted := app.BackgroundWork.Go(func() {
		worked := false
		defer func() {
			if worked {
				r.finish(deviceCode)
			} else {
				r.release(deviceCode)
			}
		}()
		worked = r.runDeviceRefresh(scope, func(budget context.Context) {
			for round := 0; ; round++ {
				r.refreshDevice(budget, deviceCode, roundKey(key, round), round)
				if !r.consumePending(deviceCode) || round+1 >= capabilityMaxFollowUpRound {
					return
				}
				// 分页 Catalog 的最后几批可能刚落地，等一拍再补一轮；
				// 这一轮会被 FreshVideoParamChannels 滤成"只补没读到的通道"。
				if !sleepContext(budget, capabilitySettleDelay) {
					return
				}
			}
		})
		if !worked {
			app.Log(scope).Named("gb28181.capability").Warn("等待设备能力刷新槽位超时",
				zap.String("event", "gb28181.capability.device_wait_timeout"), zap.String("device_id", deviceCode))
		}
	})
	if !admitted {
		// 停机中（BackgroundWork 已关闭准入）：fn 根本不会执行，
		// 不会走到上面的 defer ⇒ 必须在这里手动释放，否则该设备永远被判"正在刷新"。
		// ⛔ 这里**不能**调 finish：finish 见到 pending 会重新触发 Refresh，
		// 而准入已关 ⇒ 再次 admitted=false ⇒ 无限递归。
		r.release(deviceCode)
	}
}

func (r *deviceCapabilityRefresher) runDeviceRefresh(ctx context.Context, refresh func(context.Context)) bool {
	wait, cancelWait := context.WithTimeout(ctx, capabilityDeviceAdmissionWait)
	defer cancelWait()
	select {
	case r.deviceWorkers <- struct{}{}:
		defer func() { <-r.deviceWorkers }()
	case <-wait.Done():
		return false
	}
	budget, cancelBudget := context.WithTimeout(ctx, capabilityRefreshBudget)
	defer cancelBudget()
	refresh(budget)
	return true
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

// release 只清状态、不触发补轮。给"任务压根没被准入"的路径用。
func (r *deviceCapabilityRefresher) release(deviceCode string) {
	r.mu.Lock()
	delete(r.running, deviceCode)
	delete(r.pending, deviceCode)
	r.mu.Unlock()
}

func (r *deviceCapabilityRefresher) refreshDevice(scope context.Context, deviceCode, key string, round int) {
	var device struct {
		gbmodels.GbDevice
		AccessEpoch int64 `gorm:"column:access_epoch"`
	}
	if result := r.db.WithContext(scope).Where("device_id = ?", deviceCode).Limit(1).Find(&device); result.Error != nil || result.RowsAffected != 1 {
		return
	}
	if device.Status != gbmodels.DeviceStatusOnline {
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
			// ⛔ 必须带超时：无超时的话 4 个槽位一旦被卡住，
			// 后面所有设备都永久挂在这行且 running 标不释放（见 capabilityWorkerWait 注释）。
			if !acquireWorker(scope, r.workers, capabilityWorkerWait) {
				app.Log(scope).Named("gb28181.capability").Warn("等不到下行并发槽位，本轮跳过剩余通道",
					zap.String("event", "gb28181.capability.worker_wait_timeout"),
					zap.String("device_id", deviceCode),
					zap.String("reason_code", "capability_worker_wait_timeout"),
					zap.Duration("waited", capabilityWorkerWait),
					zap.String("likely_cause", "下行发送槽位被占满或某个配置读取长时间未返回；本轮放弃，等下一次 Catalog 完成再补"))
				wg.Wait()
				return
			}
			ch := channel
			wg.Add(1)
			admitted := app.BackgroundWork.Go(func() {
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
			if !admitted {
				// 停机中：fn 不会执行 ⇒ 手动归还槽位，否则 wg.Wait() 会永久挂住。
				<-r.workers
				wg.Done()
			}
		}
		wg.Wait()
		if end < len(pending) && !sleepContext(scope, capabilityEnqueueInterval) {
			return
		}
	}
}

// acquireWorker 在超时内抢一个下行并发槽位；ctx 取消/超时即返回 false。
func acquireWorker(ctx context.Context, workers chan struct{}, wait time.Duration) bool {
	select {
	case workers <- struct{}{}:
		return true
	default:
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case workers <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	case <-timer.C:
		return false
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

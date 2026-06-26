package metrics

import (
	"sync"
	"sync/atomic"
	"time"
)

// Aggregator 8 类事务计数 + ring buffer + SSE 订阅,所有指标存内存。
//
// 线程安全:
//   - 计数用 atomic.Int64,Begin/End 路径无锁
//   - 配对 map / 订阅者列表用 RWMutex 保护
type Aggregator struct {
	count   [9]atomic.Int64 // 8 类事务今日计数(idx 1-8 对齐 TxKind)
	success [9]atomic.Int64 // 对应成功计数
	abnorm  atomic.Int64    // 异常事务总数(失败 + 超时)
	pending atomic.Int64    // 待处理事项数(本期未实现规则,留 0)
	total   atomic.Int64    // 今日全量信令计数

	ring *Ring

	// 配对 map: key = callID + ":" + cseq
	pairMu sync.Mutex
	pair   map[string]Transaction

	// SSE 订阅
	subMu sync.RWMutex
	subs  map[chan Event]struct{}

	// 跨日清零基准(00:00 后第一次 Record 触发清零)
	lastDay int64

	clock func() time.Time
}

// NewAggregator 创建聚合器(默认 24h × 60s ring)
func NewAggregator() *Aggregator {
	return newAggregatorWithClock(time.Now)
}

func newAggregatorWithClock(clock func() time.Time) *Aggregator {
	a := &Aggregator{
		ring:  NewRing(86400),
		pair:  make(map[string]Transaction),
		subs:  make(map[chan Event]struct{}),
		clock: clock,
	}
	a.ring.SetClock(clock)
	a.lastDay = dayKey(clock())
	return a
}

// SetClock 注入时钟(测试)
func (a *Aggregator) SetClock(f func() time.Time) {
	a.clock = f
	a.ring.SetClock(f)
	a.lastDay = dayKey(f())
}

func dayKey(t time.Time) int64 {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location()).Unix()
}

// Begin 事务开始,落入配对 map
func (a *Aggregator) Begin(t Transaction) {
	if t.CallID == "" || t.CSeq == "" {
		// 缺 key 无法配对,直接当 fire-and-forget(不计入,等于丢弃)
		return
	}
	if t.StartedAt.IsZero() {
		t.StartedAt = a.clock()
	}
	key := t.CallID + ":" + t.CSeq
	a.pairMu.Lock()
	a.pair[key] = t
	a.pairMu.Unlock()
}

// End 事务结束,根据 callID+cseq 找到对应 Begin 后入计数
func (a *Aggregator) End(callID, cseq string, statusCode int, success bool) {
	if callID == "" || cseq == "" {
		return
	}
	key := callID + ":" + cseq
	a.pairMu.Lock()
	tx, ok := a.pair[key]
	if ok {
		delete(a.pair, key)
	}
	a.pairMu.Unlock()
	if !ok {
		return
	}

	a.maybeRollDay()

	idx := int(tx.Kind)
	if idx < 1 || idx > 8 {
		return
	}
	a.count[idx].Add(1)
	if success {
		a.success[idx].Add(1)
	} else {
		a.abnorm.Add(1)
	}
	a.total.Add(1)

	now := a.clock()
	failInc := 0
	if !success {
		failInc = 1
	}
	a.ring.Incr(now, 1, failInc)

	// 广播 SSE 事件(本期 SSE handler 只关心定时帧,事件作扩展占位)
	a.broadcast(Event{Kind: "tx", Timestamp: now})
}

// maybeRollDay 跨日检测:0 点后第一次进 End 触发计数清零
func (a *Aggregator) maybeRollDay() {
	today := dayKey(a.clock())
	a.subMu.Lock()
	defer a.subMu.Unlock()
	if today != a.lastDay {
		for i := range a.count {
			a.count[i].Store(0)
			a.success[i].Store(0)
		}
		a.abnorm.Store(0)
		a.total.Store(0)
		a.lastDay = today
	}
}

// CleanupExpiredPairs 清理超过 ttl 的未配对 Begin(防内存泄漏)
// 由后台 goroutine 周期调用
func (a *Aggregator) CleanupExpiredPairs(ttl time.Duration) int {
	cutoff := a.clock().Add(-ttl)
	a.pairMu.Lock()
	defer a.pairMu.Unlock()
	n := 0
	for k, v := range a.pair {
		if v.StartedAt.Before(cutoff) {
			delete(a.pair, k)
			n++
		}
	}
	return n
}

// PairMapSize 配对 map 当前大小(测试 + 监控用)
func (a *Aggregator) PairMapSize() int {
	a.pairMu.Lock()
	defer a.pairMu.Unlock()
	return len(a.pair)
}

// Subscribe SSE 订阅,返回事件 chan + 取消函数
// 缓冲 32,慢消费者直接 drop
func (a *Aggregator) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 32)
	a.subMu.Lock()
	a.subs[ch] = struct{}{}
	a.subMu.Unlock()
	cancel := func() {
		a.subMu.Lock()
		if _, ok := a.subs[ch]; ok {
			delete(a.subs, ch)
			close(ch)
		}
		a.subMu.Unlock()
	}
	return ch, cancel
}

// SubscribersCount 当前订阅者数量(测试用)
func (a *Aggregator) SubscribersCount() int {
	a.subMu.RLock()
	defer a.subMu.RUnlock()
	return len(a.subs)
}

func (a *Aggregator) broadcast(ev Event) {
	a.subMu.RLock()
	defer a.subMu.RUnlock()
	for ch := range a.subs {
		select {
		case ch <- ev:
		default:
			// 慢消费者 drop
		}
	}
}

// Snapshot 合成卡片完整 JSON
func (a *Aggregator) Snapshot(window time.Duration, precision time.Duration) *DashboardSnapshot {
	a.maybeRollDay()
	now := a.clock()

	// 8 格事务统计
	stats := make([]TransactionStat, 0, 8)
	for _, k := range AllTxKinds {
		idx := int(k)
		c := a.count[idx].Load()
		s := a.success[idx].Load()
		var rate float64
		if c > 0 {
			rate = float64(s) / float64(c)
		}
		stat := TransactionStat{
			Kind:        k,
			KindStr:     k.String(),
			LabelZh:     k.LabelZh(),
			LabelEn:     k.String(),
			TodayCount:  c,
			SuccessRate: rate,
			TrendPct:    0, // 本期不算昨日对比
			Alert:       c > 0 && rate < 0.95,
		}
		stats = append(stats, stat)
	}

	// 健康度计算输入
	in := healthInputs{
		count:   make(map[TxKind]int64, 8),
		success: make(map[TxKind]int64, 8),
		abnorm:  a.abnorm.Load(),
	}
	for _, k := range AllTxKinds {
		idx := int(k)
		in.count[k] = a.count[idx].Load()
		in.success[k] = a.success[idx].Load()
	}
	health := computeHealth(in)

	// 脉搏图
	samples := a.ring.Snapshot(window, precision)
	pulseSamples := make([]PulseSample, 0, len(samples))
	for _, s := range samples {
		failPct := 0
		if s.Msg > 0 {
			failPct = int(s.Fail * 1000 / s.Msg)
		}
		// msgPerSec 实际是"该采样区间内的消息条数"(precision=1m 时 = 每分钟条数)
		// 不做整数除法,否则 1m 精度下 < 60 条/分钟会全部归零,图表始终空白
		pulseSamples = append(pulseSamples, PulseSample{
			T: s.T, MsgPerSec: int(s.Msg), FailPct: failPct,
		})
	}
	abnormalWindows := detectAbnormalWindows(pulseSamples, 50) // 失败率 > 5% (50‰)

	return &DashboardSnapshot{
		Health:        health,
		TodayTotal:    a.total.Load(),
		TodayAbnormal: a.abnorm.Load(),
		Pending:       a.pending.Load(),
		Transactions:  stats,
		Pulse: PulseData{
			WindowMinutes:   int(window / time.Minute),
			Samples:         pulseSamples,
			AbnormalWindows: abnormalWindows,
		},
		AsOf: now.Unix(),
	}
}

// detectAbnormalWindows 把连续失败率超阈值的样本合并成异常段
func detectAbnormalWindows(samples []PulseSample, thresholdPermille int) []AbnormalWindow {
	out := []AbnormalWindow{}
	var inWin bool
	var startT int64
	var lastT int64
	for _, s := range samples {
		if s.FailPct > thresholdPermille {
			if !inWin {
				inWin = true
				startT = s.T
			}
			lastT = s.T
		} else if inWin {
			out = append(out, AbnormalWindow{StartT: startT, EndT: lastT})
			inWin = false
		}
	}
	if inWin {
		out = append(out, AbnormalWindow{StartT: startT, EndT: lastT})
	}
	return out
}

// Begin/End 接口验证:确保 Aggregator 满足 Recorder
var _ Recorder = (*Aggregator)(nil)

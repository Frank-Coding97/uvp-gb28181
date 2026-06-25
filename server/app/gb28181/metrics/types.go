// Package metrics 提供 GB28181 SIP 信令的指标采集与聚合能力。
//
// 本包对外提供两个角色:
//   - Recorder:供 SIP 收发路径(handler/UAC)埋点,只暴露 Begin/End
//   - Aggregator:供 controller 取快照 + SSE 订阅
//
// 核心抽象 Transaction:把 8 类 GB28181 协议事务统一成 Begin/End 事件,
// 用 (Call-ID, CSeq) 配对,聚合器消费后输出快照。
package metrics

import "time"

// TxKind GB28181 协议事务类型(spec §4 AC-3 八类)
type TxKind int

const (
	TxUnknown   TxKind = 0
	TxRegister  TxKind = 1 // 注册
	TxKeepalive TxKind = 2 // 心跳
	TxCatalog   TxKind = 3 // 目录查询/应答
	TxInvite    TxKind = 4 // 点播
	TxRecord    TxKind = 5 // 录像查询
	TxAlarm     TxKind = 6 // 报警
	TxPTZ       TxKind = 7 // 控制
	TxBye       TxKind = 8 // 挂断
)

// AllTxKinds 8 类事务的固定输出顺序,Snapshot 按此顺序填 transactions[]
var AllTxKinds = []TxKind{
	TxRegister, TxKeepalive, TxCatalog, TxInvite,
	TxRecord, TxAlarm, TxPTZ, TxBye,
}

func (k TxKind) String() string {
	switch k {
	case TxRegister:
		return "REGISTER"
	case TxKeepalive:
		return "KEEPALIVE"
	case TxCatalog:
		return "CATALOG"
	case TxInvite:
		return "INVITE"
	case TxRecord:
		return "RECORD"
	case TxAlarm:
		return "ALARM"
	case TxPTZ:
		return "PTZ"
	case TxBye:
		return "BYE"
	default:
		return "UNKNOWN"
	}
}

// LabelZh 返回事务的中文展示名(用于 Snapshot.transactions[i].labelZh)
func (k TxKind) LabelZh() string {
	switch k {
	case TxRegister:
		return "注册"
	case TxKeepalive:
		return "心跳"
	case TxCatalog:
		return "目录"
	case TxInvite:
		return "点播"
	case TxRecord:
		return "录像"
	case TxAlarm:
		return "报警"
	case TxPTZ:
		return "控制"
	case TxBye:
		return "挂断"
	default:
		return "未知"
	}
}

// Direction 事务方向
type Direction int

const (
	DirIn  Direction = 0 // 设备 → 平台
	DirOut Direction = 1 // 平台 → 设备
)

func (d Direction) String() string {
	if d == DirIn {
		return "in"
	}
	return "out"
}

// Transaction 单次 SIP 协议事务(Begin 阶段填的字段)
type Transaction struct {
	Kind      TxKind
	Direction Direction
	CallID    string // Begin/End 配对 key 的一部分
	CSeq      string // Begin/End 配对 key 的一部分
	DeviceID  string // 关联设备(可空)
	StartedAt time.Time
}

// Recorder 埋点接入方依赖的最小接口
// handler / UAC 只需要 Begin/End,不需要看到 Snapshot/Subscribe
type Recorder interface {
	// Begin 事务开始
	Begin(t Transaction)
	// End 事务结束(根据 callID+cseq 匹配 Begin)
	// statusCode 0 表示无应答(超时/错误)
	End(callID, cseq string, statusCode int, success bool)
}

// Event SSE 推送的增量事件(本期 SSE 用 Snapshot 全量推,Event 暂作扩展占位)
type Event struct {
	Kind      string // "snapshot" | "ping"
	Timestamp time.Time
	Snapshot  *DashboardSnapshot
}

// TransactionStat 8 宫格中单个事务的统计(对齐 plan §4.1 transactions[i])
type TransactionStat struct {
	Kind        TxKind  `json:"-"`
	KindStr     string  `json:"kind"`
	LabelZh     string  `json:"labelZh"`
	LabelEn     string  `json:"labelEn"`
	TodayCount  int64   `json:"todayCount"`
	SuccessRate float64 `json:"successRate"` // 0.0 ~ 1.0,无样本时 0
	TrendPct    float64 `json:"trendPct"`    // 对比昨日同时段(本期固定 0,留扩展)
	Alert       bool    `json:"alert"`       // SuccessRate < 0.95 视为告警
}

// PulseSample 脉搏图单个采样点
type PulseSample struct {
	T         int64 `json:"t"`         // unix 秒
	MsgPerSec int   `json:"msgPerSec"` // 该窗口内平均 msg/s
	FailPct   int   `json:"failPct"`   // 该窗口内失败率 ‰(0-1000)便于前端整数格式化
}

// AbnormalWindow 异常时间窗(失败率 > 5% 的连续段)
type AbnormalWindow struct {
	StartT int64 `json:"startT"`
	EndT   int64 `json:"endT"`
}

// PulseData 脉搏图完整数据
type PulseData struct {
	WindowMinutes    int              `json:"windowMinutes"`
	Samples          []PulseSample    `json:"samples"`
	AbnormalWindows  []AbnormalWindow `json:"abnormalWindows"`
}

// DashboardSnapshot 卡片 API 完整响应(plan §4.1)
type DashboardSnapshot struct {
	Health        float64           `json:"health"`        // 0-100,sentinel -1 表示空数据态(前端显示 "--")
	TodayTotal    int64             `json:"todayTotal"`
	TodayAbnormal int64             `json:"todayAbnormal"`
	Pending       int64             `json:"pending"`
	Transactions  []TransactionStat `json:"transactions"`
	Pulse         PulseData         `json:"pulse"`
	AsOf          int64             `json:"asOf"`
}

// HealthEmpty 空数据态健康度 sentinel(前端识别后渲染 "--")
const HealthEmpty = -1.0

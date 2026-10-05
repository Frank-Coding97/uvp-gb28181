// Package recordcache 实现「设备录像缓存到服务器」的任务编排。
//
// 与设备录像回放页原有的「下载」的区别：那条路是浏览器直接把设备推上来的
// 直播流 fetch 成 blob（要等流播完、无进度、不能取消、全堆内存）。
// 这里改成**服务端后台缓存**：
//
//	建 download 回放会话 → 对 ZLM 上的流 StartRecordWithType(MP4) 落盘
//	→ 按需分段续播（长录像）→ 用户随时从平台下载已落盘的文件
//
// 浏览器只做两件事：看进度、下载静态文件。设备与通道在任务跑完后立即释放。
package recordcache

import (
	"context"
	"errors"
	"net/http"
	"time"

	"gorm.io/gorm"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	gbplayback "uvplatform.cn/uvp-gb28181/app/gb28181/playback"
	gbrecording "uvplatform.cn/uvp-gb28181/app/gb28181/recording"
	"uvplatform.cn/uvp-gb28181/app/gb28181/recordquery"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

var (
	// ErrTaskNotFound 任务不存在或不属于当前用户可见范围。
	ErrTaskNotFound = errors.New("record cache task not found")
	// ErrChannelBusy 同一通道已有活动中的缓存任务（下载模式会话按通道排他）。
	ErrChannelBusy = errors.New("channel already has an active record cache task")
	// ErrInvalidRequest 入参不合法（区间颠倒、倍速越界、recordKey 缺失等）。
	ErrInvalidRequest = errors.New("invalid record cache request")
	// ErrNotDownloadable 任务没有可下载的产出（未完成 / 已过期 / 文件已丢失）。
	ErrNotDownloadable = errors.New("record cache task has no downloadable file")
	// ErrMergeFailed 多分片没能合并成一个文件（超上限 / 拼接失败）。
	//
	// ⛔ 必须与 ErrNotDownloadable 分开：合成一个的话，前端只能收到
	// 404「暂无可下载的文件」，而分段文件明明躺在媒体节点上 —— 用户会以为
	// 录像丢了。分开之后前端能给出正确动作：按分段下载。
	ErrMergeFailed = errors.New("record cache segments cannot be merged into one file")
	// ErrNotCancellable 终态任务不能再取消。
	ErrNotCancellable = errors.New("record cache task is not cancellable")
	// ErrNodeUnavailable 找不到任务落点所在的 ZLM 节点。
	ErrNodeUnavailable = errors.New("record cache node unavailable")
	// ErrPlaybackUnavailable 回放服务未装配。
	ErrPlaybackUnavailable = errors.New("record cache playback unavailable")
)

// DownloadSpeedOptions 是 GB28181 下载倍速的合法档位（A.2.1.20 DownloadSpeed）。
//
// ⛔ 与 device_record_playback 控制器里的校验同一张表：倍速是 SDP 的
// `a=downloadspeed` 取值，设备只认这几档，发别的值设备行为未定义。
var DownloadSpeedOptions = []int{1, 2, 4, 8}

// DefaultDownloadSpeed 是取不到设备能力时的兜底倍速。
const DefaultDownloadSpeed = 4

// SegmentWallBudget 是单个分片会话的**墙钟**预算 —— 平台**自设**，不是外部限制。
//
// ⛔ 这里曾写「真正的硬约束是回放会话的墙钟硬时限（playback.MaxSession，默认 30 分钟）」，
// 那是**误判**（2026-10-05 查证），别再用它论证分片长度：
//   - `playback.DefaultMaxSession = 30 分钟` 只是 Registry 在**没拿到配置**时的兜底；
//     真实值由 bootstrap 从配置键 `gb28181.playback.max_session_sec` 注入，
//     默认 `DefaultPlaybackMaxSessionSec = 86400`（**24 小时**），本环境 config.yml 也没覆盖。
//   - 平台也没有给 ZLM 签发过任何"30 分钟有效期"的凭据：ZLM 侧凭据只有节点级长期
//     APISecret，不按会话签发、无 TTL。
//
// 那 25 分钟是怎么来的 —— 我们**自己**给一次录制会话设的时长上限，为的是：
//   - 把"一次会话失败"的影响面钉在**这一段**上（失败只需重录这一段，不是整段录像）；
//   - 周期性地把会话拆掉重建：设备中途静默时（见 `finishSegment`），换会话本身就是恢复手段。
//
// 预算之外再多给 3 分钟（见 SegmentWallGrace）用于「停录 → 拆会话 → 采集文件」。
//
// 调大它 = 单片更长、片数更少（更容易"一个文件"），代价是单次失败要重录的区间更长、
// 设备静默后也要等更久才换会话。这是取舍，不是"必须小于某条硬限"。
const SegmentWallBudget = 25 * time.Minute

// SegmentSpeedSafety 是倍速折算的安全系数。
//
// 设备标称 4×/8× 是上限而非保证：丢包重传、磁盘慢、设备自身限速都会打折，
// 所以折算要留余量。但**不必留一半**：本环境实测有效倍速 ≈3.8×（标称 4×，
// 达成率 0.95），而 0.5 是把上限砍到 22.5 分钟墙钟的媒体量 —— 4× 设备单片只给
// 50 分钟媒体 ⇒ **1 小时录像被无谓切成 2 片**（老板 2026-10-04 再次点名）。
//
// ⭐ 取 0.9 为什么是安全的：真实倍速不达标时，墙钟保护 `SegmentWallGrace`（28 分钟）
// 会先兜住 ⇒ 走「安全收尾 → 游标按**实际录到的**媒体时长推进 → 下一片接着录」，
// **不会静默丢尾部**。所以调高系数的坏结果上限只是"仍然切成 2 片"，不是丢数据。
// （旧值 0.5 的顾虑正是"尾部丢 + 表现为成功"，那个隐患由这条兜底路径消除。
// ⛔ 它**不是**靠"28 < 30 分钟会话硬限"成立的 —— 那条线不存在，见 SegmentWallBudget。）
//
// ⛔ 真实代价（这是取舍不是纯收益）：单片请求区间越长，设备中途断流/丢包的概率越大，
// 而**一片失败 = 这一段整段重录**。分片越细，失败影响面越小。
const SegmentSpeedSafety = 0.9

// SegmentMediaCap 是单片媒体时长的绝对上限。标称 8× 时公式会算出 180 分钟，
// 再往上没有收益（墙钟预算早就到了），只是徒增请求区间 ——
// 请求区间越长，设备中途断流一次就要整段重录。
const SegmentMediaCap = 120 * time.Minute

// segmentMediaLimit 把「墙钟预算 × 倍速 × 安全系数」折算成单片媒体时长上限。
//
// ⛔ 这是分片长度的唯一口径，不要退回固定分钟数：
// 固定 20 分钟等价于假设"实际有效倍速只有 0.67×"，而实测是 4×，
// 结果是 30 分钟录像被无谓切成 2 片、用户拿到 2 个文件。
func segmentMediaLimit(speed int) time.Duration {
	if speed <= 0 {
		speed = DefaultDownloadSpeed
	}
	limit := time.Duration(float64(SegmentWallBudget) * float64(speed) * SegmentSpeedSafety)
	if limit > SegmentMediaCap {
		limit = SegmentMediaCap
	}
	return limit
}

// SegmentWallGrace 是单个分片会话允许的最长墙钟时间（异常保护）。
// 超过它仍未结束，说明设备没按预期推进（或有效倍速远低于标称）：
// 停会话、收尾、按**实际录到的内容**推进游标。
//
// ⛔ 这里曾写「必须小于 playback.MaxSession，否则这个保护永远不会生效」——
// 该论据随"30 分钟会话硬限"一起作废（真实会话上限默认 24 小时，
// 见 SegmentWallBudget 的说明）。它的语义与任何外部上限无关：
// 只是"会话没按预期推进就别再干等"，超出预算的 3 分钟是给停录/拆会话/采文件留的余量。
const SegmentWallGrace = SegmentWallBudget + 3*time.Minute

// Config 是服务运行参数。
type Config struct {
	// TickInterval 是状态机推进间隔（轮询驱动，服务重启后可从库里恢复）。
	TickInterval time.Duration
	// SessionTimeout 是单片「建会话 + 开始录制」的整体超时。
	SessionTimeout time.Duration
	// StopTimeout 是停会话/停录制的超时（SIP 拆除可能慢）。
	StopTimeout time.Duration
	// RetentionDays 是产出文件的保留天数（<=0 时用 7）。
	RetentionDays int
	// Workers 是单轮可推进的任务数上限。
	Workers int
	// MergeTimeout 是收尾期把多分片合成一个文件的整体超时（<=0 时用 10 分钟）。
	// 超时按失败处理：任务照常判定成功（分段仍可下载），只是没有现成产物。
	MergeTimeout time.Duration
}

func (c Config) withDefaults() Config {
	if c.TickInterval <= 0 {
		c.TickInterval = 2 * time.Second
	}
	if c.SessionTimeout <= 0 {
		c.SessionTimeout = 45 * time.Second
	}
	if c.StopTimeout <= 0 {
		c.StopTimeout = 20 * time.Second
	}
	if c.RetentionDays <= 0 {
		c.RetentionDays = 7
	}
	if c.Workers <= 0 {
		c.Workers = 4
	}
	if c.MergeTimeout <= 0 {
		c.MergeTimeout = 10 * time.Minute
	}
	return c
}

// Repository 是 gb_record_cache_task 的持久化边界。
//
// Scope 是数据权限范围（由控制器用 datascope.VisibilityScopeWithDB 构造），
// 与设备/通道/云端录像三处用同一个 scope 构造函数 —— 免得缓存的可见性比
// 它引用的通道更宽（那会变成越权读别人的录像）。
type Repository interface {
	Create(ctx context.Context, task *gbmodels.GbRecordCacheTask) error
	Update(ctx context.Context, task *gbmodels.GbRecordCacheTask) error
	FindByTaskID(ctx context.Context, taskID string, scope Scope) (*gbmodels.GbRecordCacheTask, error)
	List(ctx context.Context, query ListQuery) ([]gbmodels.GbRecordCacheTask, int64, error)
	Delete(ctx context.Context, id uint64) error
	// FindActiveByChannel 返回该通道上还没终结的任务（queued / running）。
	FindActiveByChannel(ctx context.Context, channelID uint) (*gbmodels.GbRecordCacheTask, error)
	// ListAdvanceable 返回需要状态机推进的任务（queued / running）。
	ListAdvanceable(ctx context.Context, limit int) ([]gbmodels.GbRecordCacheTask, error)
	// ListExpired 返回已过保留期但尚未清理的任务。
	ListExpired(ctx context.Context, now time.Time, limit int) ([]gbmodels.GbRecordCacheTask, error)
}

// Scope 是 GORM 的数据权限 scope（可选，nil 表示不过滤）。
type Scope func(db *gorm.DB) *gorm.DB

// PlaybackService 是 playback.Service 在本包用到的窄接口。
type PlaybackService interface {
	Create(ctx context.Context, request gbplayback.CreateRequest) (gbplayback.CreateResult, error)
	StopForOwner(ctx context.Context, sessionID, ownerID, reason string) error
	GetForOwner(sessionID, ownerID string) (*gbplayback.Session, bool)
}

// SnapshotResolver 与回放控制器用的是同一个录像段快照存储，
// 因此缓存任务与回放走**同一套**「录像段是否真实存在 / 是否已过期」判据。
type SnapshotResolver interface {
	Resolve(request recordquery.ResolveRequest) (recordquery.Snapshot, error)
}

// Recorder 是 ZLM 客户端在本包用到的窄接口。
type Recorder interface {
	StartRecordWithType(ctx context.Context, vhost, appName, stream string, recorderType zlm.RecorderType, maxSecond int) error
	StopRecordWithType(ctx context.Context, vhost, appName, stream string, recorderType zlm.RecorderType) error
	GetMP4RecordFiles(ctx context.Context, vhost, appName, stream, period string) ([]zlm.MP4RecordFile, error)
	DeleteMP4RecordFile(ctx context.Context, vhost, appName, stream, period, name string) error
	GetMediaInfo(ctx context.Context, schema, vhost, app, stream string) (*zlm.MediaInfo, error)
	// DownloadFile 支持 Range 透传，供同源下载复用 ContentProxy。
	DownloadFile(ctx context.Context, filePath, byteRange string) (*zlm.DownloadResponse, error)
}

// SourceLeases 登记「这条流上还有一个非浏览器消费者（录制器）」。
//
// ⛔⛔ 不接这个，缓存任务**根本跑不完**：ZLM 配了 streamNoneReaderDelayMS=20s，
// 录制器在 ZLM 里不算 reader，所以回放流建起来约 20 秒后 ZLM 就会发
// on_stream_none_reader 问平台要不要关。平台的 NoneReaderPolicy 拿 `pb-xxx`
// 流名去通道表里查（回放流天然不在通道表）→ 查不到 → 判「关」→
// hook 调 stopper.Stop 把回放流拆掉 → 本片只录到 ~20 秒墙钟（4× 倍速下 ≈84 秒媒体）
// 就收尾。表现是「任务很快成功、但每个文件只有一分多钟」，30 分钟录像被切成
// 几十个小文件。
//
// recordingplan（定时录像）走的是同一条租约通道（play.SourceLeaseRegistry），
// 缓存任务照抄即可 —— 不要另造一套「保活读者」。
type SourceLeases interface {
	Acquire(streamID string, generation uint64, consumer string) SourceLease
}

// SourceLease 是流租约句柄；Release 必须幂等（同一句柄重复释放只生效一次）。
type SourceLease interface {
	Release() error
}

// SegmentFileIndex 按 stream 提供**平台已入库**的录制文件（含真实时长与大小）。
//
// ⛔ 不能只靠 ZLM 的 getMp4RecordFile：它只解析 rootPath + paths，
// DurationMS / FileSize 恒为 nil（既有测试把这个当契约钉住了）。
// 拿它算进度会让「已缓存时长」永远停在 0、游标失去唯一可靠依据，
// 于是退化成「按请求区间推进」→ 把「实际只录到 84 秒」当成「录满 20 分钟」
// 宣布成功，用户一个文件都拿不到。
//
// 可靠来源是 on_record_mp4 hook（它带 time_len / file_size，平台已准确入库）。
type SegmentFileIndex interface {
	IndexedSegments(ctx context.Context, stream string) ([]IndexedSegment, error)
}

// IndexedSegment 是一条已入库的录制文件。
type IndexedSegment struct {
	Name       string
	Path       string
	Size       int64
	DurationMS int64
	StartedAt  time.Time
	Period     string
}

// NodeLookup 按 ZLM 节点主键取节点。
type NodeLookup interface {
	Get(id int64) (node *node.Node, ok bool)
}

// ContentProxy 是同源下载代理。
//
// ⛔ 这里直接用 recording 包的类型，不另立一套"同构"接口：Go 的接口是结构化匹配，
// 但**方法签名里的具名类型必须逐字相同** —— 把 recording.ContentRequest 换成
// 本包自己定义的 ContentRequest，*recording.ContentProxy 就实现不了这个接口
// （错误信息只说"wrong type for method Stream"，很容易被误读成"少了个方法"）。
type ContentProxy interface {
	Stream(ctx context.Context, writer http.ResponseWriter, downloader gbrecording.ContentDownloader, request gbrecording.ContentRequest) error
}

// SpeedSource 提供设备的下载倍速能力。
//
// 实现从平台**已回读过的**设备配置（gb_device_config 的 VideoParamOpt）里取
// DownloadSpeed 档位并取最大值；没有记录时返回 0，
// 由调用方回退到 DefaultDownloadSpeed。
//
// ⛔ 不在这里主动发 ConfigDownload：配置读取走 PTZ 的异步 operation 通道
// （需要 device_security 快照、授权 epoch、重试预算），在一次"创建任务"里
// 同步等一个 SIP 往返会把接口成功率绑在设备应答速度上，
// 而设备上线时平台本来就不查这项能力。等 VideoParamOpt 纳入设备配置页的
// 回读范围后，这里自然就能拿到真实档位。
type SpeedSource interface {
	MaxDownloadSpeed(ctx context.Context, devicePK int64, targetCode string) int
}

// CreateRequest 是创建缓存任务的入参（由控制器填好权限与通道事实）。
type CreateRequest struct {
	OwnerUserID uint
	OwnerName   string
	OwnerDeptID uint

	ChannelID   uint
	DevicePK    uint
	DeviceID    string
	ChannelCode string
	ChannelName string
	DeviceName  string

	RecordKey  string
	RecordType string
	PlayFrom   time.Time

	// DownloadSpeed 为 0 时由服务自行判定（设备能力 → 兜底 4）。
	DownloadSpeed int

	// Authorization / 建会话所需事实（与回放路径同源）。
	Authorization   gbplayback.AuthorizationSnapshot
	PreferredNodeID int64
	Destination     string
	Transport       string
	TCPMode         bool
	DefaultProtocol string
	Secure          bool
	Now             time.Time
}

// ListQuery 是任务列表查询。
type ListQuery struct {
	UserID    uint
	Page      int
	PageSize  int
	State     string
	Keyword   string
	ChannelID uint
	// Favorite 是收藏筛选：nil = 不筛，true = 只看收藏，false = 只看未收藏。
	// ⛔ 必须是指针：`false`（只看未收藏）和 nil（不筛）语义不同。
	Favorite *bool
	// Scope 是数据权限范围；nil 表示不过滤（仅内部任务推进使用）。
	Scope Scope
}

// FileView 是一个已落盘的产出文件。
type FileView struct {
	Index        int    `json:"index"`
	Name         string `json:"name"`
	Size         int64  `json:"size"`
	StartTime    string `json:"startTime"`
	EndTime      string `json:"endTime"`
	Downloadable bool   `json:"downloadable"`
}

// TaskView 是任务的对外视图。
type TaskView struct {
	TaskID        string `json:"taskId"`
	ChannelID     uint   `json:"channelId"`
	DeviceID      string `json:"deviceId"`
	ChannelCode   string `json:"channelCode"`
	ChannelName   string `json:"channelName"`
	DeviceName    string `json:"deviceName"`
	StartTime     string `json:"startTime"`
	EndTime       string `json:"endTime"`
	RecordType    string `json:"recordType"`
	DownloadSpeed int    `json:"downloadSpeed"`
	State         string `json:"state"`
	LastError     string `json:"lastError"`
	// Favorite 为 true 时该录像不参与保留期自动清理（只能手动删除）。
	Favorite bool `json:"favorite"`

	CachedBytes      uint64  `json:"cachedBytes"`
	CachedSeconds    float64 `json:"cachedSeconds"`
	TotalSeconds     float64 `json:"totalSeconds"`
	Progress         float64 `json:"progress"`
	SpeedBytesPerSec uint64  `json:"speedBytesPerSec"`
	EstimatedBytes   uint64  `json:"estimatedBytes"`

	Files []FileView `json:"files"`

	CreatedByName string `json:"createdByName"`
	CreatedAt     string `json:"createdAt"`
	StartedAt     string `json:"startedAt"`
	FinishedAt    string `json:"finishedAt"`
	ExpiresAt     string `json:"expiresAt"`
}

// PageView 是一页任务。
type PageView struct {
	List  []TaskView `json:"list"`
	Total int64      `json:"total"`
	Page  int        `json:"page"`
	Size  int        `json:"size"`
}

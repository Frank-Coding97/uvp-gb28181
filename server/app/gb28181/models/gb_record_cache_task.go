package models

import "time"

// 设备录像缓存任务状态机：
//
//	queued    —— 已受理，尚未开始拉流
//	running   —— 正在从设备拉流并对 ZLM 上的流录制
//	merging   —— 全部片段已录完，正在把它们无损合成一个文件
//	succeeded —— 录制完成（多片时合成也已就绪），可在保留期内下载
//	failed    —— 拉流/录制失败（终态，可重试即新建任务）
//	cancelled —— 用户主动取消（终态，保留已缓存分片）
//	expired   —— 超过保留期，文件已被清理（终态）
//
// ⛔ merging 必须是一个**独立状态**，不能借 running 表达：running 的任务会被
// Tick 当成"这一片还在录"去推进，合成期间被推一下就会重新开一段录像。
// 它也不再占用设备通道（会话早拆了），所以不能算进 RecordCacheTaskActive。
const (
	RecordCacheStateQueued    = "queued"
	RecordCacheStateRunning   = "running"
	RecordCacheStateMerging   = "merging"
	RecordCacheStateSucceeded = "succeeded"
	RecordCacheStateFailed    = "failed"
	RecordCacheStateCancelled = "cancelled"
	RecordCacheStateExpired   = "expired"
)

// GbRecordCacheTask 是「设备录像缓存到服务器」的一次任务。
//
// 与云端录像的区别：数据源是设备侧的录像（GB28181 回放/下载会话），
// 平台把流落到 ZLM 节点磁盘后，用户再从平台下载文件；
// 因此本表同时承担「任务编排状态」与「产出文件索引」两件事，
// 不写入 gb_recording_file 的常规列表（那份是云端录像计划产出的）。
type GbRecordCacheTask struct {
	ID     uint64 `gorm:"primaryKey" json:"id"`
	TaskID string `gorm:"column:task_id;size:64;not null;uniqueIndex:uk_record_cache_task_id" json:"taskId"`

	// 归属与操作人（列表按部门数据权限过滤）
	OwnerDeptID   uint   `gorm:"column:owner_dept_id;not null;default:0;index:idx_record_cache_task_dept_state,priority:1" json:"ownerDeptId"`
	CreatedByUser uint   `gorm:"column:created_by_user;not null;default:0" json:"createdByUser"`
	CreatedByName string `gorm:"column:created_by_name;size:64;not null;default:''" json:"createdByName"`

	// 通道/设备快照（渠道可能被删除，展示口径不能依赖 JOIN）
	ChannelID   uint   `gorm:"column:channel_id;not null;index:idx_record_cache_task_channel_state,priority:1" json:"channelId"`
	DeviceID    string `gorm:"column:device_id;size:20;not null" json:"deviceId"`
	ChannelCode string `gorm:"column:channel_code;size:20;not null;default:''" json:"channelCode"`
	ChannelName string `gorm:"column:channel_name;size:255;not null;default:''" json:"channelName"`
	DeviceName  string `gorm:"column:device_name;size:255;not null;default:''" json:"deviceName"`

	// 录像段与拉流参数
	StartTime  time.Time `gorm:"column:start_time;not null" json:"startTime"`
	EndTime    time.Time `gorm:"column:end_time;not null" json:"endTime"`
	RecordType string    `gorm:"column:record_type;size:16;not null;default:all" json:"recordType"`
	// RecordKey 是录像段标识（受签的快照句柄）。
	// ⛔ 必须持久化：长录像要分片续播，每一片都要用它重新向设备发起 download 回放，
	// 它不是"创建时用一次就丢"的参数。
	RecordKey     string `gorm:"column:record_key;size:255;not null;default:''" json:"recordKey"`
	DownloadSpeed int    `gorm:"column:download_speed;not null;default:4" json:"downloadSpeed"`

	// 媒体落点（用于定位 ZLM 上的流与产出文件）
	NodeID int64  `gorm:"column:node_id;not null;default:0" json:"nodeId"`
	VHost  string `gorm:"column:vhost;size:128;not null;default:__defaultVhost__" json:"vhost"`
	App    string `gorm:"column:app;size:64;not null;default:rtp" json:"app"`
	Stream string `gorm:"column:stream;size:64;not null;default:''" json:"stream"`
	// SessionID 是**回放会话 ID**（playback registry 的 `pb-xxxx`）。
	// ⛔ 它是字符型，不是自增主键：取消任务要靠它调 StopForOwner 精确停会话，
	// 写成整型列会被 GORM 静默存成 0，表现是「取消后会话仍占着通道，要到会话上限
	// （`gb28181.playback.max_session_sec`，默认 24 小时）才被清扫器释放」。
	SessionID *string `gorm:"column:session_id;size:64" json:"sessionId"`

	// 产出文件
	FileID   *uint64 `gorm:"column:file_id" json:"fileId"`
	FileName string  `gorm:"column:file_name;size:255;not null;default:''" json:"fileName"`
	FilePath string  `gorm:"column:file_path;size:1000;not null;default:''" json:"-"`
	FileSize uint64  `gorm:"column:file_size;not null;default:0" json:"fileSize"`

	// 进度（cachedBytes/estimatedBytes 只作展示，不作为完成判据）
	CachedBytes    uint64 `gorm:"column:cached_bytes;not null;default:0" json:"cachedBytes"`
	EstimatedBytes uint64 `gorm:"column:estimated_bytes;not null;default:0" json:"estimatedBytes"`

	State     string `gorm:"column:state;size:20;not null;index:idx_record_cache_task_state_created,priority:1;index:idx_record_cache_task_channel_state,priority:2;index:idx_record_cache_task_dept_state,priority:2" json:"state"`
	LastError string `gorm:"column:last_error;size:500;not null;default:''" json:"lastError"`

	// 幂等键：回放会话创建使用的 requestId，重试沿用同一个
	RequestID string `gorm:"column:request_id;size:64;not null;default:''" json:"-"`

	// CursorAt 是分段续播游标：下一分片会话的起始时间。
	// 单片会话的墙钟预算只有 25 分钟（平台**自设**，见 recordcache.SegmentWallBudget），
	// 超长录像必须切片续拉，这个字段就是「已经缓存到哪儿」。
	// nil 表示尚未开始（起点即 StartTime），succeeded 时为 EndTime。
	CursorAt *time.Time `gorm:"column:cursor_at" json:"cursorAt"`

	// Segments 是分片产出清单（JSON 数组，每项 {stream,name,path,size,ms,start,end}）。
	//
	// ⛔ 必须有它：每一片是**独立回放会话 ⇒ 独立 stream ⇒ ZLM 上独立目录**，
	// 只留 cursor_at 的话前几片的文件路径就永远找不回来了，
	// 表现是"任务显示成功、下载到的却只有最后一片"。
	Segments string `gorm:"column:segments;type:text;not null" json:"-"`

	StartedAt  *time.Time `gorm:"column:started_at" json:"startedAt"`
	FinishedAt *time.Time `gorm:"column:finished_at" json:"finishedAt"`
	ExpiresAt  *time.Time `gorm:"column:expires_at;index:idx_record_cache_task_expires" json:"expiresAt"`

	// Favorite 是「收藏」标记：收藏的录像**不参与保留期自动清理**，
	// 只有用户手动删除才会清掉文件（见 recordcache 的 cleanupExpired）。
	//
	// ⛔ 必须落库，不能只放前端：清理跑在后台 Tick 里，前端标记拦不住它 ——
	// 表现会是"我明明收藏了，过几天文件还是没了"，而且没有任何报错。
	Favorite bool `gorm:"column:favorite;not null;default:false" json:"favorite"`

	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime;index:idx_record_cache_task_state_created,priority:2" json:"createdAt"`
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updatedAt"`
}

func (GbRecordCacheTask) TableName() string { return "gb_record_cache_task" }

// RecordCacheTaskActive 判断任务是否处于「占用通道」的活动期。
// 只有活动期任务才允许取消，也只有活动期任务需要占位检查。
func RecordCacheTaskActive(state string) bool {
	return state == RecordCacheStateQueued || state == RecordCacheStateRunning
}

// RecordCacheTaskTerminal 判断任务是否已终结（不再变化）。
func RecordCacheTaskTerminal(state string) bool {
	switch state {
	case RecordCacheStateSucceeded, RecordCacheStateFailed, RecordCacheStateCancelled, RecordCacheStateExpired:
		return true
	default:
		return false
	}
}

package models

import (
	"time"

	"gorm.io/gorm"
)

// 抓拍图片来源（source 列取值）。⭐ 三套"抓拍"的产出**汇到同一张表**，
// 图像库才能统一展示、统一按保留期清理；分开建三张表会让"按通道+时间段找图"变成三次查询拼接。
const (
	// SnapshotSourceDevice 是**设备自己拍、自己 POST 回平台**（A.2.5.7 那条链路）。
	SnapshotSourceDevice = "device"
	// SnapshotSourceZLM 是平台在播放开始时从 ZLM 拉流抓帧（gb_channel.snapshot_url 的来源）。
	SnapshotSourceZLM = "zlm"
	// SnapshotSourceBrowser 是播放器本地截图。⛔ 当前**不落库**（截图只下载到操作员本机，
	// 平台零感知，见 docs/snapshot-flow-design.md §1 入口 A 的否决决议）；
	// 保留这个取值是为了将来"把某一帧存为证据"做成独立动作时不必改列语义。
	SnapshotSourceBrowser = "browser"
)

// GbChannelSnapshot 是一次成功的图像抓拍产出的**一个图片文件**。
//
// ⭐ 与 gb_channel.snapshot_url 的分工：那一列是"该通道最近一次快照"的**指针**（覆盖式），
// 本表是**全部历史图**的事实表（逐张一行，软删）。两者不是一回事：
// 前者给列表页的缩略图用（只能有一张），后者给图像库"按通道/时间段/来源找图"用。
//
// ⛔ 为什么要独立表而不复用 sys_affix：通用附件表没有 device/channel/session/time 维度，
// 而图像库的核心查询恰恰是"某通道某时间段有哪些图"、"这次抓拍会话收了哪几张"。
//
// ⛔ 唯一键 `(channel_code, file_name)` 而非 `file_name`：设备文件名（41 位图像标识）
// 前 20 位就是**设备编码**，同一台设备的不同通道可能产出同名前缀的文件，
// 单列唯一会误伤；而按"通道编码 + 文件名"去重恰好等于"同一通道不会有两个同名图"。
// 设备重传同一张图（网络抖动导致的重复 POST）因此天然幂等 —— 落库走 OnConflict 覆盖，
// 不产生第二行（也顺带把软删的行复活，见 controllers 的写入侧注释）。
type GbChannelSnapshot struct {
	ID uint `gorm:"primaryKey" json:"id"`
	// DeviceID / ChannelID 是**平台主键**（gb_device.id / gb_channel.id），用于关联筛选；
	// ChannelCode 是国标编码（20 位），是与设备报文字段对账的那个值。
	// ⛔ 查不到对应平台行时写 0（占位），不因此拒绝收图 —— 图已经落盘了，
	// 把"关联不上"升级成"收图失败"会让设备侧看到一个它无法修复的错误。
	DeviceID    uint   `gorm:"column:device_id;not null;default:0" json:"deviceId"`
	ChannelID   uint   `gorm:"column:channel_id;not null;default:0" json:"channelId"`
	ChannelCode string `gorm:"column:channel_code;size:20;not null;uniqueIndex:uk_channel_snapshot_file,priority:1" json:"channelCode"`
	// SessionID 是抓拍会话标识；zlm/browser 来源为空。
	// 会话面板"从库重载"就靠它 —— 内存里的 Registry 刷新页面即丢，图还在就得能重聚。
	SessionID *string `gorm:"column:session_id;size:64;index:idx_channel_snapshot_session" json:"sessionId,omitempty"`
	// FileName 是设备给的文件名（41 位图像标识 + `.jpg`），是**与完成通知 `SnapShotFileID`
	// 对账的唯一依据**，所以原样存，不做重命名。
	FileName string `gorm:"column:file_name;size:64;not null;uniqueIndex:uk_channel_snapshot_file,priority:2" json:"fileName"`
	// RelPath 是相对**抓拍图片基目录**的路径（`gb-device-snapshots/<sessionId>/<file>`），
	// 稳定读接口按它定位文件。⛔ 存相对路径而不是绝对路径：serverroot 会被运维改成别的卷，
	// 存绝对路径的库一旦搬机器就全部指向不存在的文件。
	RelPath string `gorm:"column:rel_path;size:255;not null" json:"-"`
	Size    int64  `gorm:"column:size;not null;default:0" json:"size"`
	// MD5 是图片字节的摘要，用于"平台手上的图"与"设备说的图"做完整性对账（以及将来去重）。
	MD5 string `gorm:"column:md5;size:32" json:"md5"`
	// CapturedAt 是**拍摄时刻**，从 41 位文件名的 17 位时间字段反解（E-4 规则），
	// ⛔ 不是平台接收时刻 —— 设备补传时两者可能差很远，而图像库的时间轴要反映"什么时候拍的"。
	// 文件名不合规（解析不出时间）时回落到接收时刻，并保留原文件名以便事后追查。
	CapturedAt time.Time `gorm:"column:captured_at;not null;index:idx_channel_snapshot_channel_time,priority:2;index:idx_channel_snapshot_captured" json:"capturedAt"`
	// Source 见 SnapshotSourceXxx 常量。
	Source    string         `gorm:"column:source;size:16;not null" json:"source"`
	CreatedBy *uint          `gorm:"column:created_by" json:"createdBy,omitempty"`
	CreatedAt time.Time      `gorm:"column:created_at;not null" json:"createdAt"`
	UpdatedAt time.Time      `gorm:"column:updated_at;not null" json:"updatedAt"`
	DeletedAt gorm.DeletedAt `gorm:"column:deleted_at;index" json:"-"`
}

func (GbChannelSnapshot) TableName() string { return "gb_channel_snapshot" }

// SnapshotLibraryRoutePath 是图像库稳定读接口**在路由组内**的相对路径
// （挂在 `/api/gb28181/device-mgmt` 组下）。
//
// ⛔ 与本文件末尾的 [SnapshotLibraryAPIPath] 拆成两个常量，是为了让"路由注册"与
// "迁移里登记的 sys_api"**不可能写歪**：前者用相对路径（gin 组会自动补前缀），
// 后者必须是被鉴权中间件实际看到的全路径。两个都手写字面量时，
// 迁错一个字符的表现是"接口通但恒 403"或"根本没注册"，且两边都不报错。
const SnapshotLibraryRoutePath = "/snapshots/:id/content"

// SnapshotLibraryAPIPath 是图像库稳定读接口的**全路径**（三处必须同名：迁移 / 路由 / 测试）。
//
// ⛔ 与上传接口那条带 token 的 `/device-snapshots/uploads/:token/:filename` 是**两条路**：
// 那条的凭证是**会话 token**（会随会话过期），只能给"刚下发的那一批"用；
// 这条的凭证是 **JWT + 权限码**，按库里的行 id 取图，图在就能取，刷新页面/重启后端都不影响。
const SnapshotLibraryAPIPath = "/api/gb28181/device-mgmt" + SnapshotLibraryRoutePath

// SnapshotListRoutePath 是图像库**列表**接口在路由组内的相对路径（挂在 `/api/gb28181/device-mgmt` 组下）。
//
// ⛔ 与 [SnapshotLibraryRoutePath] 是两条不同层的路径（`/snapshots` 与 `/snapshots/:id/content`），
// 不是一条：列表给"按通道+时间段翻历史图"用，读图给"拿到某一行后取字节"用。
// 两者都由本文件与迁移里的 sys_api 登记共用同一个常量源，避免"迁错一个字符 ⇒ 恒 403"。
const SnapshotListRoutePath = "/snapshots"

// SnapshotListAPIPath 是图像库列表接口的**全路径**（迁移 / 路由 / 测试三处同名）。
const SnapshotListAPIPath = "/api/gb28181/device-mgmt" + SnapshotListRoutePath

// SnapshotLibraryMenuPath / MenuName / MenuComponent / MenuTitle 是图像库**一级菜单**的注册口径，
// 三处必须一致：迁移里插的 sys_menu 行、前端 `@/views/**/*.vue` 的组件路径、路由测试断言。
//
// ⛔ component 的写法是**前端路由解析契约**（web/src/router/route-output.ts:70 用
// `import.meta.glob("@/views/**/*.vue")` 的 key 去掉 `views/` 与 `.vue` 后与它逐字比对）：
// 写成 `gb28181/snapshot-library/index` 就要求存在
// `web/src/views/gb28181/snapshot-library/index.vue`。**页面不存在时菜单点开是空白**，
// 所以菜单行与页面必须在同一个交付批次里落地（见 docs/snapshot-flow-design.md §3.3）。
const (
	SnapshotLibraryMenuPath      = "/gb28181/snapshot-library"
	SnapshotLibraryMenuName      = "snapshot-library"
	SnapshotLibraryMenuComponent = "gb28181/snapshot-library/index"
	SnapshotLibraryMenuTitle     = "图像库"
)

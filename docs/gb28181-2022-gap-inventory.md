# GB/T 28181-2022 功能缺口清单（平台 + 模拟器）

> ⚠️ **本文档是「现状核查」，不是开工单**。开工与进度跟踪请用
> **`docs/gb28181-2022-master-backlog.md`**（全量 61 条实施单，组织口径：模拟器实现功能、平台有操作入口）。
>
> 核查日期：2026-09-17（v2，补「设备配置」家族 + 修正图像抓拍结论）
> ⚠️ **2026-09-18 局部更新**：§1.1 的 `VideoParamAttribute` 已随 A-5 落地
> （见 §1.1-A 的两处补记与 §2.1 末行）。**其余结论仍是 09-17 口径**，未重新核查。
> 核查方式：代码级逐项核对（非依赖既有文档结论）
> 核查对象：
> - 平台（SIP 服务端 / 上级平台）`/Users/menglulu/code/uvp/UVP-GB28181`，分支 `develop`
> - 模拟器（下级设备 / IPC）`/Users/menglulu/code/uvp/uvp-gb28181-sim`
>
> 状态定义：**已实现**（有运行时链路 + 测试证据）· **部分**（协议有壳或只有单向）·
> **未实现**（无代码路径）· **不做**（已决策）
> ⚠️ 注意：数据库字段、前端类型、测试 fixture 存在 ≠ 功能可用，本清单只认运行时链路。

---

## 一、基准：2022 相对 2016 的官方变化（标准前言口径）

| # | 官方变化 | 条款 |
| --- | --- | --- |
| 1 | 媒体流通道增加 H.265、G.722.1、AAC | 4.3.1 |
| 2 | 增加媒体流数据传输的 RTP 时间戳要求（同帧同戳、异帧异戳） | 4.3.6 |
| 3 | 网络传输带宽、视频帧率要求调整（帧率统一 25fps） | 5.4、5.6 |
| 4 | 信令字符集改为 **GB18030** | 6.10 |
| 5 | 报文携带协议版本标识 **X-GB-Ver**（3.0=2022 / 2.0=2016） | 第 7 章、附录 I |
| 6 | 传输、交换、控制安全性要求更改 | 第 8 章 |
| 7 | 注册注销：增加 **NAT 模式网络传输要求**，宜增加 TCP 传输模式 | 9.1.1 |
| 8 | 增加 **注册重定向**（集群模式，302） | 9.1.2.3 |
| 9 | 设备控制和设备配置基本要求及数据类型更改（**设备配置家族大幅扩充，见 1.1**） | 9.3.1、A.2.1、A.2.3 |
| 10 | 增加 **看守位信息查询、巡航轨迹列表查询、巡航轨迹查询、PTZ 精准状态查询、存储卡状态查询**及应答 | 9.5.3、A.2.4.10~14、A.2.6.12~16 |
| 11 | 增加 **PTZ 精准位置变化事件订阅和通知** | 9.11.1、9.11.2 |
| 12 | 增加 **设备软件升级、图像抓拍**信令流程和协议接口 | 9.13、9.14 |
| 13 | 附录 A 扩充：~~Info→**ExtraInfo** 标签~~（⛔ 2026-09-17 核实**不成立**：`ExtraInfo` 全标准出现 0 次，容器名仍为 `Info`）、Channel 字段格式变更、A.4 联网系统扩展应用 | 附录 A |
| 14 | 附录 D 基于 TCP 的视音频媒体传输（原 2016 附录 L） | 附录 D |
| 15 | 附录 E 扩展类型编码：第 14 位改 **网络标识**、新增县以下区划代码 | 附录 E |
| 16 | 附录 G SDP 扩展：**a=ssvcratio（SVC）**、a 字段可带**媒体编号**（主/子码流选择）、s 字段删除 Talk | 附录 G |
| 17 | 新增附录 H **摄像机和平台路径选择**（路径优选与切换） | 附录 H |
| 18 | 更改附录 J 目录查询应答：增加**组织级查询与应答**；虚拟组织 ParentID 不再填业务分组 ID | 附录 J |
| 19 | 附录 M 多响应消息传输要求 | 附录 M |
| 20 | 附录 N 域间目录订阅通知（原 2016 附录 P） | 附录 N |
| 21 | 新增附录 O 摄像机采集部位类型代码 | 附录 O |

### 1.1 「设备配置」家族（v1 清单遗漏整块，2026-09-17 补）

> v1 只扫了「设备控制（DeviceControl）」一支，漏掉了 2022 大幅扩充的
> **「设备配置（DeviceConfig / ConfigDownload）」家族**。这一族承载的正是
> **「平台怎么改设备画面/编码/录像/上报行为」**——用户直觉里的「视频画面控制信令」就在这里。
>
> 承载方式（**已回标准原文核对**，见 `docs/gb28181-2022-device-config-ambiguity.md` 第二节的 OCR 方法）：
> - **设置**：`<Control><CmdType>DeviceConfig</CmdType>…<具体配置元素>` ，**有应答**
>   （`<Response><CmdType>DeviceConfig</CmdType><Result>OK|ERROR</Result>`）
> - **读取**：`<Query><CmdType>ConfigDownload</CmdType><ConfigType>A/B/C</ConfigType>`，
>   `ConfigType` 多值时用 `/` 分隔；应答同为 `CmdType=ConfigDownload`

| ConfigType | 含义 | 2016 | 2022 | 平台 | 模拟器 |
| --- | --- | --- | --- | --- | --- |
| `BasicParam` | 基本参数（名称/注册有效期/心跳） | 有 | 有 | 未实现 | 读 ✅ / 写 只记录 |
| `VideoParamOpt` | 视频参数范围 | 有 | 有 | 未实现 | 读 ✅ / 写 ✗ |
| `VideoParamAttribute` | 视频参数属性（编码格式/分辨率/帧率/码率） | ✗ | **新增** | ✅ 已实现（2026-09-18，写+读+回读对账；见 §1.1-A） | ✗ |
| `SVACEncodeConfig` / `SVACDecodeConfig` | SVAC 编解码配置 | 有 | 有 | 未实现 | ✗（显式忽略仍回 OK） |
| `VideoRecordPlan` | 录像计划 | ✗ | **新增** | 未实现 | ✗ |
| `VideoAlarmRecord` | 报警录像 | ✗ | **新增** | 未实现 | ✗ |
| `PictureMask` | 视频画面遮挡（隐私区域，可多区域） | ✗ | **新增** | 未实现 | ✗ |
| `FrameMirror` | 画面翻转（上下/左右/中心镜像） | ✗ | **新增** | 未实现 | ✗ |
| `OSDConfig` | **前端 OSD 字符叠加**（位置/时间显示方式/文字内容/显示方式） | ✗ | **新增** | 未实现 | ✗ |
| `AlarmReport` | 报警上报开关（按事件类型配置上报） | ✗ | **新增** | 未实现 | ✗ |
| `SnapShotConfig` | 图像抓拍配置（一次性，非常驻配置） | ✗ | **新增** | ⚠️ 能下发但 CmdType 用错 | 能收但 Android 未落地 |

> ⛔ **「2016」列的判定依据（2026-09-18 钉死，勿再写「待查」）**：2016 版附录 A
> A.2.4 的注释**逐字列出的可查配置类型只有 4 个** —— `BasicParam`、`VideoParamOpt`、
> `SVACEncodeConfig`、`SVACDecodeConfig`（OCR 原文见
> `docs/gb28181-2022-device-config-ambiguity.md` §六）。上表 8 个标 **`✗`** 的类型
> （含 `VideoParamAttribute` **与 `VideoRecordPlan` / `VideoAlarmRecord`**）
> **全部是 2022 新增**，2016 全文不含这些词 —— 不是「待查」。
> ⚠️ 但要注意：**「2016 没有这个类型」不等于「发出去会坏」** ——
> 协议骨架是向后兼容的同层追加（同上文档 §六-A），2016 设备**一定能解析**报文，
> 只是不认识那个 `ConfigType`；危害在于「宽松解析器回 `Result=OK` 却什么都没改」
> 这种**假成功**，因此 A-5 的判定手段是**强制回读对账**，不是 profile 门禁
> （见下 §1.1-A）。

**平台侧整体结论**（⚠️ **2026-09-18 部分失效，见下方补记**）：`server/` 全仓
**`ConfigDownload` / `ConfigType` / `BasicParam` / `VideoParamOpt` / `VideoParamAttribute` /
`PictureMask` / `FrameMirror` / `OSDConfig` / `AlarmReport` 零匹配**。也就是说平台
**连「读设备当前配置」都做不到**，只有 `SnapShotConfig` 一处（且 CmdType 不合规，见 2.3）。

> **⚠️ 补记（2026-09-18）**：上段是 **2026-09-17 的核查快照**，其中
> **`VideoParamAttribute` 一项已失效** —— 该类型已随 A-5 落地：
> `manscdp/video_param.go`（构建/解析/附录 G 校验）、`ptz/video_param.go`（写 `DeviceConfig`、
> 读 `ConfigDownload`、ack 同事务排自动回读对账、逐格 diff）、
> `models/gb_device_video_param.go`（+ `gb_channel.stream_number_list`）、
> `controllers/device_video_param.go`（`GET/POST /channel/:id/video-params`）、
> 前端 `videoParamCodec.ts` + 播放控制台「高级」第 4 张卡。
> 其余八项（`BasicParam` / `VideoParamOpt` / SVAC 两族 / `PictureMask` / `FrameMirror` /
> `OSDConfig` / `AlarmReport` / 录像计划两族）**仍未实现**，该结论继续成立。
>
> **§1.1-A `VideoParamAttribute` 的关键口径（写给后来人）**：
> 1. **不加 profile 门禁**。2016 设备收到不认识的 `ConfigType` 时，标准**没规定**怎么回；
>    最危险的是「宽松解析器回 `Result=OK` 却什么都没改」= **假成功**。
>    ⇒ 真相由**强制回读对账**暴露，不由 profile 断言；被误登记成 2016 的真 2022 设备
>    不试一次就永远用不了这功能。
> 2. **`Result=OK` 不是终态**。写入应答 A.2.6.8 **没有回显** ⇒ 平台在 ack 的同一事务里
>    追加一条 `ConfigDownload` 对账子 operation，逐格比对后才出结论。
> 3. **四态判据**（`ptz.DeriveVideoParamReconcileState`，纯函数）：
>    `never_read` / `pending` / `read_ok` / `type_absent`（回 OK 但**没带**该元素 ⇒
>    等价「设备不支持」，**最可靠判据**）/ `mismatch`（已接受但值未生效 ⇒ **能力边界提示，
>    不是失败**）/ `failed`。
> 4. **取值出处是附录 G 的 SDP `f` 字段**（标准页 130），不是 `VideoParamOpt` 的回读范围；
>    且**码流段数**由目录 `Info/StreamNumberList` 决定 —— 详见
>    `docs/gb28181-2022-video-param-attribute-panel.md`。

**模拟器侧**：有 `ConfigDownloadResponse.kt`，但只支持 `BasicParam` + `VideoParamOpt`
两个读，其余 `ConfigType` **显式忽略但仍回 `Result=OK`**（`ConfigDownloadResponse.kt:61`）
——这是典型的**静默假成功**，平台侧永远看不出设备不支持。

---

## 二、平台侧（UVP-GB28181）现状

### 2.1 已实现（核实过的运行时链路）

| 能力 | 证据 |
| --- | --- |
| X-GB-Ver 解析 + 2016/2022 双版本 profile 与门禁 | `server/app/gb28181/protocol/profile.go` |
| 字符集 GB18030 真实转码（非只改声明） | `server/app/gb28181/manscdp/codec.go:100-163` |
| SIP over TCP 监听/收发 | `server/app/gb28181/sip/server.go:764` |
| 多响应聚合（**仅 Catalog**：deviceID+SN+SumNum、30s 超时、去重） | `server/app/gb28181/handler/catalog.go:18-107` |
| 目录 2022 维度：CivilCode/ParentID/BusinessGroupID、业务分组(215)/虚拟组织(216) | `manscdp/catalog.go:33-51`、`catalog/classifier.go` |
| 多父级挂载（A/B 拆分） | `catalog/dto.go:28`、`gb_channel_mount`、`pipeline.go:251` |
| 精准云台控制 PTZPreciseCtrl | `manscdp/ptz_precise.go` |
| 精准状态查询 / 看守位查询 / 巡航轨迹列表 / 轨迹详情 | `ptz/query.go:34-39`（QueryKind 五类） |
| **存储卡状态查询**（2026-09-17，**刻意不走 `QueryKind`**，见 2.2 P-1） | `manscdp/storage_card.go`、`ptz/storage_card.go`、`models/gb_storage_card.go`、`controllers/device_storage_card.go`、`routes/routes.go` |
| **视频参数属性读写 `VideoParamAttribute`**（2026-09-18，配置家族第一项落地；写 `DeviceConfig` / 读 `ConfigDownload` + **强制回读对账**） | `manscdp/video_param.go`、`ptz/video_param.go`（含 `DeriveVideoParamReconcileState`）、`models/gb_device_video_param.go`、`controllers/device_video_param.go`、`routes/routes.go`、`web/src/views/gb28181/videoParamCodec.ts`、`web/src/views/gb28181/components/PlayConsoleLinked.vue`（高级 tab 第 4 卡） |
| 图像抓拍：下发 + 完成通知接收 + HTTP 收图落盘（⚠️ 默认私有口径，见 2.3） | `manscdp/snapshot.go`、`controllers/device_snapshot.go:89`、`devicecapture/registry.go:125,175` |
| 设备软件升级：下发 + 升级结果状态机 | `manscdp/device_upgrade.go`、`upgrade/service.go`、`controllers/device_firmware_upgrade.go` |
| 拉框放大/缩小 DragZoom | `manscdp/device_advanced.go:85,242` |
| 报警复位 AlarmCmd（含 2022 AlarmMethod/AlarmType 选择） | `manscdp/device_advanced.go:146,221` |
| PTZ 精准位置订阅与通知 | `handler/notify.go:88`、`bootstrap.go:759` |
| RTP over TCP（委托 ZLMediaKit） | `config.go:57,495`、`sdp/sdp.go:63`、`zlm/client.go:191` |
| SIP Date 校时（REGISTER 200 带 Date） | `handler/register.go:543` |

### 2.2 缺口

| # | 缺失能力 | 条款 | 现状说明 |
| --- | --- | --- | --- |
| ~~P-1~~ | ~~**存储卡状态查询**~~ **✅ 已实现（2026-09-17，主单 C-1）** | A.2.4.14 / A.2.6.16 | 原判「全仓 `*.go` 无 `SDCard`/`StorageCard`」**已过时**：现为 `manscdp/storage_card.go`（报文，27 单测）+ `ptz/storage_card.go`（收发落库，9 单测）+ `models/gb_storage_card.go` + `controllers/device_storage_card.go` + `routes.go` 路由 + 三方言迁移 6 文件。⚠️ **未走 `ptz/query.go` 的 `QueryKind`**（该处只有 5 类，本项刻意不并入）—— 理由见主单 C-1：附录 M 的多响应聚合三类**不含** `SDCardStatus`，A.2.6.16 一次应答即终态 |
| P-2 | **格式化 SD 卡** | A.2.3.1.13 | 无任何代码（⚠️ 破坏性命令，若做必须带权限门禁） |
| P-3 | **目标跟踪 TargetTrack** | A.2.3.1.14 | 无任何代码 |
| P-4 | **注册重定向（302）** | 9.1.2.3 | 无 302 应答（仅 third_party/sipgo 常量）— **❌ 已决策暂不做** |
| P-5 | **摄像机访问路径选择** | 附录 H | 全仓无 `X-PreferredPath` / `X-RoutePath` |
| P-6 | **域间目录订阅通知** | 附录 N / 9.11 | cascade 侧只有被动应答与推送，平台无法作为上级主动订阅下级域目录 |
| P-7 | **媒体流保活 / 丢失释放** | 附录 K | 只有 BYE 清理 + 点播对账 `play/reconciler/reconciler.go:194`；无 RTCP 保活 |
| P-8 | **SDP 扩展：SVC、媒体编号** | 附录 G | 有 s/t/m/y(SSRC)，无 `a=ssvcratio`（**❌ 已决策不做**），无 a 字段媒体编号 |
| P-9 | **音频编码扩展** | 4.3.1 | 语音广播/对讲硬编码 PCMA/8000 且硬拒其他（`talk/activation.go:30,228`）；无 G.722.1、无 AAC |
| P-10 | **安全：TLS / 信令完整性 / GB 35114** | 第 8 章 | 无 SIP over TLS、无完整性保护；现有为接入准入/限速/封禁 |
| P-11 | ~~**MobilePosition 的 MESSAGE 形态**~~ **❌ 已关闭（2026-09-17）—— 标准里不存在这个形态** | ~~9.5.4~~ **无** | 原判据「常量存在但 MESSAGE 分支缺失，仅 NOTIFY 可用」**不成立**：`manscdp.CmdMobilePosition` 服务的是**订阅链路**（订阅体构造 / NOTIFY 解析 / Event 判定），与 MESSAGE 无关；两版 §9.5 均**无 9.5.4**，A.2.6（应答命令）**无 MobilePosition 条目**。原「缺失」实为「标准无此形态，我们不补」。**替代项见 P-19** |
| P-12 | **设备配置：读取（ConfigDownload）** | 9.3.5 / A.2.5 | 无常量、无查询构造、无应答解析。平台**读不到任何设备配置** |
| P-13 | **设备配置：下发族缺失** | 9.3.5 / A.2.3.2 | 无 `CmdDeviceConfig` 常量；除 SnapShotConfig 外全部配置项均无下发能力 |
| P-14 | **多响应聚合通用化** | 附录 M | 仅 Catalog 实现；无通用按 SN 聚合、无条数上限约束 |
| P-15 | **固件分发 HTTP 服务** | 9.13 | 升级 FileURL 由外部提供，平台不托管固件下载 |
| P-16 | **NTP 校时** | 9.10 | 无 NTP 客户端（仅 SIP Date） |
| P-17 | 报警实时推送 | 工程项 | 入库/查询有，缺 Webhook/WebSocket 实时外推 |
| P-18 | **目录通道属性字段不解析** | 附录 J | ✅ **已修复（2026-09-18，B-2）**。原状：`manscdp/catalog.go` 只声明 `PTZType`+`BusinessGroupID` 且**都在 `Item` 层**，而标准把它们放在 `<Info>` 内、Go `encoding/xml` 不递归 → `IPAddress`/`Port`/`PositionType`/`RoomType`/`UseType`/`SupplyLightType`/`DirectionType`/`Resolution` **即使设备上报也被静默丢弃**（连带 `gb_channel.ptz_type` 恒为 0）。现：新增 `CatalogInfo` 双版本并存解析 + `gb_channel` 十列 + 通道详情展示。⚠️ 原记「2022 新字段」不准确 —— 其中 `PositionType`/`UseType` 是 **2016 独有**（2022 已删），详见 `gb28181-2022-master-backlog.md` 的 B-1/B-2 行 |
| P-19 | ~~**MobilePosition NOTIFY 2022 列表形态不识别**~~ **✅ 已实现（2026-09-17）** | 9.11.2.3 c）/ A.2.5.6 | `manscdp.MobilePositionNotify` 改造为**双形态共存**：新增 `MobilePositionItem`（A.2.1.14，含 `Height`）+ `MobilePositionDeviceList`，`SumNum` / `DeviceList` 用**指针**（区分「元素不存在」与「值为 0 / 空」），新增 `Positions()` 做跨版本归一化（照 `ParseAlarmNotify` 的「根字段 + 嵌套 `Info` 双读」先例）。⭐ **判定为列表形态后就在列表语义里走到底**：`DeviceList` 在场但 `Item` 为空 → 空切片，**绝不回落扁平字段**（回落会拿根上 0 值坐标合成 (0,0) 假位置）。`PositionProcessor.Process` 改为遍历 `Positions()`：单条脏数据只跳过该条 + zap warn、**不连坐同包其它设备**，整包全废才报错；`SumNum=0` 返回 `nil`（返回 error 会让 `notify.go:73` 提前 return、`last_notify_at` 不更新）。⚠️ 与 S-27 成对 —— 两侧都改完才算通。`Height` 目前**只解析不落库**（无对应列，需另开迁移单） |

### 2.3 ⚠️ 合规疑点：图像抓拍走的是私有口径（能跑通 ≠ 合规）

标准（9.14 / A.2.3.2.1 / A.2.3.2.12 / A.2.5.7）+ 权威解读 + 真实抓包一致确认：

| 环节 | 标准取值 | 本仓实际 | 位置 |
| --- | --- | --- | --- |
| 抓拍下发 `CmdType` | **`DeviceConfig`** | `DeviceControl` | `manscdp/snapshot.go:59`（`CmdType: CmdDeviceControl`） |
| 完成通知 `CmdType` | **`UploadSnapShotFinished`** | `Notify` + `<SubCmd>SnapShot</SubCmd>` | `manscdp/snapshot.go:81`、模拟器 `SnapShotNotifyBuilder.kt:28` |
| 完成通知图像标识 | `SnapShotList/SnapShotFileID`（可多张） | `SnapShotID`（单张） | `manscdp/snapshot.go:39` |
| 抓拍完成响应 | 设备需回 `DeviceConfig` + `Result` | 平台不校验 CmdType | — |

**两侧互相对齐、但都偏离标准**（全仓搜 `UploadSnapShotFinished` **零匹配**）。
后果：与自家模拟器联调一切正常，**接第三方设备/上级平台必挂**。
这是本仓「编译过 ≠ 对了」的又一个实例——建议与真设备/标准原文复核后再定改法。

---

## 三、模拟器侧（uvp-gb28181-sim）现状

### 3.1 已实现

精准云台控制与精准状态查询、看守位（含设备侧自动归位）、巡航轨迹列表/详情、辅助控制、
TeleBoot / RecordCmd / GuardCmd / AlarmCmd / IFameCmd / DragZoom、DeviceUpgrade（含结果 NOTIFY）、
图像抓拍协议链（SnapShotCmd + SnapShotConfig + 上传引擎 + NOTIFY）、目标跟踪与格式化 SD 卡**解析**、
**存储卡状态查询（标准 `SDCardStatus` 收发 + 随机假数据，2026-09-17）**、
目录订阅与增量 NOTIFY、报警上报与报警状态查询、MobilePosition（**订阅 + 周期 NOTIFY**，2026-09-17 起**双版本并存**：2022 列表 / 2016 扁平，按 `effectiveGbVersion` 分支）、
PTZ 精准位置订阅与通知、RecordInfo / Playback / Download / MediaStatus(121/122/123)、
语音广播下行、H.264/H.265 编码 + PS 封装、G.711A/AAC、RTP over UDP/TCP、RTCP SR、
多响应分包（SumNum + Num，默认 50/包）、目录树多通道与业务分组/虚拟组织模板、
SIP Date 校时 + NTP 客户端、**自带 OSD 画面叠加渲染管线**（`osd/OsdRenderer.kt`、`OsdFontAtlas.kt`、
`OsdTextPass.kt`、`IosOsdBitmapRenderer.kt`）。

> 💡 注意最后一条：模拟器**已经有真实的 OSD 叠加渲染能力**，但**没有 OSDConfig 协议入口**
> ——两边一接就能做出「平台改配置 → 画面上文字真的变」的端到端可视闭环。

### 3.2 缺口

| # | 缺失能力 | 条款 | 现状说明 |
| --- | --- | --- | --- |
| S-1 | **Catalog 的 2022 新增字段实际未输出** | 9.3.1 / 附录 J | `CatalogResponse.kt:87 buildGb2022Fields()` 备好 10 个字段，但生产路径 `CatalogSubRouter.kt:62 → buildAllFromTree → CatalogNotifyBuilder.renderItem:281` 只输出基础字段 + BusinessGroupID。**实缺 9 个**：IPAddress / Port / PTZType / PositionType / RoomType / UseType / SupplyLightType / DirectionType / Resolution。⚠️ **平台侧也不解析这 9 个**（见 P-18）→ 只补模拟器在平台上看不到任何变化，必须两侧同改 |
| S-2 | **Android 图像抓拍未真正落地** | 9.14 | `SnapshotCapture.android.kt:25 takeJpeg` 恒返 `null`（2026-06-17 骨架，注释里 T5 待办未完成），无 CameraX `ImageCapture` 绑定。运行期表现：平台下发 SnapShotConfig → `CaptureSkipped` → 无 PUT、无完成 NOTIFY，但平台仍收到 200 OK。iOS 侧有真实现（`IosSnapshotSourceHolder`）。⚠️ 与能力矩阵 2026-07-13 标记 ✅ 不一致，需真机复核 |
| ~~S-3~~ | ~~**存储卡状态查询只有 mock，且命令名不兼容**~~ **✅ 已实现（2026-09-17，主单 C-1）** | A.2.4.14 / A.2.6.16 | 原判「只认 `StorageCardStatusQuery`（写死 1 卡 32G/24G）；`SDCardStatus` 落入未识别 cmdType」**已过时**：现以标准名 `SDCardStatus` 为主入口（旧名保留为兼容别名、应答一律标准名）；报文骨架改为 `SDCardStatusInfo/Item`（`ID`/`HddName`/`Status`/`FormatProgress?`/`Capacity`/`FreeSpace`）+ `SumNum`，`Status` 取值改标准枚举。**mock 改为可注入随机源**：张数/容量只掷一次、状态与剩余空间每次抖动；`FormatProgress` 仅 `formatting` 时输出。16 单测（含 40 seed 不变量轮跑） |
| S-4 | **设备升级是假进度** | 9.13 | 5s 假进度流，不下载、不烧写（`SystemHandler.kt:136`） |
| S-5 | **字符集声明与字节不一致** | 6.10 | 声明 `GB2312`，但 `String.encodeToByteArray()` 输出 UTF-8，全仓无 GB2312/GB18030 转码（`SipResponseBuilders.kt:107`、`SipInviteBuilders.kt:177`） |
| S-6 | **注册重定向不跟随** | 9.1.2.3 | `RegistrationCoordinatorImpl.handleRegisterResponse` 只处理 2xx/401/407/4xx — **❌ 已决策暂不做** |
| S-7 | **NAT 增强不完整** | 9.1.1 | Contact 恒为 `localIp:localPort`，不用 received/rport 回填实际来源端点 |
| S-8 | ~~**SIP over TCP 断链不自愈**~~ **✅ 已实现（2026-09-17）** | 5.2 | `domain/SipReconnect.kt`：read loop 被动终止 → 停活跃流 + 作废注册会话（`RegistrationCoordinatorImpl.onConnectionLost`）→ 1s 起指数退避封顶 30s、**次数不封顶** → `close()`+`connect()` → 重新注册；`TcpSipTransport` 上报 `ConnectionLost` + 世代号守卫 + `isConnected = socket && readChannel`；UI 横幅 + `RECONNECT` 日志分类。18 单测 |
| S-9 | ~~**X-GB-Ver 半套**~~ **✅ 已实现（2026-09-17）** | 附录 I | `sip/GbVersionNegotiation.kt`（parse + min 协商）+ `RegistrationCoordinator.platformVersion` 流（解析 200/401/4xx **全部**响应头，注销时清空）+ `ManscdpContext.effectiveGbVersion` → Catalog/DeviceInfo/DeviceStatus/AlarmStatus 按 min(本机, 平台) 出站。⚠️ 原文「200 OK 响应不带」不成立：**设备不产生注册响应**，附录 I 对设备侧只剩出站一半 |
| S-10 | **摄像机访问路径头无** | 附录 H | 无 `X-PreferredPath` / `X-RoutePath` |
| S-11 | **域间目录订阅通知无** | 附录 N | 全仓无 |
| S-12 | **多父级目录无** | 附录 H/N | `CatalogNode` 仅单 `parentId` |
| S-13 | **行政区划节点不可用** | 附录 E/J | `AdministrativeRegion` 的 typeCode 为空、不能作真实区划节点，只能用 VirtualOrg+CivilCode 模拟 |
| S-14 | **组织级查询应答不完整** | 附录 J | 虚拟组织 ParentID 语义按 2022 变更未核对 |
| S-15 | **媒体流保活 / 链路释放无** | 附录 K | 只发 RTCP SR + 30s 统计日志，无 RTP 静默超时/丢包/释放（`InviteMediaPipeline.kt:258,284`） |
| S-16 | **RTCP 反馈只有 SR** | RFC3550 | 无 RR / NACK / PLI / FIR |
| S-17 | **G.722.1 无** | 4.3.1 | `AudioCodec` 仅 G711A/G711U/AAC |
| S-18 | **SVC 无** | 附录 G | 无 `a=ssvcratio` — **❌ 已决策不做** |
| S-19 | **次码流 / 辅助码流不做** | 附录 G | 2022 已给标准抓手（a 字段媒体编号），可重新评估 |
| S-20 | **语音对讲（设备→平台上行）不做** | 9.8 | 仅广播下行；且 2022 已删 s 字段 Talk 类型，需按新口径评估 |
| S-21 | **安全：TLS / SRTP / GB 35114 全无** | 第 8 章 | 已决策不做 |
| S-22 | **20 位编码校验偏弱** | 附录 E | 只校验 20 位全数字，不校验类型码段、区划段，也不校验 ID 类型码与节点类型一致（`IdEncoder.kt:38`、`CatalogTreeStore.kt:243`） |
| S-23 | **格式化 SD 卡 / 目标跟踪只记不执行** | A.2.3.1.13/.14 | 只解析写 effect（手机无场景，属协议合规壳） |
| S-24 | **SVAC 编码配置不响应** | 附录 C | `ConfigDownloadResponse.kt:61` 显式忽略仍回 OK |
| S-25 | **设备配置家族只有 2 项且静默假成功** | 9.3.5 / A.2.3.2 | 读只支持 `BasicParam`+`VideoParamOpt`；其余 ConfigType 忽略仍回 OK。**写侧**仅认 `<BasicParam>`（`DeviceControlDispatcher.kt:122`）、且只提取 Name/心跳四个字段发 `ConfigChanged` effect，**不落盘**。无 `CmdType=DeviceConfig` 的独立分派（靠元素名猜） |
| S-26 | **抓拍完成通知用私有 CmdType** | 9.14 / A.2.5.7 | `CmdType=Notify`+`SubCmd=SnapShot`+`SnapShotID`，标准为 `UploadSnapShotFinished`+`SnapShotList/SnapShotFileID`。见 2.3 |
| S-27 | ~~**MobilePosition NOTIFY 只出 2016 扁平形态**~~ **✅ 已实现（2026-09-17）** | 9.11.2.3 c）/ A.2.5.6 | `MobilePositionNotify.build` 加**必传** `gbVersion`（**刻意不给默认值** —— 默认成 2016 会把「忘了传版本」变成一次静默的错误形态上报），按 `ManscdpContext.effectiveGbVersion` 分支：**2022 出列表、2016 出扁平**。⭐ 两形态根 `<DeviceID>` 语义不同故拆两参（2016 根 = 位置来源通道；2022 根 = 目标设备、通道下沉到 `Item/DeviceID`）；⭐ 采集时间两形态共用同一算法，2022 根 `<Time>` 是**上报通知时间**（走东八区）；可选 `Height` 无数据源 → 不发。16 单测（含两份整包 golden）+ 2 例端到端接线。⚠️ 与 P-19 成对 |

---

## 四、两侧共性缺口（做联调时最容易互相"看不见"的）

| 能力 | 平台 | 模拟器 | 影响 |
| --- | --- | --- | --- |
| **设备配置家族（含 OSD / 遮挡 / 翻转 / 上报开关 / 编码属性）** | 未实现 | 只 2 项 + 静默假成功 | **最大整块空白**：平台改不了设备画面与行为 |
| 目录通道属性字段（**2016/2022 两套形态，非「2022 新字段」**） | ✅ 已解析 + 落库 + 展示（B-2，2026-09-18） | ✅ 已输出（B-1，2026-09-17） | ✅ 两侧同改已完成 |
| 图像抓拍 | 已实现但默认私有口径 | Android 未落地 + 通知 CmdType 私有 | 自家联调正常，接第三方必挂 |
| ~~存储卡状态查询~~ **存储卡状态查询 ✅ 两侧已实现（2026-09-17，C-1）** | ✅ `manscdp/storage_card.go` + `ptz/storage_card.go` + `gb_device_storage_card` + 控制台入口 | ✅ 标准报文 + 随机假数据 | ~~2022 五大新增查询里唯一没闭环的一组~~ → **五大新增查询至此全部闭环**；遗留可选项：D-2「格式化 SD 卡」尚未做（独立单） |
| 注册重定向 | 未实现 | 不跟随 | **❌ 已决策暂不做** |
| 摄像机访问路径（附录 H） | 未实现 | 未实现 | 多路径级联点播无法验证 |
| 域间目录订阅（附录 N） | 未实现 | 未实现 | 级联目录同步无法验证 |
| 媒体流保活（附录 K） | 部分 | 未实现 | 断流释放依赖 ZLM，协议层无依据 |
| SVC | 未实现 | 未实现 | **❌ 已决策不做** |
| 音频扩展（G.722.1、AAC 信令） | 未实现 | 未实现 | 2022 新增编解码只在信封层面 |
| 安全（第 8 章 / GB 35114） | 部分 | 不做 | 交付若要求等级保护需单独立项 |

---

## 五、建议推进顺序（2026-09-17 讨论后修订）

**第一梯队（能立刻闭环、现场有感）**
1. **设备配置家族：`OSDConfig` 打通**（平台下发/读取 + 模拟器接自家 OSD 渲染管线）
   —— 唯一能做「改配置 → 画面文字真的变」可视闭环的项，演示价值最高
2. ~~S-1 + P-18 **目录通道属性字段两侧同改**（改模拟器 `renderItem` + 平台 `CatalogItem` 与存储/展示）~~
   —— **✅ 已完成（S-1 = 2026-09-17 B-1；P-18 = 2026-09-18 B-2）**。两侧形态**有意不同**：
   模拟器按 `effectiveGbVersion` 双分支输出；平台侧不做版本分支，改为**两组版本独有属性并存落库、
   由设备实际报了哪一组反推形态**（`EffectiveVersion` 有 `default:2016`，按它分支会误判 2022 设备）
3. P-12 设备配置**读取（ConfigDownload）**——平台连"读设备配置"都没有，是最基础的能力空缺
4. S-2 模拟器 Android 抓拍落地（CameraX ImageCapture），顺带按 2.3 复核抓拍口径

**第二梯队**
5. 设备配置家族其余项（`PictureMask` / `FrameMirror` / `VideoParamAttribute` / `AlarmReport`）
6. ~~S-9 / P-11 X-GB-Ver 与 MobilePosition MESSAGE 形态补齐~~（S-9、P-11 均已收口）
   —— **⚠️ 2026-09-17 修订：两项都已作废**。S-9 已由 F-2 完成（2026-09-17，见第 2 章核对）；
   P-11 已判「标准无此形态」关闭。替代项 = **P-19 + S-27（MobilePosition NOTIFY 2022 列表形态，
   且 2016 扁平形态必须继续可用）**，两台侧成对做
7. P-14 多响应聚合通用化（附录 M）
8. ~~P-1 + S-3 存储卡状态查询对齐~~ **✅ 已完成（2026-09-17，主单 C-1）**
9. P-13 固件分发 HTTP 服务（升级链路闭环）

**第三梯队（按交付要求取舍）**
10. P-5 + S-10 摄像机访问路径选择、P-6 + S-11 域间目录订阅（等做多级级联时统一设计）
11. P-10 / S-21 安全能力（TLS / 完整性 / GB 35114）
12. P-9 / S-17 / S-20 音频扩展与语音对讲上行
13. P-7 / S-15 / S-16 媒体保活与 RTCP 反馈
14. P-2 / P-3 格式化 SD 卡、目标跟踪（破坏性/依赖设备 AI，最低优先）

**❌ 已明确不做**：注册重定向（P-4 / S-6）、SVC（P-8 / S-18）

---

## 附：核查证据边界

- 本清单所有"未实现"结论来自**全仓符号检索**（Go `*.go` / Kotlin `*.kt`）与**分派逻辑穷举**，不依赖任何既有进度文档。既有 `gb28181-coverage.md`（2026-06-15）已严重滞后，勿再作为进度依据。
- "已实现"仅表示当前证据范围内有运行时链路，不等于真实设备/三库/浏览器全量验收通过。
- 厂商私有扩展不计入标准功能（如海康/大华的辅助控制编号事实标准已单列）。
- ✅ **标准原文已可检索**：两份 PDF 均无文本层（CID 编码，`pdftotext`/`pypdf` 提不出中文），
  已用 **macOS PDFKit 渲染 + Vision OCR**（脚本 `.workbuddy/ocr/ocr.swift`，该目录 git 已忽略）产出可检索全文：
  `appA.txt`(2022 附录A) · `appA2016.txt`(2016 附录A) · `sec93.txt` · `sec914.txt`。
  **§1.1 的 12 个 ConfigType 与 2.3 的取值口径（`DeviceConfig` / `UploadSnapShotFinished`）均已回原文确认**，
  不再依赖解读文章与厂商文档。条款级模糊点审查见 `docs/gb28181-2022-device-config-ambiguity.md`。
- ⚠️ OCR 对正文与枚举识别质量良好，但标点/个别字符可能有误；凡要据此改代码的结论，请肉眼复核对应 PDF 页一次
  （两份文档中每条结论都标了标准印刷页码，PDF 页 = 标准页 + 7（2022）/ + 5（2016））。

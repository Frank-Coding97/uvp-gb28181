# GB/T 28181-2022 全量实施单（平台 + 模拟器）

> 生成日期：2026-09-17
> 依据：`docs/gb28181-2022-gap-inventory.md`（实现缺口）+ `docs/gb28181-2022-device-config-ambiguity.md`（条款清晰度）
> 两者已回**标准原文**核对（PDF 无文本层，用 macOS PDFKit + Vision OCR，产物在 `.workbuddy/ocr/`）
>
> 单子的组织口径：**模拟器实现设备侧功能，平台提供操作入口。**
> **§1 是能力覆盖矩阵（速览）**，§4 是逐条实施详情（平台后端落点 · 前端入口 · 模拟器侧 · 验收闭环）。

---

## 0. 怎么用这份单子

- **一条 = 一个可独立验证的交付单元**，做完把 `☐` 改成 `☑`。
- ⛔ **成对原则**：平台侧与模拟器侧都写内容的条目，**必须成对做**。只做一侧，另一端看不见任何变化
  （典型：目录通道属性字段、抓拍口径、设备配置家族）。
- 状态标记：`☐` 待办 · `◐` 进行中 · `☑` 完成 · `✗` 不做
- 优先级：
  - **P0** 地基 / 能立刻做出端到端可视闭环（16 条）
  - **P1** 家族补齐 / 现有能力收口（22 条）
  - **P2** 视级联深度与交付要求取舍（23 条）
- ⚠️ 标了「待核原文」的条目，落地前先回标准原文确认一次再动手。
- ⛔ **引条款号前先验它真的存在**：本单已有两起因二手解读引入**虚条款号**的返工（`F-6` 的「§9.5.4」两版均不存在；抓拍口径的私有值被当成标准值）。凡新增/复述条款，**先回 `.workbuddy/ocr/` 的两版全文搜一次**。

**总计 62 条** = 前置契约 6 + 设备配置家族 13 + 目录与编码 8 + 设备查询 1 + 设备控制与维护 6 +
图像抓拍整改 7 + 信令与传输 10（含 2026-09-17 新增 `F-10`、关闭 `F-6`）+ 音频 4 + 级联 3 + 合规细节 4。

---

## 1. 能力覆盖矩阵（全量速览）

> 状态口径：**已实现**（有运行时链路）· **部分**（有壳或只有单向）· **缺失**（无代码路径）·
> **✗ 不做**（已决策）· **待核原文**（结论未定，先查标准）
> 「单号」= §4 的对应条目；`—` 表示无需开发（已实现，只纳入回归）或与单号无关。

### 1.1 信令与注册

| 能力 | 平台 | 模拟器 | 单号 | 条款 |
| --- | --- | --- | --- | --- |
| X-GB-Ver 版本标识 | 已实现（解析 + 双版本门禁 + 异常告警） | 已实现（出站带头 + 解析平台版本 + min 协商） | ☑ F-2 | 附录 I |
| 信令字符集 GB18030 | 已实现（真转码） | 已实现（双向真转码，随国标版本 GB2312/GB18030） | ☑ I-1 | 6.10 |
| NAT / TCP 长连接（建立 · 复用 · 断链自愈） | ✅ 齐（**复用已具备** + **断链判离线已实现** 2026-09-17） | 部分（断链自愈 ✅、rport 自发现 ✅；缺：NAT 场景默认仍走 UDP） | ☐ F-3 | 9.1.1 f）、5.2 |
| SIP Date 校时 | 已实现 | 已实现 | — | 9.10 |
| NTP 校时 | 缺失 | 已实现 | F-5 | 9.10 |
| 多响应消息聚合 | 仅 Catalog | 已实现（50/包） | F-1 | 附录 M |
| MobilePosition 的 MESSAGE 形态 | ✗ 不做（**标准里没有这个形态**） | 私有扩展（WVP 兼容口径，非对标能力） | ✗ F-6（已关闭 2026-09-17） | **无** —— 原引「§9.5.4」两版均不存在 |
| MobilePosition NOTIFY 形态（**2022 列表**） | ✅ 已实现（2026-09-17）—— 双形态共存，`Positions()` 归一化 | ✅ 已实现（2026-09-17）—— 按 `effectiveGbVersion` 分支，两版并存 | ☑ F-10 | 9.11.2.3 c）/ A.2.5.6 |
| 注册重定向 302 | ✗ 不做 | ✗ 不做 | — | 9.1.2.3 |

### 1.2 目录与编码

| 能力 | 平台 | 模拟器 | 单号 | 条款 |
| --- | --- | --- | --- | --- |
| 目录订阅与增量 NOTIFY | 已实现 | 已实现 | — | 9.11 |
| 目录通道属性字段（2022 清单 · 2016/2022 双形态） | ✅ **已解析 + 落库 + 展示**（2026-09-18，`<Info>` 双版本并存、不做版本分支） | ✅ 已输出（2026-09-17，按 `effectiveGbVersion` 双分支） | ☑ B-1 / ☑ B-2 | 附录 J / §9.3.1 |
| 组织级查询与应答 | 部分（有业务分组/虚拟组织维度） | 部分 | B-3 | 附录 J |
| 行政区划节点 | 部分 | ✅ **空 `typeCode` 是合规的**（2026-09-17 核附录 J：区划条目用 2/4/6/8 位民政码本身当 `DeviceID`，**没有类型码段**） | B-4 | 附录 E/J |
| 目录多父级 | 已实现（A/B 拆分） | 单 `parentId` | B-7 | 附录 H/N |
| 20 位统一编码校验 | 部分 | 部分（只查 20 位数字） | B-8 | 附录 E |
| 附录 O 采集部位类型 | 待核原文 | 待核原文 | B-5 | 附录 O |
| ExtraInfo / Channel 字段格式 | ✗ **不成立**（2026-09-17 核：`ExtraInfo` 在 166 页 2022 OCR 中出现 **0 次**，容器名没改） | ✗ 同上 | ✗ B-6 **建议销单** | 附录 A |

### 1.3 设备查询（2022 五大新增查询）

| 能力 | 平台 | 模拟器 | 单号 | 条款 |
| --- | --- | --- | --- | --- |
| 精准云台控制 PTZPreciseCtrl | ✅ 已实现；⭐ **2026-09-20 海康真机逐字节核对通过**（平台侧信令正确），⛔ **但真机不执行且平台无法观测** —— 详见 §5 同行注释 | 已实现 | — | 9.5 / A.2.3.1.11 |
| PTZ 精准状态查询 | 已实现 | 已实现 | — | A.2.4.10 |
| PTZ 精准位置订阅通知 | 已实现 | 已实现 | — | 9.11.1/2 |
| 看守位信息查询 | 已实现 | 已实现 | — | A.2.4.11 |
| 巡航轨迹列表 / 详情查询 | 已实现 | 已实现 | — | A.2.4.12/13 |
| **存储卡状态查询** | ✅ 已实现（2026-09-17）—— 收发/落库/前端入口全链 | ✅ 已实现（2026-09-17）—— 标准报文 + 随机假数据 + **设备屏幕「存储卡」OSD（查询到达亮起）** | ☑ C-1 | A.2.4.14 / A.2.6.16 |

### 1.4 设备配置家族（`DeviceConfig` 写 / `ConfigDownload` 读）

| 能力 | 平台 | 模拟器 | 单号 | 条款 |
| --- | --- | --- | --- | --- |
| 配置读取 `ConfigDownload` | 缺失（读不到任何配置） | 仅 2/12 类 + 静默回 OK | A-1 | A.2.4.7 |
| 配置下发 `DeviceConfig` | 缺失（无常量） | 按元素名猜，非按 `CmdType` | A-2 | A.2.3.2.1 |
| 配置落盘 / 变更可见 | — | 不落盘 | A-3 | 工程 |
| `OSDConfig` 前端 OSD | 缺失 | 有渲染管线、无协议入口 | A-4 | A.2.1.12 |
| `VideoParamAttribute` 视频参数属性 | ✅ 已实现（2026-09-18）—— 写+读+**强制回读对账**+前端卡片 | 缺失 | ☑ A-5 | 2022 新增（**2016 不适用**） |
| `VideoParamOpt` 视频参数范围 | 缺失 | 已实现（读） | A-9 | 2016 已有 |
| `BasicParam` 基本参数 | 缺失 | 只记不落盘 | A-8 | A.2.1.19 |
| `PictureMask` 画面遮挡 | 缺失 | 缺失 | A-6 | A.2.1.17 |
| `FrameMirror` 画面翻转 | 缺失 | 缺失 | A-7 | 2022 新增 |
| `VideoRecordPlan` 录像计划 | 缺失 | 缺失 | A-10 | 2022 新增 |
| `VideoAlarmRecord` 报警录像 | 缺失 | 缺失 | A-11 | 2022 新增 |
| `AlarmReport` 上报开关 | 缺失 | 缺失 | A-12 | A.2.1.18 |
| SVAC 编解码配置 | 缺失 | 显式忽略回 OK | A-13 | 2016 已有 |
| 配置家族前端统一入口 | 缺失 | — | D-6 | 工程 |

### 1.5 设备控制与维护

| 能力 | 平台 | 模拟器 | 单号 | 条款 |
| --- | --- | --- | --- | --- |
| 图像抓拍 | 已实现，**但私有口径** | Android 占位 + 私有通知 | E-1~E-3 / E-6 | 9.14 |
| 抓拍文件命名 41 位 | 未校验 | 未按规则生成 | E-4 | 9.14.1 / 表 4 |
| 抓拍失败语义（部分失败） | 未处理 | 未处理 | E-5 | A.2.5.7 |
| 设备软件升级 | 已实现（FileURL 靠外部喂） | 假进度（5s） | D-3 / D-4 | 9.13 |
| 目标跟踪 `TargetTrack` | 缺失 | 只解析 | D-1 | A.2.3.1.14 |
| 格式化 SD 卡 | ✅ 已实现（2026-09-20）—— 独立路由 + 独立权限码 `gb28181:device:format_sd` + 二次确认 + 8 轮观察窗跟踪 | ✅ 已实现（2026-09-20）—— `VirtualStorageCards.format()` 真执行语义 + 进度 + 完成态 | ☑ D-2 | A.2.3.1.13 |
| 拉框放大 DragZoom | 已实现 | 已实现 | — | A.2.3.1 |
| 报警复位 `AlarmCmd` | 已实现 | 已实现 | — | — |
| TeleBoot / RecordCmd / GuardCmd / IFameCmd | ✅ 已实现（2026-09-18 复核：控制台**真实调用**，原写「3 处占位未接」有误） | 已实现 | ~~D-5~~ ✗ 销单 | — |
| 辅助控制（雨刷/红外/加热） | 已实现 | 已实现 | — | — |

### 1.6 媒体

| 能力 | 平台 | 模拟器 | 单号 | 条款 |
| --- | --- | --- | --- | --- |
| RTP over TCP | 已实现（委托 ZLM） | 已实现 | — | 附录 D |
| 媒体流保活 / 丢失释放 | 靠点播对账 | 缺失 | F-7 | 附录 K |
| RTCP 反馈 | 未确认（依赖 ZLM） | 仅 SR | F-8 | RFC 3550 |
| Subject 媒体链路标识 | 待核原文 | 待核原文 | F-9 | 附录 L |
| H.265 / PS 封装 | 已实现（委托 ZLM） | 已实现 | — | 附录 C/F |
| RTP 时间戳要求 | 待核原文 | 待核原文 | I-3 | 4.3.6 |
| 视频帧率统一 25fps | —（不编码） | 待核 | I-4 | 5.4 / 5.6 |

### 1.7 音频

| 能力 | 平台 | 模拟器 | 单号 | 条款 |
| --- | --- | --- | --- | --- |
| 语音广播（下行） | 已实现 | 已实现 | — | 9.8 |
| 语音对讲（上行） | 缺失 | 缺失 | G-4 | 9.8 |
| 音频 G.722.1 | 缺失（硬拒） | 缺失 | G-2 | 4.3.1 |
| 音频 AAC | 缺失（硬拒） | 已实现 | G-3 | 4.3.1 |

### 1.8 级联 / 安全

| 能力 | 平台 | 模拟器 | 单号 | 条款 |
| --- | --- | --- | --- | --- |
| 摄像机路径选择 | 缺失 | 缺失 | H-1 | 附录 H |
| 域间目录订阅通知 | 缺失 | 缺失 | H-2 | 附录 N |
| 多级级联端到端验证 | — | — | H-3 | 工程 |
| SIP over TLS / 信令完整性 / GB 35114 | 缺失 | 缺失 | I-2 | 第 8 章 |
| SVC（`a=ssvcratio`） | ✗ 不做 | ✗ 不做 | — | 附录 G |
| 流健康检测 / 探针 | 已实现 | — | — | 工程 |

---

## 2. 通用工程要求（每条都适用，条目里不再重复）

### 2.1 平台侧（UVP-GB28181）

| 层 | 落点 | 要求 |
| --- | --- | --- |
| 协议常量 | `server/app/gb28181/manscdp/manscdp.go` | 新命令先加 `Cmd*` 常量，别在业务里写字符串字面量 |
| XML 构造/解析 | `server/app/gb28181/manscdp/`（新增 `device_config.go` 之类） | 编码/解码集中在 manscdp，controller 只做编排 |
| 查询类命令 | `server/app/gb28181/ptz/query.go` | 新查询加进 `QueryKind` 枚举，别另起一套 |
| HTTP 入口 | `server/app/gb28181/controllers/` | 薄 controller，路由注册在 `routes/routes.go` |
| 收应答/通知 | `server/app/gb28181/handler/message.go`、`notify.go` | 新 CmdType 必须在这里有分支，否则应答被静默丢弃 |
| **权限** | 菜单/按钮表 | 读接口绑菜单 `type=2`、写接口绑按钮 `type=3`；**每个新接口都要补** |
| **迁移** | `resource/database/` | 新增列/表必须有 **三方言 + down** 齐全的迁移；⛔ 手跑迁移不写 `gb_schema_migrations` |
| 前端 API | `web/src/api/gb28181.ts` | 类型与调用集中放这里，测试同目录 `.test.ts` |
| **前端入口** | 见下表 | 每个能力都要有**人能点到**的入口，不能只有接口 |

平台前端入口现状（新入口往这三处挂）：

| 入口 | 文件 | 现状 |
| --- | --- | --- |
| 主控制台（联动版） | `web/src/views/gb28181/components/PlayConsoleLinked.vue` | 已承载精准 PTZ / 预置位 / 巡航轨迹 / 看守位 / 流诊断 —— **设备动作类入口优先挂这里** |
| ~~简化控制台~~ 🗑️ **孤儿，已删除（2026-09-18）** | ~~`web/src/views/gb28181/components/ControlConsole.vue`~~ | 🗑️ `cb49bc1b`（2026-07-23）已「将 ControlConsole 替换为已上线的 PlayConsoleLinked」，此后**无任何 import、无菜单行指向**；它那 3 个「待接入」占位按钮（设备信息 / 请求关键帧 / 远程重启）**从未上线过** → **2026-09-18 物理删除**。⚠️ 教训（别忘）：① `router/route-output.ts:70` 有 `import.meta.glob("@/views/**/*.vue")` → **孤儿组件也会出 dist chunk**，**别据「dist 里有 chunk」判活**；② **同源孤儿** `ChannelSnapshotCell.vue`（全历史 `-S` 零引用）**已于同日一并删除** —— 它实现的「快照缩略图」能力现在由 `device-mgmt/index.vue:428 snapshotImageUrl()` 内联实现，删的是抽象不是功能。真入口见上一行；远程重启在设备管理页 |
| 设备管理页 | `web/src/views/gb28181/device-mgmt/index.vue` + `DeviceMaintenanceMenu.vue` + 各 Drawer | 已有「固件升级 / 维护记录 / 重启设备」；**配置类入口建议在此新建统一 Drawer** → 见 D-6 |

### 2.2 模拟器侧（uvp-gb28181-sim）

| 主题 | 落点 | 要求 |
| --- | --- | --- |
| 命令路由 | `shared/src/commonMain/.../domain/coord/ManscdpRouterImpl.kt` | ⛔ 未识别 cmdType 只打日志、仍回 200 → 必须能区分「已处理」与「未识别」 |
| 设备控制分派 | `.../domain/DeviceControlDispatcher.kt` | ⛔ 现在**按 XML 元素名 when 分支**（`<SnapShotConfig>` / `<BasicParam>`…）而不是按 `CmdType` → A-3 要改成按 CmdType 分派 |
| 配置读取 | `.../gb28181/ConfigDownloadResponse.kt` | 现在只支持 `BasicParam` + `VideoParamOpt`，其余**显式忽略仍回 OK**（静默假成功） |
| 配置落盘 | 新增 ConfigurationStore | 现在配置只发 effect 不持久化 —— 读回来永远是默认值 |
| 可视化 | 模拟中心 UI | 设备侧行为变化要在屏幕上看得见（这是「联调时能自证」的关键） |
| 版本分支 | `SimConfig.gbVersion` | 2022/2016 差异必须显式分支，别只切信封 |
| 测试 | `:shared:jvmTest` | 每个协议分支配测试；改动后必须回归 |

### 2.3 通用验收方法（四路对照，缺一路不算过）

```
平台 HTTP 接口  →  SIP 明文（gb_sip_trace_message 解密）  →  模拟器屏幕  →  设备侧日志
```

- 只用「接口返回 200」判成功是本仓反复踩过的坑（抓拍就是典型：平台收到 200，设备侧其实 `CaptureSkipped`）。
- 相关技能：`uvp-sip-trace-triage`（取报文并解密原文）、`uvp-gb28181-sim-app`（模拟器侧改动）。

---

## 3. 前置契约（先定死，否则 A 组与 E 组每改必返工）

标准在这几处**留白或自相矛盾**（详见 `gb28181-2022-device-config-ambiguity.md` 四、A/B 档），
不先定口径就会出现「两个实现方各理解一次」。

| 编号 | 契约 | 要定什么 | 建议取值 | P |
| --- | --- | --- | --- | --- |
| ☐ 0-1 | **坐标基准** | 遮挡区坐标与 OSD 文字坐标的原点、随分辨率变化怎么换算 | 统一按**播放窗口左上角像素原点**（与 OSD 的 `TimeX/TimeY` 注释对齐）；`VideoParamAttribute` 改分辨率时按比例换算 | P0 |
| ☐ 0-2 | **必选性口径** | 注释说「可选」而 Schema 隐含必选（`default` 无 `minOccurs`）时怎么办 | **取并集**：注释与 Schema 任一为必选就按必选收，宁可多收字段 | P0 |
| ☐ 0-3 | **「不支持」与部分失败怎么表达** | `Result` 枚举只有 `OK`/`ERROR`，不设备注是哪个失败；标准**完全没规定**设备不支持某 `ConfigType` 时怎么回 | 自定并写进对接文档：不支持的 `ConfigType` 回 `Result=ERROR` + 设备日志留痕（**不再静默回 OK**）；一条报文含多个配置元素时，任一失败即 `ERROR` | P0 |
| ☐ 0-4 | **图片上传 HTTP 契约** | 标准只写「**宜**采用 http」（推荐性），方法/字段名/请求头/SessionID 回传全留白 | `POST multipart/form-data`；文件字段 `file`；SessionID 走 query；补充规定超时与重试次数 | P0 |
| ☐ 0-5 | **平台下发通道** | 新的下发类命令走哪条路：复用 `device_maintenance.go` 的排队 + `operationId` + 超时/回执模型，还是直发 | **复用维护操作模型**（已有操作记录、SIP 状态、设备错误、队列截止时间），保证「下发了没、设备收到没」可查 | P0 |
| ☐ 0-6 | **命名与 DTO 映射表** | XML 元素名 ↔ Go 结构体 ↔ 前端字段 的三方对照 | 落成一张表放 `manscdp` 文档注释里；⛔ 注意标准自身有 `SnapShot`/`SnapShotConfig`、`snapshotCfgType`/`snapShotCfgType` 两处**大小写不一致**，我们要自己定唯一名字 | P0 |

---

## 4. 分组条目

### 4.A 设备配置家族（`DeviceConfig` 写 / `ConfigDownload` 读）

> 这是整份单子里**最大的一块空白**：平台侧连「读设备当前配置」都做不到。
> 2022 相对 2016 **新增 8 个配置类型**（2016 只有 `BasicParam`、`VideoParamOpt`、`SVACEncodeConfig`、`SVACDecodeConfig` 4 个）。

| 编号 | 任务（条款） | 平台侧（后端 · 前端入口） | 模拟器侧 | 验收闭环 | P |
| --- | --- | --- | --- | --- | --- |
| ☐ A-1 | **配置读取通道 `ConfigDownload`**<br>（A.2.4.7 / A.2.6.9 / §9.3.5） | `manscdp` 加 `CmdConfigDownload` + 查询构造器（`ConfigType` 多值用 `/`）+ 应答解析；多响应按 SN 聚合（依赖 F-1）；新建 `controllers/device_config.go`；路由 + 菜单/按钮权限 + 三方言迁移；前端只读视图 | `ConfigDownloadResponse.kt` 扩到 12 类 + 走向落盘真值 + 按 `CmdType` 分派 | 平台点「读取设备配置」→ SIP 明文含 `ConfigDownload`+`ConfigType` → 设备日志显示收到 → 平台展示 12 类**真实值**（非 mock） | P0 |
| ☐ A-2 | **配置下发通道 `DeviceConfig`**<br>（A.2.3.2.1 / A.2.6.8 / §9.3.1 e)） | `manscdp/device_config.go` 写构造器（一条报文可带多个配置元素）；应答按 `CmdType=DeviceConfig` + `Result` 解析；下发走 0-5 决定的通道 | 新增 `CmdType=DeviceConfig` 的独立分派分支（现靠元素名猜） | 平台下发任一配置 → 设备回 `CmdType=DeviceConfig` + `Result=OK` → 平台记录并展示结果 | P0 |
| ☐ A-3 | **模拟器配置落盘 + 变更可见**（工程地基） | 无需改（A-1 读回来的即真值） | 新增 ConfigurationStore 持久化；所有配置读写走它；变更产生 effect → 模拟中心可见「配置已变更：xxx」 | 下发配置 → **重启模拟器** → 再读回仍是新值 | P0 |
| ☐ A-4 | **`OSDConfig` 前端 OSD 字符叠加**<br>（A.2.1.12） | 写+读 DTO（时间显示开关/方式/坐标/文字内容/显示方式）；前端 OSD 编辑器（可拖动位置预览）；下发 | 接自家 OSD 渲染管线 `osd/OsdRenderer.kt` + `OsdFontAtlas.kt` + `OsdTextPass.kt` + `IosOsdBitmapRenderer.kt` → **画面叠加真的变** | 平台改 OSD 文字 → 模拟器画面文字真的变（前后截图对比） | P0 ⭐ |
| ☑ A-5 | **`VideoParamAttribute` 视频参数属性**（2022 新增）<br>✅ **2026-09-18 完成**（设计+实施全记录见 `docs/gb28181-2022-video-param-attribute-panel.md`）。落地要点五条：<br>① **码流分段由「目录」决定**，不是由 `VideoParamOpt` 决定 —— 出处是产物 `Catalog Item/Info/StreamNumberList`（2022 独有，可多值 `/` 分隔）。为此补落 `gb_channel.stream_number_list` 列 + 加进 `ChannelVO`；面板按它渲染几路码流，未上报时退化成「按已回读到的行」。<br>② **`VideoBitRate` 是条件必选**：仅 `BitRateType=1`(CBR) 时出现；VBR 时**报文里不含该元素**（前端把该格**禁用**而不是「填了忽略」，后端同样拒发）。<br>③ **取值唯一出处是附录 G 的 SDP `f` 字段（标准页 130）**：`VideoFormat` 1=MPEG-4/2=H.264/3=SVAC/4=3GP/5=H.265；`Resolution` 1=QCIF…6=1080P，其余用 `WxH`；`FrameRate` 0~99；`BitRateType` 1=CBR/2=VBR；`VideoBitRate` 0~100000 kb/s。**报文里发码值，人读串只允许出现在前端**（库列原样存码值串，否则对账会比两套表示）。<br>④ **对账闭环**：写入应答 A.2.6.8 **没有回显** ⇒ `Result=OK` 只说明「收到并接受」，**不许当终态**。服务层在 ack 的**同一事务**里追加一条 `ConfigDownload` 对账子 operation，逐格比对下发值与回读值；不一致标 `VIDEO_PARAM_RECONCILE_MISMATCH` 落 `mismatch`（是**能力边界提示，不是失败**）。<br>⑤ **2016 不适用**：`VideoParamAttribute` 不在 2016 的 4 个 `ConfigType` 里。解法是**不加 profile 门禁 + 强制回读**：`Result=OK` 但应答**没带**该元素 ⇒ `type_absent`，等价「设备不支持」（最可靠判据）。门禁只用于「平台自动下发」（`Capabilities.VideoParamAttribute`），**绝不挡操作员手点**。<br>⚠️ 仍待：模拟器侧 `ConfigDownloadResponse` 只输出 2 类配置（其余静默丢弃）、A-2 的 `DeviceControlDispatcher` 只有 `<BasicParam>` 一个分支 ⇒ **端到端（真机/模拟器）验收前可能要补模拟器侧** | 改**实际编码参数**（H.264/H.265、分辨率、帧率、码率），出的流参数真变 | 平台改分辨率 → 拉流实测分辨率变化（探针/ffprobe） | P0 |
| ☐ A-6 | **`PictureMask` 视频画面遮挡**（2022 新增，A.2.1.17） | 写+读（≤4 区域）；前端**可视化画框**编辑器；区域坐标按 0-1 契约 | 画面上真的绘遮挡；遮挡方式与是否影响录像按契约写明 | 平台画 2 个区域 → 模拟器画面出现遮挡（截图） | P1 |
| ☐ A-7 | **`FrameMirror` 画面翻转**（2022 新增） | 写+读；前端下拉（关 / 上下 / 左右 / 中心） | 渲染管线真翻转（含录像/回放是否同步翻转的取舍） | 平台选「左右镜像」→ 画面左右翻转（截图对比） | P1 |
| ☐ A-8 | **`BasicParam` 基本参数**（2016 已有，A.2.1.19） | 写+读（设备名称 / 注册有效期 / 心跳间隔）；前端表单 | **真落盘并生效**（心跳间隔真改、有效期真用）；现只提 4 个字段发 effect，不落盘 | 平台改心跳 30s → 抓包/日志看心跳实际 30s；改名称 → 目录与设备信息里名字变 | P1 |
| ☑ A-9 | **`VideoParamOpt` 视频参数范围**（2016 已有）<br>⛔ **原写「用回读范围约束 A-5 表单 / 输入范围取 A-9 的设备回读值」有误（2026-09-18 随 A-5 落地核出）**：`VideoParamOpt` 只有 `DownloadSpeed` + `Resolution` 两个字段，**帧率与码率没有范围可用**；而 `Resolution` 的**取值出处是附录 G 的码值表**（1-6 或 `WxH`），不是「设备支持哪些档位」。<br>⇒ **A-5 表单的合法性判据一律取附录 G**（`manscdp.ValidateVideoParamItems` 与前端 `videoParamCodec.ts` 同一套规则，严格发）；A-9 的定位**降级为「展示设备支持的档位」**这一只读信息（若日后要做，也只作**可选提示**，不作合法性判据 —— 设备报的范围窄于标准时不该把标准允许的值判成非法）。<br>⚠️ 模拟器侧已修一处同源缺陷（`ConfigDownloadResponse` 原发人读串 `"1920×1080"`，且只报当前值不报档位全集）→ 现发码值全集 `4/5/6`；`CatalogNode.resolution` 的 `*` 分隔归一为 `WxH` **仍未动**（见 `gb28181-2022-video-param-attribute-panel.md` §九-2） | 设备**实际支持**的档位可见；A-5 表单的合法性判据见左栏 | 读应答字段完整度核对（现支持但需核字段） | 平台能展示设备支持的档位；A-5 表单**不因 A-9 缺失而阻塞**（合法性只认附录 G） | P1 |
| ☐ A-10 | **`VideoRecordPlan` 录像计划**（2022 新增） | 写+读；前端计划编辑器（星期 × 时段 × 码流） | 落盘 + 屏幕展示「已生效的录像计划」 | 下发计划 → 读回一致 → 设备屏幕显示该计划 | P1 |
| ☐ A-11 | **`VideoAlarmRecord` 报警录像**（2022 新增） | 写+读；前端开关/条件 | 落盘 + 与报警上报联动（触发报警时标记录像） | 下发开关 → 读回一致 → 设备侧报警录像状态可查 | P1 |
| ☐ A-12 | **`AlarmReport` 报警上报开关**（2022 新增，A.2.1.18） | 写+读；前端按事件类型勾选 ⚠️ 标准只定义 2 个开关且**都必选** → 按 0-2 契约定「只想改一个」怎么办 | 开关**真生效**：关掉移动侦测 → 该类报警不再上报 | 关掉某类 → 触发该类事件 → 平台**收不到**；打开 → 收得到 | P1 |
| ☐ A-13 | **`SVACEncodeConfig`/`SVACDecodeConfig` 读取**（2016 已有） | 读并明确展示「设备不支持 SVAC」 | 按 0-3 契约表达不支持（现显式忽略仍回 `OK`） | 平台点读 SVAC → 得到明确「不支持」，不是空白 `OK` | P2 |

### 4.B 目录、设备信息与编码

| 编号 | 任务（条款） | 平台侧（后端 · 前端入口） | 模拟器侧 | 验收闭环 | P |
| --- | --- | --- | --- | --- | --- |
| ☑ B-1 | **目录通道属性字段 · 设备侧输出**（§9.3.1 / 附录 A）<br>⛔ **原写「2022 九字段」有误**（2026-09-17 核原文）：`PositionType` / `UseType` 是 **2016 字段、2022 已删除**；且这一组字段**在 `<Info>` 容器内**，不是 `Item` 直接子级 | 无需改（但 B-2 不做则平台看不到） | ✅ **2026-09-17 完成** —— `CatalogNotifyBuilder.renderItem` **按 `effectiveGbVersion` 双分支**：<br>· 共有（`<Info>` 内）：`PTZType` / `RoomType` / `SupplyLightType` / `DirectionType` / `Resolution`<br>· 共有（`Item` 层）：`IPAddress` / `Port`<br>· **V2016**：`<Info>` 内多出 `PositionType` / `UseType`，`BusinessGroupID` **留在 `<Info>` 内**<br>· **V2022**：`<Info>` 内多出 `PhotoelectricImagingType` / `CapturePositionType` / `StreamNumberList`，`BusinessGroupID` **提到 `Item` 层**<br>⛔ **不得合并成「一条报文两版都写」的超集** —— 严格 XSD 校验下两版都不合规，且同字段双写会产生「哪个说了算」歧义<br>⛔ 已删除死代码 `CatalogResponse.buildGb2022Fields()`（它把字段平铺在 `Item` 层，两版都不符合）<br>⚠️ 顺带修正 4 个枚举值域：`RoomType` 编码曾写反（标准 1-室外 / 2-室内）、`PositionType` 值域整体重写（1-省际检查站…10-交通干线）、`PtzType` 补 2022 的 5-7、`SupplyLightType` 补 2022 的 4/9 | SIP 明文/设备日志里目录应答含上述字段，且 **2016 与 2022 两种形态各自正确**（2016 支不缺 `PositionType`/`UseType`；2022 支不出现它们） | P0 |
| ☑ B-2 | **目录通道属性字段 · 平台解析+存储+展示**（§9.3.1 / 附录 A）<br>✅ **2026-09-18 完成**。<br>· **解析层**：新增 `manscdp.CatalogInfo`（12 字段，全 `string` 收原文）+ `CatalogItem.normalize()`（`Item` 层与 `<Info>` 层双读、`<Info>` 优先），`ParseCatalogResponse` / `ParseCatalogNotify` 两条路径都调用；新增导出 `ParseAttrInt()` 供适配器转数值。<br>· **⛔ 刻意不做版本分支**（与 B-1 的设备侧相反）：设备注册声明的 `GbDevice.EffectiveVersion` 有 `default:2016`，对 2022 设备会误判；而 2016 的 `PositionType`/`UseType` 与 2022 的 `PhotoelectricImagingType`/`CapturePositionType` 在 XSD 上**互斥**，谁来上报就落谁，天然可反推设备实际形态 → 两组**并存落库**即可，且比按 `effectiveGbVersion` 分支更稳。（`RoomType` 两版编码一致；`DirectionType` 两版一致;`SupplyLightType` 2016 只到 3、2022 加 4/9 → 用并集表不会误判。）<br>· **落库层**：`gb_channel` 新增 **十列**（`room_type`/`supply_light_type`/`direction_type`/`resolution`/`ip_address`/`port` + 版本独有的 `position_type`/`use_type`/`photoelectric_imaging_type`/`capture_position_type`），六文件迁移 `2026-09-18-channel-catalog-attributes{,-postgresql,-sqlserver}{,-down}.sql` + 4 个快照同步（`gb_channel.sql` / 三个全量快照）。**已在 220 开发库实测 up→幂等重跑→down→再 up**。<br>· **展示层**：`ChannelVO` 扩 10 字段；新增 `web/src/views/gb28181/device-mgmt/channelAttributeText.ts`（枚举映射 + `channelAttributeEntries()` + `catalogShapeFromAttributes()`）与通道详情抽屉「设备上报属性」区块；`0`/`''` 一律渲染为**未上报**（弱化斜体），与「值为 0」区分开；版本独有属性带「仅 2016 / 仅 2022」角标，并用 `catalogShapeText()` 直接标出设备报的是哪一版形态。<br>· **枚举同步**：`devicemgmt.go` 手工编辑校验 `>4` → `>7`（消息同步）；`sys_dict_item` 补 `5遥控半球 / 6多目设备的全景·拼接通道 / 7多目设备的分割通道`；`basicPTZCapability` `case 1,2,4` → `case 1,2,4,5`（**6/7 故意留 Unknown** —— 标准未声明多目通道具备云台）。⚠️ **前端无同源错处**：真控制台 `PlayConsoleLinked.vue` 的云台面板按**权限**（`gb28181:ptz:*`）门禁、不看 `ptzType`；曾有的 `ControlConsole.vue`（原型里的 `[1,2,4]`）已于同日作为孤儿组件删除（commit `ac491830`），故前端无需同步。<br>· **回归锚点**（`subscribe/catalog_test.go`）：2016 形态（`BusinessGroupID` 在 `<Info>` 内）与 2022 形态（在 `Item` 层）**各一条 XML→管道→目录树挂载**端到端用例 + 一条「不带 `<Info>` 的 UPDATE 不得清零已知属性」数据保全用例。变异自检：把 `<Info>` 内 `BusinessGroupID` 回填短路后，**只有 2016 用例变红**、2022 用例保持绿 —— 证明锚点确实守住了 ① 的回归。<br>✅ 门禁：`go build ./...` 干净；`go test ./app/gb28181/...`（60 包）全绿；`vue-tsc --noEmit` 干净；前端 `vitest` 1423/1424（唯一红项 `zlm/SchedulerStrategy.test.ts` 为 **develop 上既存失败**，与本单无关，见 §9 P-1）。<br>⛔ **原写「2022 九字段」有误**（同 B-1）：字段在 `<Info>` 容器内、非 `Item` 子级；`PositionType`/`UseType` 是 **2016 字段**不是 2022 新增 | ⛔ **开工前现状（2026-09-18 复核，含一次自我纠错；下列为历史记录，现已全部修复）**：`manscdp/catalog.go:33-51` 的 `CatalogItem` **只声明了 `PTZType int` + `BusinessGroupID string`**（且**都在 Item 层**），其余字段连结构体字段都没有；`grep -E 'xml:"Info' app/` → Catalog 的 `<Info>` **无任何反序列化**（`xml:"Info"` 只出现在 MobilePosition/报警复位/云台控制/广播，可照抄这个嵌套写法）。⚠️ **前一版此处误写「`BusinessGroupID` 解析完即丢、0 消费者」——错**（根因：BSD grep 不支持 BRE 交替写法「反斜杠+竖线」→ 静默空结果）。实际两者**都有消费者，且都是承重字段**：<br>· `BusinessGroupID` → `catalog/pipeline.go:96-105 resolveBusinessParent` → `SplitParentIDs(it.BusinessGroupID)` 决定**通道在目录树挂到哪个父节点**（业务组织优先于物理 ParentID）。`SplitParentIDs("")` 返回**空切片**（`dto.go:30-42` 对 `""` 直接 `continue`）。<br>· `PTZType` → `catalog/upsert.go:150,185` 落 `gb_channel.ptz_type` 列；`handler/catalog.go:234`、`subscribe/catalog.go:145`、`DeviceAdvancedControl` 的 `ParseControlCapabilities(channel.Capabilities, channel.PTZType)` 也读它。<br>⛔⛔ **由此确认三个真实缺陷**：<br>① **`PTZType` 被声明在 `Item` 层，但标准把它放在 `<Info>` 内** → Go 不递归 → **合规设备（含 B-1 后的模拟器）上报的 `PTZType` 平台根本收不到，`gb_channel.ptz_type` 恒为 0**（模拟器 B-1 前压根不发该字段，B-1 后发在 `<Info>` 里，两种情况都读不到 → 无回归，但功能一直缺失）。<br>② 🔴 **B-1 引入一处 2016 回归（本行必须与 B-1 同批的直接理由）**：模拟器 V2016 现在把 `BusinessGroupID` 写进 `<Info>`（**这是 2016 标准要求的正确位置**），而平台只读 Item 层 → 2016 设备 `it.BusinessGroupID` 变空 → **目录树里通道不再挂到业务组织下**（退化为只按 ParentID/行政区划）。**是平台侧潜在 bug 被 B-1 暴露，不是 B-1 写错**。<br>③ `devicemgmt.go:636` 手动编辑校验 `*body.PTZType > 4` **显式拒绝 2022 的 5/6/7**；`gb_channel.ptz_type` 列注释也是 2016 值域（`0未知1球机2半球3固定枪机4遥控枪机`）→ 平台侧枚举需同步扩。<br>⛔ `PTZType` 现声明为 `int`，而 2022 附录 A 已改为 `string` → 需一并改。<br>⭐ 要做：补 `<Info>` 反序列化（`Info struct` 内含各版本字段）+ `Item` 层 `IPAddress`/`Port`；`gb_channel` 落库列 + **三方言迁移 up/down 齐全**；目录列表/详情展示（云台类型、室内外、补光方式、分辨率…）。<br>⛔ **范围界定（2026-09-18 核，回答「级联用不用」）**：**级联不用这些字段，本行也不含级联**。级联上行走独立一套 —— `cascade/catalog/responder.go:142 catalogItemXML` 只有 **11 个字段**（DeviceID/Name/Manufacturer/Model/Owner/CivilCode/Address/Parental/ParentID/Secrecy/Status，**连 `<Info>` 容器都没有**），`cascade/catalog/snapshot.go:45 CatalogItem` 同样无这些属性；且级联**想发也发不了**（`gb_channel` 没这些列）→ **B-2 是级联能发这些字段的前置条件，但 B-2 ≠ 级联要改**；级联若需发出，是**另一条独立单**（先确认客户/上级平台是否真要求，再开）。<br>⛔ **原约束修正（2026-09-18，落 B-2 时发现原表述会误伤实现）**：原文写「解析与展示**必须按 `effectiveGbVersion` 双分支，禁止全局替换字段表**」。**「禁止全局替换字段表」这个意图保留且已满足**（2016 `RoomType`(1-2)/`PTZType`(1-4) + `PositionType`/`UseType` 与 2022 `PTZType`(1-7) + `PhotoelectricImagingType` 等**并存**，没有任何一版的字段被丢掉；`RoomType`/`DirectionType` 两版编码一致故共用表，`SupplyLightType` 用并集表），**但「必须按 `effectiveGbVersion` 双分支」这条做法在平台侧被否决**：<br>· `GbDevice.EffectiveVersion` 带 `default:2016`，对没声明版本的 2022 设备会**误判成 2016**，按它分支会整段丢掉 2022 属性；<br>· 目录报文里 2016 的 `PositionType`/`UseType` 与 2022 的 `PhotoelectricImagingType`/`CapturePositionType` 在 XSD 上**互斥**，**报文自己就带了版本证据**，比设备注册声明更可靠。<br>→ 故平台侧采用**「两组并存落库 + 由上报了哪一组反推形态」**（`catalogShapeFromAttributes()`），设备侧（模拟器）仍按 `effectiveGbVersion` 双分支输出 —— **两侧做法不同是有意的**：设备侧要「按声明挑一套写」，平台侧要「不管声明、按收到的识别」。<br>⚠️ 顺带：B-1 已修正模拟器侧 4 个枚举值域，**平台侧同源错处已定位**（`gb_channel.ptz_type` 列注释 + `devicemgmt.go:636` 的 `>4` 校验）→ 需一并扩到 2022 值域 | 无需改 | 平台通道详情能看到设备上报的云台类型/室内外/补光方式/分辨率等真实值；**且 2016 设备（`PositionType`/`UseType`、`<Info>` 内 `BusinessGroupID`）与 2022 设备（`BusinessGroupID` 在 `Item` 层、`PTZType` 5-7）上报后都能正确区分与展示** | P0 ⚠️**必须与 B-1 同批** |
| ☐ B-3 | **组织级查询与应答**（附录 J） | 支持按组织维度发起目录查询并解析 | 行政区划 / 业务分组 / 虚拟组织三种节点均可作查询目标；虚拟组织 `ParentID` 按 2022 新语义 | 分别查三种组织节点 → 各自返回正确子树 | P1 |
| ☐ B-4 | **行政区划节点可用**（附录 E/J）<br>⛔ **原描述「`typeCode` 补全」方向有误**（2026-09-17 核附录 J）：区划条目在 `DeviceID` 位直接放 **2/4/6/8 位民政区划码本身**，**没有类型码段可补** → `CatalogTree.kt:12 AdministrativeRegion("", 1)` 的空 `typeCode` **是合规的，不是缺陷**。真正待办是「能否作为可挂载的真实区划节点出现在目录树」 | 目录树正确展示行政区划层级 | 核 `AdministrativeRegion` 是否被当作可挂载节点处理（当前只能拿 VirtualOrg + CivilCode 模拟）；若平台要求区划节点具备 `ParentID` 分层，需核对附录 J 的父子表达方式 | 设备建「省-市-区」区划节点 → 平台目录树正确分层 | P1 |
| ☐ B-5 | **附录 O 摄像机采集部位类型代码** | 枚举 + 展示 | 上报部位类型 | 设备上报部位 → 平台正确展示 | P2 ⚠️**待核原文**该字段在哪个命令里 |
| ✗ B-6 | **附录 A 扩充：`Info`→`ExtraInfo`、Channel 字段格式变更**<br>⛔ **已销单 2026-09-17 —— 该改动不存在**：全 166 页 2022 标准 OCR 中 `ExtraInfo` 出现 **0 次**，容器名**仍是 `Info`**。真正的 2022 变化是**字段内容**（新增 `PhotoelectricImagingType` 等、`PTZType` 扩到 1-7、`BusinessGroupID` 上提），**不是容器改名** → 该变化已被 **B-1/B-2 覆盖**，勿再按原名开工 | — | — | — | ✗ 销单 |
| ☐ B-7 | **目录多父级**（附录 H/N） | 已有 A/B 拆分（`gb_channel_mount`）→ 复核是否满足 2022 多父级语义 | `CatalogNode` 仅单 `parentId` → 支持多父级 | 同一通道挂两个父节点 → 平台两处都能看到 | P2 |
| ☐ B-8 | **20 位统一编码校验**（附录 E） | 校验类型码段 / 区划段 / ID 类型码与节点类型一致 | `IdEncoder.kt`、`CatalogTreeStore.kt:243` 同上（现只校验 20 位全数字） | 编一个区划段非法的 ID → 被拒且给出明确原因 | P2 |

### 4.C 设备查询（2022 五大新增查询的收口）

| 编号 | 任务（条款） | 平台侧（后端 · 前端入口） | 模拟器侧 | 验收闭环 | P |
| --- | --- | --- | --- | --- | --- |
| ☑ C-1 | **存储卡状态查询**（A.2.4.14 / A.2.6.16）✅ **2026-09-17 完成**<br>原写「2022 五大新增查询里唯一没闭环」→ 现已闭环，**五大新增查询全部收口**<br>⭐ **2022 新增性已核**：2016 版附录 A OCR（`.workbuddy/ocr/appA2016.txt`）里 `SDCard` / `存储卡` **零命中** → 确属 2022 新增（不是改名） | ✅ 报文层 `manscdp/storage_card.go`（`BuildSDCardStatusQuery` / `BuildSDCardStatusQueryWithProfile` / `ParseSDCardStatusResponse[For]`，**27 单测**）<br>✅ 落库层 `models/gb_storage_card.go`（`gb_device_storage_card`，唯一键 **(device_id, target_code, card_id) 三元组**）<br>✅ 收发放 `ptz/storage_card.go`（`RefreshStorageCards` / `applyStorageCardResponse` / `persistStorageCardsWithDB`，**9 单测**）<br>✅ 分派 `ptz/handler.go`（`case manscdp.CmdSDCardStatus` + 「只查未终态」白名单）+ 重试重建 `ptz/scheduler.go` + `handler/message.go` 两处 `TxPTZ` 归类<br>✅ 接口 `GET /api/gb28181/device-mgmt/channel/:id/storage-cards[?refresh=true]`（`controllers/device_storage_card.go`）+ `routes.go`<br>✅ 权限与迁移：`sys_api` / `sys_menu_api`（绑既有 `gb28181:ptz:view`）+ `sys_casbin_rule`；**三方言 up/down 共 6 个文件**（契约门禁 `contractThreshold=2026-08-14` 已过）<br>✅ 前端 `PlayConsoleLinked.vue` 控制台「高级」tab（见右栏） | ✅ `DeviceControlSubRouter.kt`：`CmdType` 由自造的 `StorageCardStatusQuery` 改标准 **`SDCardStatus`**（旧名仍作为兼容入口，但应答一律标准名）+ 容器 `StorageList` → `SDCardStatusInfo/Item` + 字段改为 `ID`/`HddName`/`Status`/`FormatProgress`/`Capacity`/`FreeSpace` + `SumNum`；`Status` 取值由 `Normal` 改标准枚举<br>✅ 随机假数据（可注入 `Random`，惯例同 `MockGpsSource`）：**张数/容量只掷一次（物理属性）、状态与剩余空间每次抖动**；0/1/2 张、8G~128G；`FormatProgress` 仅在 `formatting` 时输出（**其余状态不得补 0**）<br>✅ `DeviceControlSubRouterTest` **19 例**（含 40 seed 不变量轮跑、同实例连查两次容量/名称必须稳定、旧名入口→标准名应答；**本轮 +3 例锁单一真源**：报文读数 ≡ Model 读数、查询命令不动 `lastCommand`、`storageCardQueryCount` 每次 +1）<br>✅ **单一真源** `shared/domain/StorageCard.kt` 的 `VirtualStorageCards`（`AppEngine` 装配 → `ManscdpRouterImpl` → `DeviceControlSubRouter`，UI 与报文共用同一实例）+ `VirtualStorageCardsTest` **6 例**<br>✅ **设备屏幕「存储卡」OSD** `ui/simulate/StorageCardPanel.kt`（落在模拟中心 3D 画布**右上角**，避开右下角 PtzThumbnail 与左上角 Aux 角标；`LaunchedEffect(storageCardQueryCount)` → 亮起 1.8s 衰减，**触发键用计数不用时间戳**）+ `StorageCardPanelUiTest` **9 例** | 平台点「存储卡状态」→ 平台列表显示容量/剩余/状态，**同时设备屏幕右上角那张卡片亮起、显示同一份读数**（两侧数字必然一致，见刻意决策 ⑦） | ☑ P1 |
> 已实现的 2022 新增查询（看守位 / 巡航轨迹列表 / 巡航轨迹详情 / PTZ 精准状态 / **存储卡状态**）**两侧 + 前端入口都齐**，
> 列入第 5 节回归清单，不在本组重复开工。**2022 五大新增查询至此全部收口。**

> ⭐ **C-1 八处刻意决策**（改前必读，避免"顺手改回去"）：
> ① **不做 2022 版本门禁** —— 理由同 `RefreshHomePosition`：profile 只是登记的说法、不是事实；
>    被登记成 2016 而实际按 2022 应答的设备，发这一帧是平台唯一的发现手段。
> ② **不走 `queryStage` 聚合、不加 `QueryKind` 类** —— 附录 M 点名的多响应聚合三类是
>    「目录查询响应 / 文件查询响应 / 订阅后的通知消息」，**不含 `SDCardStatus`**；A.2.6.16 本身就是完整列表
>    （`Item maxOccurs="8"`），一次应答即终态。原计划里的「`ptz/query.go` 的 `QueryKind` 加一类」**已作废**。
> ③ **清理"本轮没再出现的卡"用 `source_operation_seq < operation.ID`，不用「不在本次列表里」** ——
>    后者会误删另一个 `target_code` 的行，或更晚一次查询刚写进来的行
>    （专门防回归测试：`TestPersistStorageCardsDoesNotTouchOtherTargets`）。
> ④ **不校验 `Result`** —— A.2.6.16 的 schema 里**没有 `Result` 元素**，校严了会把合规应答判成失败。
> ⑤ **未识别 `Status` 落 `unknown` 而非 `error`** ——「设备报了个新状态 ≠ 卡坏了」；
>    报文层 `SDCardState` / 落库层 `StorageCardState` / 前端 TS 联合，**三层字面量一致**。
> ⑥ **`FormatProgress` 用 `*int`** —— 标准标了 `minOccurs="0"`，必须区分「设备没给」与「给了 0」。
> ⑦ **模拟器侧读数只掷一次骰子**（2026-09-18 补）—— `shared/domain/StorageCard.kt` 的
>    `VirtualStorageCards` 一个实例同时喂「回给平台的报文」与「设备屏幕上的 OSD 卡片」；
>    `sendStorageCardStatusResponse` 里 `read()` **只调一次**再对半分给两边。
>    否则两处各掷各的骰子 → **平台上 64G / 设备屏幕上 32G**，是验收时最难解释的一种"假失败"。
>    同时：**查询命令绝不写 `lastCommand`** —— 它是云台活动信号，`CameraActivity` 靠它清零看守位空闲倒计时。
> ⑧ **OSD 亮起用 `storageCardQueryCount` 当 key，不用 `storageCardQueriedAtMs`**（2026-09-18 补）——
>    同一毫秒内的两次查询时间戳相同，`LaunchedEffect` 键不变 → **动效不重播**（连点两次只亮一次）。
>
> ⛔ **标准本身的两个反直觉点**（写代码时最容易做错）：
> · **请求与应答的 `CmdType` 同名，都叫 `SDCardStatus`** —— 与「查询问 / 应答答」那族（如 `HomePositionQuery` / `CruiseTrackListQuery`）的命名习惯**不同**，别照抄那族的命名。
> · **元素名是 `HddName`**（不是 `SDCardName`），照字段语义猜名字会全部解析不到。

### 4.D 设备控制与维护

| 编号 | 任务（条款） | 平台侧（后端 · 前端入口） | 模拟器侧 | 验收闭环 | P |
| --- | --- | --- | --- | --- | --- |
| ☐ D-1 | **目标跟踪 `TargetTrack`**（A.2.3.1.14） | 下发 + 前端入口（选择目标 / 开启关闭） | 现只解析写 effect → 至少回**合规应答** + 屏幕状态可见；真行为依赖设备 AI，可用「模拟目标框」表达 | 平台开目标跟踪 → 设备屏幕显示跟踪状态 | P2 |
| ☑ D-2 | **格式化 SD 卡 `FormatSDCard`**（A.2.3.1.13）✅ **2026-09-20 完成（平台 + 模拟器双侧）**<br>⛔⛔ **本轮最重大结论:这是一条「无应答命令」**。标准三处证据:① **9.3.1 d)**（OCR 行 1300-1306）把「存储卡格式化」与云台控制 / 远程启动 / 强制关键帧 / 拉框放大缩小 / PTZ 精准控制 / 目标跟踪**列在同一句**——「目标设备**不发送应答命令**」;② **表 1 序号 12** 的应答命令章节写作「**（无）**」。⇒ `ResponseRequired: false`（同族先例 `TeleBoot`）。⛔ 设成 `true` 的后果**不是「多等一会儿」,而是把一个成功当失败报**:设备按标准不回 → operation 排到 transport deadline 才落 timeout/unknown → 前端把一次正常下发显示成「结果未知」。<br>⭐ **「格式化成没成」的唯一判据 = 事后再查一次 SDCardStatus**（`Status=formatting` + `FormatProgress` 0-100,或已回到 `ok`/`unformatted`）。⛔ 2022 全文里 SDCardStatus **只有「查询 + 应答」这一对,没有 NOTIFY/推送那一半** ⇒ 不主动查永远看不到进度。平台侧因此做 **8 轮 × 6s ≈ 50s 观察窗**（`FORMAT_WATCH_ROUNDS`/`FORMAT_WATCH_DELAY_MS`）,每轮 = 一次真实 SIP MESSAGE;卡回到非 formatting 立即收工、切页/手动查询立即停。⛔ 续跟踪只能在**重读完列表之后**判断（`await load()` 之后）,否则拿上一轮事实会过早收工。<br>⛔ **`DiskNum` 是自造名（2022 全文 / 2022 附录 A / 2016 附录 A 三处 0 命中）,本轮从两侧拔掉**。**`FormatSDCard` 的元素值本身就是卡号**:A.2.3.1.13 = `<element name="FormatSDCard" minOccurs="0">` + `restriction base="integer"` + `<minInclusive value="0"/>`,注释原文「SD 卡编号,从1开始编号。**该值0时,对所有存储卡进行格式化**」。⛔ **解析失败绝不回落成 `0`**（= 全部卡 = 最重的破坏性操作）;模拟器原 `?: 0` 已改为「记 Warning + 只写 `lastCommand` + 不下发」,平台侧请求体用 `*int` 区分「没传」与「给了 0」,前端**不得对 0 做 falsy 兜底**。<br>⛔ **必须走独立路由 `POST /channel/:id/storage-cards/format`**:`POST /channel/:id/device-control` 整条绑 `gb28181:device:control`（`2026-09-05-button-permission-catalog.sql:769`）⇒ 做成它的 action 在 casbin 层同权,**新权限码形同虚设**。新增独立权限码 **`gb28181:device:format_sd`**（照「设备重启」先例;三方言 up/down + 三快照齐全）。**破坏性动作门禁顺序 = 登录 → 显式确认 → 权限 → 参数**（确认在权限**之前**,避免泄露「这账号有没有格式化权限」）。<br>⭐ **UI 措辞纪律**:无应答命令的结果只能叫「已下发」,**不允许出现「已完成」**。<br>⭐ `ptz.Execute` 双语义（`operation.go:229-334`）:**无应答命令由 Execute 同步发出**,一次调用后即终态 `sent` ⇒ 断言从 `queued` 改 `sent`、`sender.calls` 从 0 改 1。 | ✅ 后端: `ptz/storage_card.go`（`FormatStorageCard` + `ResponseRequired:false` + `MaxAttempts:1` + 强制回读对账）+ 独立控制器 `device_storage_card.go` + 新权限码三方言迁移/三快照 + 维护记录<br>✅ 前端: `api/gb28181.ts` 新增 `formatStorageCard`（统一补 `confirmed:true`,独立路由）+ `StorageCardFormatDialog.vue`（二次确认,`oneWay` 由**服务端** `responseRequired` 字段判断,不前端写死）+ `StorageCardStatusPanel.vue`（卡片行「格式化」按钮,**禁用而非隐藏**;`canFormat` fail-closed;下发后进 8 轮跟踪,`freshnessText` 跟踪态优先）+ `index.vue` 接线 `canFormatStorageCard = hasPermission("gb28181:device:format_sd")` | ✅ 模拟器（`uvp-gb28181-sim`）: `VirtualStorageCards` 原为**只读随机源** ⇒ 平台下发后设备读数毫无变化、闭环在设备侧断开（且**看起来正常**）。本轮补写操作 `format(cardIndex): StorageCardFormatOutcome`（`Accepted`/`Rejected`）+ 格式化会话 + **完成态持久覆盖**（否则剩余空间会从满值掉回去）+ 注入 `nowMs` 时钟 + `FORMAT_DURATION_MS = 15_000`（由平台观察窗反推:观察窗里依次看到 ≈0% → ≈40% → ≈80% → 已完成）。落点 **5 处**（单一真源类 / `DeviceControlActions` 接口 / `SystemHandler` / `ManscdpRouterImpl` 匿名对象**必须用注入实例** / `SimulateScreen` 措辞）+ **源码级装配防回归测试**（`StorageCardFormatWiringTest`,4 条） | 授权用户二次确认后下发 → 存储卡剩余变满值;未授权用户按钮不可用 ✅<br>✅ **2026-09-20 海康真机验证通过**：平台下发 → 真设备格式化成功。这一跳**反证了 `ResponseRequired:false`**——若当初写成 `true`,海康按 9.3.1 d) 不回执,前端只会把这次成功显示成「结果未知」。<br>⛔ **待回填**：真机上的**中间态与耗时**（有没有真看到 `formatting` + `FormatProgress`?耗时是否在 8×6s ≈ 50s 观察窗内?）——决定观察窗轮数要不要调;模拟器侧 `FORMAT_DURATION_MS=15_000` 仍是按观察窗反推的估值,未与真机对齐。 | P2 |
| ☐ D-3 | **固件分发 HTTP 服务**（§9.13） | 平台托管固件文件 + 生成 `FileURL`（现在靠外部喂 URL）+ 下载鉴权/过期 | 真下载固件（替代 5s 假进度） | 平台选固件 → 设备真发起 HTTP 下载 → 进度真实递进 | P1 |
| ☐ D-4 | **模拟器升级真进度**（§9.13） | 复核升级结果状态机能接收各阶段（已实现） | `SystemHandler.kt:136` 的 5s 假进度 → 真下载 + 分阶段进度 + 状态机 | 升级过程中平台看到阶段推进，最终收到 `DeviceUpgradeResult` | P1 ⚠️**与 D-3 成对** |
| ✗ D-5 | ~~**控制台「待接入」按钮接线**（工程）~~ ⛔ **2026-09-18 销单 —— 前提失效，该做的基本都已经做了**<br>原写「`ControlConsole.vue:365-367` 三个占位 → 接后端已有接口」，但 `ControlConsole.vue` 是**孤儿文件**（`cb49bc1b` 2026-07-23 已被 `PlayConsoleLinked` 替换，此后无人 import、无菜单行指向）→ 那 3 个占位**从未上线**，接它等于给死文件接线。<br>⭐ 复核真实链路（2026-09-18 逐条核）→ **4 个动作全已是真实调用**：<br>· `IFameCmd` 请求关键帧 → `PlayConsoleLinked.vue:4398` `runAdvancedAction('iframe')`<br>· `RecordCmd` 录像 → 同文件 `4402`/`4405` `record_start`/`record_stop`<br>· `GuardCmd` 布防·撤防 → 同文件 `4415`/`4418` `guard_set`/`guard_reset`<br>· `TeleBoot` 远程重启 → **不在控制台**，在设备管理页 `device-mgmt/DeviceRebootDialog.vue`（← `index.vue:85`）+ 权限门禁 `gb28181:device:reboot` + 维护记录 + 轮询<br>（`runAdvancedAction` = `PlayConsoleLinked.vue:3333` 真链路：`controlDevice(channelId,{action,idempotencyKey})` → 按 `operationId` 轮询到终态）<br>⭐ **且「设备信息」连缺口都不算**：后端 `handler/deviceinfo_trigger.go:44 uacDeviceInfoTrigger.Trigger` **注册时自动异步发 DeviceInfo 查询**（失败仅记日志）→ 无需按钮<br>🗑️ **纯清理已执行（2026-09-18）**：`ControlConsole.vue` 已物理删除。⚠️ 纠正上次的一处误述——**并不存在「`control-console-modal` 样式」**：全仓（含 `dist`）只有 1 处 `control-console-modal` 命中，是 Arco `modal-class` 的**属性名**，从未有 CSS 定义，无可连带删除。连带扫描另发现**第二个孤儿** `ChannelSnapshotCell.vue`（`444a3835`「T6 add ChannelSnapshotCell component」引入，`git log --all -S` 全历史**零引用**，且它仅剩的 2 处注释还在引用已删的 ControlConsole）→ **已于同日一并删除**（其「快照缩略图」能力现由 `device-mgmt/index.vue:428 snapshotImageUrl()` + `:2466`/`:2083` 的 `v-if="…snapshotUrl"` 内联实现，**功能未丢，删的只是没人用的抽象**）。已应用迁移 `2026-07-20-channel-snapshot.sql` 里的同名注释**刻意保留**（迁移是历史记录，不重写） | — | — | — | ✗ 销单 |
| ☐ D-6 | **设备配置家族统一前端入口**（A 组收口） | 新建 `web/src/views/gb28181/device-mgmt/DeviceConfigDrawer.vue`，分 Tab 承载 A-4~A-13；挂到设备管理行操作 + 控制台 | 无需改 | 一个入口能看到全部配置项，**读回值与下发值一致** | P1 ⚠️A 组做完再做，避免每项各开一个入口 |

### 4.E 图像抓拍口径整改（跨两侧，**当前是私有口径**）

> 标准全流程是 `CmdType=DeviceConfig` + `<SnapShotConfig>`，完成通知 `CmdType=UploadSnapShotFinished`。
> 本仓是 `DeviceControl` + `Notify/SubCmd=SnapShot`，**全仓搜标准值零匹配** —— 自家联调全绿，接第三方必挂。

| 编号 | 任务（条款） | 平台侧（后端 · 前端入口） | 模拟器侧 | 验收闭环 | P |
| --- | --- | --- | --- | --- | --- |
| ☐ E-1 | **抓拍下发 CmdType 改标准口径**<br>（§9.14.3 b) / A.2.3.2.1 / A.2.3.2.12） | `manscdp/snapshot.go:59` 的 `CmdDeviceControl` → `DeviceConfig`；收到的应答按 `DeviceConfig`+`Result` 校验 | `<SnapShotConfig>` 分支从 DeviceControlDispatcher 迁到 `DeviceConfig` 分派 | SIP 明文可见 `CmdType>DeviceConfig` + `<SnapShotConfig>` | P0 |
| ◐ E-2 | **完成通知改 `UploadSnapShotFinished`**（A.2.5.7） | ✅ **平台侧已修（2026-09-20）**：① `handler/message.go` 放行 `CmdType=UploadSnapShotFinished`（新增分支，与私有形态那支**并列**、各走各的 sink 方法）；② `manscdp.ParseUploadSnapShotFinished` 新增（一对多，见下方"落点更正"）；③ `devicecapture` 的完成计数由"按通知条数"改为**按文件标识去重计数**（`session.notified`→`notifiedIDs`）。⛔ 原落点 `handler/notify.go` 写错，见下 | ⏳ 未做：`SnapShotNotifyBuilder.kt:28` → `CmdType=UploadSnapShotFinished` + `SnapShotList/SnapShotFileID`（可多张）。⚠️ 平台侧已**双向兼容**，故此侧不阻塞联调，只是模拟器暂不覆盖标准路径 | ✅ 真机原文锚点 `TestParseUploadSnapShotFinished_FollowsHikvisionWire`（海康 `37010301021320000002`，2026-09-20 13:31:04 原文）；入站门禁 `TestMessageHandlerDispatchesUploadSnapShotFinished`；会话终结 `TestRegistryCompletesSessionOnStandardFinishedNotify` | P0 |
| ✅ E-3 | **图片上传 HTTP 落地**（§9.14.3 c) 留白 → 按 0-4 契约） | ✅ **平台侧全链完成（2026-09-20）**：① 收图接口兼容真机三形态（POST catch-all + multipart 流式 + 文件名从部件头取）；② 落盘搬出公开静态目录（`<serverroot 父目录>/gb-device-snapshots/…`）；③ **入库** `gb_channel_snapshot`（三方言迁移 + 三快照，唯一键 `(channel_code,file_name)` 让重传幂等）；④ **稳定读接口** `GET /api/gb28181/device-mgmt/snapshots/:id/content`（JWT + `gb28181:device:snapshot`，按库行 id 取图，不随会话过期）；⑤ **图像库入口后端就绪**：列表接口 `GET /api/gb28181/device-mgmt/snapshots`（筛选/分页/按通道归属套数据范围）+ 一级菜单行「图像库」+ 角色授权（三方言 + 三快照）。真机端到端实测 136273B 真 JPEG 落盘并通过读接口取回。⑥ **图像库前端页**（2026-09-20 **与菜单行同批收口**）：`web/src/views/gb28181/snapshot-library/index.vue`（设备/通道/来源/时间筛选 + 缩略图网格 + 大图预览 + 分页），并在抓拍会话面板加「在图像库中查看本次抓拍」入口（带 `sessionId` 深链）。✅ **菜单已打进开发库（2026-09-20）**：刷新页面即见（不需要重启后端），并修正了菜单 `sort` 撞车（80→55）。⏳ **未做**：目录按天分桶（§2.3）与保留期清理（§2.5） | 按契约真上传（Android 侧当前根本没上传） | 抓拍 → 图片落到平台、入库、可按 id 预览（**平台侧已达成**；模拟器侧未做） | P0 |
| ◐ E-4 | **抓拍文件命名 41 位**（§9.14.1 + 表4） | ✅ **平台侧解析已做（2026-09-20）**：`manscdp.ParseSnapshotFileID` 按"设备码 20 + 图像码 2 + 时间 17 + 序列码"从**前往后**切，反解 `captured_at` 落库；`StandardCompliant()` 单独报告是否严格 41 位。⛔ **不按 41 位硬校验**（真机是 40 位，见 §4.E 的 ③）⇒ 不合规只影响 `captured_at` 回落接收时刻，不拒收 | 按 `设备编码 20 + 图像编码 02 + 时间 17 + 序列码` 生成；⚠️ **补零到 2 位**（真机不补零，模拟器按标准补零更规范） | 平台收到的文件名能反解出拍摄时刻；不合规不影响收图 | P1 |
| ☐ E-5 | **抓拍失败语义**（A.2.5.7 注释） | 按「文件标识个数 < 要求张数 = 部分失败」展示 | 缺失时如实少报标识 | 故意让 1/3 张失败 → 平台显示**部分失败**而非整体成功 | P1 |
| ☐ E-6 | **Android 抓拍真落地**（CameraX ImageCapture） | 无需改 | `SnapshotCapture.android.kt:25 takeJpeg` 恒返 `null` → 接 CameraX；iOS 侧 `IosSnapshotSourceHolder` 已有真实现可参考 | Android 真机抓拍产出 JPEG | P1 ⚠️**必须等 E-1~E-3 口径定完再动手** |
| ☐ E-7 | **抓拍配置读取**（走 ConfigDownload 家族） | 纳入 A-1 的 12 类 `ConfigType` | 同上 | 能读回最近一次抓拍配置 | P2 |

#### 📌 E-2 落点更正与真机形态（2026-09-20 核，勿再按原描述找落点）

**① 原落点路径写错**：原写 `handler/notify.go`，但那是**订阅 NOTIFY** 处理器
（`NotifyHandler` / `SubscriptionState` / `PTZPrecisePosition`），与抓拍无关。真实门禁在
`handler/message.go` 的 `head.CmdType == manscdp.CmdNotify`（`CmdNotify = "Notify"`）——
真机发的是 `UploadSnapShotFinished` ⇒ **连分支都进不去**，所以症状一直是"平台收不到完成通知"。

**② 真机原文**（海康 `37010301021320000002`，2026-09-20 13:31:04，`SN=9402` 那次 `DeviceConfig`
下发 3 张抓拍后主动上报）：

```xml
<Notify><CmdType>UploadSnapShotFinished</CmdType><SN>9402</SN>
<DeviceID>37010301021320000002</DeviceID><SessionID>probe-b-…</SessionID>
<SnapShotList><SnapShotFileID>37010301021320000002022026092013305816101</SnapShotFileID></SnapShotList>
<SnapShotList><SnapShotFileID>3701030102132000000202202609201331014002</SnapShotFileID></SnapShotList>
<SnapShotList><SnapShotFileID>3701030102132000000202202609201331044003</SnapShotFileID></SnapShotList></Notify>
```

⛔ **三个必须照抄的点**（少一个就静默解析出 0 个标识，两侧都不报错）：
1. 根元素是 **`<Notify>`** 不是 `<Response>` ⇒ 解析结构体**不能带 `XMLName`**；
2. `<SnapShotList>` 是**一张一个的并列元素**（真机 3 张 = 3 个并列 `SnapShotList`），而标准文字
   写的是"一个 `SnapShotList` 里放 N 个 `SnapShotFileID`" ⇒ **外层收切片、内层再收切片**，
   两种写法都要吃下（只认真机那种 = 把厂商实现当标准）；
3. `DeviceID` **有** ⇒ 能落在 `handler/message.go` 的 `head.DeviceID != ""` 分支内。

**③ 顺带拿到 E-4 的真机锚点 —— 而且它同时是个反例（2026-09-20 复核更正）**：
`SnapShotFileID` = 设备编码 20 + 图像编码 2 + 时间 17（`yyyyMMddHHmmss` + **3 位毫秒**）+ 序列码。

⛔⛔ **序列码不补零，总长不一定是 41 位**。真机三张的实际长度是 **41 / 40 / 40**：

| 文件名 | 长度 | 时间字段 | 序列码 |
| --- | --- | --- | --- |
| `37010301021320000002022026092013305816101` | 41 | `…133058` + `161` | `01` |
| `3701030102132000000202202609201331014002` | **40** | `…133101` + `400` | `2` |
| `3701030102132000000202202609201331044003` | **40** | `…133104` + `400` | `3` |

⛔ 本文档上一版把后两条记成"`…133101400`+`02`"（按 2 位序列码从后往前切）—— 那是**错的**：
按那个切法时间字段会少一位（`…13310140`），解出的拍摄时刻整体偏移。正确切法是
**从前往后**：前 22 位是设备码+图像码，紧接着固定 17 位是时间，**剩下的（可能只有 1 位）才是序列码**。
判定与解析见 `manscdp.ParseSnapshotFileID`（`snapshot_file_id.go`，含连拍时刻递增的锚点用例）。

⭐ 所以平台的策略是「**能解析就解析，合规性单独判**」：`StandardCompliant()` 报告是否严格 41 位
（用于告警/治理），而**解析不要求 41 位** —— 按 41 位硬校验会让真机 3 张里 2 张的时间轴回落到接收时刻。

**④ ⛔ 为什么必须同时改"计数口径"，光放行报文不够**：`devicecapture.Registry.completeLocked`
的条件是 `len(Files) >= SnapNum && NotifiedCount >= SnapNum`。标准形态是**一条报文带 N 个标识**，
而原实现按"收到一条通知 +1"计（`session.notified` 按**单个** `SnapShotID` 去重）⇒
即使放行了报文，`NotifiedCount` 最多到 1，**会话永远完不成**，前端轮询到的一直是"未完成"。
现在按**文件标识**去重计数（`session.notifiedIDs`），且两种形态共用同一份记账
（`devicecapture.Registry.recordFinished`）—— 形态可以有两种，"哪些标识算完成"只能有一个答案。

**⑤ 变异自检暴露的用例盲区（已补）**：只写"来源设备也外来"的归属用例时，把报文 `DeviceID`
那道校验删掉**仍然全绿**（来源校验先把它挡住了）⇒ 归属校验的**两条独立通道各要一条用例**
（来源不对 / 来源对但报文声称别的设备）。

#### 📌 E-3 真机收图形态（2026-09-20 实测 → **当天平台侧已修并真机验证**）

问「海康的抓拍图片现在能传上来吗」→ 修之前 **不能**。不是配置问题，是**接口形态三处不兼容**，
每一处单独就足以失败（真机 `37010301021320000002`，假收图端点 + 探针直接下 `SnapShotConfig` 取证）：

| # | 设备实际发出 | 平台现在的接口 | 实测 |
| --- | --- | --- | --- |
| 1 | **`POST`** | 只注册了 `PUT` | gin **路由级 404**（`404 page not found` 纯文本，**没进 handler**） |
| 2 | 路径到 `/uploads/<token>/` 就结束，**没有文件名段** | `/uploads/:token/:filename`（`:filename` 必填） | 同上（路由不匹配） |
| 3 | body 是 **`multipart/form-data`**（部件 `name="file"`，文件名在**部件头**），部件内 `Content-Type: image/jpeg` | 要求 body 是**裸 JPEG**（首尾 `ffd8`/`ffd9` + 请求头 `image/jpeg`） | 即便路由放行，`Registry.Upload` 也返 `ErrInvalidImage` ⇒ 422 |

**真机请求原文**（`SessionID` 是**查询串**不是路径段）：
```
POST /api/gb28181/device-snapshots/uploads/<token>/?SessionID=<sessionId> HTTP/1.1
User-Agent: IP Camera
Content-Type: multipart/form-data; boundary=------------------------6c38787168025c9d
Content-Length: 136980
--…  Content-Disposition: form-data; name="file"; filename="37010301021320000002022026092014090311401.jpg"
```
⭐ **判别两种 404**：`404 page not found`（纯文本）＝ gin **没匹配上路由**；
`{"code":404,"message":"抓拍图片接收失败"}`＝**进了**控制器但 token 不认识。

**两条顺带事实**：
- `SnapShotFileID`（完成通知里）＝ 上传文件名**去掉 `.jpg`** ⇒ 可据此把"落盘的图"与"通知里的标识"对上（补上 E-4 的另一半）。
- ⛔⛔ **完成通知 ≠ 上传成功**：A/B 那次 `UploadURL` 指向 `192.168.10.120:9/probe`（discard 端口、必然拒连），
  设备**照样**回 `UploadSnapShotFinished` 带 3 个标识。⇒ 判"有没有传上来"**只能看平台有没有收到字节**。
- ⚠️ **另有第 4 处隐患（本次未改）**：`snapshotUploadURL` 的 host —— ⛔ **更正**：代码**已经**优先读
  `X-Forwarded-Host`、只在为空时才回落 `c.Request.Host`（`controllers/device_snapshot.go:188-191`），
  所以代码侧是对的，**缺陷在 nginx 只设 `X-Forwarded-Proto`、没设 `X-Forwarded-Host`**
  ⇒ 操作员用 `localhost` 打开控制台时下发的地址是 `http://localhost/…`，设备连不到
  （与扫码引导页那个坑同源）。修的时候改 nginx 或改配置项，**别去改这个函数**。

**验收锚点**：真机上传成功 ⇒ `GET /channel/:id/snapshot-sessions/:sessionId` 的 `files` 非空、
`receivedCount` 等于 `snapNum`，且文件名 = 通知里 `SnapShotFileID` + `.jpg`。
取证配方与脚本固化在技能 `uvp-device-config-family` §19.7/§19.8（`scripts/snap_upload_capture.py` / `send_snapconfig.py` / `scripts/snap_e2e/`）。

**✅ 平台侧修法（2026-09-20）**：

| # | 改法 | 落点 |
| --- | --- | --- |
| 1 | POST 注册 **catch-all** `…/uploads/*token`。⛔ **不能**用 `:token` + gin 的 `RedirectTrailingSlash` 307 兜（307 要设备跟着重发，**设备不跟随**）；gin 也不允许同一层同时注册 `:token` 与 `*token`（panic） | `routes/routes.go` |
| 2 | `resolveDeviceSnapshotUploadTarget` 统一剥 catch-all 的前导/尾斜杠、切出 token 与路径文件名（PUT 具名参数形态同时兼容） | `controllers/device_snapshot.go` |
| 3 | 按 `Content-Type` 分流：multipart 走**流式** `MultipartReader()`（不落临时文件），文件名/类型从**部件头**取；判"文件部件"看 `FileName()` 是否为空，**不死认 `name="file"`**。裸 JPEG 老路保留兼容 PUT | 同上 |
| 4 | 落盘搬出公开静态目录：`bootstrap.deviceCaptureBaseDir()` 取 `serverroot` 的**父目录** ⇒ 默认 `./resource/gb-device-snapshots/…`（⛔ 基目录必须返回父目录：`Registry.Upload` 里已拼了一层 `gb-device-snapshots`，返回它本身会重复一层） | `bootstrap.go` |

⭐ **真机端到端已实测通过**（用生产代码另起端点，**不重启后端**）：设备回传
**136273B / 2560×1440 真 JPEG**，落盘名 = 部件头里的 41 位标识
`37010301021320000002022026092014333016801.jpg`；读接口 `200 / 136273B / image/jpeg`，
错 token 与错文件名均 `404`。6/6 变异精确红。

#### 📌 E-3 第三批（图像库列表接口 + 一级菜单入库）—— 验证与**两条被纠正的口径**

改动面：列表接口 `GET /api/gb28181/device-mgmt/snapshots`（筛选 / 分页封顶 200 / 按 `gb_channel`
归属套数据范围）+ 一级菜单行「图像库」+ 列表接口登记与角色授权（三方言迁移 + 三份全量快照）。
**20/20 变异精确红 + sha256 全部还原**（驱动：`tmp/run_mutations_snapshot_library.py`）。

⛔⛔ **纠正 ①：失败响应不是"HTTP 200 + 业务码"，是 `HTTP 400 + {"code":1}`。**
`DefaultResponseHandler.Fail`（`app/utils/response/response.go:82`）默认
`httpCode = http.StatusBadRequest`，而 `ListSnapshots` 调 `FailAndAbort` 时**从不显式传状态码**。
之前按 200 记录，是因为同包 `zlm_node_test.go` 的 `init()` 把全局 `app.Response` 换成了
`mockResponse`（其 `Fail` 硬编码 200）——**单跑看见的是 mock，全量跑才看见真相**。
⇒ 这类用例必须先 `useRealResponseHandler(t)`（同包既有 helper，save/restore 模式）。

⛔⛔ **纠正 ②：`routes["GET "+常量]` 不能证明"路由与迁移登记同路径"** —— 拿常量断言常量，
常量与路由一起改歪时两侧同时变、照样绿；而迁移里写的是字面量。
⇒ 在常量定义侧（`models` 包）加 `TestSnapshotLibraryPathConstantsMatchTheMigrationLiterals`
把常量比到字面量；`routes` 包保留"确实注册在该路径上"的断言，注释不再声明未覆盖的性质。

⭐ 本批变异自检新增 3 类必须背下来的坑（详见技能 §21.10）：全局可变量被同包 mock 泄漏、
拿常量断言常量、**播种断言缺"若不过滤就会被命中"的归属行**（`type=3` 那条因此存活过一轮）。

#### 📌 E-3 第四批（图像库**前端页** + 会话深链）—— 前端契约与 3 条前端坑

改动面：`web/src/views/gb28181/snapshot-library/{index.vue,snapshotLibraryState.ts,*.test.ts}`、
`api/gb28181.ts` 的列表接口、`device-mgmt/SnapshotConfigPanel.vue` 的图像库入口。
**10/10 变异精确红 + sha256 全部还原**（驱动：`tmp/run_mutations_snapshot_library_web.py`）。

| # | 前端必须遵守的点 | 落点 |
| --- | --- | --- |
| 1 | ⛔ **缩略图不能直连 `item.url`**：取图接口在鉴权组内，`<img>` 带不了 Authorization 头 ⇒ 整页 401 破图且不报错。统一经 `snapshotContentImageUrl(url, token, baseUrl)` 补 `?token=` | `snapshotLibraryState.ts` |
| 2 | ⛔ **`pageSize` 选项别超 200**：后端硬截到 200，给 500 会变成"显示 500/页、实际只回 200 条"，页数也跟着错 | 同上 |
| 3 | ⛔ **跳图像库前先探测路由**（`router.resolve(...).matched.length`）：菜单行没生效时路由不存在，`push` 会落到 404 白屏，比按钮置灰更难懂 | `SnapshotConfigPanel.vue` |

⭐ **跨端契约断言**（`index.layout.test.ts`）：直接读 Go 侧迁移与 models 源码，断言
① 菜单 `component` 指向的文件**真实存在**（否则菜单点开空白）、② 前端请求路径与后端路由常量一致、
③ `SNAPSHOT_LIBRARY_PATH` 与迁移里的菜单 `path` 逐字相同。文件不存在时自动跳过，不影响独立打包场景。

⛔⛔ 变异自检抓到**第 4 个假锚点**（同一形态第 4 次）：源码断言
`toContain("if (token !== requestToken) return;")` —— 该行在 try/catch 各出现一次，
把**主路径**那处删掉仍然绿。⇒ 判据：断言"某语句在不在"时，匹配串必须带上**只有该处才有的邻句**。
（前三次：菜单 path 被授权语句带上、`routes["GET "+常量]`、拿常量断言常量。）

#### 📌 E-3 第五批（菜单**打进开发库** + 一处真缺陷修正）—— 2026-09-20

背景：用户"没看到菜单"，需把迁移打进开发库（220 `uvp_gb28181`）。

| # | 做了什么 | 结论 |
| --- | --- | --- |
| 1 | 三查开发库现状 | **只跑过上一批**：`gb_channel_snapshot` 表已建、读图接口 `sys_api` 已登记；本批的列表接口 / 菜单行 / 授权**一条都没进** |
| 2 | 手工执行 `2026-09-20-channel-snapshot-library.sql` | 菜单行 id `140509`、`sys_api` 589(列表)/587(取图)、`sys_menu_api` 绑到抓拍按钮 `140450`、`sys_role_menu` role 1、casbin 2 条 —— 全部落地 |
| 3 | 验证 | 库侧逐项计数；路由侧**不带 token 打新接口 = 401（不是 404）⇒ 运行中的后端已含本批代码**（零成本判据） |
| 4 | ⛔ 生效条件澄清 | **本批不需要重启后端**：菜单 `getRouters` 每次查库 + 前端 route store 无 `persist` ⇒ 刷新即见；casbin `autoloadpolicyseconds=120` 自动重载 ⇒ ≤2min 自行生效。（建表类迁移仍需重启） |

⛔⛔ **修正一处真缺陷：菜单 `sort` 撞车**。初版给图像库取 `sort=80`，撞上已有的
「SIP 接入信息」(80)。而一级菜单排序走 `TreeSort()` —— **`sort.Slice`（非稳定）+ 相等时
`return aSort < bSort` = false，没有任何 tiebreak** ⇒ 同 sort 两项**顺序不确定**，
表现为"菜单栏位置偶尔会变"（极难归因）。已改 `sort=55`（「云端录像」50 与「录像计划」60 之间，
与云端录像同属"历史媒体资源浏览"同组），**六处同步**（三方言迁移 + 三快照）。
⇒ 通用判据：**给菜单类迁移定 `sort` 前，先查同层现有取值**（技能 §24.4）。

### 4.F 信令与传输

| 编号 | 任务（条款） | 平台侧（后端 · 前端入口） | 模拟器侧 | 验收闭环 | P |
| --- | --- | --- | --- | --- | --- |
| ☐ F-1 | **多响应消息聚合通用化**（附录 M） | 把 `handler/catalog.go:18-107` 的聚合（deviceID+SN+SumNum / 30s 超时 / 去重）**泛化**为通用聚合器，供 ConfigDownload、RecordInfo 等复用；补条数上限约束 | SumNum/Num 分批上限（现默认 50/包）与 TCP 分包边界复核 | 同一 SN 的多响应被正确拼装；超时分包场景不丢条 | P1 ⚠️**A-1 的依赖** |
| ☑ F-2 | **X-GB-Ver 补全**（附录 I）✅ 2026-09-17 | ✅ `handler/register.go` 鉴权通过后按 `WarningCode` 四类异常留 `gb28181.register.version_header_abnormal` 告警（missing/invalid/unknown/legacy）—— 此前 `protocol.Resolve` 产出的 `Warning` **全仓零消费**。⛔ 原描述「200 OK 响应也带」已更正：**设备不产生注册响应**，附录 I 的「注册及其响应」对设备侧只剩出站一半；响应侧平台早已覆盖（`newRegisterResponse` 对 200/401/403/500 全带头） | ✅ `sip/GbVersionNegotiation.kt`（parse + min 协商）+ `RegistrationCoordinator.platformVersion` 流（解析 200/401/4xx **全部**响应头；注销时清空）+ `ManscdpContext.effectiveGbVersion` → Catalog/DeviceInfo/DeviceStatus/AlarmStatus 按 **min(本机, 平台)** 出站 + 设置页显示协商结果 | 模拟器切 2016 后平台门禁生效；反向（平台 2.0 × 设备 2022）时设备应答降级为 2016 形态 | P1 |
| ☐ F-3 | **NAT 场景 TCP 长连接**（建立 · 复用 · 断链自愈）（§9.1.1 f、§5.2）**⛔ 原 F-4 已并入本条** | ① ✅ **「连接复用」已具备，勿再当缺口**：`bootstrap.go:613` 硬编码监听 `{udp,tcp}`；accept 的 TCP 连接按**远端地址**入池（`transport_tcp.go:198-199` `pool.Add(raddr, c)`）；`connectionReuse` 默认 `true`（`transport_layer.go:139`）→ 下行 `ClientRequestConnection` 用 `GetConnection(raddr.String())` 命中**同一条**已建连接（两侧同为 `net.JoinHostPort` 格式）；下行 transport 全部取 `device.Transport`（`ptz/operation.go:276`、`ptz/device_reboot.go:290`、`subscribe/service.go:292`、`play/service.go:710`、`cascade/control/target_loader.go:59`、`upgrade/service.go:347`、`talk/activation.go:169`）。⛔ **2026-09-17 上一轮写的「现每次 `SetDestination()` 重新 Dial」是误判**，特此更正 ② ✅ **「TCP 断开立即判设备掉线」已实现**（2026-09-17，见本节末「平台侧实现清单」）—— 原缺口：`closeObserver` 只接了 trace 与 security（`sip/server.go:336-346`），设备在线靠心跳超时（`keepalive_interval=60` × `keepalive_timeout_count=3` = **180s**），与 f)「若 TCP 通道断开，则认为 SIP 代理异常掉线」不符 | ① ✅ **断链自愈已完成**（2026-09-17，`domain/SipReconnect.kt`：被动断开 → 停活跃流 + 作废注册会话 → 1s 起指数退避封顶 30s、**次数不封顶** → `close()`+`connect()` → 重新注册；`TcpSipTransport` 上报 `ConnectionLost` + 世代号守卫；18 单测）② ⛔ 默认 `transport = UDP`（标准要求 NAT 内侧**用 TCP**）③ ✅ **`received`/`rport` 回填已消费**（2026-09-17）—— 落点是**自发现**（解析平台回值 → 判定 DIRECT/NAT/UNKNOWN → 设置页「地址转换」行展示，NAT 时提示改用 TCP），**不是改 Contact**：⛔ 平台只读 Contact 头里的 `expires` 参数（`handler/register.go:568-585`），**地址部分完全不用**，改它属伪需求 | ① 设备 TCP 注册后，平台**所有**下行（点播/控制/查询/广播）复用同一条连接 ② **拔网线 / 杀连接 → 平台秒级判离线**（不是 180s）③ 设备自愈重连后平台恢复在线且绑定正确 | P1（原 F-3 P2 + F-4 P1 合并后取 P1） |
| ☐ F-5 | **NTP 校时**（§9.10） | 补 NTP 客户端（现仅 SIP Date），前端可发起 | 已有 NTP 客户端 → 复核可用性 | 平台发起校时 → 设备时间同步 | P2 |
| ✗ F-6 | **MobilePosition 的 MESSAGE 形态**（原引 §9.5.4）**⛔ 已关闭 2026-09-17 —— 标准里不存在这个形态，勿再当成缺口开工**（依据见下方「📌 F-6 条款号核验」；**替代项 = 紧邻的 `F-10`**） | 不做（不补 MESSAGE 分支；`manscdp.CmdMobilePosition` 的现存用途只有订阅体构造 / NOTIFY 解析 / Event 判定，**服务的是订阅链路，不是 MESSAGE**） | 私有扩展，**非对标能力**：`CatalogSubRouter.kt:55` → `MobilePositionResponse.kt`（KDoc 原写「§9.5.4」= 虚号源头） | — | — |
| ☑ F-10 | **MobilePosition NOTIFY 2022 列表形态 + 2016 向下兼容**（§9.11.2.3 c）/ A.2.5.6 / A.2.1.14）✅ 2026-09-17 | ✅ `manscdp.MobilePositionNotify`（`subscription.go`）改造为**双形态共存**：保留 2016 扁平根字段，新增 `MobilePositionItem`（A.2.1.14，含 `Height`）+ `MobilePositionDeviceList`；`SumNum` / `DeviceList` 用**指针**（为的是区分「元素不存在」与「值为 0 / 空」，裸 `int` 做不到），`Positions()` 做跨版本归一化。⭐ **一旦判定为列表形态就在列表语义里走到底**：`DeviceList` 在场但 `Item` 为空 → 返回**空切片**，**绝不回落扁平字段** —— 回落会拿根上的 0 值坐标合成一条 (0,0) **假位置**（前端地图上漂到几内亚湾）。`PositionProcessor.Process` 改为遍历 `Positions()`、抽出 `saveOne`：**单条脏数据只跳过该条 + zap warn，不连坐同包其它设备**，整包全废才返回首个错误（保持改动前「零坐标必须报错」契约 —— 单设备场景恰好 1 条）；`SumNum=0` 空列表返回 `nil`（返回 error 会让 `notify.go:73` 直接 return，连 `last_notify_at` 都不更新） | ✅ `MobilePositionNotify.build` 加**必传** `gbVersion`（**刻意不给默认值** —— 默认成 2016 会把「忘了传版本」变成一次静默的错误形态上报）：2022 出列表形态、2016 出扁平形态（**逐字节不变**）。⭐ 两形态的根 `<DeviceID>` 语义不同故拆两参：2016 根 = 位置来源通道（平台按它定位通道 / 写 `SourceCode`）；2022 根 = **目标设备**，通道**下沉到 `Item/DeviceID`**（传错会让合规平台认不出订阅目标而丢弃整条 NOTIFY）。⭐ 采集时间两形态**共用同一算法**（同一个 fix 在两版报文里必须是同一串字符），2022 根 `<Time>` 是**上报通知时间**（新 `notifyTimeMs`，走东八区而非跟随系统时区）；可选 `Height` 本仓无数据源 → **不发**。调用点 `SubscriptionNotifyHandler.sendPositionNotify` 传 `ctx.effectiveGbVersion` + `config.device.deviceId` | ① ✅ 单测覆盖「2022 列表 / 2016 扁平 / 两版切换」三路：模拟器 **16 例**（含**两份整包 golden** 锁死元素序 + CRLF） + **2 例端到端接线**（守「有效版本真的传到构造器」，builder 单测抓不到接错版本源）；平台 `manscdp` **5 例** + `subscribe` **4 例** ② ✅ **2016 回归**：既有 9 例**显式标 `V2016`** + golden 逐字节锁，两版并存被证明而非假设 ③ ✅ 变异自证：让 `Positions()` 退回「只认扁平」→ **6 例红**，失败输出正是那条 `{… 0 0 0 0 0 0}` 假位置 ④ ⚠️ **真机 e2e（模拟器 2022 → 平台 → 前端「设备管理 · 地图」）未跑** | P1 |
| ☐ F-7 | **媒体流保活 / 丢失释放**（附录 K） | RTP 静默超时判定 + 链路释放（现只有 BYE + 点播对账 `play/reconciler`） | 静默/丢包检测与链路释放 | 设备侧停流 → 平台在超时内释放会话 | P2 |
| ☐ F-8 | **RTCP 反馈**（RFC3550） | 消费反馈（可先只记录入日志/库；现状未确认） | 补 RR / NACK / PLI / FIR（现只发 SR） | 抓包能看到反馈报文；丢包时平台可请求关键帧 | P2 |
| ☐ F-9 | **Subject 媒体链路标识**（附录 L） | 核对现有 Subject 构造与 2022 口径 | 同上 | 点播/回放/广播的 Subject 符合标准 | P2 ⚠️**待核原文**口径 |

#### 📌 F-3 标准原文（2026-09-17 核，勿再转述二手解读）

**GB/T 28181-2022 §9.1.1 f)（标准印刷页 16，PDF 页 = 标准页 + 7）**：

> f）对于处于开启网络地址转换（NAT）功能的路由器内侧的 SIP 代理，宜支持使用 TCP 发起 SIP 注册，并在注册成功后保持 TCP 连接不关闭，SIP 代理及服务器在该 TCP 通道里发送心跳、刷新注册、视音频点播、控制等所有请求及响应 SIP 消息。若 TCP 通道断开，则认为 SIP 代理异常掉线，SIP 代理应按前述要求间隔一定时间后重新发起注册。

- **2022 新增**：GB/T 28181-2016 §9.1.1（标准印刷页 15，PDF 页 = 标准页 + 5）只有 **5 条**（认证 / 刷新注册 / 失败重试 ≥60s / 过期缺省 86400s / 在线离线判定），**无 NAT 条款**。2022 版前言亦记「更改了注册和注销基本要求（见 9.1.1，2016 年版的 9.1.1）」。
- ⛔ **本单原描述「用 received/rport 回填实际来源端点」不是标准要求**：全文 OCR 检索（166 页）中 `rport`、`received` **零出现**。那是 RFC 3581 的 NAT 穿透手段，属**工程实现选择**。标准给 NAT 的答案自始至终是**一条 TCP 长连接复用**。
- ✅ **边界已决策（2026-09-17）：合并为一条** —— f) 的后半句「TCP 通道断开 → 判异常掉线 → 重新注册」与前半句同源，拆成 F-3/F-4 会让「连接绑定」与「断链恢复」各改一遍、互相返工。**原 F-4 已并入 F-3**，验收闭环 = **建立 → 复用 → 断链恢复**三段连起来跑通。
- ✅ 平台侧现状（**2026-09-17 二核结论：复用已具备**）：注册时 `device.RegisterInfo{IP,Port} = req.Source()`、`Transport = req.Transport()`（`handler/register.go:340,343`）+ `security.TrustEndpoint`；下行统一 `net.JoinHostPort(device.IP, device.Port)` 且 transport 取 `device.Transport`；回包侧 sipgo 的 `NewResponseFromRequest` 已按 RFC 3581 回填 `rport/received`（`sip/response.go:225-232`），`Response.Destination()` 优先用报文真实来源。**复用链四段全通**：`bootstrap.go:613` 监听 `{udp,tcp}` → accept 连按远端地址入池（`transport_tcp.go:198-199`）→ `connectionReuse=true` 默认（`transport_layer.go:139`）→ 下行 `GetConnection(raddr.String())`（`transport_layer.go:464-467`）命中同一条。
- ⛔ **上一轮「每次 `SetDestination()` 重新 Dial」是误判**：当时只看到 `uac.go:130` 设了 Destination 就下了结论，没往下走到 sipgo 传输层的池查询。**排查这类问题必须走到 `ClientRequestConnection` 为止**。
- ✅ **平台侧真缺口（本轮新发现，当日已修）**：原 `closeObserver` 只接了 trace + security（`sip/server.go:336-346`），**TCP 断开不触发设备离线** —— 仍靠心跳超时（60×3=180s）。而 f) 明说「若 TCP 通道断开，则认为 SIP 代理异常掉线」。**2026-09-17 已实现**，见下方清单。
- ⚠️ 模拟器侧现状：Via 已带裸 `;rport`（8 处，如 `SipRegisterBuilders.kt:29`），2026-09-17 起**已消费**（自发现流，见上）；`Contact` 恒为 `localIp:localPort`（`SipRegisterBuilders.kt:23`、`PlaybackCoordinatorImpl.kt:286`）**保持不改**，理由见上。

#### 📌 平台侧实现清单：「TCP 断开立即判设备掉线」（2026-09-17）

**实现链**：sipgo 可靠性传输的 read loop 退出 → `closeObserver` → 新增的 `link_loss` 判定 → `DeviceLinkSink` 接口（`sip` 定义、`device` 实现，**避免 `device → sip` 成环**）→ `bootstrap.go` 的 `WithDeviceLinkSink(device.NewLinkWatcher(...))` 唯一接线点。

| 文件 | 作用 |
| --- | --- |
| `sip/link_loss.go`（新） | `DeviceLinkSink` 接口 + `WithDeviceLinkSink`；`decideLinkLoss()` 五态判定；`Server.notifyLinkLoss()`；`Server.HasConnection()` |
| `device/link_watch.go`（新） | `LinkWatcher.DeviceLinkLost()` → 按 `ip+port+status=online` 反查设备 → `MarkOfflineWithReason(link_closed)` |
| `sip/server.go` | `deviceLinkSink` 字段 + closeObserver 链尾包裹（`serverRef` 闭包，构造期 Server 尚未 new）+ `linkLossMuted` |
| `models/gb_device_status_event.go` | 新事件 `DeviceEventLinkClosed`（「链路断开」）+ 来源 `DeviceEventSourceLinkWatcher` |
| `bootstrap.go` | `startSIPDependencies` 里注入 sink |

**三重防误判**：① 同端点已有新连接 → 按重连处理不判离线（`HasConnection`）；② 传输方式与库内记录不符 → 不判；③ 平台关停 → `linkLossMuted` 静音（否则一次重启会写上千条「链路断开」）。
**两处坑**：`HasConnection` 拿到连接后必须 `TryClose()` **归还引用计数**（否则连接永远关不掉）；`markEndpointOffline` 必须**异步**（sipgo 在 read loop 退出路径上**同步**调 close observer）。
**测试**：`sip/link_loss_test.go` 9 例 + `device/link_watch_test.go` 8 例，全绿。

复核命令：`swift .workbuddy/ocr/ocr.swift "<pdf>" out.txt <起> <止>`，产物在 git 已忽略的 `.workbuddy/ocr/`。

#### 📌 F-6 条款号核验：原引「§9.5.4」两版都不存在（2026-09-17 核，勿再转述二手解读）

- ⛔ **虚条款号**：GB/T 28181-2022 与 2016 的 §9.5（网络设备信息查询）**都只有 9.5.1 / 9.5.2 / 9.5.3**，**没有 §9.5.4**。两版 §9.5.1 的查询命令枚举均为「设备目录 / 前端设备信息 / 前端设备状态信息 / 设备配置 / 预置位」——**都没有移动位置**。2022 修订说明把查询命令族限定为 `A.2.4.10～A.2.4.14`（**不含 A.2.4.9**）；2016 修订说明原文是「增加了移动设备**订阅通知**要求（见 9.11.1、A.2.4 和 A.2.5）」。
- ✅ **标准里 MobilePosition 只有一条路（订阅 + 通知）**：`§9.11.1.3 c)` 规定 SUBSCRIBE 的消息体用 **A.2.4.9**；`§9.11.2.3 c)` 规定 NOTIFY 的消息体用 **A.2.5.6**。**A.2.6（应答命令）里没有 MobilePosition 条目** → 标准里从来没有「查询 / 应答」这一对，「MESSAGE 形态」是我们自己的私有扩展。
- ⛔ **虚号传染链（6 处，2026-09-17 已逐处修正）**：模拟器 `MobilePositionResponse.kt` KDoc → `docs/gb28181-2022-gap-inventory.md` 的 `P-11` → 本文档 `F-6` → Atlas 能力矩阵 `3.7 / 7.6 / 7.7 / 8.3` 四行。**同类前科**：图像抓拍口径（标准值全仓零匹配）——「自家联调能跑通」不等于对标。

#### 📌 F-10 标准原文：2022 A.2.5.6 改了 NOTIFY 形态（2026-09-17 核）

**GB/T 28181-2022 A.2.5.6「移动设备位置数据通知」（标准印刷页 87–88，PDF 页 = 标准页 + 7）**，结构为：

```
<Notify>
  <CmdType>MobilePosition</CmdType>   ← 必选
  <SN>…</SN>                          ← 必选
  <DeviceID>…</DeviceID>              ← 必选（**两版都有**，所以旧解析器不会报错，只会拿到 0 坐标）
  <Time>…</Time>                      ← 必选（**上报通知时间**）
  <SumNum>…</SumNum>                  ← 必选（**移动设备位置总数**）
  <DeviceList Num="…">                ← minOccurs=0
    <Item>…</Item>                    ← itemMobilePositionType（A.2.1.14）
  </DeviceList>
</Notify>
```

**A.2.1.14 `itemMobilePositionType`（标准印刷页 64–65）** = `DeviceID` / `CaptureTime` / `Longitude`（WGS-84）/ `Latitude`（WGS-84）/ `Speed?`（km/h）/ `Direction?`（0≤x<360，正北顺时针）/ `Altitude?`（米）/ `Height?`（地面高度，米）。

- **2016 版**：`Time` / `Longitude` / `Latitude` / `Speed` / `Direction` / `Altitude` 直挂 `<Notify>`（无 `SumNum` / `DeviceList` / `Item`，无 `CaptureTime`，**无 `Height`**）。
- **差异落点**：2022 把「单设备单点」改成「**一次通知多个设备位置**」，`Time` 语义从「采集时间」变成「**上报通知时间**」，采集时间下沉到 `Item/CaptureTime`；新增可选的 `Height`（地面高度，意义不同于 `Altitude` 海拔）。
- ⛔ **兼容是硬约束**：GB/T 28181-2016 设备仍在网，**2022 形态与 2016 形态必须在同一解析器/构造器内并存**，由 `gbVersion` / `effectiveGbVersion`（min 协商）切换 —— 与 3.3 / 3.9 / 3.10 的既有双版本做法一致。**不允许"改造 2022 就打破 2016"**。

### 4.G 音频

| 编号 | 任务（条款） | 平台侧（后端 · 前端入口） | 模拟器侧 | 验收闭环 | P |
| --- | --- | --- | --- | --- | --- |
| ☐ G-1 | **广播/对讲音频编码放开**（§4.3.1） | `talk/activation.go:30,228` 硬编码 PCMA/8000 且**硬拒其它** → 支持协商 G.722.1 / AAC | 对应编码支持 | 协商 AAC 时广播仍能出声 | P2 |
| ☐ G-2 | **G.722.1 编码**（2022 新增，§4.3.1） | 信令声明支持 | `AudioCodec` 加 G.722.1（现仅 G711A/G711U/AAC） | 以 G.722.1 建立广播/对讲并出声 | P2 |
| ☐ G-3 | **AAC 信令对齐**（§4.3.1） | SDP 编码名/采样率口径核对 | 同上 | AAC 流可建立 | P2 |
| ☐ G-4 | **语音对讲上行（设备→平台）**（§9.8） | 对讲上行链路（⚠️ 2022 已删 SDP 的 Talk 类型，须按新口径重建） | 现仅广播下行 → 补上行采集与发送 | 现场对讲声音到平台 | P2 |

### 4.H 级联

| 编号 | 任务（条款） | 平台侧（后端 · 前端入口） | 模拟器侧 | 验收闭环 | P |
| --- | --- | --- | --- | --- | --- |
| ☐ H-1 | **摄像机访问路径选择**（附录 H） | 生成/消费 `X-PreferredPath` / `X-RoutePath` | 同上 | 多路径场景下按优选路径点播成功 | P2 ⚠️依赖「级联做到哪一级」的决策 |
| ☐ H-2 | **域间目录订阅通知**（附录 N） | 作为**上级主动订阅**下级域目录（现只有被动应答与推送） | 作为下级接受域间订阅并发通知 | 下级目录变化 → 上级收到域间通知 | P2 ⚠️同上 |
| ☐ H-3 | **多级级联端到端验证**（工程） | 搭 3 级链路验证 | 同上 | 跨级点播 / 目录 / 云台可用 | P2 |

### 4.I 合规细节

| 编号 | 任务（条款） | 平台侧（后端 · 前端入口） | 模拟器侧 | 验收闭环 | P |
| --- | --- | --- | --- | --- | --- |
| ☑ I-1 | **GB18030 真实字节编码**（§6.10）✅ 2026-09-17 | 出站 28 处全走 `manscdp.MarshalProfiledXML`（2022 profile → 真 GB18030 字节）、入站 `DecodeProfiledXML` **声明优先**（GB2312→`simplifiedchinese.GBK` / GB18030→GB18030 / UTF-8→直通）→ 代码复核通过，**无需改动** | ✅ 双向真转码。新建 `gb28181/SignalingCharset.kt`（`SignalingCharset` 枚举 + `of(version)`/`parse(label)` 别名表 + `encodeSignalingBody`/`decodeSignalingBody` + `stripXmlDeclaration`/`declaredSignalingCharset`）+ android/jvm/ios 三端 actual。出站：23 处内联声明字面量（19×`GB2312` + 4×`UTF-8`）**全部收口**到 `encodeSignalingBody`，声明与字节同源；编码器不支持目标字符集时**连声明一起降级 UTF-8**，杜绝「声明 A、字节 B」。入站：`ManscdpRouterImpl` 两处（SUBSCRIBE body / MESSAGE 入口）改 `decodeSignalingBody`，按报文声明解码、无声明回退有效版本对应字符集；`PtzPositionMessage.parse` 的前缀剥离改 `stripXmlDeclaration`（原写法在切 GB18030 时**静默剥不掉**）。UI：设备配置页「国标版本」卡加只读展示行（出站字符集 + §6.10 应/宜） | 含中文的目录/报警报文，**字节层面**为 GB18030。`SignalingCharsetTest` 17 case 全绿（GBK 金标字节硬编码 `BA A3 BF B5 CD FE CA D3`） | P1 |
| ☐ I-2 | **安全能力**（第 8 章 / GB 35114） | SIP over TLS、信令完整性 | TLS / SRTP | TLS 下注册与点播可用 | P2 ⚠️按交付要求单独立项 |
| ☐ I-3 | **RTP 时间戳要求**（§4.3.6） | 核对（同帧同戳、异帧异戳） | 核对 | 抓包验证 | P2 |
| ☐ I-4 | **视频帧率统一 25fps**（§5.4/5.6） | — | 编码参数 | 码流帧率符合 2022 口径 | P2 |

---

## 5. 已完成（纳入回归验收，不再开发）

这些能力**两侧 + 前端入口都已具备**，只在下述场景需要复核：

| 能力 | 平台证据 | 模拟器证据 | 复核触发时机 |
| --- | --- | --- | --- |
| X-GB-Ver 解析 + 2016/2022 双版本 profile 与门禁 | `protocol/profile.go` | 出站带版本（`GbVersionNegotiation` 做 min 协商） | 回归（F-2 已完成 2026-09-17） |
| GB18030 真实转码 | `manscdp/codec.go:100-163` | ✅ `gb28181/SignalingCharset.kt`（双向真转码） | 回归（I-1 已完成 2026-09-17） |
| NAT / TCP 长连接（复用 + 断链自愈 + **断链判离线**） | `bootstrap.go:613`、`transport_layer.go:464-467`、`sip/link_loss.go` + `device/link_watch.go` | ✅ 已实现 + **断链自愈**（`domain/SipReconnect.kt`）+ **rport 自发现**（`sip/ViaObservedEndpoint.kt`） | 回归（F-3 平台侧 + 模拟器自发现均已落地 2026-09-17） |
| 精准云台控制 PTZPreciseCtrl | `manscdp/ptz_precise.go`（2022 profile → `DeviceControl` + `PTZPreciseCtrl/Pan,Tilt,Zoom`） | 已实现 | ⭐ **2026-09-20 海康真机实测（DS-2DC2C40MY-DE，channel 3748，SN 10110）**：平台帧经 SIP trace 解密与 A.2.3.1.11 + A.2.1.11 **逐字节一致**、设备 **9ms 回 `200 OK`**，但**画面完全不变**（Pan/Tilt/Zoom 多组组合、含 `Zoom=3`/`Zoom=4` 均无变倍）；**同一条链路上** `PTZCmd left 6s` 画面明显转动 ⇒ **链路是通的，是这条命令设备不执行**。⛔ 该机型规格是 **PT-定焦**（2.8mm 定焦、**无光学变倍**、水平 0°~350°/垂直 0°~100°、水平≤20°/s）；对**同属 2022 精准定位特性族**的 `PTZPosition` 查询直接回 `<Result>ERROR</Result><Reason>Cann't get ptz position</Reason>`。⛔⛔ 标准**表 1 序号 10** 的「应答命令」列写作「**（无）**」⇒ 平台**永远**拿不到执行反馈；且 7.3 b) 把「PTZ 精准控制」列为「**宜**」（可选）⇒ **这不是信令 bug，是设备能力边界 + 标准的可观测性缺口**。<br>**平台侧真缺口 = 不可观测（待做）**：① `SupportsPrecisePTZ()` 只按 `effective_version` 派生（本机 report 3.0 → 判 2022 → 按钮可用），**没有负向证据通道**（`PTZPosition` 收到 `Result=ERROR` 未落库成「该设备不支持精准定位」，不同于 `ResolveHomePositionCapabilities` 的历史证据机制）；② 前端**无条件**提示「精准定位请求已受理」，把「已发出」说成「已受理」。建议：把 `Result=ERROR/Reason` 收成负向能力证据并在卡片上区分「未验证/不支持/已下发」，文案改「**已下发（设备无应答命令，须以画面为准）**」。 | 回归 |
| PTZ 精准状态查询 | `ptz/query.go:34-39` | 已实现 | 回归 |
| 看守位查询 + 设备侧自动归位 | `ptz/query.go`、`controllers/device_ptz_home_position_test.go` | 已实现 | 回归 |
| 巡航轨迹列表 / 详情查询 | `ptz/query.go`、`controllers/device_ptz_query.go` | 已实现 | 回归 |
| **存储卡状态查询**（A.2.4.14 / A.2.6.16） | `manscdp/storage_card.go`、`ptz/storage_card.go`、`gb_device_storage_card`、`controllers/device_storage_card.go`（27 + 9 单测） | ✅ `DeviceControlSubRouter.kt`（标准报文 + 随机假数据，19 单测）+ `VirtualStorageCards`（6 单测）+ **设备屏幕 OSD `StorageCardPanel.kt`**（9 单测） | 回归（C-1 已完成 2026-09-17，OSD 于 2026-09-18 补）—— **`Status` 各枚举取值、0 卡空列表、`FormatProgress` 缺席三条要各跑一遍**；另加两条设备侧观感：**连点两次查询卡片必须亮两次**（计数当 key）、**平台列表数字与设备屏幕数字必须逐字相同**（单一真源） |
| 上述**五项**的**前端入口** | `PlayConsoleLinked.vue`（精准模式 / 预置位 / 巡航轨迹 / 看守位 / **存储卡状态**卡片 + 管理抽屉） | — | 回归 |
| 目录订阅与增量 NOTIFY | `handler/notify.go` | 已实现 | 回归 |
| 目录项字段渲染（`Item` 基字段 + `<Info>` 容器，**2016 含 `PositionType`/`UseType`/Info 内 `BusinessGroupID`；2022 含 `PhotoelectricImagingType`/`CapturePositionType`/`StreamNumberList`/`SSVCRatioSupportList`、`PTZType` 1-7、`BusinessGroupID` 上提 `Item` 层**） | ✅ **2026-09-18 完成**：`manscdp.CatalogInfo` 双版本并存解析 + `gb_channel` 十列 + 通道详情「设备上报属性」区块（`StreamNumberList`/`SSVCRatioSupportList` 属取流能力、非属性展示，**仍在 manscdp 层解析但不落库**，见 B-2 行） | ✅ `gb28181/CatalogNotifyBuilder.kt` 按 `effectiveGbVersion` 双分支 | 回归 —— **2016 形态回归必须一起跑**（模拟器侧 `CatalogResponseTest` 的 `infoBody()` 锚点 + 平台侧 `subscribe/catalog_test.go` 的 2016/2022 端到端 XML 锚点） |
| 目录多父级挂载（A/B 拆分） | `catalog/dto.go:28`、`gb_channel_mount` | ✗（见 B-7） | B-7 改完后 |
| 报警上报 / 报警复位 AlarmCmd | `manscdp/device_advanced.go:146,221` | 已实现 | 回归 |
| MobilePosition（NOTIFY 形态，**2016 扁平 + 2022 列表**双版本） | `handler/notify.go`、`manscdp/subscription.go`（`Positions()` 归一化） | `MobilePositionNotify.kt`（按 `effectiveGbVersion` 分支） | 回归 —— **2016 形态回归必须一起跑** |
| RecordInfo / Playback / Download / MediaStatus | `manscdp/record_info.go` | 已实现 | 回归 |
| TeleBoot / RecordCmd / GuardCmd / IFameCmd / DragZoom | `manscdp/device_advanced.go` | 已实现 | 回归（2026-09-18 核：控制台/设备管理页均为**真实调用**，`D-5` 已销单） |
| 设备软件升级下发 + 结果状态机 | `manscdp/device_upgrade.go`、`upgrade/service.go` | 假进度（见 D-4） | D-3/D-4 改完后 |
| 语音广播（下行） | `manscdp/broadcast.go`、`talk/activation.go` | 已实现 | G-1/G-4 改完后 |
| H.264/H.265 编码 + PS 封装 | 委托 ZLMediaKit | 已实现 | 回归 |
| 目录 2022 维度（CivilCode / BusinessGroupID / 业务分组 / 虚拟组织） | `manscdp/catalog.go`、`catalog/classifier.go`、`catalog/pipeline.go` | ✅ B-1/B-2 已改完（`<Info>` 内 `BusinessGroupID` 的 2016 路径已有端到端锚点） | 回归 |
| 探针/流健康检测 | `probe/` | — | 回归 |

---

## 6. 明确不做（已决策）

| 能力 | 条款 | 决策 | 影响面 |
| --- | --- | --- | --- |
| 注册重定向（302 Moved Temporarily） | §9.1.2.3 | ✗ 不做（2026-09-17） | 平台侧 P-4 + 模拟器侧 S-6 一并搁置；集群/多节点部署时需重新评估 |
| SVC（`a=ssvcratio`） | 附录 G | ✗ 不做（2026-09-17） | 平台侧 P-8 + 模拟器侧 S-18；只影响可伸缩编码场景 |

---

## 7. 建议开工顺序（前 12 条）

**批次 1 — 定契约（阻塞后面全部）**
`0-1` → `0-2` → `0-3` → `0-4` → `0-5` → `0-6`

**批次 2 — 打通最大空白 + 拿可视闭环**
`A-1` `A-2` `A-3`（配置读写通道 + 设备落盘）→ `A-4`（OSD，**画面文字真的变**）
→ `B-1` + `B-2`（目录通道属性字段，**必须同批**）

**批次 3 — 修私有口径（越晚越贵）**
`E-1` `E-2` `E-3`（抓拍口径）→ `E-6`（Android 抓拍落地）

**批次 4 — 家族补齐与收口**
`A-5` `A-9` → `A-6` `A-7` `A-8` → `A-10` `A-11` `A-12` → `D-6`（统一入口）
→ `D-3` `D-4` → `F-1` `F-3`
（原列的 `F-2` / `I-1` / **`F-10`** 已于 2026-09-17 完成，移出待办；**原 `F-4` 已并入 `F-3`**——NAT/TCP 长连接是「建立 · 复用 · 断链恢复」一件事；
**原 `F-6` 已于 2026-09-17 关闭**——标准里不存在 MESSAGE 形态，替代项是新增的 `F-10`；
**`C-1` 已于 2026-09-17 完成**，移出待办 —— 2022 五大新增查询至此全部收口；
**`D-5` 已于 2026-09-18 销单**——其引用的 `ControlConsole.vue` 是孤儿文件，四个动作在 `PlayConsoleLinked.vue` / 设备管理页**早就是真实调用**，无需开工；该孤儿文件已于同日删除）

---

## 8. 证据与关联文档

| 文档 | 内容 |
| --- | --- |
| `docs/gb28181-2022-master-backlog.md` | **本文档** —— 全量实施单（61 条）：§1 覆盖矩阵 + §4 逐条详情 |
| `docs/gb28181-2022-gap-inventory.md` | 实现缺口清单：21 项官方变化基准 + 平台 18 缺 + 模拟器 26 缺 + 证据边界 |
| `docs/gb28181-2022-device-config-ambiguity.md` | 设备配置家族的**条款级**审查：A 档注释与 Schema 矛盾 4 处 / B 档留白 6 处 / C 档硬伤 4 处 / D 档冗余 3 处 |
| `.workbuddy/ocr/`（git 已忽略） | 标准原文 OCR 产物 + `ocr.swift` 脚本（本机无 poppler/tesseract，走 macOS PDFKit + Vision） |

- ⚠️ **OCR 残余不确定性**：正文与枚举识别质量很好，但标点/个别字符可能有误。
  A 档「注释与 Schema 矛盾」与 C-1「`A.2.3.2.13` 不存在」这类结论，**改代码前建议肉眼复核对应 PDF 页一次**
  （PDF 页 = 标准页 + 7（2022）/ + 5（2016），各条页码见 ambiguity 文档）。
- 既有 `gb28181-coverage.md`（2026-06-15）已严重滞后，**勿再作为进度依据**。

## 9. 既存问题登记（**不是本单条目，不并进任何单据结论**）

> 做 B-1/B-2 期间顺带发现、**与 B-1/B-2 无关**的问题。按项目约定单独列出，避免混进主结论。
> 处理这些不需要开本单的条目，需另立工单。

### P-1 · `web/src/views/gb28181/zlm/SchedulerStrategy.test.ts` 在 develop 上是红的

- **事实**：该测试断言 `SchedulerStrategyPanel.vue` 含 `effectiveFrom` 与「只影响新点播」，但
  **同一提交里面板文件并不含这两个串**。已用 `git show HEAD:` 逐文件核对：
  测试文件（HEAD 版本）含 `effectiveFrom`，面板文件（HEAD 版本）**不含** → **develop 上既存失败**，
  与本次改动无关（本次未触碰 `web/src/views/gb28181/zlm/` 任何文件，`git status` 该路径为空）。
- **影响**：前端全量 `vitest` 恒有 1 红，会掩盖真正的回归。
- **修法二选一**：① 补齐面板实现（`effectiveFrom` + 「只影响新点播」文案）；② 若该需求已撤销，
  删掉这两条断言。**需先确认需求是否仍在**，不要直接删测试。

### P-2 · `postgresql_converted.sql` / `sqlserver_converted.sql` 两个转换快照与 MySQL 快照严重漂移

- **事实**（2026-09-18 核，`CREATE TABLE` 计数）：MySQL 全量快照 **76** 张；
  `postgresql_converted.sql` **53** 张；`sqlserver_converted.sql` **57** 张。
  **25 张表只存在于 MySQL 快照**，含 `gb_channel`、`gb_catalog_node`、`gb_cascade_platform` 等核心表。
- **且**：两个转换快照里仍留有 `initialization_contract_test.go` **明令禁止**的
  `demo_students` / `demo_teacher` / `example` 表。
- **为什么现在测试还是绿的**：`initialization_contract_test.go` 只断言「**存在/不存在某些 token**」，
  **不校验表集合相等**，所以漏表与残留表都逃过门禁。
- **为什么本次没顺手修**：这是**既存**问题，且修它等于把 PG/SQL Server 快照整个重建，
  超出 B-2 范围、会把 B-2 的 diff 淹没。本次按既有惯例（参照更早的 `recording_mode`）
  在这两个快照里**追加了守卫式 `ALTER TABLE gb_channel ADD ...` 补丁**，
  保证「快照路径」与「增量迁移路径」在新列上一致。
- **影响**：任何依赖这两个快照建库的 PG / SQL Server 部署，都会得到**缺 25 张表**的库。
- **建议**：另立工单 —— 要么用转换脚本重新全量生成两个快照，要么给 `initialization_contract_test.go`
  加「表集合与 MySQL 快照相等」的断言把漂移钉死。


#### 📌 E-3 第六批（**建表漏 COLLATE ⇒ 列表接口恒 400** 的定位与修复）—— 2026-09-20

现象：用户报「查询图像库失败，请稍后重试」+「统计抓拍图片失败」。

| 环节 | 事实 |
| --- | --- |
| 日志 | `ERROR db … event=db.query_failed error={type=*mysql.MySQLError code=1267}` + `route=/api/gb28181/device-mgmt/snapshots http_status=400` |
| `1267` | `ER_CANT_AGGREGATE_2COLLATIONS`（Illegal mix of collations） |
| 根因 | 建表写成 `DEFAULT CHARSET=utf8mb4`（无 `COLLATE`）⇒ MySQL 取**该字符集的默认排序规则** `utf8mb4_0900_ai_ci`，而 `gb_channel`/`gb_device` 与库默认都是 `utf8mb4_general_ci` ⇒ `ch.channel_id = s.channel_code` 两侧不一致 |
| 为什么只在列表炸 | MySQL 只在**列对列**比较时报 1267；`列 = 参数` 按列规则走 ⇒ 建表、收图写入、按 id 读图全正常，唯独带 JOIN 的列表接口必挂 |
| 那两句用户文案 | 「统计抓拍图片失败」= 后端 **COUNT** 查询（同样带 JOIN）的失败文案；「查询图像库失败」= 前端页面兜底文案。**同一个根因** |

修复（三处，缺一不可）：
1. **DDL 源码**：`migrations/2026-09-20-channel-snapshot-library.sql` + MySQL 全量快照
   `uvp-gb28181.sql` 的建表收尾补 `COLLATE=utf8mb4_general_ci`（仓库既有 73 张表都是这个写法；
   PG/SQL Server 快照不用改 —— 没有 per-table collation）。
2. **开发库已建出来的表**：`ALTER TABLE gb_channel_snapshot CONVERT TO CHARACTER SET
   utf8mb4 COLLATE utf8mb4_general_ci;`
   ⛔⛔ `CREATE TABLE IF NOT EXISTS` 让"改完 DDL 重跑"**修不好已建出来的表**；
   且该迁移文件 **15:08 已被 `gb_schema_migrations` 记账** ⇒ 之后往文件里追加的内容一条都不会自动进库。
3. **防回归锚点**：`TestChannelSnapshotDeclaresGeneralCollationOnMySQL`（在 `models` 包，
   断言迁移文本 + MySQL 快照建表块内含 `charset=utf8mb4 collate=utf8mb4_general_ci`）——
   ⛔ 必须做**文本**断言：行为测试只跑权限段 DML，方言 DDL 在 sqlite 上被跳过，覆盖不到建表收尾那一行。

⭐ 端到端验证**不重启后端**完成：自签 JWT（复用活跃会话，见技能 `uvp-local-dev-setup`
的 `scripts/mint_dev_token.py`）打正在跑的进程，一次拿到 5 条契约证据 ——
`?source=device` 200 / `?source=alien` 400+`source 不合法` / `?pageSize=500` 回落 200 /
`?channelId=abc` 400 / 正常查询 200 `{code:0,total:0}`。全量回归 EXIT=0。

⚠️ 周边（**不在本批**）：本库另有 11 张表是 `0900_ai_ci`、1 张是 `unicode_ci`（历史批次同样漏写），
当前无活 bug（只按 int/参数 JOIN），但**将来给它们加"按编码 JOIN"就会重犯**。

#### 📌 E-3 第七批（**抓拍报文从未发出**：`snapshot_config` 没被 ptz 认识）—— 2026-09-20

现象：用户报「海康的设备我下发了抓拍设备没上传」。会话起得来、operation 记
`accepted` + `device_result=OK`，**设备一张图都不上传**，两侧日志全绿。

| 环节 | 事实 |
| --- | --- |
| trace 取证 | `gb_sip_trace_message` 里三条 `snapshot_config` 操作（SN **10179/10181/10183**）的**出向 MESSAGE 正文全部是** `<Control><CmdType>DeviceConfig</CmdType><SN>…</SN><DeviceID>…</DeviceID><VideoParamAttribute Num="0"></VideoParamAttribute></Control>` |
| 也就是说 | 平台发的是「把视频参数清空」，`SnapShotConfig` **一个字节都没发出去**；设备那句 `Result=OK` 是对空视频参数配置的应答 |
| 根因 | `snapshot_config` 只是 `controllers/device_snapshot.go` 里的**字符串字面量**，`ptz` 包完全不认识它 ⇒ **三处「按 action 分流」全部落到 A-5 视频参数分支** |

三处断面（**漏一个都不算修好**）：

| # | 位置 | 不修的后果 |
| --- | --- | --- |
| 1 | `ptz/scheduler.go::buildScheduledPTZBody` | **报文错**（本次的直接原因） |
| 2 | `ptz/handler.go::OnPTZMessage` 的 `CmdDeviceConfig` 支 | ack 按 A-5 走，派生的对账去回读 `VideoParamAttribute` |
| 3 | 由 2 派生的对账 | 与父 payload 的块一个都对不上 ⇒ 差异 0 条 ⇒ **判 read_ok = 没对账**（比不排对账更坏：它报"正常"） |

修复（**判据换维度**，不是补一条白名单）：
1. `ptz/device_config.go` 新增导出常量 `ActionSnapshotConfig` + 判定函数
   `deviceConfigOperationForm(action, payloadJSON)`：判据取 **payload 里 `blocks` 是否非空**
   这个**数据事实**；名单里声明属于配置族却没有 `blocks` ⇒ **报错**（不许回落成 A-5）。
   ⇒ 将来新增配置族 action 只要用 `blocks` 装 payload 就自动走对，**不会再漏**。
2. `scheduler.go` / `handler.go` **共用这一个函数**；`controllers` 改用常量，字面量清零。
3. 防回归锚点 3 条（全部通过变异自检「精确红 + sha256 还原」）：
   `TestBuildScheduledPTZBodyBlocksPayloadNeverRebuildsAsVideoParamAttribute`（**性质测试**，
   含一个不存在的 action 名代表"将来的新 action"）、
   `TestOnPTZMessageRoutesSnapshotConfigAckToBlockFamily`、
   `controllers/device_snapshot_upload_url_test.go`。
   ⛔ 变异注入坑：把 handler 的**参数**换成常量是**等价变异**（payload 有 blocks 时判据与 action
   无关）会假绿，必须**同时短路 payload 判据**才复刻出原缺陷。

⭐ **同批加固的第二个断点**（独立于上面，属同一类"下发了个设备够不着的地址"的静默失效）：
`snapshotUploadURL` 用**浏览器请求的 Host** 派生 UploadURL，而前端 dev server 的 `xfwd: true`
会把原始 Host 透传 ⇒ **用 `localhost:5177` 开平台就会下发出 `http://localhost:5177/…`**，
设备解析成自己，永远传不上来且平台不报错。已加门禁 `snapshotUploadHostUnreachableForDevice`：
localhost / 回环 / 通配 **当场拒发**（503 + 文案），**域名一律放行**。
⇒ 操作口径：**用平台所在机器的局域网 IP 开平台**。

⚠️ 生效条件：**必须重启后端**（dev 是 VSCode 调试会话 ⇒ F5），重启后重新下发一次抓拍
（设备侧记住的仍是上一轮手工实验的配置，`UploadURL` 指向早已关闭的实验端口）。
全量 `go test ./app/gb28181/... ./resource/... -p 1` → 66 包 ok / 零 FAIL。

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
  （典型：目录九字段、抓拍口径、设备配置家族）。
- 状态标记：`☐` 待办 · `◐` 进行中 · `☑` 完成 · `✗` 不做
- 优先级：
  - **P0** 地基 / 能立刻做出端到端可视闭环（16 条）
  - **P1** 家族补齐 / 现有能力收口（21 条）
  - **P2** 视级联深度与交付要求取舍（24 条）
- ⚠️ 标了「待核原文」的条目，落地前先回标准原文确认一次再动手。

**总计 61 条** = 前置契约 6 + 设备配置家族 13 + 目录与编码 8 + 设备查询 1 + 设备控制与维护 6 +
图像抓拍整改 7 + 信令与传输 9 + 音频 4 + 级联 3 + 合规细节 4。

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
| MobilePosition 的 MESSAGE 形态 | 缺失（仅 NOTIFY 可用） | 已实现 | F-6 | 9.5.4 |
| 注册重定向 302 | ✗ 不做 | ✗ 不做 | — | 9.1.2.3 |

### 1.2 目录与编码

| 能力 | 平台 | 模拟器 | 单号 | 条款 |
| --- | --- | --- | --- | --- |
| 目录订阅与增量 NOTIFY | 已实现 | 已实现 | — | 9.11 |
| 目录 2022 九字段 | 不解析 | 不输出 | B-1 / B-2 | 附录 J |
| 组织级查询与应答 | 部分（有业务分组/虚拟组织维度） | 部分 | B-3 | 附录 J |
| 行政区划节点 | 部分 | 不可作真节点（typeCode 为空） | B-4 | 附录 E/J |
| 目录多父级 | 已实现（A/B 拆分） | 单 `parentId` | B-7 | 附录 H/N |
| 20 位统一编码校验 | 部分 | 部分（只查 20 位数字） | B-8 | 附录 E |
| 附录 O 采集部位类型 | 待核原文 | 待核原文 | B-5 | 附录 O |
| ExtraInfo / Channel 字段格式 | 待核原文 | 待核原文 | B-6 | 附录 A |

### 1.3 设备查询（2022 五大新增查询）

| 能力 | 平台 | 模拟器 | 单号 | 条款 |
| --- | --- | --- | --- | --- |
| 精准云台控制 PTZPreciseCtrl | 已实现 | 已实现 | — | 9.5 |
| PTZ 精准状态查询 | 已实现 | 已实现 | — | A.2.4.10 |
| PTZ 精准位置订阅通知 | 已实现 | 已实现 | — | 9.11.1/2 |
| 看守位信息查询 | 已实现 | 已实现 | — | A.2.4.11 |
| 巡航轨迹列表 / 详情查询 | 已实现 | 已实现 | — | A.2.4.12/13 |
| **存储卡状态查询** | **缺失** | 仅 mock + 命名不兼容 | C-1 | A.2.4.14 |

### 1.4 设备配置家族（`DeviceConfig` 写 / `ConfigDownload` 读）

| 能力 | 平台 | 模拟器 | 单号 | 条款 |
| --- | --- | --- | --- | --- |
| 配置读取 `ConfigDownload` | 缺失（读不到任何配置） | 仅 2/12 类 + 静默回 OK | A-1 | A.2.4.7 |
| 配置下发 `DeviceConfig` | 缺失（无常量） | 按元素名猜，非按 `CmdType` | A-2 | A.2.3.2.1 |
| 配置落盘 / 变更可见 | — | 不落盘 | A-3 | 工程 |
| `OSDConfig` 前端 OSD | 缺失 | 有渲染管线、无协议入口 | A-4 | A.2.1.12 |
| `VideoParamAttribute` 视频参数属性 | 缺失 | 缺失 | A-5 | 2022 新增 |
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
| 格式化 SD 卡 | 缺失 | 只解析 | D-2 | A.2.3.1.13 |
| 拉框放大 DragZoom | 已实现 | 已实现 | — | A.2.3.1 |
| 报警复位 `AlarmCmd` | 已实现 | 已实现 | — | — |
| TeleBoot / RecordCmd / GuardCmd / IFameCmd | 已实现（控制台 3 处占位未接） | 已实现 | D-5 | — |
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
| 简化控制台 | `web/src/views/gb28181/components/ControlConsole.vue` | 右侧「设备动作」面板有 3 个「待接入」占位按钮（设备信息 / 请求关键帧 / 远程重启）→ 见 D-5 |
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
| ☐ A-5 | **`VideoParamAttribute` 视频参数属性**（2022 新增） | 写+读；前端表单（编码格式 / 分辨率 / 帧率 / 码率）；输入范围取 A-9 的设备回读值 | 改**实际编码参数**（H.264/H.265、分辨率、帧率、码率），出的流参数真变 | 平台改分辨率 → 拉流实测分辨率变化（探针/ffprobe） | P0 |
| ☐ A-6 | **`PictureMask` 视频画面遮挡**（2022 新增，A.2.1.17） | 写+读（≤4 区域）；前端**可视化画框**编辑器；区域坐标按 0-1 契约 | 画面上真的绘遮挡；遮挡方式与是否影响录像按契约写明 | 平台画 2 个区域 → 模拟器画面出现遮挡（截图） | P1 |
| ☐ A-7 | **`FrameMirror` 画面翻转**（2022 新增） | 写+读；前端下拉（关 / 上下 / 左右 / 中心） | 渲染管线真翻转（含录像/回放是否同步翻转的取舍） | 平台选「左右镜像」→ 画面左右翻转（截图对比） | P1 |
| ☐ A-8 | **`BasicParam` 基本参数**（2016 已有，A.2.1.19） | 写+读（设备名称 / 注册有效期 / 心跳间隔）；前端表单 | **真落盘并生效**（心跳间隔真改、有效期真用）；现只提 4 个字段发 effect，不落盘 | 平台改心跳 30s → 抓包/日志看心跳实际 30s；改名称 → 目录与设备信息里名字变 | P1 |
| ☐ A-9 | **`VideoParamOpt` 视频参数范围**（2016 已有） | 读；用回读范围**约束 A-5 表单**，超出即前端拦下 | 读应答字段完整度核对（现支持但需核字段） | 平台表单可选范围来自设备真实回读，越界值被拦 | P1 |
| ☐ A-10 | **`VideoRecordPlan` 录像计划**（2022 新增） | 写+读；前端计划编辑器（星期 × 时段 × 码流） | 落盘 + 屏幕展示「已生效的录像计划」 | 下发计划 → 读回一致 → 设备屏幕显示该计划 | P1 |
| ☐ A-11 | **`VideoAlarmRecord` 报警录像**（2022 新增） | 写+读；前端开关/条件 | 落盘 + 与报警上报联动（触发报警时标记录像） | 下发开关 → 读回一致 → 设备侧报警录像状态可查 | P1 |
| ☐ A-12 | **`AlarmReport` 报警上报开关**（2022 新增，A.2.1.18） | 写+读；前端按事件类型勾选 ⚠️ 标准只定义 2 个开关且**都必选** → 按 0-2 契约定「只想改一个」怎么办 | 开关**真生效**：关掉移动侦测 → 该类报警不再上报 | 关掉某类 → 触发该类事件 → 平台**收不到**；打开 → 收得到 | P1 |
| ☐ A-13 | **`SVACEncodeConfig`/`SVACDecodeConfig` 读取**（2016 已有） | 读并明确展示「设备不支持 SVAC」 | 按 0-3 契约表达不支持（现显式忽略仍回 `OK`） | 平台点读 SVAC → 得到明确「不支持」，不是空白 `OK` | P2 |

### 4.B 目录、设备信息与编码

| 编号 | 任务（条款） | 平台侧（后端 · 前端入口） | 模拟器侧 | 验收闭环 | P |
| --- | --- | --- | --- | --- | --- |
| ☐ B-1 | **目录 2022 九字段 · 设备侧输出**（附录 J / §9.3.1） | 无需改（但 B-2 不做则平台看不到） | `CatalogNotifyBuilder.renderItem` 补 9 字段：`IPAddress` / `Port` / `PTZType` / `PositionType` / `RoomType` / `UseType` / `SupplyLightType` / `DirectionType` / `Resolution`<br>⛔ 现 `CatalogResponse.buildGb2022Fields()` 已备好字段，但**生产路径不走它** | SIP 明文/设备日志里目录应答含这 9 个字段 | P0 |
| ☐ B-2 | **目录 2022 九字段 · 平台解析+存储+展示** | `manscdp/catalog.go` 加字段解析；`gb_channel` 相关列 + 三方言迁移；目录列表/详情展示（云台类型、室内外、补光方式、分辨率…） | 无需改 | 平台通道详情能看到设备上报的云台类型/分辨率等真实值 | P0 ⚠️**必须与 B-1 同批** |
| ☐ B-3 | **组织级查询与应答**（附录 J） | 支持按组织维度发起目录查询并解析 | 行政区划 / 业务分组 / 虚拟组织三种节点均可作查询目标；虚拟组织 `ParentID` 按 2022 新语义 | 分别查三种组织节点 → 各自返回正确子树 | P1 |
| ☐ B-4 | **行政区划节点可用**（附录 E/J） | 目录树正确展示行政区划层级 | `AdministrativeRegion` 的 `typeCode` 补全，可作**真实区划节点**（现在为空，只能拿 VirtualOrg + CivilCode 模拟） | 设备建「省-市-区」区划节点 → 平台目录树正确分层 | P1 |
| ☐ B-5 | **附录 O 摄像机采集部位类型代码** | 枚举 + 展示 | 上报部位类型 | 设备上报部位 → 平台正确展示 | P2 ⚠️**待核原文**该字段在哪个命令里 |
| ☐ B-6 | **附录 A 扩充：`Info`→`ExtraInfo`、Channel 字段格式变更** | 解析对齐 | 应答对齐 | 设备信息/目录应答符合 2022 格式 | P2 ⚠️**待核原文** |
| ☐ B-7 | **目录多父级**（附录 H/N） | 已有 A/B 拆分（`gb_channel_mount`）→ 复核是否满足 2022 多父级语义 | `CatalogNode` 仅单 `parentId` → 支持多父级 | 同一通道挂两个父节点 → 平台两处都能看到 | P2 |
| ☐ B-8 | **20 位统一编码校验**（附录 E） | 校验类型码段 / 区划段 / ID 类型码与节点类型一致 | `IdEncoder.kt`、`CatalogTreeStore.kt:243` 同上（现只校验 20 位全数字） | 编一个区划段非法的 ID → 被拒且给出明确原因 | P2 |

### 4.C 设备查询（2022 五大新增查询的收口）

| 编号 | 任务（条款） | 平台侧（后端 · 前端入口） | 模拟器侧 | 验收闭环 | P |
| --- | --- | --- | --- | --- | --- |
| ☐ C-1 | **存储卡状态查询**<br>（A.2.4.14 / A.2.6.16）<br>⛔ 2022 五大新增查询里**唯一没闭环**的一组 | `ptz/query.go` 的 `QueryKind` 加一类 + 构造器 + 应答解析（卡号/容量/剩余/状态）；controller + 路由 + 菜单按钮权限 + 迁移；前端在控制台或设备详情加入口 | ① 命令名兼容（`SDCardStatus` **与** `StorageCardStatusQuery` 都认，现只认后者，前者落「未识别 cmdType」）② 去掉写死的 mock（1 卡 32G/24G）→ 可配置真值 | 平台点「存储卡状态」→ 显示模拟器配置的容量/剩余/状态 | P1 |

> 已实现的 2022 新增查询（看守位 / 巡航轨迹列表 / 巡航轨迹详情 / PTZ 精准状态）**两侧 + 前端入口都齐**，
> 列入第 5 节回归清单，不在本组重复开工。

### 4.D 设备控制与维护

| 编号 | 任务（条款） | 平台侧（后端 · 前端入口） | 模拟器侧 | 验收闭环 | P |
| --- | --- | --- | --- | --- | --- |
| ☐ D-1 | **目标跟踪 `TargetTrack`**（A.2.3.1.14） | 下发 + 前端入口（选择目标 / 开启关闭） | 现只解析写 effect → 至少回**合规应答** + 屏幕状态可见；真行为依赖设备 AI，可用「模拟目标框」表达 | 平台开目标跟踪 → 设备屏幕显示跟踪状态 | P2 |
| ☐ D-2 | **格式化 SD 卡 `FormatSDCard`**（A.2.3.1.13） | 下发 + **权限门禁**（破坏性）+ 二次确认 + 前端入口；操作记入维护记录 | 执行语义（清空模拟存储卡）+ 应答 | 授权用户二次确认后下发 → 存储卡剩余变满值；未授权用户按钮不可用 | P2 |
| ☐ D-3 | **固件分发 HTTP 服务**（§9.13） | 平台托管固件文件 + 生成 `FileURL`（现在靠外部喂 URL）+ 下载鉴权/过期 | 真下载固件（替代 5s 假进度） | 平台选固件 → 设备真发起 HTTP 下载 → 进度真实递进 | P1 |
| ☐ D-4 | **模拟器升级真进度**（§9.13） | 复核升级结果状态机能接收各阶段（已实现） | `SystemHandler.kt:136` 的 5s 假进度 → 真下载 + 分阶段进度 + 状态机 | 升级过程中平台看到阶段推进，最终收到 `DeviceUpgradeResult` | P1 ⚠️**与 D-3 成对** |
| ☐ D-5 | **控制台「待接入」按钮接线**（工程） | `ControlConsole.vue:365-367` 的「设备信息 / 请求关键帧 / 远程重启」三个占位 → 接后端已有接口（DeviceInfo / IFameCmd / TeleBoot） | 已有实现 → 复核应答 | 三个按钮点了有真实动作（设备屏幕/日志可见） | P1 |
| ☐ D-6 | **设备配置家族统一前端入口**（A 组收口） | 新建 `web/src/views/gb28181/device-mgmt/DeviceConfigDrawer.vue`，分 Tab 承载 A-4~A-13；挂到设备管理行操作 + 控制台 | 无需改 | 一个入口能看到全部配置项，**读回值与下发值一致** | P1 ⚠️A 组做完再做，避免每项各开一个入口 |

### 4.E 图像抓拍口径整改（跨两侧，**当前是私有口径**）

> 标准全流程是 `CmdType=DeviceConfig` + `<SnapShotConfig>`，完成通知 `CmdType=UploadSnapShotFinished`。
> 本仓是 `DeviceControl` + `Notify/SubCmd=SnapShot`，**全仓搜标准值零匹配** —— 自家联调全绿，接第三方必挂。

| 编号 | 任务（条款） | 平台侧（后端 · 前端入口） | 模拟器侧 | 验收闭环 | P |
| --- | --- | --- | --- | --- | --- |
| ☐ E-1 | **抓拍下发 CmdType 改标准口径**<br>（§9.14.3 b) / A.2.3.2.1 / A.2.3.2.12） | `manscdp/snapshot.go:59` 的 `CmdDeviceControl` → `DeviceConfig`；收到的应答按 `DeviceConfig`+`Result` 校验 | `<SnapShotConfig>` 分支从 DeviceControlDispatcher 迁到 `DeviceConfig` 分派 | SIP 明文可见 `CmdType>DeviceConfig` + `<SnapShotConfig>` | P0 |
| ☐ E-2 | **完成通知改 `UploadSnapShotFinished`**（A.2.5.7） | `handler/notify.go` 按新 CmdType 解析（现只认 `Notify/SubCmd=SnapShot`） | `SnapShotNotifyBuilder.kt:28` → `CmdType=UploadSnapShotFinished` + `SnapShotList/SnapShotFileID`（可多张） | 全仓搜标准值**有**匹配；抓拍完成后平台收到通知并关联到会话 | P0 |
| ☐ E-3 | **图片上传 HTTP 落地**（§9.14.3 c) 留白 → 按 0-4 契约） | 收图接口兼容新口径 + 落盘 + 入库（现有 registry 收图链路复用） | 按契约真上传（Android 侧当前根本没上传） | 抓拍 → 图片落到平台并可预览 | P0 |
| ☐ E-4 | **抓拍文件命名 41 位**（§9.14.1 + 表4） | 解析校验（不合规可告警） | 按 `设备编码 20 + 图像编码 02 + 时间 17 + 序列码 2` 生成 | 平台收到的文件名符合 41 位规则 | P1 |
| ☐ E-5 | **抓拍失败语义**（A.2.5.7 注释） | 按「文件标识个数 < 要求张数 = 部分失败」展示 | 缺失时如实少报标识 | 故意让 1/3 张失败 → 平台显示**部分失败**而非整体成功 | P1 |
| ☐ E-6 | **Android 抓拍真落地**（CameraX ImageCapture） | 无需改 | `SnapshotCapture.android.kt:25 takeJpeg` 恒返 `null` → 接 CameraX；iOS 侧 `IosSnapshotSourceHolder` 已有真实现可参考 | Android 真机抓拍产出 JPEG | P1 ⚠️**必须等 E-1~E-3 口径定完再动手** |
| ☐ E-7 | **抓拍配置读取**（走 ConfigDownload 家族） | 纳入 A-1 的 12 类 `ConfigType` | 同上 | 能读回最近一次抓拍配置 | P2 |

### 4.F 信令与传输

| 编号 | 任务（条款） | 平台侧（后端 · 前端入口） | 模拟器侧 | 验收闭环 | P |
| --- | --- | --- | --- | --- | --- |
| ☐ F-1 | **多响应消息聚合通用化**（附录 M） | 把 `handler/catalog.go:18-107` 的聚合（deviceID+SN+SumNum / 30s 超时 / 去重）**泛化**为通用聚合器，供 ConfigDownload、RecordInfo 等复用；补条数上限约束 | SumNum/Num 分批上限（现默认 50/包）与 TCP 分包边界复核 | 同一 SN 的多响应被正确拼装；超时分包场景不丢条 | P1 ⚠️**A-1 的依赖** |
| ☑ F-2 | **X-GB-Ver 补全**（附录 I）✅ 2026-09-17 | ✅ `handler/register.go` 鉴权通过后按 `WarningCode` 四类异常留 `gb28181.register.version_header_abnormal` 告警（missing/invalid/unknown/legacy）—— 此前 `protocol.Resolve` 产出的 `Warning` **全仓零消费**。⛔ 原描述「200 OK 响应也带」已更正：**设备不产生注册响应**，附录 I 的「注册及其响应」对设备侧只剩出站一半；响应侧平台早已覆盖（`newRegisterResponse` 对 200/401/403/500 全带头） | ✅ `sip/GbVersionNegotiation.kt`（parse + min 协商）+ `RegistrationCoordinator.platformVersion` 流（解析 200/401/4xx **全部**响应头；注销时清空）+ `ManscdpContext.effectiveGbVersion` → Catalog/DeviceInfo/DeviceStatus/AlarmStatus 按 **min(本机, 平台)** 出站 + 设置页显示协商结果 | 模拟器切 2016 后平台门禁生效；反向（平台 2.0 × 设备 2022）时设备应答降级为 2016 形态 | P1 |
| ☐ F-3 | **NAT 场景 TCP 长连接**（建立 · 复用 · 断链自愈）（§9.1.1 f、§5.2）**⛔ 原 F-4 已并入本条** | ① ✅ **「连接复用」已具备，勿再当缺口**：`bootstrap.go:613` 硬编码监听 `{udp,tcp}`；accept 的 TCP 连接按**远端地址**入池（`transport_tcp.go:198-199` `pool.Add(raddr, c)`）；`connectionReuse` 默认 `true`（`transport_layer.go:139`）→ 下行 `ClientRequestConnection` 用 `GetConnection(raddr.String())` 命中**同一条**已建连接（两侧同为 `net.JoinHostPort` 格式）；下行 transport 全部取 `device.Transport`（`ptz/operation.go:276`、`ptz/device_reboot.go:290`、`subscribe/service.go:292`、`play/service.go:710`、`cascade/control/target_loader.go:59`、`upgrade/service.go:347`、`talk/activation.go:169`）。⛔ **2026-09-17 上一轮写的「现每次 `SetDestination()` 重新 Dial」是误判**，特此更正 ② ✅ **「TCP 断开立即判设备掉线」已实现**（2026-09-17，见本节末「平台侧实现清单」）—— 原缺口：`closeObserver` 只接了 trace 与 security（`sip/server.go:336-346`），设备在线靠心跳超时（`keepalive_interval=60` × `keepalive_timeout_count=3` = **180s**），与 f)「若 TCP 通道断开，则认为 SIP 代理异常掉线」不符 | ① ✅ **断链自愈已完成**（2026-09-17，`domain/SipReconnect.kt`：被动断开 → 停活跃流 + 作废注册会话 → 1s 起指数退避封顶 30s、**次数不封顶** → `close()`+`connect()` → 重新注册；`TcpSipTransport` 上报 `ConnectionLost` + 世代号守卫；18 单测）② ⛔ 默认 `transport = UDP`（标准要求 NAT 内侧**用 TCP**）③ ✅ **`received`/`rport` 回填已消费**（2026-09-17）—— 落点是**自发现**（解析平台回值 → 判定 DIRECT/NAT/UNKNOWN → 设置页「地址转换」行展示，NAT 时提示改用 TCP），**不是改 Contact**：⛔ 平台只读 Contact 头里的 `expires` 参数（`handler/register.go:568-585`），**地址部分完全不用**，改它属伪需求 | ① 设备 TCP 注册后，平台**所有**下行（点播/控制/查询/广播）复用同一条连接 ② **拔网线 / 杀连接 → 平台秒级判离线**（不是 180s）③ 设备自愈重连后平台恢复在线且绑定正确 | P1（原 F-3 P2 + F-4 P1 合并后取 P1） |
| ☐ F-5 | **NTP 校时**（§9.10） | 补 NTP 客户端（现仅 SIP Date），前端可发起 | 已有 NTP 客户端 → 复核可用性 | 平台发起校时 → 设备时间同步 | P2 |
| ☐ F-6 | **MobilePosition 的 MESSAGE 形态**（§9.5.4） | 补 MESSAGE 分支（常量存在但分支缺失，现仅 NOTIFY 可用） | MESSAGE 形态的响应/上报 | 按 MESSAGE 形态查询位置能得到应答 | P2 |
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
| 精准云台控制 PTZPreciseCtrl | `manscdp/ptz_precise.go` | 已实现 | 回归 |
| PTZ 精准状态查询 | `ptz/query.go:34-39` | 已实现 | 回归 |
| 看守位查询 + 设备侧自动归位 | `ptz/query.go`、`controllers/device_ptz_home_position_test.go` | 已实现 | 回归 |
| 巡航轨迹列表 / 详情查询 | `ptz/query.go`、`controllers/device_ptz_query.go` | 已实现 | 回归 |
| 上述四项的**前端入口** | `PlayConsoleLinked.vue`（精准模式 / 预置位 / 巡航轨迹 / 看守位卡片 + 管理抽屉） | — | 回归 |
| 目录订阅与增量 NOTIFY | `handler/notify.go` | 已实现 | 回归 |
| 目录多父级挂载（A/B 拆分） | `catalog/dto.go:28`、`gb_channel_mount` | ✗（见 B-7） | B-7 改完后 |
| 报警上报 / 报警复位 AlarmCmd | `manscdp/device_advanced.go:146,221` | 已实现 | 回归 |
| MobilePosition（NOTIFY 形态） | `handler/notify.go` | 已实现 | F-6 改完后 |
| RecordInfo / Playback / Download / MediaStatus | `manscdp/record_info.go` | 已实现 | 回归 |
| TeleBoot / RecordCmd / GuardCmd / IFameCmd / DragZoom | `manscdp/device_advanced.go` | 已实现 | D-5 中三个占位接完后 |
| 设备软件升级下发 + 结果状态机 | `manscdp/device_upgrade.go`、`upgrade/service.go` | 假进度（见 D-4） | D-3/D-4 改完后 |
| 语音广播（下行） | `manscdp/broadcast.go`、`talk/activation.go` | 已实现 | G-1/G-4 改完后 |
| H.264/H.265 编码 + PS 封装 | 委托 ZLMediaKit | 已实现 | 回归 |
| 目录 2022 维度（CivilCode / BusinessGroupID / 业务分组 / 虚拟组织） | `manscdp/catalog.go:33-51`、`catalog/classifier.go` | 部分 | B-1/B-2 改完后 |
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
→ `B-1` + `B-2`（目录九字段，**必须同批**）

**批次 3 — 修私有口径（越晚越贵）**
`E-1` `E-2` `E-3`（抓拍口径）→ `E-6`（Android 抓拍落地）

**批次 4 — 家族补齐与收口**
`A-5` `A-9` → `A-6` `A-7` `A-8` → `A-10` `A-11` `A-12` → `D-6`（统一入口）
→ `C-1` `D-3` `D-4` `D-5` → `F-1` `F-3`
（原列的 `F-2` / `I-1` 已于 2026-09-17 完成，移出待办；**原 `F-4` 已并入 `F-3`**——NAT/TCP 长连接是「建立 · 复用 · 断链恢复」一件事）

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

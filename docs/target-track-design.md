# 目标跟踪 `TargetTrack` 设计与验收（GB/T 28181-2022 A.2.3.1.14）

> 单号：backlog **D-1** ｜ 状态：**平台侧 + 模拟器侧双向已成**（2026-09-21）
> 真机验证：**未做**（见 §8 待回填）
> 关联：[`gb28181-2022-master-backlog.md`](./gb28181-2022-master-backlog.md)（D-1 / §1.5 / §5）·
> [`play-console-ux-architecture.md`](./play-console-ux-architecture.md)（前端交互设计）

---

## 1. 一句话结论

目标跟踪是**无应答命令**，标准里**也没有任何查询命令**能读回跟踪态。
⇒ 本特性在两侧的落点都只能是**「平台意图」**：
平台把"让它跟踪什么"落库并显示，设备把"收到过什么"画在自己的屏幕上——
**两端都不能声称知道"设备此刻在跟踪什么"**。

---

## 2. 标准依据（原文，逐条核过）

### 2.1 无应答（决定整条链路的形态）

**9.3.1 基本要求 d)**（标准页 30）：

> 源设备向目标设备发送摄像机云台控制、远程启动、强制关键帧、拉框放大、拉框缩小、PTZ 精准控制、
> 存储卡格式化、**目标跟踪**命令后，**目标设备不发送应答命令**，命令流程见 9.3.2.1。

**9.3.2.1 无应答命令流程**：`MESSAGE → 200 OK`，**仅此而已**（无 MANSCDP `<Response>`）。

**表 1 设备控制功能与请求命令和应答命令 XML 消息体格式定义章节对应关系**：

| 序号 | 功能 | 请求命令章节 | 应答命令章节 |
| --- | --- | --- | --- |
| 11 | PTZ 精准控制 | A.2.3.1.11 | （无） |
| 12 | 存储卡格式化 | A.2.3.1.13 | （无） |
| **13** | **目标跟踪** | **A.2.3.1.14** | **（无）** |

### 2.2 报文（A.2.3.1.14，标准页 77-78）

```xml
<element name="TargetTrack" minOccurs="0">
  <simpleType><restriction base="string">
    <enumeration value="Auto"/><enumeration value="Manual"/><enumeration value="Stop"/>
  </restriction></simpleType>
</element>
<element name="DeviceID2" type="tg:deviceIDType" minOccurs="0"/>  <!-- 全景相机中的全景通道ID -->
<element name="TargetArea" minOccurs="0">   <!-- 手动跟踪时需要 -->
  <!-- Length / Width / MidPointX / MidPointY / LengthX / LengthY ，六个整数字元素全必选 -->
</element>
```

标准尾注与正文注释的三条关键口径：

1. **SN 之后的 `DeviceID`（必选）= 全景相机的球机通道**；`DeviceID2`（可选）= 全景通道。
   两者是**不同**的通道，别混。
2. **比例换算由设备做**：正文原话「由于平台与设备画面比例大小不同，需要进行比例关系转化。
   **因此，平台应提供画面大小**：播放窗口长度像素值和播放窗口宽度像素值。」
   ⇒ 平台只需如实发出"用户实际看到的播放窗口像素尺寸 + 该坐标系里的框选坐标"，
   **不要**自作主张乘宽高比。
3. `Auto` 要求设备自行找目标（「设备根据已配置参数执行跟踪操作，**无需平台下发坐标参数**」）。

### 2.3 由此推出的两条硬约束

| 约束 | 后果（如果做错） |
| --- | --- |
| `ResponseSemantic` 必须是**无业务应答**（`ResponseRequired: false` / `MaxAttempts: 1`） | 不是"多等一会儿"，而是**把成功当失败报**：设备按标准不回执 → operation 排到 transport deadline 才落 timeout → 前端把一次正常下发显示成「结果未知」。同族先例 `FormatSDCard` 已在海康真机上反证过。 |
| 落库表语义 = **平台意图**，不是设备状态 | 「设备现在在跟踪什么」在协议上**不可回答**。写成设备状态，界面就会长出一句永远无法证伪的「正在跟踪」。 |

---

## 3. 数据模型（平台侧）

`gb_device_target_track`（迁移 `2026-09-21-target-track.sql` + PostgreSQL / SQL Server 三方言 up/down）：

| 列 | 说明 |
| --- | --- |
| `device_id` / `target_code` | **唯一键**。`target_code` = 报文里 SN 之后的 `DeviceID`（**球机通道编码**） |
| `channel_id` | 界面上下文用的通道（不进唯一键） |
| `mode` | 标准原样拼写 `Auto`/`Manual`/`Stop`，**不做小写化**（排障时表里看到的就是线上报文那个词） |
| `device_id2` | 可选，`not null default ''` —— 空串与"没带"是同一件事，所以用值类型 |
| `area_length` … `area_length_y` | `TargetArea` 六个子元素，**整体可空**（`*int`）。⛔ 不许兜底成 0 —— 那会在界面上显示一个钉在左上角的假跟踪框 |
| `source_operation_seq` | **CAS 依据**（单调序号，非时间）：迟到的幂等重放不会把更晚一次下发的意图覆盖回去 |
| `source_sn` / `source_operation_id` / `commanded_by` / `commanded_by_dept_id` / `commanded_at` / `raw_summary` | 审计与排障 |

**为什么"全景通道不进唯一键"**：同一台球机换个全景通道再下发一次，是**同一条跟踪指令的更新**，
不是第二条记录。

**为什么仍要落库**（而不是只留 `gb_ptz_operation` 那行审计）：意图要能在刷新页面、
换一个操作员、甚至审计记录老化之后仍然看得见。先例是 `GbPTZHomePosition` 的
`Source=control_ack` 一行。

---

## 4. 平台侧链路

```
前端 buildTargetTrackArea()  ──TargetTrackCommand──▶  POST /channel/:id/target-track
                                                          │
                       manscdp.ValidateTargetTrackCommand ─┤ 下发前自检（手动跟踪没带框 → 不落表）
                       manscdp.BuildTargetTrackControlWithProfile ─┤ 组帧
                                                          ▼
                                              ptz.Service.TrackTarget
                                                          │  落意图（CAS）+ 建 operation
                                                          ▼
                                     gb_device_target_track  +  gb_ptz_operation
```

| 落点 | 文件 |
| --- | --- |
| 报文层 | `manscdp/device_advanced.go`（`TargetTrackCommand` / `TargetTrackArea` / `TargetTrackMode` / `ParseTargetTrackMode` / `ValidateTargetTrackCommand` / `BuildTargetTrackControl[WithProfile]`） |
| 服务层 | `ptz/target_track.go`（`TrackTarget`） |
| 控制器 | `controllers/device_target_track.go`（GET 读意图 / POST 下发）+ `device_advanced_control.go`（`deviceControlPayload` 抽出复用） |
| 模型 | `models/gb_target_track.go`（含 `TargetTrackRoutePath` / `TargetTrackAPIPath` 两个常量，路由从它派生） |
| 路由 | `routes/routes.go`（`dmgmt.GET/POST(gbmodels.TargetTrackRoutePath, …)`） |
| 迁移 | `migrations/2026-09-21-target-track{,-postgresql,-sqlserver}{,-down}.sql` + 三份全量快照 |

### 4.1 权限：**同路径、按方法分档**（照 `video-params` 先例）

| 方法 | 路径 | 权限码 |
| --- | --- | --- |
| GET | `/api/gb28181/device-mgmt/channel/:id/target-track` | `gb28181:ptz:view` |
| POST | 同上 | `gb28181:ptz:control` |

**刻意不新造独立权限码**（与 `FormatSDCard` 的 `gb28181:device:format_sd` 相反）：
目标跟踪是**可逆**的普通控制，不像存储卡格式化会清数据，不该比云台控制更严。

### 4.2 写侧鉴权

`SetChannelTargetTrack` 首行 `requireMaintenanceAuthentication` ⇒ 匿名请求得到明确的
未认证语义，而不是像 `/channel/:id/device-control` 那样一路走到 `loadHomePositionActor`
才报「无法识别看守位操作者」。

### 4.3 三个容易写歪的地方（都有测试钉住）

- **路径常量两处派生**：`TargetTrackRoutePath`（相对，gin 组用）与 `TargetTrackAPIPath`
  （全路径，鉴权中间件看到的）。手写两处字面量，错一个字符的表现是"接口通但恒 403"
  或"根本没注册"，**且两边都不报错**。`routes/device_target_track_routes_test.go` 用
  字面量 + 常量对账双向钉住。
- **`deviceControlPayload` 必须复用**：目标跟踪在同一份返回体上再挂意图快照 +
  `deviceAcknowledged`。少一个 `responseRequired` 字段的表现是「前端以为要等应答、永远转圈」。
- **`deviceAcknowledged` 恒 `false`**（不是"暂时还不知道"）：标准里根本没有回执通道。

### 4.4 下发前自检在**报文层**，不在控制器

`ValidateTargetTrackCommand` 拦下不合法的命令，让「手动跟踪没带框」这类请求**不落到
operation 表**（否则会留下一条永远查不到结果的 operation）。

---

## 5. 前端

### 5.1 框选换算（`views/gb28181/targetTrackBox.ts`，纯函数 + 单测）

`buildTargetTrackArea({start, end, renderedWidth, renderedHeight}) → TargetArea`

| 约定 | 取值 | 说明 |
| --- | --- | --- |
| 坐标基准 | `getBoundingClientRect()` 量到的**画面渲染像素** | 不是视频原始分辨率、不是播放器元素外框 |
| 最小框 | `TARGET_TRACK_MIN_BOX_RATIO = 0.02` | 与拉框变焦一致，用户不必记两套。⛔ 门禁在**前端**：服务端只校验"尺寸为正、中心在窗口内"，1px 的框完全合法 |
| 浮点容差 | `RATIO_EPSILON = 1e-9` | `0.2 + 0.02 - 0.2` 在 IEEE754 下是 `0.01999999999999999`，不加容差会**随机**拒掉"刚好卡阈值"的框 |
| 越界 | **钳位，不拒发** | 与后端刻意不同的一条：操作员拖到画面边缘、目标贴边被裁都是正常操作 |
| 取整次序 | 先算浮点、**最后统一取整** | 分别取整会产出"中心点 + 半宽 比 右边界多 1px"这种自相矛盾的一帧 |

⛔ **基准与遮挡（`PictureMask`）刻意不同**：遮挡用"设备 OSD 声明/上报的图像尺寸"（真机 704×576，
与页面渲染无关）；目标跟踪的框长在**渲染坐标**里。两套基准不能互相套用。

### 5.2 交互接入（`components/PlayConsoleLinked.vue`）

- 第四个「画面上拖」模式，与**拉框变焦 / 遮挡框选 / OSD 调位置**四处**互斥**
  （`toggleDragZoomMode` / `startMaskDraw` / `enterOsdEditMode` / `toggleTargetTrackMode`
  互相 `exit*`）。
- **手动跟踪下完一次就退出框选模式**（与拉框变焦"保持模式"不同）：一次下发 = 一个目标。
- `canvasModeArmed` 纳入 `targetTrackMode`；Esc 归属由它把关，layer 监听用
  capture + `stopPropagation()`（Arco 的全局 Esc 监听是并行的、随后才跑）。
- 覆盖层用**琥珀色**虚线框，与拉框变焦的青色一眼可分。
- 侧栏三个按钮：`ptz-target-track-auto` / `-manual` / `-stop`，外加意图行 / 状态行 / 提示行 / 错误行。

### 5.3 措辞纪律（同 `FormatSDCard`）

| 允许 | 禁止 |
| --- | --- |
| 「已下发，设备未回执」 | 「正在跟踪」「跟踪中」 |
| 「平台最近一次下发：<模式>」 | 任何以**设备**为主语的进行时 |

主语永远是**平台**——因为设备状态在协议上不可知。`PlayConsoleLinked.test.ts` 里有断言
专门检查"不得出现「正在跟踪」/「已完成」"。

---

## 6. 模拟器侧（`uvp-gb28181-sim`）

### 6.1 协议行为：**不发应答**（本来就对，现在被钉住）

`DeviceControlSubRouter.handleDeviceControl` 在 `dispatch` 之后取 `<RecordCmd>`，
取不到就 `?: return` ⇒ 目标跟踪报文**天然不发 MANSCDP 应答**（只回 SIP 200 OK），
符合 9.3.1 d)。

原先这条正确性是"顺带成立"的，本轮把它变成**显式契约**：
`DeviceControlSubRouterTest.targetTrack_landsState_butEmitsNoManscdpResponse`
同时断言「设备确实处理了」+「没有 `<CmdType>DeviceControl</CmdType>` 应答出栈」。

### 6.2 屏幕可见状态（本轮新增的主体）

| 层 | 文件 | 内容 |
| --- | --- | --- |
| 领域 | `domain/TargetTrack.kt`（新） | `TargetTrackState`（mode / box / deviceId2 / startedAtMs）+ `TargetTrackBox`（归一化框 + 比值换算）+ `SIMULATED_AUTO_BOX` |
| 模型 | `domain/DeviceControlModel.kt` | 新字段 `targetTrack: TargetTrackState?`，**`Stop` = 置 null**（没有"停止中的跟踪态"） |
| Handler | `domain/devicecontrol/SystemHandler.kt` | 写跟踪态与 `lastCommand` 用**同一次** `nowMs()` |
| 画布 | `ui/simulate/SimulateOverlays.kt` | `TargetTrackOverlay`：矩形 + 四角取景框角标 + 模式角标，**持续显示**（不淡出） |
| 头条 | `ui/simulate/StatusHeadline.kt` | 「目标跟踪中 · 手动 / 自动（模拟目标）/ 手动 · 无框选区域」 |

**三条刻意的设计决定**：

1. **持续显示 vs 淡出**：拉框放大的画布线框是"收到过"的一次性提示（200ms 入 / 放大 / 600ms 淡出）；
   跟踪是**持续状态**，`Stop` 才是终点。跟着淡出的话，屏幕上的"正在跟踪"2 秒后自己消失——
   而平台既没回执也没查询，现场只能看到"点了没反应"。
2. **`Auto` 说明"模拟"**：`Auto` 的框是模拟器编的（设备 AI 没接真源），文案必须是
   「自动（**模拟目标**）」且框是**确定性常量**（掷骰子没法做回归与截图）。
   `Manual` 的框是**平台报文里的真值**，所以不带任何"模拟"字样。
3. **`Manual` 没有可用框时 `box = null`，绝不回落成 `SIMULATED_AUTO_BOX`** ——
   回落会把"平台没框选"在屏幕上画成"框选成功"，而这条命令双方都没有回执可对。

**不进存档**：真机断电重启后跟踪算法不会接着盯上一条指令的目标；更要紧的是
**平台没有任何手段能发现自己看到的是重启前的旧状态**（无回执 + 无查询）。
另 `startedAtMs` 与 `auxTimestamps` 同类，必须与本次进程同基准。

### 6.3 跨仓契约锚点

`TargetTrackPlatformPayloadContractTest`（新）：把**平台侧真实构造**的三段报文
（Manual+全字段 / Auto / Stop，由 `manscdp.BuildTargetTrackControlWithProfile` 跑出来的，
来源提交 `7168c8c2`）**照抄进模拟器**并断言设备侧状态。

理由：两个仓库各自都有一堆绿测试，但"平台发的东西模拟器认不认"这条缝**没人管**——
平台侧只断言自己拼的 XML，模拟器侧只喂自己手写的 XML。元素名一旦漂移（例如平台改成
嵌套、或模拟器改成只认 `<Mode>` 不认裸 `<TargetTrack>`），**两仓都能全绿**，
而现场表现是"平台点了手动跟踪，设备屏幕毫无反应"。
⛔ **平台改报文时这三段必须跟着重抓。**

---

## 7. 验收证据（原始输出）

### 7.1 平台侧（🔎 在 **HEAD 干净快照**上跑，隔离别人的未提交改动）

```
$ git archive HEAD | tar -x -C /tmp/uvp-tt-head && cd /tmp/uvp-tt-head/server
$ go test ./app/gb28181/controllers/ ./app/gb28181/ptz/ ./app/gb28181/manscdp/ \
           ./app/gb28181/routes/ ./app/gb28181/protocol/ ./app/gb28181/migration/ -count=1
ok  uvplatform.cn/uvp-gb28181/app/gb28181/controllers  8.817s
ok  uvplatform.cn/uvp-gb28181/app/gb28181/ptz          2.994s
ok  uvplatform.cn/uvp-gb28181/app/gb28181/manscdp      0.816s
ok  uvplatform.cn/uvp-gb28181/app/gb28181/routes       2.545s
ok  uvplatform.cn/uvp-gb28181/app/gb28181/protocol     1.835s
ok  uvplatform.cn/uvp-gb28181/app/gb28181/migration    5.137s
```

> ⚠️ **工作区当前跑 `./app/gb28181/controllers/` 会看到红**，两条且每次不同
> （`TestDeviceMgmt_ControlPTZWiperRejectsUnknownAction` / `TestStreamProbeControllerScopesStream`）。
> 逐条核过：那是**另一拨「雨刷」改动**（`device_ptz_resources.go` / `_test.go` 未提交，
> 测试先行、实现未跟上），与目标跟踪无关 —— `HEAD` 快照上全绿即为证据。

前端（同样在 HEAD 快照上）：

```
$ npx vitest run src/views/gb28181/targetTrackBox.test.ts \
                 src/api/gb28181.targetTrack.test.ts \
                 src/views/gb28181/components/PlayConsoleLinked.test.ts
Test Files  3 passed (3)
     Tests  201 passed (201)
```

### 7.2 模拟器侧

```
$ ./gradlew :shared:jvmTest :composeApp:testDebugUnitTest
BUILD SUCCESSFUL

shared     : tests=1804  failures=0  errors=0
composeApp : tests=164   failures=0  errors=0
```

本轮新增用例：

| 测试文件 | 条数 | 钉住什么 |
| --- | --- | --- |
| `TargetTrackStateTest` | 12 | 比值换算（尺子不能拿反）、缺尺子/退化框/整框出界 → null、部分出界夹取、`Manual` 不回落模拟框 |
| `TargetTrackPlatformPayloadContractTest` | 4 | **平台真实报文** → 设备侧状态；`Stop` 清空；三份都走单命令分支 |
| `DeviceControlDispatcherTest`（T4-7…T4-12） | 6 | 落态/换算/`Auto` 常量/`Stop` 清空/无框不回落/未知 mode 不动状态/顺序覆盖 |
| `DeviceControlSubRouterTest`（1 条） | 1 | **不发 MANSCDP 应答**（9.3.1 d)） |
| `TargetTrackOverlayLabelTest`（composeApp） | 4 | 角标文案：`Auto` 必须带"模拟"、`Manual` 不得带、无框要明说 |

---

## 8. 未完成 / 待回填

| 项 | 说明 |
| --- | --- |
| **真机验证** | 未做。目标跟踪依赖设备 AI + 双目（全景 + 球机）结构，手上没有对应机型。D-2（存储卡格式化）当年的真机一跑就把「无应答」这条结论反证了；本条的 `ResponseRequired:false` **目前只有标准原文支撑**，尚无真机证据。 |
| **模拟器真机点击** | 未做。模拟器是 Android/iOS 应用（`composeApp` 无 JVM/桌面目标），本机没有跑起来的设备 ⇒ 「平台点一下 → 手机屏幕上出现跟踪框」这一跳**没有实机走过**。已用跨仓契约测试（§6.3）覆盖报文到状态的这一段，但**画布渲染**仍只在单测层验证。 |
| **`Auto` 的框** | 是写死的 `SIMULATED_AUTO_BOX`（`0.34, 0.28, 0.30, 0.40`）。真机上是设备 AI 的输出，模拟器只能代答——文案已自曝"模拟目标"，但数值本身不代表任何真实目标分布。 |
| **平台侧「设备不支持」的负向证据** | 未做。`targetTrack?: ControlCapability` 默认 `unknown`（后端只认设备显式声明）；与精准云台（P-…）同一类缺口：无应答 + 无查询 ⇒ 平台拿不到"这台设备不支持目标跟踪"的负向证据，界面只能停在 `unknown`。 |

---

## 9. 防回归锚点清单（改这条链时逐条过）

| # | 锚点 | 位置 |
| --- | --- | --- |
| 1 | `ResponseRequired: false` / `MaxAttempts: 1` | `ptz/target_track.go` |
| 2 | 路由路径两常量对账（字面量 + `TargetTrackRoutePath` / `TargetTrackAPIPath`） | `routes/device_target_track_routes_test.go` |
| 3 | 写路径不继承读权限（复用 `videoParamCasbinModel` 断言） | 同上 |
| 4 | `deviceAcknowledged` 恒 `false` | `controllers/device_target_track_test.go` |
| 5 | 前端不得出现「正在跟踪 / 已完成」 | `PlayConsoleLinked.test.ts` |
| 6 | 浮点容差 `RATIO_EPSILON`（阈值边界不被随机拒） | `targetTrackBox.test.ts` |
| 7 | 模拟器**不发** DeviceControl MANSCDP 应答 | `DeviceControlSubRouterTest` |
| 8 | 平台报文 → 模拟器状态（元素名漂移即红） | `TargetTrackPlatformPayloadContractTest` |
| 9 | `Auto` 角标必须带"模拟" | `TargetTrackOverlayLabelTest` |
| 10 | `Manual` 无框不得回落成模拟框 | `TargetTrackStateTest` + `DeviceControlDispatcherTest.T4-10` |

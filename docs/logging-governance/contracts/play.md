# C04 · `play` 链路定位契约（草案 · 待裁决）

- **状态**：🔄 2.2 已裁决为 **A′（改签名 + 内部兜底）**，影响面已评估，待实施
- **范围**：`server/app/gb28181/play/**`、`server/app/gb28181/snapshot/**`
- **样本**：42 个日志调用点（静态扫描 + 关键点人工核对）
- **口径提醒**：本文所有"现有字段"来自静态扫描，`zap.Field` 变量展开处已人工核对；
  个别拼接消息的调用点可能未捕获。

---

## 一、这条链路要回答什么

播放是现场最高频的排障场景，出问题时要能回答四件事：

| 问题 | 需要的字段 |
|---|---|
| 哪台设备、哪个通道？ | `device_id` / `channel_id` |
| 哪路流、在哪个媒体节点？ | `stream_id` / `node_id` |
| 这次请求对应哪次 SIP 会话？ | `call_id`（+ `correlation_id`） |
| 成了没有？没成是为什么？ | `outcome` / `reason_code` |

**结论：`gb28181.play.*` 主流程这条线基本达标**（T09/T14 的成果）——
`requested` → `node_selected` → `rtp_allocated` → `invite_accepted` → `media_ready`
一路都带 `device_id` + `channel_id` + `node_id` + `stream_id`，后段还带 `call_id`。

问题集中在**三个地方**。

---

## 二、必须先解决的三个问题

### 2.1 字段命名两套并存（最硬的问题）

同一个 `play` 模块内部，**曾经**两套命名同时在用（下表为 2026-09-14 改前快照，现已统一）：

| 文件 | 命名风格 | 例子 |
|---|---|---|
| `play/service.go` | snake_case | `device_id` `channel_id` `stream_id` `node_id` |
| `play/reconciler/check.go`、`reconciler.go` | camelCase + 大写 ID | `deviceID` `channelID` `streamID` `nodeID` |
| `snapshot/service.go` | 无 `_id` 后缀 | `device` `channel` |

**这不是洁癖问题，是已经造成实际损失的问题**：我写的统计脚本一度把 `play` 判为
"95% 无定位字段"，真相是 `reconciler` 用了 `streamID` 而识别表里只有 `stream_id`/`streamId`。
**工具认不出 → 门禁也认不出 → 人 grep 也会漏。** 同一条流在两组日志里是两个键名，跨日志关联直接断掉。

**建议**：全链路统一 `snake_case`（`device_id` / `channel_id` / `stream_id` / `node_id`）。
`reconciler` 那批改为 snake_case —— 顺带让它与 `service.go` 的日志能用同一个 grep 串起来。

- [x] 2.1 `reconciler/*.go` 的 `deviceID`/`channelID`/`streamID`/`nodeID` → snake_case
      （**实测 18 处**：`check.go` 12 + `reconciler.go` 6；2026-09-14 完成）
- [x] 2.1 `snapshot/service.go` 的 `device`/`channel` → `device_id`/`channel_id`
      （**实测 4 处**；2026-09-14 完成）

> 改后 `play` 链路 **camelCase / 大写 ID 字段名清零**（复跑：`grep -rnE 'zap\.(String|Int64|Bool|Int)"[a-zA-Z]*[A-Z]' app/gb28181/play/ app/gb28181/snapshot/`）。
> 唯一字段名 173 → **167**，仓库"驼峰+大写ID"风格 23 → **5**。
> ⚠️ 测试断言同步改了 3 处（`reconciler/logging_background_events_test.go:115-117` 的
> `record["streamID"]` 等）——**这是改字段名的必查项**，门禁与契约测试往往直接按字段名取值。

### 2.2 停播链路缺设备与通道（已选 A，影响面见下）

```go
func (s *Service) Stop(ctx context.Context, streamID string) (err error) {
    fields := []zap.Field{zap.String("stream_id", streamID), zap.Float64("duration_ms", ...)}
```

`Stop` 的签名**只收 `streamID`**，`device_id` / `channel_id` 根本不在作用域内。于是停播三个事件
（`stop_requested` / `stop_completed` / `stop_failed`）全都只有 `stream_id`。

**为什么这是问题**：运维看到"停播清理失败"时，手上只有一串 `stream_id`，没法直接回答
"是哪台设备停不掉" —— 还得先去查映射表。而停播失败往往正是"某台设备卡住了"。

#### 2.2.1 调用面盘点（2026-09-14 逐点核对）

先说一个**容易误判的前提**：这三个事件只在 `Service.Stop` 自身发出。
`StopIfPersistedCurrent`（`recovery.go:228`）与协调器的 `stopCurrentResult`（`service.go:917`）
**都不打这三个事件** —— 所以 `reconciler` 走条件停播、`Stop` 内部走持久化清理时，
`play.stop_*` 根本不会出现。真正产出这三个事件的只有下列 4 个调用点。

| # | 位置 | 触发场景 | 手上有没有 device/channel |
|---|---|---|---|
| 1 | `controllers/play.go:263` | HTTP `DELETE /api/gb28181/play/:streamId` | ⚠️ **已经有但被丢弃** —— `streamVisible()`（`:309`）为做数据权限校验已把整个 `GbChannel` 行查出来了，只 return 了 bool |
| 2 | `handler/hook.go:516` | ZLM `on_stream_none_reader`（rtp 动态实时流） | ❌ body 只有 `App`/`Stream`/`Schema` |
| 3 | `handler/hook.go:529` | 同上，非 rtp 分支 | ❌ 同上 |
| 4 | `handler/hook.go:609` | ZLM `on_rtp_server_timeout` | ❌ body 只有 `StreamID`/`App`/`SSRC`/`MediaServerID`（`SSRC` 可反查，但不是现成的） |
| — | `play/reconciler/reconciler.go:231` | 对账清理假阳性（`CurrentSSRC == ""` 才走） | ✅ `ch.DeviceID` / `ch.ChannelID` 就在手上，零成本 |

**要改的签名/接口共 4 个定义 + 5 个调用点（生产 9 处）**：

| 类别 | 位置 |
|---|---|
| 方法定义 | `play/service.go:947` |
| 接口 `reconciler.Stopper` | `play/reconciler/reconciler.go:33` |
| 接口 `handler.PlayStopper` | `handler/hook.go:36` |
| 接口 `controllers.PlayService.Stop` | `controllers/play.go:36` |

**测试面 15 处**（改签名会全部报编译错，属好事）：

| 类别 | 位置 |
|---|---|
| mock 定义 4 个 | `handler/hook_test.go:130`、`reconciler/reconciler_test.go:45`、`controllers/play_stop_test.go:47`、`controllers/play_authorization_test.go:35` |
| play 包内直调 11 处 | `recovery_stop_failure_test.go:104/134/177/199`、`service_test.go:674/696`、`fixed_playback_service_test.go:211/334`、`fixed_authorization_test.go:239`、`start_cleanup_pending_test.go:101`、`coordinator_test.go:491` |

生产 9 + 测试 15 = **24 处**。签名变更由编译器强制找齐，**不会漏** —— 这是 A 相对 B 的最大优点。

#### 2.2.2 两个必须一起定的点（否则 A 会做成负收益）

**① hook 三处拿不到值 → 传空会打出 `device_id=""`，比现在更糟。**
`console_encoder.go:308` 把空串渲染成 `""`（不是省略），而统计脚本/门禁按"字段存在"判断，
于是**"看起来有定位字段、实际是空的"**——比字段缺席更难发现。所以 A 必须配套一条规则：
**组装停播字段时空值不 append**。

**② 空值路径要有兜底来源，否则 hook 路径等于白改。**
`Stop` 内部其实有三个现成的 device/channel 来源，都不在调用方手里：

| 来源 | 位置 | 代价 | 覆盖 |
|---|---|---|---|
| 内存会话 `uac.SessionManager.Get(streamID)` → `sess.DeviceID`/`ChannelID` | `s.sessions`（`service.go:163`，`uac.Session` 自带这两个字段） | 零（纯 map 查找） | 动态流（`streamID == ssrc`）最准 —— 这次停的就是这个会话 |
| DB 通道行 `FindChannelByStream` | `service.go:963`（**Stop 本来就已经在查**，`stopDirect` 又查一次 `:981`） | 0~1 次查询 | 全量；注：这条查询现在重复了 2 次 |
| 固定流 ID 解析 `ParseFixedStreamID` | `service.go:969`（已调用） | 零 | 固定流（`streamID = deviceID_channelID`） |

**B 方案被低估了**：它并没有"凭空多一次查询"，因为 `Stop` 主路径本来就要查通道行；
入口查一次、后续复用反而能**把现在重复的 2 次查询收敛成 1 次**。

#### 2.2.3 落地建议：A + 内部兜底（记为 A′）

1. 签名加两个参数：`Stop(ctx, streamID, deviceID, channelID)`；
2. **有值的调用方直接传**：`reconciler.go:231`（`ch.DeviceID`/`ch.ChannelID`）、
   `controllers/play.go:263`（把 `streamVisible` 改成返回 `*GbChannel`，顺手就有了）；
3. **没值的（hook 3 处）传空**，`Stop` 内部按上表兜底：
   内存会话 → 固定流解析 → 已有的 `FindChannelByStream` 结果；
4. 组装字段时**空值不 append**（见 2.2.2 ①）；
5. 收益是双份的：调用方有值时不查库，且日志恒定带上设备/通道。

#### 2.2.4 会影响功能可用性吗（结论：不会，但有三条纪律）

**签名变更本身零风险** —— 新增的两个参数只进日志，不参与任何控制流、路由、鉴权或 CAS 判断。

**风险全在「内部兜底」**，它可能引入新的 I/O 与新的失败点。逐条约束：

| 纪律 | 违反后果 |
|---|---|
| ① 兜底只为取日志字段：**查询失败 / `ctx` 已取消 / 查不到 → 字段省略，绝不 `return err`，绝不改变控制流** | 一次 DB 抖动就能把"停播成功"翻成"停播失败" —— 这才是真正伤可用性的写法 |
| ② device/channel **只作日志输入**，永远不拿它替代 `streamID` 去做定位 / 鉴权 / 跳过查询 | 将来有人"顺手优化"成按 device/channel 路由 → 变成行为变更 |
| ③ 第一版**不动 `stopDirect` 的查询复用**（`service.go:981`），入口也**不新增 DB 查询** | 复用会改掉 `stopDirect` 的契约与假阳性判定时序，属行为改动 |

**兜底首选内存会话，几乎不需要查库**：
`uac.SessionManager.Get(streamID)` 是 `RLock` + map 查表（`uac/uac.go:541-545`）——
**无 I/O、无 error 返回、无副作用**；而 `uac.Session` 在发 INVITE 时就写入了
`DeviceID`/`ChannelID`（`service.go:699-709`）。

两个来源天然互补，覆盖了全部 4 个调用点：

| 场景 | 内存会话在吗 | 取值来源 |
|---|---|---|
| HTTP 停播 / hook 无人观看 / hook RTP 超时 | ✅ 会话还活着 | **内存会话**（零 I/O） |
| 重启后对账清理假阳性 | ❌ 会话已丢 | **调用方 `ch.DeviceID`/`ch.ChannelID`**（本来就有） |

→ `2.2c` 的兜底链**第一选择内存会话**；DB 那条只在两者都落空时考虑，且失败必须静默降级。
既然每个场景都有一条零成本来源，**DB 查询这一版可以完全不引入**。

**不动的三件事**：`streamVisible()` 的校验语义（"流不存在或无权停播"照旧拒绝）、
`Stop` 的返回值语义、既有路径的时序。`streamVisible` 仅把 `return bool` 换成 `return *GbChannel`。

**唯一可观测差异**：停播日志多出 `device_id`/`channel_id`，**前端与框架都不用改** ——
已核对两端排序表都已预留且都在最前：Go `consoleFieldOrder`（`console_encoder.go`）与页面
`FIELD_ORDER`（`web/src/views/gb28181/realtime-log/log-line.ts`）的第一项就是
`device_id` / `channel_id`（`channel_id` 紧随其后），排在 `stream_id`/`event` 之前 → 渲染即"先身份后事件"。
（若采用"入口查库"的激进版，才会多一次 SELECT 延迟 —— 故不采用。）

> 为什么不直接选 B：B 只在 `Stop` 内部改，**编译器不会帮你找调用方**，
> 将来新增调用点很容易又漏掉；A 把"该不该带定位"变成签名上的显式契约。
> 但 A 单独用会漏掉 hook 那 3 条最活跃的路径，所以取 A′。

- [x] 2.2a `Stop` 签名加 `deviceID`/`channelID`，4 个接口/定义同步（生产 9 处）
- [x] 2.2b 4 个测试 mock + 11 处测试直调同步改签名
- [x] 2.2c `Stop` 内部兜底：内存会话 → 固定流解析 → 通道行（未新增任何 DB 查询）
- [x] 2.2d `streamVisible()` 改为返回 `*GbChannel`，HTTP 停播传真实设备/通道
- [x] 2.2e 空值字段不 append（避免 `device_id=""` 假达标）
- [x] 2.2f 验收：`go build ./...` 通过 + play/reconciler/handler 三包测试全绿；
      新增 7 条回归测试锁定兜底链与空值省略（见 2.2.5）

#### 2.2.5 实施记录（2026-09-14）

**生产代码改动（9 处）**

| 文件 | 改动 |
|---|---|
| `play/service.go` | `Stop(ctx, streamID, deviceID, channelID)`；新增 `stopLogIdentity`（兜底链）与 `stopLogFields`（空值不 append）；`FindChannelByStream` 命中后顺手补缺失字段（无新查询） |
| `controllers/play.go` | `PlayService.Stop` 接口加两参；`streamVisible` 由 `bool` 改为 `(*gbmodels.GbChannel, bool)`（**拒绝理由与校验语义一字未动**）；HTTP 停播把已查出的整行透传 |
| `handler/hook.go` | `PlayStopper.Stop` 接口加两参；3 处调用传 `"", ""`（回调体本就无这两个值） |
| `play/reconciler/reconciler.go` | `Stopper.Stop` 接口加两参；调用点传 `ch.DeviceID`/`ch.ChannelID`（手上已有） |

**新增回归测试（`play/stop_log_identity_test.go`，7 条）**

| 测试 | 锁住的规则 |
|---|---|
| `TestStopLogFieldsOmitsEmptyIdentity` | §2.2e 空值不 append（覆盖"只缺一个"的全部组合） |
| `TestStopLogIdentityPrefersCallerValues` | 调用方给了值就不回退（哪怕会话里挂着另一组值） |
| `TestStopLogIdentityFallsBackToMemorySession` | §2.2c 首选内存会话 |
| `TestStopLogIdentityFallsBackToFixedStreamID` | §2.2c 会话已丢时走固定流解析 |
| `TestStopLogIdentityDegradesSilently` | §2.2.4① 两个来源都落空 → 静默返回空，不报错 |
| `TestStopEventsCarryDeviceAndChannel` | 端到端：`stop_requested`/`stop_completed` 都带 `device_id`/`channel_id` |
| `TestStopEventsOmitIdentityWhenUnresolvable` | 端到端：解析不到时字段**缺席**，而不是打出空串 |

`reconciler` 侧的透传也补了断言：`TestT6_6_Q5LocationMissAllOffline` 额外校验
`Stop` 收到的是 `dev-ssrc-6/ch-ssrc-6`（即 `ch.DeviceID`/`ch.ChannelID`）。

**验收结果**

- `go build ./...` ✅
- `go test ./app/gb28181/play/... ./app/gb28181/handler/...` ✅ 全绿
- `go test ./app/gb28181/controllers/` ⚠️ 只有 `TestLoggingGBHTTPContextWiring` 红 ——
  该用例是**既有红灯**：findings 落在 `controllers/device_delete.go:257/328`（直接用 `app.ZapLog`）
  与 `cascade/controller/management.go:166`（gin ctx 传给标准 context），两个文件本次均未改动。
  与 2.2 无关，属日志门禁的另一条待办线。

**实测指标变化（`scan-logging.py --root .`，改前用 `git stash` 单文件对照）**

| 指标 | 改前 | 改后 |
|---|---|---|
| 无任何定位字段 | 171/337 = 50% | **172/337 = 51%** |
| `play` 命名空间 | 20 总 / 10 无定位 = 50% | 20 总 / 10 无定位 = **50%（不变）** |
| snake_case 字段出现次数 | 269 | 268 |

**这 +1 是扫描器的盲区，不是治理回退。** 停播三事件的字段现在由 `stopLogFields()`
组装，脚本的正则只能把 `append(` 的第一个标识符当变量名（这里是**函数名**）→ 展开不出来
→ 把 `gb28181.play.stop_requested` 计入"无定位"。运行时它**确实带** `device_id`/`channel_id`，
由 `TestStopEventsCarryDeviceAndChannel` 锁定。

⚠️ **不要为了指标好看去放宽脚本的判定** —— 那会把"条件省略"也算成"已达标"，
正好踩中 §2.2.2① 那条"看起来有定位字段、实际是空的"的坑。盲区已写进
`scan-logging.py` 文件头。

**与计划的偏差（1 处）**：`2.2c` 原写"…并把重复的 `FindChannelByStream` 收敛"。
实施时按 §2.2.4 纪律③**未做收敛** —— 复用会改掉 `stopDirect` 的契约与假阳性判定时序，
属行为改动，不在"只为补日志字段"的范围内。

#### 2.2.6 实机验收记录（2026-09-15）

验收方法已固化为通用手册 [`testing-guide.md`](../testing-guide.md)（四层：门禁 / 自动化 / 形态 / 场景）。

**⭐ 场景 2 是本轮最有价值的一条证据** —— 它**正式证伪了 §2.2.5 的静态盲区**：
此前只知道"运行时确实带字段（由单测锁定）"，现在有**真实链路日志**证明。

```
# 无人观看断流 → hook 调用 Stop 时传的是 ""，""（值不可能来自入参）
08:33:10.690 INFO  hook   ZLM Hook on_stream_none_reader            stream=0200000001
08:33:10.692 INFO  play   停播事务开始  device_id=37010301021180000007 channel_id=34020000001320000020 \
                          stream_id=0200000001 request_id=499b5e3c-... event=gb28181.play.stop_requested
08:33:10.753 INFO  play   停播清理完成  device_id=37010301021180000007 channel_id=34020000001320000020 \
                          stream_id=0200000001 request_id=499b5e3c-... event=gb28181.play.stop_completed
```

两行 `request_id` 与 hook 那行**完全一致** → 确属同一次调用；`device_id`/`channel_id` 非空 →
**只可能由 `Stop` 内部兜底链从内存会话取出**（`stream_id=0200000001` 是动态流，
`ParseFixedStreamID` 解析不了，故走的正是 `s.sessions.Get()` 分支）。

| 场景 | 触发 | 结果 |
|---|---|---|
| 1 · HTTP 停播（调用方有值） | `DELETE /api/gb28181/play/0200000000` | ✅ 带 `device_id=37010301021180000007` + `channel_id=34020000001320000010`；`released: true` |
| 2 · 无人观看（调用方传空 → 兜底） | 点播后不消费，等 ~30s | ✅ 字段由兜底链补出（见上） |
| 3 · 功能回归 | 停播后查 monitor | ✅ `{"code":1,"message":"流不存在"}` —— 流真的停了 |
| 4 · 对账路径 | — | ⏸ **本机不可测**：`reconcile_interval_sec=0`（启动日志明确打印"对账 reconciler 未启用"），仅单测覆盖 |

**负面判据**：`device_id=""` **0 条**；本轮 16 条 play 事件**全部**带定位字段（反查为空）。

### 2.3 派生字段占了行内位置

点播/停播事件的字段尾巴是这样的：

```
event=gb28181.play.requested stage=request outcome=started device_id=… channel_id=…
```

`stage` 和 `outcome` 是**前端派生字段**（控制台按它们做状态推导），人读时是噪声——
`outcome=started` 和消息"点播事务开始"说的是同一件事。`reason_code` 有排障价值，保留。

**建议**：`stage`/`outcome` 保留但**移到行尾**（或由编码器降为低对比度）。
这属于框架层（`consoleFieldOrder`）的取舍，与 C03 一起做，不在本链路单独改。

- [ ] 2.3 将 `stage`/`outcome` 归入机器字段段（框架层，见 C03）

---

## 三、逐事件契约

### 3.1 点播主流程（`play/service.go`，已达标，仅需核对）

| 事件 | 等级 | 定位字段 | 判定 |
|---|---|---|---|
| `gb28181.play.requested` | INFO | `device_id` `channel_id` | ✅ 保留 |
| `gb28181.play.validation_succeeded` | INFO | `device_id` `channel_id` | ⚠️ 见下 |
| `gb28181.play.node_selected` | INFO | `device_id` `channel_id` `node_id` `stream_id` | ✅ |
| `gb28181.play.rtp_allocated` | INFO | + 同上 | ✅（可补 `rtp_port`） |
| `gb28181.play.invite_accepted` | INFO | + `call_id` `cseq` | ✅ |
| `gb28181.play.media_ready` | INFO | + `call_id` `cseq` | ✅ 成功终点 |
| `gb28181.play.failed` | WARN | `device_id` `channel_id` `reason_code` | ✅（经 `fields` 变量携带，已核对） |

> ⚠️ **`validation_succeeded` 按判据②应降级**：它紧跟在 `requested` 之后，只表示"请求合法"，
> 运维看到它没有动作可做。校验失败会走 `:469` 的 `failed`，所以失败信息不会丢。
> **建议降为 DEBUG**（或合并进 `requested`）。这是本链路最直接的一次"减日志"。

- [x] 3.1 `gb28181.play.validation_succeeded` → DEBUG（2026-09-14 完成，`service.go:505`）

### 3.2 停播（见 2.2）

| 事件 | 等级 | 现状定位字段 | 判定 |
|---|---|---|---|
| `gb28181.play.stop_requested` | INFO | `device_id` `channel_id` `stream_id` | ✅ 已补（2.2） |
| `gb28181.play.stop_completed` | INFO | 同上 | ✅ 已补（2.2） |
| `gb28181.play.stop_failed` | WARN | 同上 | ✅ 已补（2.2，失败更需要定位） |

### 3.3 流复用探测（`play/service.go` 335–419、529–537）

7 个事件共用 `playEventReuse*` 常量（**已有常量，好事**），字段是 `stream_id` / `node_id`。

| 事件 | 现状 | 判定 |
|---|---|---|
| `gb28181.play.reuse_probe_failed` | WARN，`stream_id` | 保留；建议补 `node_id` |
| `gb28181.play.reuse_bound_node_offline` | INFO，`stream_id` `node_id` `online` | ✅ |
| `gb28181.play.reuse_bound_node_missing` | WARN，`stream_id` `node_id` | ✅ |
| `gb28181.play.reuse_fallback_probe` | INFO，`stream_id` `active_nodes` | ✅ |
| `gb28181.play.reuse_fallback_node_failed` | DEBUG，`stream_id` `node_id` | ✅ |
| `gb28181.play.reuse_fallback_hit` | INFO，`stream_id` `node_id` | ✅ |
| `gb28181.play.reuse_success` / `reuse_cleanup` | INFO，`device_id` `channel_id` `stream_id` | ✅ |

> 这组日志**带 `node_id` 是对的**——流复用的故障 90% 是"节点没了但 LocationMap 还记着"。保留 `active_nodes`。

**判定：`reuse_probe_failed` 不补 `node_id`。** 它只在**单节点路径**（`!s.useMultiNode()`，
`service.go:364`）打；单节点下 `s.zlm` 是唯一 client（`ZLM` 接口只有 3 个方法，**没有任何节点标识**），
`node_id` 只会是一个恒定值 —— **补它属于"为了指标好看而打印"**（判据②）。
多节点路径的探测失败走的是 `reuse_bound_node_offline` / `reuse_fallback_node_failed`，那两条**本来就带 `node_id`**。
保留 WARN 等级（复用没达成、退化为新建流，属"功能降级但仍在跑"）。

- [x] 3.3 `reuse_probe_failed` **判定为不补 `node_id`**（单节点无节点维度；理由见上）—— 2026-09-14

### 3.4 通道快照令牌（`play/service.go` 835–857）

3 个 WARN 事件（`snapshot_token_unavailable` / `snapshot_node_unavailable` / `snapshot_token_failed`），
字段 `device_id` `channel_id` `stream_id`/`node_id`。**定位够用，保留。**

### 3.5 `snapshot` 模块（3 条）

| 事件 | 等级 | 现状 | 判定 |
|---|---|---|---|
| `gb28181.snapshot.panic` | ERROR | `event` `panic_type` `stack` `device` `channel` | 补正则外字段名（见 2.1） |
| `gb28181.snapshot.capture_failed` | WARN | `event` `device` `channel` | 同上 |
| `gb28181.snapshot.captured` | INFO | `event` `device` `channel` `bytes` `url` | 同上；`url` 需确认已脱敏 |

### 3.6 `reconciler`（17 条）

全部用常量 + camelCase 字段。功能上分三类：

| 类别 | 事件 | 等级 | 说明 |
|---|---|---|---|
| **对账结论** | `round_completed` | INFO | 字段 `scanned` `cleaned` `skipped` `failed` `elapsed` |
| **假阳性清理** | `cleanup_succeeded` / `cleanup_failed` | INFO / ERROR | `streamID` `deviceID` `channelID` |
| **探测过程** | `probe_failed`×3、`location_fallback`、`node_inactive`、`cross_node_hit`、`no_nodes` | DEBUG/WARN | `streamID` `nodeID` |

**两个具体问题**：

1. **`play.reconcile.probe_failed` 同名不同级**：出现 3 次，两处 WARN（:61、:84）一处 DEBUG（:116）。
   同名事件跨等级会让按事件聚合的统计失真。**建议统一为 DEBUG**——
   单节点探测失败是对账过程中的正常抖动，真出问题会由 `no_nodes` / `round_completed.failed` 兜住。

2. **`round_completed` 缺"对账范围"**：只有计数和耗时，没有"这一轮扫的是哪个节点/哪批流"。
   排查时看到 `cleaned=5` 却不知道动了哪些。**建议补 `node_id`（若按节点扫）或 `sample_stream_id`**。

#### ⚠️ 例外：这 7 条**不该**补设备字段（别误伤）

统计显示 `play` 命名空间约 50% "无定位字段"，但其中一大半属于**组件级事件**，
定位对象是"reconciler 这个组件"而不是某台设备/某路流：

| 事件 | 现有字段 | 定位对象 |
|---|---|---|
| `play.reconcile.disabled` | `interval` | 组件配置 |
| `play.reconcile.duplicate_start` | — | 组件状态机 |
| `play.reconcile.panic` | `panic_type` | 组件自身 |
| `play.reconcile.stopped` | — | 组件生命周期 |
| `play.reconcile.overlap_skipped` | — | 轮次调度 |
| `play.reconcile.interrupted` | — | 组件生命周期 |
| `play.reconcile.list_failed` | — | 查库失败 |

**判据要分层**：设备级事件必须带 `device_id`/`channel_id`；组件级事件只需能定位到组件 + 状态。
给 `stopped` 硬塞一个 `device_id` 是错的——**那不是治理，那是为了指标好看而打印**
（正好违反判据②）。门禁的"必需定位字段"校验要按**事件声明的定位维度**来，不能一刀切。

- [x] 3.6.3 在事件目录里把上述 7 条登记为「组件级事件」，豁免设备定位字段要求
      —— 2026-09-14 建 [`event-catalog.md`](./event-catalog.md) §7.3

- [x] 3.6.1 `play.reconcile.probe_failed` 三处等级统一为 DEBUG（2026-09-14，`check.go:61/84/116`）
- [x] 3.6.2 `play.reconcile.round_completed` 补对账范围字段（2026-09-14，补 `sample_stream_id`）

---

## 四、本链路勾选清单

- [x] 2.1 `reconciler` 字段改 snake_case（**实测 18 处**，见 2.1）
- [x] 2.1 `snapshot` 的 `device`/`channel` → `device_id`/`channel_id`（**实测 4 处**）
- [x] 2.2a `Stop` 签名加 `deviceID`/`channelID` + 4 个接口/定义同步（见 2.2.1）
- [x] 2.2b 4 个测试 mock + 11 处测试直调同步
- [x] 2.2c/d/e `Stop` 内部兜底（**首选内存会话**，未新增 DB 查询）· `streamVisible` 返回通道 · 空值字段不 append
- [x] 2.2f 验收：`go build ./...` ✅ + play/reconciler/handler 全绿 ✅（`controllers` 的既有红灯见 2.2.5）
- [x] 2.2g 纪律自检：兜底失败静默降级（不改 `Stop` 返回值 / 控制流）✅、device/channel 只作日志输入 ✅、
      入口未新增 I/O ✅（`sessions.Get` + `ParseFixedStreamID` 均零 I/O）、
      `streamVisible` 校验语义一字未动 ✅（见 2.2.4）
- [x] 3.1 `gb28181.play.validation_succeeded` → DEBUG（2026-09-14 完成，`service.go:505`）
- [x] 3.6.1 `probe_failed` 等级统一（三处 → DEBUG）
- [x] 3.6.2 `round_completed` 补对账范围（`sample_stream_id`）
- [x] 3.6.3 `reconciler` 7 条组件级事件登记豁免（不补设备字段）→ [`event-catalog.md`](./event-catalog.md) §7.3
- [x] 3.3 `reuse_probe_failed` **判定为不补 `node_id`**（单节点无节点维度，见 3.3）
- [x] 3.2b（新发现）`play.attempt_begin_failed` / `attempt_finish_failed` 补 `device_id`/`channel_id`
      （`controllers/play.go:104/119`，此前不在清单里，逐条核对时翻出；见 5.3）
- [x] 验收：`play/**` 无 camelCase / 大写 ID 字段名字段名 ✅；
      `play` 桶剩余"无定位"**全部可解释**（7 条组件级豁免 + 轮次级 + 2 处静态盲区），见 5.4

**预计新增日志：0 条；减少：1 条（`validation_succeeded` 降级）。**
这一轮主要是"改字段名 + 补定位"，不是"多打日志"——符合"不为打印而打印"。

---

## 五、第二轮实施记录（2026-09-14 · 2.1 / 3.1 / 3.3 / 3.6）

### 5.1 代码改动（4 个文件 24 处字段名 + 3 处等级 + 2 处补定位）

| 文件 | 改动 |
|---|---|
| `play/reconciler/check.go` | 12 处 `streamID`/`nodeID` → `stream_id`/`node_id`；`probe_failed` 两处 **WARN → DEBUG** |
| `play/reconciler/reconciler.go` | 6 处 `streamID`/`deviceID`/`channelID` → snake_case；`round_completed` 补 `sample_stream_id` |
| `snapshot/service.go` | 4 处 `device`/`channel` → `device_id`/`channel_id` |
| `play/service.go` | `validation_succeeded` **INFO → DEBUG** |
| `controllers/play.go` | 两处 attempt 事件补 `device_id`/`channel_id` |
| `play/reconciler/logging_background_events_test.go` | 3 处字段名断言同步；新增"无样本时字段缺席"用例 |

**`sample_stream_id` 的空值处理**用 `zap.Skip()`：编码器（`console_encoder.go:235`）遇到
`zapcore.SkipType` 直接跳过 → 没有可扫的流时该字段**整个不出现**，而不是
`sample_stream_id=""`（后者看起来像"已带范围字段"——同 §2.2.2① 那条坑）。
好处是**字段列表仍然全部静态内联**，不引入 `fields` 变量 + `append(...)...` 那个盲区。

### 5.2 验收结果

- `go build ./...` ✅
- `play` / `play/reconciler` / `snapshot` / `handler` **全绿** ✅
- `controllers`：`TestLoggingGBHTTPContextWiring` 仍红，**findings 与 2.2 时逐字一致**
  （`device_delete.go:257/328`、`cascade/controller/management.go:166`）——`play.go` **零新增**，既有红灯
- `gofmt -l` 全部改动文件 ✅

### 5.3 逐条核对时新翻出来的两个事件（**这就是建目录的价值**）

用 `scan-logging.py --filter play --limit 0` 把 play 链路 13 条"无定位"全部列出来逐条判定，
发现契约清单里**漏了两个**：`controllers/play.go:104` 的 `play.attempt_begin_failed` 与
`:119` 的 `play.attempt_finish_failed`。两者只带 `error`，而 `deviceID`/`channelID` 就在同一函数作用域里
（`:80-81` 已取出）→ 已补，零成本。

### 5.4 指标变化（`scan-logging.py`，改前 = 2.2 完成后的状态）

| 指标 | 改前 | 改后 |
|---|---|---|
| 无任何定位字段 | 172 / 337 = 51% | **167 / 337 = 49%**（-5） |
| Warn 占比 | 143 / 337 = 42% | **141 / 337 = 41%**（-2：`probe_failed` 降级） |
| 唯一字段名 | 173 | **167**（-6：reconciler 4 个 + snapshot 2 个 camelCase 消失） |
| 命名风格「驼峰+大写ID」 | 23 | **5**（-18） |
| snake_case 字段出现次数 | 292 | **296**（+4：`sample_stream_id` 等） |
| `play` 桶无定位 | 10 / 20 = 50% | **8 / 20 = 40%**（-2：`attempt_*` 补字段） |

**⚠️ `play` 桶剩下的 8 条，一条都不该补 —— 这就是它的下限：**

| 条数 | 类型 | 处置 |
|---|---|---|
| 7 | 组件级事件（`disabled`/`duplicate_start`/`panic`/`stopped`/`overlap_skipped`/`list_failed`/`interrupted`） | **豁免**（§3.6.3 → [event-catalog](./event-catalog.md) §7.3） |
| 1 | `round_completed`（轮次级：计数 + 样本流） | 不该塞 `device_id`（塞了也不知道是哪台设备） |

**结论**：`play` 链路的"无定位字段"已在**设备级事件层面归零**——
剩下的 8 条全部有白纸黑字的豁免依据。
另外 `gb28181.play.stop_requested`（记在 `gb28181` 桶里）是**静态盲区**：
脚本读不出 `stopLogFields()` 组装的字段，运行时确实带 `device_id`/`channel_id`（见 2.2.5，由单测锁定）。
**不要为了让这个数字变成 0 去给组件级事件硬塞 `device_id`**（判据②）。

### 5.5 顺带增强：`scan-logging.py` 支持逐模块核对

新增 `--filter <子串>`（过滤明细）与 `--limit <n>`（0 = 全量）。C04 的验收动作：

```bash
cd server && python3 ../docs/logging-governance/scan-logging.py --filter play --limit 0
```

**为什么必须能看全量明细**：只打印"前 40 条"时，play 链路的明细被排在后面，
`attempt_*` 那两个事件就是这么被漏掉的（见 5.3）。**逐条判定才是治理，看汇总数字不是。**

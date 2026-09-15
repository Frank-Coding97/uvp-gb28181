# C07 · 媒体节点（`zlm`）定位契约

- **状态**：✅ 2026-09-15 完成（门禁保持 0 findings；豁免清单 92 → 77）
- **范围**：`zlm.*` 事件命名空间全部调用点（32 条）——`app/gb28181/zlm/**` 运行期日志，
  加上 `app/gb28181/bootstrap.go` / `bootstrap_zlm_management.go` 里语义属于媒体节点的
  装配与降级日志。
- **口径**：静态扫描（`scan-logging.py --dump-ns zlm`）+ 逐调用点人工判定；
  门禁（`internal/loggingcontract`）作为"能不能被验证"的判据。
- 判据来源：[`README.md`](../README.md) 判据三问；维度定义见
  [`event-catalog.md`](./event-catalog.md) §二。

---

## 一、这条链路要回答什么

ZLM（ZLMediaKit）是媒体面：流在哪台节点、就绪没有、节点还活着没有、调度决策落没落库。
网络断了、节点重启了、DB 慢了，都表现为"点播没图"。出事时运维要能回答：

| 问题 | 需要的字段 |
|---|---|
| 是**哪台**媒体节点？ | `node_id`（`meta_node` 主键）+ `name` / `endpoint`（人读） |
| 是**哪一路**流？ | `stream_id` |
| 节点活着吗？ | `zlm.node.offline` / `zlm.thread_load.*` / `zlm.probe.*` |
| 调度决策落库了吗？ | `zlm.scheduler.persist_failed`（`node_id` + `algorithm`） |
| 启动期为什么没起来？ | `zlm.registry.*` / `zlm.scheduler*.`* 系列降级事件 |

**这条链路最大的特点是"两类定位对象混在一个命名空间里"**：运行期事件的定位对象是
**节点**（`node_id` 必须有），而装配期事件发生时**节点注册表可能还不存在** ——
那时强行塞 `node_id` 就是判据①说的占位值。契约把这两类分开登记（§三 / §四）。

---

## 二、五个问题与裁决

### 2.1 `zlm.media_online.*` 三条漏了 `node_id`（值白丢了）

`Client` 持有 `node *node.Node`（`client.go:26`），`c.node.ID` 就是节点主键。
但这三条日志只打了 `endpoint`（`http://host:port/index/api`）：

```go
logger.Warn("IsMediaOnline 请求失败",
    zap.String("event", "zlm.media_online.request_failed"),
    zap.String("endpoint", c.baseURL),   // ← 只有地址，没有节点主键
    ...
```

**为什么这算欠账**：同模块其他事件（`zlm.probe.failed` / `zlm.node.offline` /
`zlm.scheduler.persist_failed`）全都带 `node_id`，唯独这三条不带 ——
**同一模块内不一致**，导致"按节点算 API 失败率"这类聚合做不到（`endpoint` 是字符串地址，
做不了维度键）。处置：补 `zap.Int64("node_id", c.node.ID)`。

同时加了 `c.node == nil` 守卫（`errors.New("zlm: client is not bound to a node")`）：
`NewClientForNode` 是唯一构造器、正常路径必非空，守卫只防测试里手工构造的 `&Client{}`
（`recording_client_test.go:88` 等 5 处），避免取值 panic。

### 2.2 `setupZLMRegistry` 的四条降级日志**没有 `event`**

| 调用点 | 原来的样子 | 后果 |
|---|---|---|
| `DB 不可用,跳过 ZLM Registry 装配` | 裸 `Warn("…")` | 按命名空间统计**看不见它** |
| `Registry LoadAll 失败,可能 meta_node 表未建` | 只有 `zap.Error` | 同上 |
| `默认节点 seed 失败` | 只有 `zap.Error` | 同上 |
| `Registry 已装配` | 只有 `nodes` | 同上 |

这四条是**启动期最关键的降级信号**：DB 不可用会让整个媒体面退到 deprecated 单节点路径。
没有 `event`，它既进不了任何命名空间指标，也无法被告警规则匹配。处置：补
`zlm.registry.db_unavailable` / `zlm.registry.load_failed` / `zlm.node.default_seed_failed` /
`zlm.registry.ready`。

顺带修掉一个**丢弃返回值**：`_, err := reg.Add(...)` 把刚建出来的节点丢了 →
`zlm.node.default_seeded` 因此一直打不出 `node_id`。改成 `seeded, err := ...` 后补上。

### 2.3 装配链其余 Warn/Error 同样缺 `event`（7 条）

`scheduler` / `scheduler_log` / `controller` 三条装配链上的降级分支（Registry 未装配、
`switch` 失败、`assemble` 失败、DB 不可用、prune 失败、跳过 Controller）全部是裸消息。
补 `zlm.scheduler.*` / `zlm.scheduler_log.*` / `zlm.controller.*` 事件名（见 §三 表 3.6）。

**同时修掉 camelCase**（属于 zlm 的共 4 处，全仓 62 → 58）：

| 位置 | 改前 | 改后 |
|---|---|---|
| 节点配置收敛两条（`bootstrap.go`） | `nodeId` | `node_id` |
| 心跳启动日志（`bootstrap.go:447`） | `checkInterval` / `offlineThreshold` | `check_interval` / `offline_threshold` |

### 2.4 豁免清单会校验"恰好匹配 1 次" —— 补了 `event` 必须同步删豁免条目

`reviewed_legacy.json` 里的条目按 `(file, function, method, message, count)` 匹配，
**并且要求恰好命中 1 个调用点**。给这些日志补上 `event` 后，门禁的 `missing_event`
规则不再触发 → 条目匹配 0 次 → 门禁报：

```
legacy_exception_mismatch: legacy exception matched 0 call(s), want exactly 1
```

这不是 bug，是**豁免清单的自我校验在正常工作**（"这条已经不违规了，把豁免删掉"）。
本次共清掉 15 条（`setupZLMRegistry` 4 条 + 后续 11 条），**92 → 77，只减不增**。

⚠️ **顺序要求**：改代码（补 `event`）→ 跑门禁看失配清单 → 删对应条目 → 复跑门禁。
反过来（先删条目）会让门禁在中途变红，且失配信息丢失。

### 2.5 `repeatFailure` 的事件名来自变量 —— 静态不可见的第三族写法

`heartbeat/watcher.go:28`：

```go
app.Log(ctx).Named(key.Component).Warn("Background operation failed",
    zap.String("event", key.Event),      // ← 值来自 logging.RepeatKey 结构体字段
    zap.Int64("node_id", key.NodeID), ...)
```

它承载 3 个事件：`zlm.node.offline_persist_failed`（`watcher.go`）、
`zlm.thread_load.net_failed` / `zlm.thread_load.work_failed`（`thread_load_poller.go`）。
**运行时输出正确、字段齐全**（有 `node_id`），但：

- `scan-logging.py` 读不出 `event` 值 → 这三个事件从不计入 `zlm` 桶；
- 门禁对它的处理是**显式豁免**：`reviewed_adapters.json` 里
  `app/gb28181/zlm/heartbeat/watcher.go` 带 **SHA-256 源码封条** + `kind: "dynamic_event"`。

**处置：不改**。理由：① 这是 T11 已经审核过的受控设计（`RepeatKey` 的事件名来自
`watcher.go` / `thread_load_poller.go` 两处调用点，且 `RepeatKey.Component` 同时决定
logger 名）；② 封条机制"改文件即失效"是有意为之；③ 运行时行为已被
`heartbeat/logging_repeat_test.go` 断言（含 `zlm.node.offline_persist_failed` 的
`recovered` / `summary` / `window` 三种形态）。

**登记在这里，是为了让下一个复核的人不去"顺手把 watcher.go 改了"** —— 那会让门禁
因为封条失效而变红，且属于绕过审核而非治理。

---

## 三、逐事件契约（32 条）

`fields` 列为**当前实际字段**（`--dump-ns zlm` 可复跑核对）。

### 3.1 `zlm.media_online.*` —— 流就绪探测（`zlm/client.go`）

| event | 等级 | 触发条件 | 定位字段 |
|---|---|---|---|
| `zlm.media_online.request_failed` | WARN | ZLM API 调用失败 | `node_id` + `endpoint` + `stream_id` + `app` + `error` |
| `zlm.media_online.not_ready` | DEBUG | `code != 0`（流不存在 / 未就绪） | `node_id` + `endpoint` + `stream_id` + `app` + `code` |
| `zlm.media_online.ready` | DEBUG | 探测成功 | `node_id` + `endpoint` + `stream_id` + `app` + `online` |

⚠️ `not_ready` / `ready` 必须留在 DEBUG：点播就绪轮询会连发上百次，
`logging_client_test.go` 有专项断言（`TestLoggingBackgroundEventsZLMNotReadyDoesNotFloodInfo`）。

### 3.2 心跳与线程负载（`zlm/heartbeat/`）

| event | 等级 | 触发条件 | 定位字段 |
|---|---|---|---|
| `zlm.node.offline` | INFO | 心跳间隔超阈值，节点标记离线 | `node_id` + `name` + `uuid` + `heartbeat_gap` |
| `zlm.node.offline_persist_failed` | WARN | 离线状态落库失败（重试） | `node_id`（**动态事件名**，见 §2.5） |
| `zlm.thread_load.net_failed` | WARN | 拉 NetThread 负载失败 | `node_id`（**动态事件名**） |
| `zlm.thread_load.work_failed` | WARN | 拉 WorkThread 负载失败 | `node_id`（**动态事件名**） |

### 3.3 `zlm.probe.*` —— 启动探活（`zlm/probe/probe.go`）

| event | 等级 | 触发条件 | 定位字段 |
|---|---|---|---|
| `zlm.probe.maintenance_skipped` | INFO | maintenance 节点跳过探活 | `node_id` + `name` |
| `zlm.probe.failed` | WARN | 探活失败 | `node_id` + `name` + `endpoint` + `duration_ms` + `state_before` + `error` |
| `zlm.probe.activation_failed` | WARN | 探活通过但 `MarkActive` 落库失败 | `node_id` + `name` + `error` |
| `zlm.probe.node_active` | INFO | 状态由 offline 翻到 active | `node_id` + `name` + `endpoint` + `duration_ms` |
| `zlm.probe.passed` | INFO | 探活通过（非翻转） | `node_id` + `name` + `endpoint` + `duration_ms` + `state_before` |

### 3.4 `zlm.scheduler.*` —— 调度决策日志写入（`zlm/scheduler/log.go`）

| event | 等级 | 触发条件 | 定位字段 |
|---|---|---|---|
| `zlm.scheduler.persist_failed` | WARN | 决策日志落库失败 | `node_id` + `algorithm` + `error` |
| `zlm.scheduler.buffer_full` | WARN | 进程内队列满而丢弃（每 100 条采样一行） | `dropped_count`（**组件级，见 §四**） |

### 3.5 `zlm.node.*` —— 节点配置（`zlm/service/node_service.go`）

| event | 等级 | 触发条件 | 定位字段 |
|---|---|---|---|
| `zlm.node.restore_failed` | WARN | 节点配置恢复失败 | `node_id` + `error` |
| `zlm.node.restored` | INFO | 节点配置恢复完成 | `node_id` |

### 3.6 启动期装配与降级（`bootstrap.go` / `bootstrap_zlm_management.go`）

| event | 等级 | 触发条件 | 定位字段 |
|---|---|---|---|
| `zlm.node.config_converge_failed` | WARN | 启动收敛某个节点失败 | `node_id` + `name` + `error` |
| `zlm.node.config_converged` | INFO | 启动收敛某个节点完成 | `node_id` + `name` |
| `zlm.client.unavailable` | WARN | Registry 空 → 不下发 Hook 配置 | 组件级（§四） |
| `zlm.registry.db_unavailable` | WARN | DB 不可用 → 走 deprecated 单节点路径 | 进程级（§四） |
| `zlm.registry.load_failed` | WARN | `meta_node` 表读失败 | 进程级（§四） |
| `zlm.registry.ready` | INFO | 注册表装配完成 | `nodes`（§四） |
| `zlm.node.default_seed_failed` | WARN | yaml 默认节点 seed 失败 | 进程级（§四） |
| `zlm.node.default_seeded` | INFO | yaml 默认节点 seed 成功 | `node_id` + `uuid` + `endpoint` |
| `zlm.scheduler.registry_unavailable` | WARN | Registry 未装配 → 跳过 Scheduler | 组件级（§四） |
| `zlm.scheduler.setting_read_failed` | WARN | `scheduler_setting` 读失败 → fallback | 组件级（§四） |
| `zlm.scheduler.switch_failed` | WARN | 算法切换失败 → fallback roundrobin | `requested`（§四） |
| `zlm.scheduler.assemble_failed` | ERROR | roundrobin fallback 也失败，Manager 留空 | 组件级（§四） |
| `zlm.scheduler_log.skipped` | WARN | Scheduler 未装配 → 跳过调度日志服务 | 组件级（§四） |
| `zlm.scheduler_log.db_unavailable` | WARN | DB 不可用 → 跳过调度日志服务 | 组件级（§四） |
| `zlm.scheduler_log.prune_failed` | WARN | 调度日志清理失败 | 进程级（§四） |
| `zlm.controller.scheduler_skipped` | WARN | Scheduler 未装配 → 跳过 Controller | 组件级（§四） |
| `zlm.metrics.sample_failed` | WARN | OverviewSampler 整轮采样失败 | 组件级（§四） |

---

## 四、豁免：17 条不带定位字段（登记，不是漏了）

判定依据：**事件发生的那一刻，要定位的对象不存在**。给它们塞 `node_id` 会让扫描器
判成"已达标"，把真问题盖住。

| event | 为什么不补 `node_id` |
|---|---|
| `zlm.registry.db_unavailable` | DB 不可用 → 连节点表都读不到 |
| `zlm.registry.load_failed` | LoadAll 失败 → 注册表是空的 |
| `zlm.node.default_seed_failed` | seed 失败 → 节点没建出来（该事件本身就是"没有节点"的产物） |
| `zlm.registry.ready` | 装配汇总，`nodes` 已表达规模 |
| `zlm.client.unavailable` / `zlm.scheduler.registry_unavailable` / `zlm.scheduler_log.skipped` / `zlm.controller.scheduler_skipped` | 都是"Registry / Scheduler 不存在所以跳过"，**没有可归属的节点** |
| `zlm.scheduler.setting_read_failed` / `switch_failed` / `assemble_failed` | 算法装配是**全节点共享**的配置，不是某节点的问题 |
| `zlm.scheduler_log.db_unavailable` / `prune_failed` | 调度日志服务是进程级组件；prune 是定时清理，无节点归属 |
| `zlm.metrics.sample_failed` | `OverviewSampler.sampleOnceLocked` 调 `source.GetOverview(ctx)` 拿**整轮结果**，失败粒度就是整轮，没有单节点错误可挂 |
| `zlm.scheduler.buffer_full` | 见下（最容易"顺手补错"的一条） |
| `zlm.probe.empty` | 注册表为空，启动探活跳过 |
| `zlm.probe.completed` | 轮次汇总（`total`/`pass`/`fail`/`skipped`），单节点结果已各有一行 |

### `zlm.scheduler.buffer_full` 为什么**不能**补 `node_id`（重点）

丢弃动作发生在 `LogService.Emit` 的 `select` 里 —— 那时丢弃的 `entry` **确实**带 `NodeID`
（`SchedulerLog` 有该字段），所以"值就在手上"看起来理所当然。但：

- 丢弃是**队列级**的：buffer 满时，所有节点的 entry 一起被丢；
- 日志**每 100 条才采样一行**（`dropped%100 == 1`）；
- 那一行里出现的 `node_id` 只是"碰巧排在第 1 条"的那个节点。

打出来会把"**所有节点**的调度日志都在丢"谎报成"**这个节点**在丢" ——
从"信息不完整"降级成"信息错误"。判据②的答案是"调大队列 / 查 DB 写入是否卡住"，
**动作在组件上，不在节点上**。

这条判定已用契约测试锁住：`zlm/scheduler/logging_scheduler_test.go`
（`TestSchedulerLoggingBufferFullStaysComponentLevel`，断言 `NotContains(node_id)`）。

---

## 五、改动与验收

### 5.1 改动清单

| 文件 | 改动 |
|---|---|
| `zlm/client.go` | `IsMediaOnline` 三条日志补 `node_id`；加 `c.node == nil` 守卫 |
| `bootstrap.go` | `setupZLMRegistry` 4 条补 `event` + `reg.Add` 返回值不再丢弃 → `default_seeded` 补 `node_id`；节点配置收敛两条补 `event` 并把 `nodeId` → `node_id`；`scheduler` / `scheduler_log` / `controller` 装配链 7 条降级日志补 `event`；心跳启动日志的 `checkInterval`/`offlineThreshold` → snake_case |
| `internal/loggingcontract/reviewed_legacy.json` | 删除已不再需要的豁免条目 **15 条（92 → 77）** |
| `zlm/client_test.go` | `newMockClient` 的节点显式给 `ID`（`mockZLMNodeID`），让 `node_id` 可断言 |
| `zlm/logging_client_test.go` | 三条 `media_online` 事件加 `node_id` 断言 |
| `zlm/scheduler/logging_scheduler_test.go`（新增） | 2 条契约测试（见 §5.3） |

**未改**：`heartbeat/watcher.go`（封条，见 §2.5）、`zlm` 事件的等级（等级校准属 C03）、
`app` / `endpoint` 字段名（全仓一致性 + 脱敏规则，见 §六）。

### 5.2 验收命令与结果

```bash
cd server
# L1 门禁：必须 0 findings
go test -count=1 -run TestLoggingPolicyRepository ./internal/loggingcontract/     # ok, 4.2s
# L2 构建 + 包测试
go build ./...
go test -count=1 ./app/gb28181/zlm/...                                            # 全绿
go test -count=1 ./app/gb28181/                                                    # 唯一红灯为既有 T14 两条
# L2 扫描
python3 ../docs/logging-governance/scan-logging.py --root . --dump-ns zlm
```

**实测结果（2026-09-15）**

| 指标 | C06 后 | C07 后 |
|---|---|---|
| 门禁 findings | 0 | **0** |
| `reviewed_legacy.json` 条目 | 92 | **77** |
| `camelCase` 字段（次数） | 62 | **58** |
| 唯一 event | 272 | **287** |
| zlm 命名空间调用点 | 17 | **32** |
| zlm 无定位 | 4 | **17**（全部为 §四 豁免） |

⚠️ **`zlm` 桶"变难看"是口径变化，不是退步**：原来 `Warn("msg")` 这类**没有任何
`zap.Field` 参数**的调用根本不被脚本计为"日志调用点"（`calls_in` 要求段内有 `zap.`）。
补上 `event` 之后它们第一次进入统计 —— **脚本看见了原本就存在的东西**（与 §4.7
"事件名常量化导致指标假跌"是同一族现象，方向相反）。同一批改动也让全局调用点
343 → 349、无定位 146 → 152。

### 5.3 契约测试

| 测试 | 锁住什么 |
|---|---|
| `zlm/logging_client_test.go`（加强） | 三条 `media_online` 事件带 `node_id`（值 = 节点主键）+ `request_failed` 的错误不落原文 |
| `zlm/scheduler/logging_scheduler_test.go`（新增） | `persist_failed` 带 `node_id` + `algorithm` + 结构化 `error`；`buffer_full` **不带** `node_id` |
| `zlm/probe/logging_background_events_test.go`（既有） | `probe.failed` / `probe.node_active` 的 `node_id` + 凭据不落日志 |
| `heartbeat/logging_repeat_test.go`（既有） | 三个动态事件名的运行时取值 |
| 门禁 | 16 条新增 `event` 的"必须有 event"由 `missing_event` 规则持续锁定 |

---

## 六、判定与复核边界（复核时不要做的事）

1. **别给 §四 的 17 条补 `node_id`** —— 逐条理由见 §四；`buffer_full` 那条有测试锁着。
2. **别给装配期日志补 `node_id`** —— `registry.*` / `scheduler*.`* 系列失败时节点表
   读不到或根本没建。
3. **别改 `endpoint` 字段名** —— 它命中 `sanitize.go` 的 URL 规则（洗掉 `userinfo` 里的
   密码），且 `entrypoint_test.go` 有门禁测试依赖。`zlm.media_online.*` 的
   `endpoint` 与 `zlm.probe.*` 的 `endpoint` 是同一个受控字段。
4. **别改 `app` 字段名** —— 全仓 5 处（`hook.go` 2、`client.go` 3）写法一致，
   改名是破坏性改动且没有收益（"能用新增解决的，不要改名"）。
5. **别动 `heartbeat/watcher.go`** —— `reviewed_adapters.json` 有 SHA-256 封条（§2.5）。
6. **别把 `zlm.probe.not_ready` 类轮询日志升到 INFO** —— 见 §3.1 的刷屏断言。
7. **补了 `event` 之后必须同步删 `reviewed_legacy.json` 对应条目**，否则门禁报
   `legacy_exception_mismatch`（§2.4）。

---

## 七、移交给 C08 的清单

`bootstrap.go` 里还有一批**语义属于 ZLM、但至今没有 `event`** 的 **INFO 级"已装配 / 已启动"**
日志（`ZLM 节点/配置 controller 已装配`、`心跳 Collector / Watcher 已启动`、
`线程负载 Poller 已启动`、`ZLM Scheduler 已装配`、`调度日志服务已启动`、
`Scheduler Controller 已装配`），共 **6 条**（另有一条 INFO 级
`GB28181 scheduler_setting 表空,fallback roundrobin` 是"新装没配过"的正常初始状态，同样移交）。

**本次刻意不动**，原因：它们判据②答不出动作（"装配成功"本身不需要人做什么），
属于**价值判定**而非定位问题 —— 与 C03（等级校准）和 C08（命名空间归属 / `其他` 桶）
是同一议题。在 C07 里单独给它们补 `event` 会变成"给噪声发身份证"。
移交清单见 [`content-plan.md`](../content-plan.md) C08。

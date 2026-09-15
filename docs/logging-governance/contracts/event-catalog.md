# 事件目录 · Event Registry（C01 种子 · 当前只覆盖 `play` 链路）

- **状态**：🔄 `play` 链路已登记（45 类事件）；其余 200+ 条见 C01.3/C01.4
- **`cascade` 链路（17 类）已在 [`cascade.md`](./cascade.md) §三 按**同样的四格**登记**；
  **`gb28181` 核心链路（register / catalog / deviceinfo / hook，约 30 类）已在
  [`gb28181-core.md`](./gb28181-core.md) §三 按同样的四格登记**
  （2026-09-15，C05 的产出）。C01.2 把目录迁成 Go 包时，两处一起合并 —— 现在不重复维护。
- **为什么先做这一小块**：C04 §3.6.3 要求把 `reconciler` 的 7 条**组件级事件**登记为
  「豁免设备定位字段」——**没有目录，豁免就无处可依**，门禁只能一刀切要求 `device_id`，
  结果就是往 `play.reconcile.stopped` 上硬塞一个 `device_id`（那不是治理，是为了指标好看而打印）。
- **最终形态**：C01.1 会把它迁成 Go 包（`app/utils/logging/logevent/`）供门禁读取；
  本文件先承载**语义**，是迁移的输入。
- ⚠️ **C09（2026-09-15）后，机器可读的那半已经有了**：`server/internal/loggingcatalog/registry.json`
  登记了**全部 330 个事件**的**名字**与**定位字段并集**（`contracts/registry.md`）。
  两者关系是**互补而不是重复**：
  - 登记册 = 门禁读的**基线**（只保证"已有的定位信息不退回"，**不判断它对不对**）；
  - 本文件 = 四格表（`event` / 定位维度 / 必需字段 / 等级+触发条件），是**判断依据**。

  所以「本文件没登记某个事件」**不代表**门禁放过了它 —— 门禁要求它在登记册里；
  反过来，「登记册里有它」**也不代表**有人裁决过它的四格。C01 的活是把后者补完，
  届时登记册的 `events` 段会长出 `requires` 字段，`locating_free_exceptions`
  那套"事件级豁免"会被**声明的维度**取代。

---

## 一、登记格式（四格）

| 列 | 含义 |
|---|---|
| `event` | 事件名（点分命名空间，snake 段） |
| 定位维度 | 出问题要定位**谁** |
| 必需字段 | 该维度下必备的定位字段 |
| 等级 + 触发条件 | 什么级别、什么时候打**一次** |

**填不出「等级 + 触发条件」的事件，说明它本来就不该存在**——填表过程本身就是一次清理。

---

## 二、定位维度（本目录的关键概念）

| 维度 | 定位对象 | 必需字段 | 典型场景 |
|---|---|---|---|
| `device` | 一台设备 / 一条通道 | `device_id` `channel_id` | 点播主流程、停播、快照 |
| `stream` | 一路流 | `stream_id`（能拿到时一并带 `device_id`/`channel_id`） | 停播、流复用、录像收尾 |
| `node` | 一个媒体节点 | `node_id`（+ `stream_id`） | 流复用探测、reconciler 探测 |
| `component` | 组件自身的状态 | 组件名（`component` 字段）+ 状态字段；**豁免设备/通道字段** | reconciler 生命周期 |

**判定规则**：

1. 设备级事件**必须**带 `device_id` + `channel_id`；
2. 流级事件**必须**带 `stream_id`，若调用点手上已有设备/通道则一并带（`Stop` 的 A′ 就是这条的落地）；
3. 节点级事件**必须**带 `node_id`；
4. **组件级事件只要求能定位到组件 + 状态** —— 给它塞 `device_id` 是错的。

---

## 三、`play` 点播链路（10）

### 3.1 事务主流程（8）

`server/app/gb28181/play/service.go`，维度 `device`，同时带 `stage` / `outcome`（前端派生字段）。

| event | 等级 | 触发条件 | 定位字段 |
|---|---|---|---|
| `gb28181.play.requested` | INFO | 收到点播请求、进了事务 | `device_id` `channel_id` |
| `gb28181.play.validation_succeeded` | **DEBUG**（2026-09-14 由 INFO 降级） | 设备在线 + 通道存在 + 非 TCP-Active | 同上 |
| `gb28181.play.node_selected` | INFO | 调度器选定媒体节点 | + `node_id` `stream_id` |
| `gb28181.play.rtp_allocated` | INFO | ZLM 打开 RTP 接收端口成功 | 同上 |
| `gb28181.play.invite_accepted` | INFO | 设备回 200 OK | + `correlation_id` `call_id` `cseq` |
| `gb28181.play.media_ready` | INFO | 首帧到位，**成功终点** | 同上 |
| `gb28181.play.completed` | INFO | 事务正常收尾 | + `stream_id` `reused` |
| `gb28181.play.failed` | WARN | 事务失败，**失败终点** | 同上 + `reason_code` |

> `validation_succeeded` 降级依据：它紧跟在 `requested` 之后，只说明"请求参数合法"，
> 运维看到它**无动作可做**（C04 判据②）。校验失败会走 `failed`，信息不会丢。

---

### 3.2 HTTP 接入层（2）

`server/app/gb28181/controllers/play.go`，维度 `device`。**2026-09-14 补 `device_id`/`channel_id`**：
两个事件原本只带 `error`，而 `deviceID`/`channelID` 就在同一个函数作用域里（`:80-81` 已取出），补上零成本。

| event | 等级 | 触发条件 | 定位字段 |
|---|---|---|---|
| `play.attempt_begin_failed` | WARN | 点播 attempt 登记失败（幂等账丢失，主流程继续） | `device_id` `channel_id` |
| `play.attempt_finish_failed` | WARN | 回写 attempt 结果失败 | 同上 |

> 这两个事件此前**不在契约的清单里**——是 2026-09-14 用 `scan-logging.py --filter play` 逐条核对时
> 才被翻出来的。**这就是建目录的价值**：没有逐条核对，它们会一直躲在"play 桶 50%"里不被发现。

---

## 四、停播（3）· 维度 `stream`

| event | 等级 | 触发条件 | 定位字段 |
|---|---|---|---|
| `gb28181.play.stop_requested` | INFO | 停播事务开始 | `stream_id` `device_id` `channel_id` |
| `gb28181.play.stop_completed` | INFO | BYE + 关 RTP + Unbind 全部完成 | 同上 |
| `gb28181.play.stop_failed` | WARN | 清理过程出错（**失败更需要定位**） | 同上 |

> 这三个事件**只由 `play.Service.Stop` 自身发出**；`StopIfPersistedCurrent` 与协调器的
> `stopCurrentResult` 都不打。设备/通道的取得顺序见 `play.md` §2.2.3（A′：调用方传值 →
> 内存会话 → 固定流解析 → 已有的通道行）。

---

## 五、流复用与录像收尾（9）

| event | 维度 | 等级 | 触发条件 | 定位字段 |
|---|---|---|---|---|
| `gb28181.play.reuse_probe_failed` | stream | WARN | **单节点路径**探测失败（保守视为流不在） | `stream_id` |
| `gb28181.play.reuse_bound_node_offline` | node | INFO | 绑定节点上探测未在线 | `stream_id` `node_id` `online` |
| `gb28181.play.reuse_bound_node_missing` | node | WARN | LocationMap 记的节点已不在 registry | `stream_id` `node_id` |
| `gb28181.play.reuse_fallback_probe` | node | INFO | LocationMap 无 binding，开始跨节点兜底探测 | `stream_id` `active_nodes` |
| `gb28181.play.reuse_fallback_node_failed` | node | DEBUG | 兜底探测中单节点失败，继续下一个 | `stream_id` `node_id` |
| `gb28181.play.reuse_fallback_hit` | node | INFO | 兜底探测命中并回填 LocationMap | `stream_id` `node_id` |
| `gb28181.play.reuse_success` | device | INFO | 复用现有流成功 | `device_id` `channel_id` `stream_id` |
| `gb28181.play.reuse_cleanup` | stream | INFO | 复用判定失败，清理残留后重新 INVITE | `device_id` `channel_id` **`stale_stream_id`** |
| `gb28181.play.recording_end_failed` | stream | WARN | 停流前收尾云端录像失败 | `stream_id` |

> ⚠️ `reuse_cleanup` 用的是 **`stale_stream_id`** 而非 `stream_id` —— 语义确实不同
> （它指"将被清掉的残留流"，不是"正在点播的流"）。**保留这个区分**，已登记进字段字典（C02）。
> 这一组带 `node_id` 是对的：流复用的故障 90% 是"节点没了但 LocationMap 还记着"。

---

## 六、通道快照令牌（3）· 维度 `device`

| event | 等级 | 触发条件 | 定位字段 |
|---|---|---|---|
| `gb28181.play.snapshot_token_unavailable` | WARN | 多节点下行但拿不到签发令牌所需的前置条件 | `device_id` `channel_id` `stream_id` |
| `gb28181.play.snapshot_node_unavailable` | WARN | 解析不到媒体节点的 `MediaServerUUID` | `device_id` `channel_id` `node_id` |
| `gb28181.play.snapshot_token_failed` | WARN | 内部播放令牌签发失败 | `device_id` `channel_id` `stream_id` |

---

## 七、`reconciler`（15）· 含 **7 条组件级豁免**

`server/app/gb28181/play/reconciler/`，组件名 `play.reconcile`。

### 7.1 设备 / 流级（3）

| event | 维度 | 等级 | 触发条件 | 定位字段 |
|---|---|---|---|---|
| `play.reconcile.cleanup_succeeded` | device | INFO | 判定假阳性并清理成功 | `stream_id` `device_id` `channel_id` |
| `play.reconcile.cleanup_failed` | device | ERROR | 清理失败（对账没达成目的） | 同上 |
| `play.reconcile.round_completed` | 轮次 | INFO | 每轮对账结束 | `scanned` `cleaned` `skipped` `failed` `elapsed` + **`sample_stream_id`**（3.6.2 新增） |

> `round_completed` 补 `sample_stream_id`：只报计数和耗时时，看到 `cleaned=5` 却不知道动了哪些。
> **只取样本不打印全量**——`scanned` 可能有数百条，那是为打印而打印。无流可扫时该字段
> **整个不出现**（`zap.Skip()`），而不是打出空串。

### 7.2 节点 / 流级探测（5）

| event | 维度 | 等级 | 触发条件 | 定位字段 |
|---|---|---|---|---|
| `play.reconcile.probe_failed` | node | **DEBUG**（3.6.1 统一，原两处 WARN / 一处 DEBUG） | 单次探测失败（多节点 / 单节点 / 跨节点遍历共 3 处） | `stream_id` `node_id` |
| `play.reconcile.location_fallback` | node | DEBUG | LocationMap 命中但 registry 里没这个节点 | `stream_id` `node_id` |
| `play.reconcile.node_inactive` | node | DEBUG | 绑定节点非 active，跳过等恢复（Q4） | `stream_id` `node_id` `state` |
| `play.reconcile.cross_node_hit` | node | DEBUG | LocationMap miss 但跨节点遍历命中 | `stream_id` `node_id` |
| `play.reconcile.no_nodes` | node | WARN | 一个 active 节点都没有，只能判假阳性 | `stream_id` |

> `probe_failed` 统一为 DEBUG 的依据：单节点探测失败是对账过程的**正常抖动**（节点重启 / 网络瞬断），
> 真出问题会由 `no_nodes` 与 `round_completed.failed` 兜住。原先同名事件跨 WARN/DEBUG
> 会让按事件聚合的统计失真。
> **`probe_failed` 不补 `node_id` 的地方**：单节点路径（`play/service.go` 的
> `reuse_probe_failed`）没有节点维度，见 §十。

### 7.3 **组件级豁免（7）——不补设备/通道字段**

| event | 等级 | 触发条件 | 能定位到什么 | 现有字段 |
|---|---|---|---|---|
| `play.reconcile.disabled` | INFO | `interval <= 0`，对账不启动 | 组件 + 配置 | `interval` |
| `play.reconcile.duplicate_start` | WARN | 重复 `Start()` | 组件状态机 | — |
| `play.reconcile.panic` | ERROR | loop 内 panic | 组件 + 崩溃原因 | `panic_type` `stack` |
| `play.reconcile.stopped` | INFO | 收到停止信号，loop 退出 | 组件生命周期 | — |
| `play.reconcile.overlap_skipped` | DEBUG | 上一轮未跑完，本轮跳过 | 轮次调度 | — |
| `play.reconcile.interrupted` | INFO | 轮次中途收到停止信号 | 组件生命周期 | — |
| `play.reconcile.list_failed` | WARN | 查 DB 失败，本轮跳过 | 组件 + 依赖 | `error` |

> **豁免理由**：这 7 条的定位对象是「reconciler 这个组件」而不是某台设备/某路流。
> `play` 命名空间约 50% "无定位字段"里，**一大半就是这 7 条**——按设备级标准统计它们是**误伤**。
> 判据必须分层：给 `stopped` 硬塞 `device_id` 属于"为了指标好看而打印"。

---

## 八、`snapshot` 模块（3）· 维度 `device`

`server/app/gb28181/snapshot/service.go`（2026-09-14 字段名由 `device`/`channel` 统一为 `device_id`/`channel_id`）。

| event | 等级 | 触发条件 | 定位字段 |
|---|---|---|---|
| `gb28181.snapshot.panic` | ERROR | 抓拍 goroutine panic | `device_id` `channel_id` `panic_type` `stack` |
| `gb28181.snapshot.capture_failed` | WARN | 抓拍失败 | `device_id` `channel_id` |
| `gb28181.snapshot.captured` | INFO | 抓拍成功落盘 | `device_id` `channel_id` `bytes` `url` |

> `url` 是 `relURL`（相对路径，`snapshot/service.go:165` 生成的 `absPath/relURL` 对），
> **不含域名、不含 token，已脱敏**，可保留。

---

## 九、自动点播（2）

`server/app/gb28181/play/auto_start_dispatcher.go`。

| event | 等级 | 触发条件 | 定位字段 |
|---|---|---|---|
| `play.auto_start.succeeded` | INFO | 后台自动点播启动成功 | `device_id` `channel_id` `node_id` `stream_id` `reason` |
| `play.auto_start.failed` | WARN | 后台自动点播启动失败 | `device_id` `channel_id` `node_id` `reason` |

---

## 十、已知盲区与待办（本轮实测发现）

1. **`fields` 变量 + `append(...)...` 静态读不到**（门禁 `unresolved_logger` 与统计脚本同一处盲区）。
   已确认三处：
   - `play/service.go:465-476`（`requested` / `failed` / `completed` 攒在 `fields` 里）——
     运行时字段正确，静态扫描会把 `completed` 判成"无定位"；
   - `play/auto_start_dispatcher.go:283-298`（同上，`succeeded`/`failed`）；
   - `cascade/cascade_invite_handler.go` 的 `log()` helper（C05 的核心项，9 条 finding）。
   **待办**：这批统一改成"字段直接内联在调用点"（或由 C01.6 门禁提供显式声明），
   否则门禁永远看不清它们。**没做的原因**：属行为无关的重构，本轮不动 `service.go` 的
   `fields` 变量（它同时承载 `Play()` 的错误路径逻辑）。
2. **`reuse_cleanup` 的 `stale_stream_id`** 需在 C02 字段字典里登记为独立语义，不能并入 `stream_id`。
3. **组件级豁免需要机器可读**：C01.1 落地时，这 7 条要在事件登记里带 `dimension: component`，
   供 C09.3 的"必需定位字段校验"跳过。
4. **归 C06（`handler/hook.go`）的两条点播相关事件**，本轮**未动**（超出 C04 范围）：
   - `gb28181.hook.play.denied`（`hook.go:1045`）—— 点播鉴权被拒，只有 `reason`。**排障价值高**
     （"为什么点播没起来"的第一嫌疑），应补 `device_id`/`channel_id`/`stream_id`。
   - `gb28181.hook.playback_ended_unmatched`（`hook.go:645`）—— 有 `stream` 字段，
     但 **hook 模块用的是 `stream` 而非 `stream_id`**，统计脚本与门禁都认不出来。
   - ⇒ **`handler/hook.go` 需要一次"字段名对齐 `snake_case`"**（`stream` → `stream_id` 等），
     否则这个模块的日志在跨模块检索时永远接不上。C06 立项时一并做。
5. **`scan-logging.py` 本轮加了 `--filter` / `--limit`**（逐模块核对用）。C04 的验收动作就是
   `--filter play --limit 0` 把 play 链路的无定位点**全部**列出来逐条判定——
   上面 §3.2 的两个事件正是这样翻出来的。

---

## 十一、复核方法（可复跑）

```bash
cd server
# 1. 命名风格：play 链路不应再出现 camelCase / 大写 ID 字段名
grep -rnE 'zap\.(String|Int64|Bool)\("[a-zA-Z]*[A-Z][a-zA-Z]*"' app/gb28181/play/ | grep -v _test.go
# → 期望：空

# 2. 定位字段覆盖：跑治理统计脚本，看 play 命名空间
/Users/menglulu/.workbuddy/binaries/python/versions/3.13.12/bin/python3 \
  ../docs/logging-governance/scan-logging.py --root .
# → 期望：play 桶"无定位"条数下降；剩余项应能被 §7.3 的组件级豁免解释

# 3. 事件名是否都登记在本目录
grep -rhoE '"gb28181\.play\.[a-z_]+"|"play\.[a-z_.]+"' app/gb28181/play/ | sort -u
```

# C06 · `gb28181` 核心链路定位契约

- **状态**：✅ 2026-09-15 完成（**门禁全绿**：2 findings → 0）
- **范围**：`server/app/gb28181/handler/`（`register` / `catalog` / `catalog_trigger` /
  `deviceinfo` / `deviceinfo_trigger` / `message` / `hook` / `hook_auth`）+
  `server/app/gb28181/controllers/device_delete.go`
- **口径**：静态扫描（`scan-logging.py`）+ 逐调用点人工核对；门禁
  （`internal/loggingcontract`）作为"**字段能不能被验证**"的判据。
- 判据来源：[`README.md`](../README.md) 判据三问；维度定义见
  [`event-catalog.md`](./event-catalog.md) §二。

---

## 一、这条链路要回答什么

`handler` 是 GB28181 的**设备侧入口**（与 C05 的级联相反，这里我方是上级）。
它没有 HTTP request id，只有 SIP 事务的三件套：**`device_id` / `call_id` / `cseq`**。

| 问题 | 需要的字段 |
|---|---|
| 是哪**台**设备？ | `device_id`（国标号，不是自增 id） |
| 是**哪一次**事务？ | `call_id` + `cseq`（与 `gb_sip_trace_message` 对上的唯一键） |
| 设备从**哪个地址**来的？ | `peer`（REGISTER）/ `source_ip`（Hook 回调，ZLM 主动打过来，没有 SIP 信封） |
| 哪**条流**？ | `stream_id` |
| 哪个**媒体节点**？ | `node_id` / `media_server_id` |
| 成了没有？没成是为什么？ | `error` / `reason_code` |

**"设备注册成功了，但通道一直不出来"是这条链路最高频的排障场景**，它的排查顺序是：

```
register.succeeded(catalog_triggered?) → catalog.query_sent → catalog.response_processed
                                       ↘ deviceinfo.query_sent → deviceinfo.updated
```

这条链上任何一个环的日志少一个关联键，排查就会断在那一环。本项做的就是把它接通。

---

## 二、六个问题与裁决

### 2.1 `traceFields ...zap.Field` 变参传播（本项核心）

`HandleCatalogResponse` 和 `HandleDeviceInfoResponse` 的签名是：

```go
func HandleCatalogResponse(ctx context.Context, body []byte, traceFields ...zap.Field) {
	logger := app.Log(ctx).Named("gb28181.catalog").With(traceFields...)
```

调用点（`message.go`）传 `zap.String("device_id", …)` 等。**运行时字段是在的**，
但字段名在**日志语句所在的函数里读不到** —— 它藏在调用点。后果有两层：

1. `scan-logging.py` 把这两条链路的所有调用点判成"无定位"（假阳性）；
2. 门禁的"字段可验证性"检查看不到它们 —— 和 C05 的 `fields []zap.Field` 容器是**同一族反模式**。

**裁决：改成显式参数**（`deviceID, callID, cseq string`），字段在每个日志调用点内联写全。

```go
func HandleCatalogResponse(ctx context.Context, body []byte, deviceID, callID, cseq string) {
	logger := app.Log(ctx).Named("gb28181.catalog")
```

调用点随之从 3 个 `zap.Field` 变成 3 个字符串实参。**这是本项改动量最大、收益最直接的一项**：
`catalog` / `deviceinfo` 两条链路共 13 条日志一次性从"静态不可见"变成逐条可验证。

### 2.2 `stream` 与 `stream_id` 并存

`hook.go`（18 处）+ `zlm/client.go`（3 处）用 `zap.String("stream", …)`，
而 `play` / `cascade` 链路用 `stream_id`，扫描器的定位字段基线里也只有 `streamid`。

**裁决**：日志字段名统一为 **`stream_id`**（21 处）。
⚠️ **只改日志字段名** —— ZLM 回调载荷的 JSON 字段名（`json:"stream"`）、
ZLM API 的查询参数都不能动，改了会直接打断回调解析。

### 2.3 hook 拒绝类事件只有 `reason`，缺"哪条流"

```go
// 改前
func (h *HookController) denyPlayback(c *gin.Context, reason, message string) {
	hookLog(c).Info("播放鉴权 Hook 已拒绝",
		zap.String("event", "gb28181.hook.play.denied"), zap.String("reason", reason))
```

**只有 `reason` 的拒绝日志 = "有人被拒了，但不知道是谁"**。而"被拒"这件事在 hook 链路里
恰恰是最需要定位的（ZLM 侧的日志只知道"平台拒绝了"，不知道哪条流）。

**裁决**：
- `reason` → **`reason_code`**（受控短码，与 register / playauth 两侧命名一致）
- 补 `stream_id`，**取不到时字段缺席**（`if streamID == ""` 分支不打该字段，
  而不是打空串 —— 空串会被人和脚本都当成"已带定位字段"）
- kebab → snake：`payload-node-mismatch` → `payload_node_mismatch` 等 16 个短码
- `gb28181.hook.auto_on_demand.accepted` 里 `reason="accepted"` **删除**：
  它和事件名（`.accepted`）说的是同一件事，答不出判据②（看到它要做什么动作），是纯噪声。

### 2.4 `is_first` 不该改名 `catalog_triggered`（**计划原判有误**）

C06 原计划写的是"`is_first` 改名 `catalog_triggered`"。**核实后判定为错**：

```go
catalogTriggered := isFirst && h.catalogTrigger != nil && gbconfig.SyncChannelsOnOnline()
```

两者**不等价**。`is_first` 是"本次注册让设备从离线转在线"（它还决定订阅唤醒、DeviceInfo 查询），
而 Catalog 查询额外受 `SyncChannelsOnOnline()` 开关约束。
**改名会让这条日志在"同步开关关掉"时谎报"已同步"** —— 从"信息不完整"变成"信息错误"，更糟。

**裁决**：保留 `is_first`，**新增** `catalog_triggered` 回答另一个问题
（"通道同步有没有真的被发起"）。`catalog_triggered` 在打日志**之前**算好，
触发块复用它（不是把条件写两遍）。

### 2.5 `device_delete.go` 两条日志没有 `event`（门禁最后两条红）

```go
// 改前
if retired > 0 && app.ZapLog != nil {
	app.ZapLog.Info("删除设备时回收了级联共享投影",
		zap.String("deviceCode", dev.DeviceID), zap.Int64("retiredProjections", retired))
}
```

三个问题叠在一起：**没有 `event`**（门禁 `missing_event`）、**camelCase 字段名**、
用**全局 logger**（挂不到这次 HTTP 请求的链上）。

**裁决**：补 `event`（`gb28181.device.delete_shared_projections_retired` /
`gb28181.channel.delete_shared_projections_retired`）+ snake_case + `app.Log(c.Request.Context())`。
**这两条做完，全仓门禁从 2 → 0，转绿。**

### 2.6 `hook_auth` 字段名不规范 + `catalog_trigger.go` 事件名缺前缀

- `hook_auth.go`：`node` → `node_id`、`payloadNode` → `payload_node_id`、
  `sourceIp` → `source_ip`、`reason` → `reason_code`。
  其中 `source_ip` 是**真实定位字段**（被拒绝时对方还没通过认证，`node_id` 是它自称的、
  不可信，只有来源地址是事实），已登记进扫描器基线。
- `catalog_trigger.go` 的 3 个事件是裸 `catalog.query_*`，而**对称文件**
  `deviceinfo_trigger.go` 是 `gb28181.deviceinfo.query_*` —— 同一模块两个镜像文件命名不一致，
  裸前缀还会让这几个 event 掉进扫描器的"其他"命名空间（治理时看不见）。
  裁决：统一加 `gb28181.` 前缀。

---

## 三、逐事件契约

### 3.1 `register` —— 设备注册 / 注销（`register.go`）

| event | 等级 | 必需字段 | 触发条件 |
|---|---|---|---|
| `gb28181.register.succeeded` | INFO | `device_id` `call_id` `cseq` **`peer`** **`expires`** `transport` `is_first` **`catalog_triggered`** | 注册鉴权通过且在线态写入成功 |
| `gb28181.register.unregistered` | INFO | `device_id` `call_id` `cseq` **`peer`** | `Expires: 0`（GB28181 的注销表达，不是"立即过期"） |
| `gb28181.register.authentication_succeeded` | INFO | `device_id` `call_id` `cseq` `stage` `outcome` | Digest 校验通过 |
| `gb28181.register.digest_failed` | WARN | `device_id` `call_id` `cseq` | 摘要校验失败，回 401 challenge |
| `gb28181.register.endpoint_trust_failed` | WARN | `device_id` `call_id` `cseq` + `error` | 安全端点信任写入失败（不阻断注册） |
| `gb28181.register.state_update_failed` | ERROR | `device_id` `call_id` `cseq` + `error` | `handleRegister` 返回错误 |
| `gb28181.register.unregister_failed` | ERROR | `device_id` `call_id` `cseq` + `error` | `handleUnregister` 返回错误 |

**`peer` / `expires` 为什么值得打**：设备在 NAT 后面时 `peer` 是平台侧唯一知道的回连地址
（"注册成功但平台发不出消息"第一个要核对的值）；`expires` 决定"这台设备多久后判离线"，
排"设备反复上下线"时它是第一个要看的数 —— 两个值**都已经被解析过却没打**。

### 3.2 `catalog` —— 目录查询与应答（`catalog.go` / `catalog_trigger.go`）

**查询侧**（`catalog_trigger.go`，异步 goroutine）：

| event | 等级 | 必需字段 |
|---|---|---|
| `gb28181.catalog.query_sent` | INFO | `device_id` `transport` `sn` |
| `gb28181.catalog.query_build_failed` | WARN | `device_id` + `error` |
| `gb28181.catalog.query_send_failed` | WARN | `device_id` `destination` `transport` + `error` |

**应答侧**（`catalog.go`）：

| event | 等级 | 必需字段 |
|---|---|---|
| `gb28181.catalog.response_processed` | INFO | `device_id` `call_id` `cseq` `sn` `item_count` `received_count` `total_count` `complete` |
| `gb28181.catalog.response_parse_failed` | WARN | `device_id` `call_id` `cseq` `reason_code` + `error` |
| `gb28181.catalog.ingest_failed` | ERROR | `device_id` `call_id` `cseq` + `error` |
| `gb28181.catalog.pipeline_unavailable` | DEBUG | `device_id` `call_id` `cseq` |

**关键裁决**：`response_parse_failed` **必须带信封上的 `device_id`**。
"报文解析失败 ≠ 不知道是谁发的" —— MESSAGE 信封（From / Call-ID / CSeq）在进入
`HandleCatalogResponse` 之前就解开过了。否则一台设备反复发坏应答时，日志里只有
一串"解析失败"，没有任何线索指向那台设备。

**为什么用 `errors.Join`**：`TransactionResult` 的 `TransportErr` 与 `BuildErr` 互斥
（`SendMessage` 在 build 失败时直接返回），但原代码写的是**两个 `zap.Error(...)`** ——
会输出两个同名 `"error"` 键。合成一个字段，语义不变、JSON 合法。

### 3.3 `deviceinfo` —— 设备信息查询与回写（`deviceinfo.go` / `deviceinfo_trigger.go`）

全部沿用 3.2 的三件套（`device_id` `call_id` `cseq`）：

| event | 等级 | 必需字段 |
|---|---|---|
| `gb28181.deviceinfo.updated` | INFO | 三件套 + `updated_field_count` + 4 个 `*_updated` |
| `gb28181.deviceinfo.unchanged` | DEBUG | 三件套 |
| `gb28181.deviceinfo.response_parse_failed` | WARN | 三件套 + `reason_code` + `error` |
| `gb28181.deviceinfo.missing_device_id` | WARN | 三件套（应答体里没有 DeviceID，但信封上有） |
| `gb28181.deviceinfo.device_missing` | WARN | 三件套 |
| `gb28181.deviceinfo.lookup_failed` / `update_failed` | ERROR | 三件套 + `error` |
| `gb28181.deviceinfo.store_unavailable` | DEBUG | 三件套 |

### 3.4 `hook` / `hook_auth` —— ZLM 回调（`hook.go` / `hook_auth.go`）

ZLM 是**主动打过来**的（HTTP hook），没有 SIP 信封、没有 HTTP request id 可继承。

| event | 等级 | 必需字段 |
|---|---|---|
| `gb28181.hook.play.denied` | INFO | `reason_code` + `stream_id`（**取不到则缺席**） |
| `gb28181.hook.flow.ignored` | DEBUG | 同上 |
| `gb28181.hook.auto_on_demand.denied` | DEBUG | 同上 |
| `gb28181.hook.auto_on_demand.accepted` | INFO | `device_id` `channel_id`（`reason="accepted"` 已删） |
| `gb28181.hook.auth.rejected` | WARN | **`node_id`** **`source_ip`** `hook_event` `reason_code`（同一来源 30 分钟窗口内**只打首次**，见下） |
| `gb28181.hook.auth.rejected_summary` | WARN | **`node_id`** **`source_ip`** `hook_event` `reason_code` `suppressed_count` `window_seconds` |
| `gb28181.hook.auth.node_mismatch` | WARN | **`node_id`** **`payload_node_id`** `hook_event` |
| `gb28181.hook.server_started` | INFO | **`media_server_id`** **`node_id`** |
| `gb28181.hook.server_started_node_unresolved` | WARN | `reason_code` + `media_server_id`（**可缺席**） |
| `gb28181.hook.keepalive.process_failed` | WARN | `media_server_id`（可缺席 → `source_ip` 接棒）`body_len` + `error` |
| `gb28181.hook.keepalive.read_failed` | WARN | **`source_ip`** + `error` |
| `gb28181.hook.keepalive.collector_unavailable` | DEBUG | **`source_ip`** |
| `gb28181.hook.flow.collect_failed` | WARN | `stream_id` `media_server_id` `player` + `error` |
| `gb28181.hook.stream.cleanup_failed` | WARN | `stream_id` `reason_code` + `error` |

**`server_started_node_unresolved` 是新事件**：改前 `OnServerStarted` 在解不出节点时
**静默 return**（"节点重启了但重启通知没发下去"，查不到任何痕迹）。现在三种原因分别落到
`reason_code`：`payload_missing_media_server_id` / `resolver_unavailable` / `node_unknown`。
其中 `node_unknown`（ZLM 侧存在但不在注册表里）正是"ZLM 换了实例、平台还认旧的"这类故障的现场。

**`stopCleanupPending` 的 `failureMessage` 改 `reasonCode`**：原来传的是中文句子
（`"流注销清理失败"`）—— 人类可读的"是哪条路径"已由事件名回答，句子只会让字段无法聚合。

**`rejected_summary` 是新事件（2026-09-21）**：hook 的 `node` 参数是**对方自报**的，
平台删掉一个节点之后对端不会知道，于是继续按自己的周期回调。现场原型：220 上一个已从平台移除的
ZLM 实例（`eeyelog-zlm`）仍留着指向平台的 hook 配置，`on_server_keepalive` 每 10s 一条 ——
**每天 8640 行同一条事实**，而 `HookAuthenticator` 原有的令牌桶（8/s、burst 16）只防秒级风暴，
对 0.1/s 的慢性重复完全无效。

处置是**折叠**不是降级，也不是关掉：同一「`reason_code` + 自称 `node_id` + `source_ip` + `hook_event`」
在 30 分钟窗口内只留一条明细（`auth.rejected`，等级**仍是 WARN** —— `levels.md` 把它归在
"身份认证失败、需要人去核对凭据"那一类，这个判断没变），窗口到期补一条
`auth.rejected_summary`，用 `suppressed_count` 交代被压掉的条数。10s 心跳因此从 180 条/窗口
降到 1 条/窗口，而"有个来源在持续被拒"这件事仍然看得见。

⚠️ 同源的 `logging.Repeater`（T11）**没被复用**：它的身份维度是 `NodeID int64` + `JobID`
（平台自己的实体）、恢复靠 `Recovered(key)`，而这边的"谁"是对方自报的字符串 + 对端地址，
且未知节点没有"恢复"信号。形状相近不等于同一个东西 —— 但字段名（`suppressed_count`）
刻意与它保持一致，免得同一个概念长出两种叫法。

**`reason_code=node_retired` 是 2026-09-21 新分出来的一类（L4 对端解约）**：认证 miss 时
先问一次"这个 uuid 是不是平台**自己删过**的节点"（退休凭据表 `meta_node_retired`）。命中就是
`node_retired`，没命中才是 `node_unknown`。两者必须分开：

- `node_retired` —— 平台**认识**它，是自己删掉的，凭据还在 ⇒ 对端残留本该被撤掉，平台会去撤；
- `node_unknown` —— 平台根本不认识它 ⇒ 伪造 / 串台 / 别人家平台配错了地址。

混成一种，"我们删过、还没撤干净"和"陌生人一直在敲门"在日志里就长得一模一样。
两类都进折叠白名单，等级都是 WARN（`levels.md` 的判断没变：这是"要人去核凭据"的事）。

撤约动作本身的事件在 `zlm.md` §3.7（`zlm.node.hook_revoked` / `zlm.node.hook_revoke_failed`）。

### 3.5 `device` / `channel` 删除（`controllers/device_delete.go`）

| event | 等级 | 必需字段 |
|---|---|---|
| `gb28181.device.delete_shared_projections_retired` | INFO | `device_id` `retired_projections` |
| `gb28181.channel.delete_shared_projections_retired` | INFO | `channel_id` `retired_projections` |

**`device_id` 必须是国标号而不是自增 id**：上级平台目录里那条撤不下来的幽灵设备
是按国标号认的；而"重接入后撞 1062"的根因正是**自增 id 变了、国标号没变**。
打自增 id 等于把最需要区分的两个值搞混（有测试锁住）。

---

## 四、豁免与"条件字段"

### 4.1 条件字段（4 条）—— **不是欠账**

扫描器「无定位」是**按调用点**判定的，下列 4 条在 `if streamID == ""` 的**缺席那一支**：

```
gb28181.hook.play.denied / gb28181.hook.flow.ignored /
gb28181.hook.auto_on_demand.denied / gb28181.hook.server_started_node_unresolved
```

工具已升级：明细行会标 `← [条件字段] 同 event 的其他调用点带了定位字段`，
**判定结果不变**（只看单条会误判，但也别用"取并集"去改判定 —— 那会让另一支的盲区变成假绿）。

### 4.2 组件级 / 进程级（登记，不是漏了）

| event | 定位对象 | 理由 |
|---|---|---|
| `gb28181.sip.*`（6 条） | 本进程的 SIP 服务 | 监听器生命周期（起/停/失败），没有"某台设备"这个对象 |
| `gb28181.config.trace_retention_invalid` | 配置项 | 已有 `configured` / `default` 两个值说明"错在哪" |
| `gb28181.device.scanner.query_failed` | 本轮扫描 | 批量查询失败，没有具体设备；按轮次定位 |
| `gb28181.hook.keepalive.read_failed` / `collector_unavailable` | 本进程的回调入口 | 载荷读不出/collector 未装配；已补 `source_ip`（唯一能回答"是不是同一台在反复发"） |

### 4.3 不在 C06 范围内（已登记，归后续项）

- `bootstrap.go` / `bootstrap_shutdown.go` 的启动装配日志（含 `(无 event)` 那批）→ **进程级**，
  由 C08 统一复核是否该补 `event`
- `zlm/*`（`scheduler` / `probe` / `metrics`）→ **C07**
- `ptz/*` / `recording/*` / `controllers/setup.go` → **C08**
- `cascade/*` 的 3 条 → C05 已裁决豁免
- `play/reconciler/*` 7 条 → C04 已裁决（轮次级）

---

## 五、改动与验收

### 5.1 改动清单

| 文件 | 改动 |
|---|---|
| `handler/catalog.go` | 签名 `traceFields ...zap.Field` → `deviceID, callID, cseq string`；5 条日志补三件套；两个 `zap.Error` 合成 `errors.Join` |
| `handler/deviceinfo.go` | 同上（9 条日志）；`missing_device_id` 用**信封上**的 device_id |
| `handler/message.go` | 2 个调用点改为传字符串实参 |
| `handler/register.go` | `succeeded` 补 `peer` / `expires` / `catalog_triggered`（触发块复用该布尔值）；`unregistered` 补 `peer` |
| `handler/catalog_trigger.go` | 3 个事件补 `gb28181.` 前缀（与 `deviceinfo_trigger.go` 对称） |
| `handler/hook.go` | 21 处 `stream` → `stream_id`；3 个拒绝出口改 `stream_id` + `reason_code` + 条件缺席；删 `reason="accepted"`；`OnServerStarted` 补 `media_server_id`/`node_id` + **新事件** `server_started_node_unresolved`；keepalive 三条补 `source_ip`/`media_server_id`/`body_len`；`stopCleanupPending` 中文句子 → 受控短码；16 个 kebab 短码 → snake |
| `handler/hook_auth.go` | `node` → `node_id`、`payloadNode` → `payload_node_id`、`sourceIp` → `source_ip`、`reason` → `reason_code`；`rejectHook` 形参改名 |
| `controllers/device_delete.go` | 两处补 `event` + snake_case + `app.Log(c.Request.Context())` |
| `zlm/client.go` | 3 处 `stream` → `stream_id`（属 C07，与 2.2 同一改法） |
| `docs/.../scan-logging.py` | **两处工具能力**：① 定位字段基线加 `sourceip`；② 明细行标注 `[条件字段]`（见 §六） |

### 5.2 验收（可复跑）

```bash
cd server

# 1. 门禁：必须**全绿**（C06 之前是 2 findings，都在 device_delete.go）
go test -count=1 -run TestLoggingPolicyRepository ./internal/loggingcontract/
#    → ok（0 findings）

# 2. 编译与包测试
go build ./...
go test -count=1 ./app/gb28181/handler/
go test -count=1 ./app/gb28181/controllers/
#    → handler ok；controllers 唯一红灯 TestLoggingGBHTTPContextWiring 为**既有**
#      （cascade/controller/management.go:166，属 C08）

# 3. 命名：handler 层不应再有 camelCase 字段名
grep -rnE 'zap\.[A-Za-z0-9]+\("[A-Za-z0-9_]*[A-Z]' --include='*.go' app/gb28181/handler/ | grep -v _test.go
#    → 空

# 4. catalog / deviceinfo 的 `traceFields` 应清零
grep -rn "traceFields" --include='*.go' app/ | grep -v _test.go
#    → 空

# 5. 指标
python3 ../docs/logging-governance/scan-logging.py --root .
#    → 无定位 167 → 146；gb28181 桶 120/17 → 123/14（其中 4 条是条件字段）
```

### 5.3 新增与加强的测试

| 文件 | 内容 |
|---|---|
| `handler/register_logging_test.go`（新） | `succeeded` 的**三态** `catalog_triggered` / `peer` / `expires` / `transport`；`unregistered` 的 `peer` |
| `handler/catalog_logging_test.go`（新） | `response_processed` 三件套 + `complete`；`response_parse_failed` 保留信封身份 |
| `handler/hook_auth_test.go` | `DoesNotLogCapability` 加强：断言 `source_ip` / `node_id` 在场、`reason_code` 非空且**非 kebab** |
| `controllers/device_delete_cascade_test.go` | 两个既有删除测试加日志断言（`event` 存在、国标号非自增 id、`retired_projections`、无 camelCase 键） |
| `handler/logging_hook_test.go` | `stream` → `stream_id`，并加 `require.NotContains(t, fields, "stream")` 防回退 |
| `handler/auto_on_demand_test.go` / `hook_play_auth_test.go` | `reason` → `reason_code`；拒绝事件补 `stream_id` 在场断言 |

**`catalog_triggered` 为什么值得单测**：它是三重 `&&` 的结果，
任一项被误当成另一项，这条日志就开始骗人。三态用例各锁一个变量。

---

## 六、顺带发现（不并入本项结论）

1. **扫描器分不清"字段缺席"与"字段在另一支"**（已修，零判定变更）：
   同一 event 在 `if/else` 两支带不同字段是正常设计（有值时带、取不到时缺席），
   但逐调用点判定只看到一支。现在明细行标 `← [条件字段] 同 event 的其他调用点带了定位字段`。
   ⚠️ **只用于打标，不改判定** —— 改成并集判定会产生"某 event 在 A 处带了 device_id、
   B 处没带，并集判成有定位"的假绿，B 处那条依然是盲区。
2. **定位字段基线加 `sourceip`**：hook 链路没有 SIP `call_id`、也没有 HTTP request id
   （ZLM 主动打过来），"谁在跟我说话"只能由它回答。与 `peer` 同属"对端地址"维度。
   ⚠️ 这是"把真实存在的定位字段登记进基线"，不是放宽判定 —— 全仓调用点很少且都确实用它定位。
3. **门禁 `collectFieldFunctions()` 按文件收集**（C05 已登记）：本项再次踩到
   —— `cascadeCallIDField` 那类 `func(...) zap.Field` helper 必须与调用点同文件，
   更推荐**返回 string 内联进 `zap.String`**（扫描脚本也只认内联形式）。
4. **全仓仍有大量裸前缀事件名**（`audit.*` / `auth.*` / `casbin.*` / `codegen.*` / `db.*` /
   `device.*` / `setup.*` …）→ 会掉进扫描器的"其他"命名空间（**71% 无定位**，是最大的一个桶）。
   本项只修了 `catalog.*`（因为对称文件已经带了前缀）。**C08 应把"命名空间归属"当独立议题**。
5. `cascade/controller/management.go:166` 把 `*gin.Context` 传给 `context.Context`
   （`TestLoggingGBHTTPContextWiring` 的既有红灯）→ C08。

---

## 七、复核时不要做的事

- ⛔ **别把 `is_first` 改名 `catalog_triggered`**（§2.4）。两者不等价，
  改名会让日志在"同步开关关掉"时**谎报已同步** —— 比信息不完整更糟。
- ⛔ **别给条件字段打空串**。"取不到"要字段缺席，不要 `""`：
  空值会被读日志的人和扫描脚本都当成"已带定位字段"，把"哪条流被拒了"这个问题糊过去。
- ⛔ **别把 `zap.Error(err)` 批量换成 `logging.Error(err)`**（C05 已登记）：
  后者只渲染 `class`/`type`/`code`，业务错误文本（`channel is not shared to platform 7`）
  会退化成 `{"class":"unknown"}`。它适合"不想让原文进日志"的场景（DSN / 连接串）。
- ⛔ **别把 `.With(traceFields...)` 加回来**。它运行时是对的，但字段名在日志语句里读不到 ——
  扫描判"无定位"、门禁验证不了。要复用公共字段就**写显式参数**。
- ⛔ **别动 ZLM 回调载荷的 JSON 字段名**。`stream` → `stream_id` 只改**日志字段名**；
  `json:"stream"`、ZLM API 查询参数改了会直接打断回调解析。
- ⛔ **别把 `catalog.response_parse_failed` 的 `device_id` 去掉**。解析失败指的是**应答体**，
  信封（From/Call-ID/CSeq）早就解开了 —— 去掉它等于让"反复发坏应答的设备"无法被指认。

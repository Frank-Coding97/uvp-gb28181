# C05 · `cascade` 链路定位契约

- **状态**：✅ 2026-09-15 完成（门禁 11 条 cascade findings 归零）
- **范围**：`server/app/gb28181/cascade_invite_handler.go`、`cascade_catalog_handler.go`、
  `cascade_catalog_push.go`、`cascade_runtime.go`
- **口径**：静态扫描（`scan-logging.py`）+ 逐调用点人工核对；门禁
  （`internal/loggingcontract`）作为"能不能被验证"的判据。
- 判据来源：[`README.md`](../README.md) 判据三问；维度定义见
  [`event-catalog.md`](./event-catalog.md) §二。

---

## 一、这条链路要回答什么

级联（国标级联，我方作为**下级平台**被上级平台接入）是**纯 SIP 链路**：
没有 HTTP request id 可以继承，也没有设备侧的心跳可看。出事时运维要能回答：

| 问题 | 需要的字段 |
|---|---|
| 是哪**台**上级平台？ | `platform_id`（配置主键） |
| 认不出平台时，是**谁**在说话？ | `peer`（源地址:端口/传输层 + From 用户） |
| 是**哪一次**会话 / 哪一条报文？ | `call_id`（与 `gb_sip_trace_message` 对上的唯一键） |
| 成了没有？没成是为什么？ | `error` / `reason_code` / `status` |

**"认不出平台"是这条链路最常见的故障**（配置里 Host 与实际上游源地址不一致、
端口写错、上游多接入实例），而这恰恰是最需要 `peer` 的场景 ——
见 `cascadePeer` 的注释（源 IP 会随路由变，身份是 From + 端口 + 传输层）。

---

## 二、四个问题与裁决

### 2.1 日志字段静态不可见（本项的核心）

`cascadeVideoRuntime.log()` 把字段攒进 `fields []zap.Field` 再
`append(fields, zap.String("event", ...))...` 展开。**运行时输出是对的**，
但静态分析读不到 —— 门禁报 9 条 `unresolved_logger`，`scan-logging.py` 把它算作
"经 zap.Field 变量展开（静态不可见）"。

**这不是"少了字段"，比那更糟**：门禁红着，而一个红着的门禁等于没有门禁 ——
往里加真问题时没人看得见。所以本项的处置是**消除这个写法**，不是补字段。

**落地**：拆成 9 个具名方法（`logACKInvalid` / `logFailed` / …），字段在每个方法体里写全。

### 2.2 `camelCase` 与 `snake_case` 并存

| 文件 | 改前 | 改后 |
|---|---|---|
| `cascade_catalog_handler.go` | `platformId` ×2、`channelId` | `platform_id`、`channel_id` |
| `cascade_catalog_push.go` | `platformId` ×2 | `platform_id` |
| `cascade_invite_handler.go` | `platformId`、`configuredHost` | `platform_id`、`configured_host` |

**为什么这算治理**：同一台平台在 cascade 日志里叫 `platformId`、在 play 日志里叫
`platform_id`；工具认不出、门禁认不出、人 grep 也会漏。C04 里 `play` 就是被这件事
坑过一次（脚本一度把 play 判成 95% 无定位）。

### 2.3 `forward_failed` 有两个写法

同一事件名 `cascade.control.forward_failed`，一处带硬编码
`reason="PTZ service unavailable"`、一处带 `error=<err>`。

**同名事件两种字段集**的代价：聚合统计与排障都不确定该读哪个键。合并成一条：

- `reason_code`：受控短码，说"哪一类失败"（`ptz_service_unavailable` / `forward_rejected`）；
- `error`：具体错在哪 —— **只在真的失败时出现**（`zap.Error(nil)` 是 no-op，字段缺席）。

### 2.4 `catalog.query_failed` 没有定位字段

它用全局 `app.ZapLog`、且只带 `status` + `error`。两个改进：

1. 换成 `app.Log(ctx)`（统一入口，能继承调用方 ctx 里的身份）；
2. 补 `call_id` + `peer`。

⚠️ **这里刻意没有补 `platform_id`**：多数失败（多个候选 / 认不出上游 / 平台被禁用）
恰恰发生在**认定平台之前**。硬塞一个 `platform_id=0` 就是判据①说的
"带了但是占位值" —— 它会让扫描脚本认为这行已达标。

---

## 三、逐事件契约（17 条）

`platform_id` 标 `*` 表示**认定平台之前缺席**（不是 0 占位）；`error` 标 `*` 表示
只在失败时出现。`app.Log(ctx)` / `app.ZapLog` 的口径见 §五。

### 3.1 `cascade.video.*` —— 上级平台点播我们的通道（`cascade_invite_handler.go`）

| event | 等级 | 触发条件 | 定位字段 |
|---|---|---|---|
| `cascade.video.failed` | WARN | INVITE 在任一阶段被拒（一次会话一次） | `platform_id`\* + `call_id` + `peer` + `error` |
| `cascade.video.established` | INFO | 会话进入 active（一次会话一次） | `platform_id` + `call_id` |
| `cascade.video.ack_invalid` | WARN | ACK 无法读入对话框 | `platform_id` + `call_id` + `error` |
| `cascade.video.answer_failed` | WARN | 应答 / ACK 阶段失败 | `platform_id` + `call_id` + `error` |
| `cascade.video.rtp_cleanup_failed` | WARN | 停发 RTP 失败，保留占用并重试 | `platform_id` + `call_id` + `error` |
| `cascade.video.state_save_failed` | WARN | 会话状态落库失败 | `platform_id` + `call_id` + `error` |
| `cascade.video.bye_failed` | WARN | 配置变更时发 BYE 失败 | `platform_id` + `call_id` + `error` |
| `cascade.video.peer_mismatch` | INFO | 源 IP 与配置 Host 不符但身份唯一命中 | `platform_id` + `call_id` + `peer` + `configured_host` |
| `cascade.video.release_timeout` | WARN | 进程关闭时等不到会话释放 | **进程级，无**（见 §四） |

### 3.2 `cascade.catalog.*` —— 目录查询与推送（`*_catalog_handler.go` / `*_catalog_push.go`）

| event | 等级 | 触发条件 | 定位字段 |
|---|---|---|---|
| `cascade.catalog.query_failed` | WARN | 上级目录查询被拒（一次查询一次） | `call_id` + `peer` + `status` + `error` |
| `cascade.catalog.response_send_failed` | WARN | 目录响应批次发送失败 | `platform_id` + `status` + `error` |
| `cascade.catalog.response_completed` | INFO | 响应批次全部发完 | `platform_id` + `items` + `batches` |
| `cascade.catalog.push_failed` | WARN | 主动推送批次失败（管理接口触发） | `platform_id` + `sent` + `status` + `error` |
| `cascade.catalog.push_completed` | INFO | 主动推送完成 | `platform_id` + `items` + `batches` |

### 3.3 `cascade.control.*` —— 上级下发云台控制（`cascade_catalog_handler.go`）

| event | 等级 | 触发条件 | 定位字段 |
|---|---|---|---|
| `cascade.control.forward_failed` | WARN | 云台命令未能送达通道 | `platform_id` + `channel_id` + `reason_code` + `error`\* |

`reason_code` 取值（受控短码，不是自由文本）：

| 值 | 含义 |
|---|---|
| `ptz_service_unavailable` | 本进程 PTZ 服务未就绪 |
| `forward_rejected` | 已交给 `control.Service.Forward` 但被拒（通道未共享 / PTZ 权限关闭 / 设备离线） |

### 3.4 `cascade.*` 生命周期（`cascade_runtime.go`）

| event | 等级 | 触发条件 | 定位字段 |
|---|---|---|---|
| `cascade.credential_key_unavailable` | WARN | 启动期凭据密钥不可用（每次启动最多一次） | `env` + `config_write` + `encrypted_platform_runtime` |
| `cascade.shutdown_incomplete` | ERROR | 级联子系统关闭未完成 | **进程级，无**（见 §四） |

---

## 四、豁免：3 条不带定位字段（登记，不是漏了）

| 调用点 | 为什么不补 |
|---|---|
| `cascade.video.release_timeout`（`cascade_invite_handler.go`） | 关闭时等不到**所有**会话释放，定位对象是"本次进程关闭"，不是某台平台或某次会话。补 `platform_id` 需要任选一个会话，是错的。 |
| `cascade.credential_key_unavailable`（`cascade_runtime.go:262`） | 启动期组件级事件，定位对象是"进程 + 配置项"；它已有的 `env` 字段就是"定位谁"的答案。 |
| `cascade.shutdown_incomplete`（`cascade_runtime.go:320`） | 同 release_timeout，进程级。 |

**判定依据**（§二 的维度规则）：组件级事件只要求能定位到组件 + 状态；
给它塞 `device_id`/`platform_id` 是"为了指标好看而打印"。

---

## 五、改动与验收

### 5.1 改动清单

| 文件 | 改动 |
|---|---|
| `cascade_invite_handler.go` | 删除 `log()` 的 `fields` 容器写法 → 9 个具名方法；`platform_Id`/`configuredHost` → snake_case；会话级事件统一补 `call_id` |
| `cascade_catalog_handler.go` | `query_failed` 改用 `app.Log(ctx)` + 补 `call_id`/`peer`；`platformId` → `platform_id`；`forward_failed` 两处合并 + `reason_code`；两个 `zap.Error` 合并为 `errors.Join`（消除同名键） |
| `cascade_catalog_push.go` | 两条日志补 `event`（原来没有）；`platformId` → `platform_id`；改用 `app.Log(ctx)` |
| `cascade_logging_test.go`（新增） | 4 条日志契约测试（见 §5.3） |

**没有改的东西，以及为什么**：

- **`zap.Error(err)` 保持原样，不换成 `logging.Error(err)`。** 后者只渲染
  `class`/`type`/`code`，对 cascade 这类业务错误（`channel is not shared to platform 7`）
  会退化成 `{"class":"unknown","type":"*errors.errorString"}` —— **定位信息直接归零**。
  `logging.Error` 适合的是"不想让原文进日志"的场景（DSN / 连接串），不是这里。
- **`cascade/controller/management.go` 的同类 `fields` 容器写法没动**（见 §六）。

### 5.2 验收（可复跑）

```bash
cd server
# 1. 门禁：cascade 的 11 条 finding 必须归零
go test -count=1 -run TestLoggingPolicyRepository ./internal/loggingcontract
#    → 期望只剩 device_delete.go 的 2 条（属 C06，本项未碰）

# 2. 命名：cascade 链路不应再有 camelCase / 大写 ID 字段名
grep -rnE 'zap\.(String|Int|Int64|Uint|Uint64|Bool|Float64|Duration|Time)\("[A-Za-z]*[A-Z]' \
  app/gb28181/cascade*.go app/gb28181/cascade/ | grep -v _test.go     # → 期望空

# 3. 日志契约测试
go test -count=1 -run TestCascade ./app/gb28181/

# 4. 指标
python3 ../docs/logging-governance/scan-logging.py --root . --filter cascade --limit 0
```

实测结果（2026-09-15）：

| 指标 | 改前 | 改后 |
|---|---|---|
| 门禁 cascade findings | **11**（9 `unresolved_logger` + 2 `missing_event`） | **0** |
| 门禁全仓 findings | 13 | **2**（`device_delete.go` 既有，属 C06） |
| cascade 文件 camelCase 字段名 | 5 | **0** |
| `scan-logging.py` "经变量展开（静态不可见）" | 12（全仓） | **3**（全仓，cascade 已清零） |
| cascade 命名空间调用点 | 15（9 条藏在一个 helper 里） | **18**（显式化 + push 补 event） |
| cascade 无定位调用点 | 2 | **3**（全部是 §四 的进程级豁免） |

> ⚠️ **"无定位"从 2 变成 3 不是退步**：改前那条 `query_failed` 被算作"无定位"，
> 而 9 条 `cascade.video.*` 因为整段静态不可见，被算作"有定位"（脚本保守合并了变量）。
> 现在它们**逐条可验证**了：18 条里只有 3 条真的没有定位字段，且都是进程级。

### 5.3 新增测试（4 条）

`app/gb28181/cascade_logging_test.go`：

| 测试 | 断言 |
|---|---|
| `TestCascadeVideoFailureBeforeIdentificationKeepsPeerOnly` | 认定平台前失败：`platform_id` **缺席**、`peer` + `call_id` 在场、`error` 在场 |
| `TestCascadeVideoFailureAfterIdentificationCarriesPlatform` | 认定平台后失败：`platform_id` 在场 |
| `TestCascadeControlForwardFailureUsesSingleShape` | 两种失败路径产出**同一字段集**；`err == nil` 时不留空 `error` |
| `TestCascadeCatalogQueryFailureKeepsCallIDAndPeer` | 跑真实 handler：`call_id` + `peer` + `status` 都在 |

四条都额外跑 `requireNoCamelCaseFields()` —— 遍历该场景产出的**每一条**日志，
任何字段名含大写字母即失败。这是把 §2.2 变成回归测试，而不是靠 grep。

---

## 六、顺带发现（不并入本项结论）

1. **门禁缺陷：字段构造函数按文件收集，而不是按包。**
   `policy.go:collectFieldFunctions()` 的注释写的是 "one hop deep **and intra-package**"，
   实现却是 `for _, syntaxFile := range pkg.Syntax` 里边收集边检查 ——
   定义在**另一个文件**里的 `func(...) zap.Field` helper 会被判成
   `unresolved_logger: logger field helper could not be inspected`。
   本次实测复现：把 `cascadeCallIDField` 放在 `cascade_invite_handler.go`、
   在 `cascade_catalog_handler.go` 调用 → 立刻多 2 条 finding。
   **当前处置**：把 helper 改成返回 `string` 内联进 `zap.String`（对扫描脚本也更友好）。
   **建议**：单独修门禁（先收集整个 package，再检查），修前先量影响面。
2. **`cascade/controller/management.go:328-337` 是同类写法**（`fields := []zap.Field{...}`
   再 `...` 展开）。它当前**不**触发门禁 finding，因为它走的是 `a.origins` 里
   "变量有来源"的追踪分支，而 `cascade_invite_handler.go` 那种**内联 append** 没有来源。
   两者的"静态不可见"性质是一样的 —— 属 C06/C08 范围。
3. **`bootstrap.go:1243` 用 `zap.String("callId", ...)`**（camelCase）。
   不在本项范围（C02.5 / C06）。
4. **cascade 桶 Warn 占比 13/18 = 72%**（全仓最高之一）。
   等级校准属 **C03.4**，本项只做定位，不动等级。
5. 两个 `zap.Error(...)` 写同一 key 的问题，在 `response_send_failed` /
   `push_failed` 两处已用 `errors.Join` 合并（两者互斥）。全仓是否还有同类，
   未做全面清点。

---

## 七、复核时不要做的事

- **不要为了把 `release_timeout` / `shutdown_incomplete` 弄成"有定位"而塞 `platform_id`**：
  见 §四。
- **不要放宽 `scan-logging.py` 的 `peer` 判定之外的东西**：`peer` 是本次唯一新增的定位字段
  （依据：级联场景里"谁在跟我说话"就是它，全仓 3 个调用点）。往里加"看起来像定位"的字段
  会让指标撒谎。
- **不要把 `zap.Error(err)` 批量换成 `logging.Error(err)`**：见 §5.1 的说明。

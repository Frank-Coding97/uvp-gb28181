# 等级契约（C03）

> **一句话**：等级只回答一个问题 —— **看到这条日志，谁要去做一件什么事？**
>
> 答得出来 → `WARN` / `ERROR`；答不出来 → `INFO` / `DEBUG`。
>
> 本文是 C03 的判定正本。它**不是**一张"哪个事件该打哪级"的表，
> 而是一条能在下次遇到新事件时继续用的规则 + 本次逐条判定后的结论。

---

## 一、四档语义

| 等级 | 含义 | 判定问句 |
|---|---|---|
| `ERROR` | 需要人**立即**介入，功能**已不可用** | "哪块功能没了？" —— 答得出具体功能 |
| `WARN` | 需要人**关注**，功能**降级但仍在跑** | "降了什么？" —— 答得出被牺牲的东西 |
| `INFO` | 业务流程节点 / 被正确拒绝的外部输入 | "走到哪了？" 或 "对方做错了什么？" |
| `DEBUG` | 过程细节 / 占位诊断，仅在排查时开 | 只有把日志级别调低时才有意义 |

**最容易犯的错**：拿不准就打 `WARN`。拿不准应该是 `INFO` 或 `DEBUG`。
`WARN` 一旦能"拿不准就放进来"，它就不再是告警，而是一个跟 `INFO` 重复的桶。

### 一条额外的边界：`ERROR` 不是"更严重的 WARN"

`ERROR` 的定义是**功能已不可用**，不是"失败得更厉害"。一次点播失败是 `WARN`
（点播这块功能对其他请求仍然可用）；数据库连不上、媒体面整块装配失败才是 `ERROR`。

---

## 二、为什么把「Warn < 10%」这个目标撤销了

`content-plan.md` 原定目标：**Warn 占比 54% → < 10%**。

C03 落地时把 149 个 Warn 调用点逐条摆开看了一遍，结论是**这个目标不成立**：

| 分组 | 条数 | 说明 | 该是什么等级 |
|---|---|---|---|
| A 我方操作失败 | **约 110** | `*_failed` / `*_timeout` / `*_missing` / `*_incomplete` / `*_unavailable`，每条都答得出"降了什么" | **WARN（保留）** |
| B 启动期装配降级 | **约 24** | `zlm.registry.db_unavailable` / `…repository_assemble_failed` 等，进程继续服务其他功能 | **WARN（保留）** |
| C 外部输入被正确拒绝 | **16** | 对方报文不合法、权限不足、入参非法、幂等重复调用 | **INFO（本次降级）** |
| D 占位/无内容诊断 | **3** | `db.diagnostic`（GORM 回调文本被故意丢弃） | **DEBUG（本次降级）** |
| E 纯流程叙述（脚本口径外） | **3** | `bootstrap.go` 的三条"已运行/尚未配置" | **INFO（本次降级）** |

**A + B 合计约 134 条，占 Warn 的 90%。** 把它们降到 `INFO` 才能凑出 < 10%，
而那等于把"断流失败""级联点播失败""媒体节点装配失败"全部藏进 `INFO` ——
**这是信息错误，比通胀更糟**（`README` 判据①：定位不到对象就排不了障）。

所以目标改成三条**可以真正判定**的：

| 新目标 | 怎么判 | 现状 |
|---|---|---|
| ① `Warn` 里不存在"答不出动作"的条目 | 本文第三节的判据 + 逐条复核 | ✅ 已把 19 条移出 |
| ② 同一 `event` 的等级唯一 | **机检**：`go test ./internal/loggingcatalog/` 的 `event_level_divergence` | ✅ 基线 0 |
| ③ `ERROR` 只用于"功能已不可用" | 人工复核（C03 未展开，见第六节） | 🔄 未做 |

> **数量目标并没有被取消**，只是换成了可达成的方向：`Warn` 从 43% → **37%**，
> 业务域 52% → **47%**，平台支撑域 20% → **12%**（`casbin` 33%→0%、`db` 20%→0%）。
> 剩下的每一条都写得出"降了什么"。

---

## 三、判据：怎么判断"答不出动作"

按下面的顺序问，**第一个答"是"的就决定等级**：

1. **功能完全没了吗？** → `ERROR`
   （例：媒体面装配失败、SIP 服务启动失败）
2. **我们自己的操作失败了吗？（有东西被牺牲掉）** → `WARN`
   （例：断流失败 → 会话残留；级联点播失败 → 上级看不到画面）
3. **被拒绝/被丢弃的是外部输入，而我们做对了？** → `INFO`
   （例：设备发的报文解析不了 → 丢弃；用户没权限 → 403）
4. **什么都没发生（正常态 / 幂等忽略）？** → `INFO`
   （例：重复启动被忽略；首启尚未配置 SIP）
5. **只有排查时才需要看的细节或占位？** → `DEBUG`

### 第 3 类里两个**故意留 WARN** 的边界

这两条边界是刻意划的，不是漏降：

| 保留 `WARN` | 为什么 |
|---|---|
| **身份认证失败**：`gb28181.register.digest_failed` / `gb28181.register.server_id_mismatch` / `gb28181.hook.auth.rejected` | 这是**"对方没通过认证"**，可能意味着凭据被改/被冒充。它与下面那条不同：需要人**去核对设备配置**。 |
| **对方的错改变了本地状态推进**：`cascade.video.ack_invalid` **除外**（已降级）；但 `gb28181.deviceinfo.device_missing`（应答对应的设备不在库）保留 | 那是**本地数据与设备行为对不上**，要人去查库/查注册。 |

对照着看**已降级**的那条：`casbin.permission.denied` 是"**已认证**主体的权限不足"，
返回 403 是设计内行为，没人会去"修"一个用户缺权限 —— 所以它是 `INFO`。

---

## 四、本次改动清单（19 条）

### 4.1 降 `INFO`（脚本口径内 16 条）

| 事件 | 位置 | 为什么 |
|---|---|---|
| `gb28181.message.parse_failed` | `handler/message.go` | 设备发的 MESSAGE 格式不合法，我们丢弃 |
| `gb28181.notify.parse_failed` | `handler/notify.go` | 同上（NOTIFY） |
| `gb28181.catalog.response_parse_failed` | `handler/catalog.go` | 设备回的 Catalog 应答坏了，丢弃 |
| `gb28181.deviceinfo.response_parse_failed` | `handler/deviceinfo.go` | 同上（DeviceInfo） |
| `gb28181.deviceinfo.missing_device_id` | `handler/deviceinfo.go` | 应答体缺 `DeviceID`，丢弃 |
| `ptz.response.unexpected` | `ptz/handler.go` | 单向操作收到多余应答，保持 `sent` 不变 |
| `ptz.response.unmatched` | `ptz/handler.go` | 应答对不上任何 operation，丢弃 |
| `ptz.response.ignored` | `ptz/handler.go` | 有意不写事实，保库里一致 |
| `ptz.response.protocol_invalid` | `ptz/handler.go` | 设备回了非法协议，按 `ptzErrorProtocolInvalid` 拒绝 |
| `casbin.permission.denied` | `utils/casbinhelper/` | 授权拒绝是设计内行为 |
| `casbin.role_inheritance.add_rejected` | `service/casbinservice.go` | 入参自校验，拒绝后 `return nil` |
| `casbin.role_inheritance.update_rejected` | `service/casbinservice.go` | 同上 |
| `casbin.role_inheritance.remove_rejected` | `service/casbinservice.go` | 同上 |
| `auth.demo_account.denied` | `middleware/demoaccount.go` | 演示账号写操作被策略拒绝 |
| `play.reconcile.duplicate_start` | `play/reconciler/reconciler.go` | 幂等调用被忽略（同文件 `disabled` 本来就是 INFO） |
| `cascade.video.ack_invalid` | `cascade_invite_handler.go` | 上级平台的 ACK 不合规，我们继续跑 |

> **`ptz.response.unmatched` 的证据**：`app/utils/logging/sanitize.go` 的注释里写着它是
> **"the highest-volume WARN in the tree (~600 lines per log file)"**。
> 也就是说这条 WARN 早就被认定为最高频噪声 —— 它把真告警淹掉了，这才是 C03 要修的东西。

### 4.2 降 `DEBUG`（1 个事件 / 3 个调用点）

| 事件 | 位置 | 为什么 |
|---|---|---|
| `db.diagnostic` | `utils/gormhelper/log.go` `Info`/`Warn`/`Error` 三支 | GORM 的 `logger.Interface` 只告诉你"我发了条消息"，**文本被故意丢弃**（`text_omitted: true`）。判据①（定位谁）与判据②（做什么）都答不出。SQL 层面的真相另有其处：`db.query` / `db.query_failed` / `db.slow_query` 带 dialect/operation/fingerprint/rows/duration_ms |

**顺带的效果**：`db.diagnostic` 是**全仓唯一**一个跨等级的 event（Info/Warn/Error 各一处），
降完正好归零 —— 于是第五节的门禁规则可以做到**零例外**。

### 4.3 降 `INFO`（脚本口径**外** 3 条）

`app/gb28181/bootstrap.go` 的三条**没有任何 `zap.Field` 参数**，
所以扫描脚本（`calls_in()` 要求段内有 `zap.`）**根本看不见它们**：

| 行 | 消息 | 为什么 |
|---|---|---|
| `bootstrap.go:527` | GB28181 SIP 已进入停止流程,拒绝重复启动 | 进程正在停，等价于这次调用没发生 |
| `bootstrap.go:534` | GB28181 SIP 已运行,忽略重复启动 | 幂等调用被忽略 |
| `bootstrap.go:565` | GB28181 SIP 尚未配置,跳过 SIP 依赖并继续启动后台 | **首启的正常态**，不是异常 |

> ⚠️ 这三条的降级**不会体现在任何指标里**（脚本看不见无字段调用）。
> 复核靠 `grep -nE "已进入停止流程|已运行,忽略重复启动|尚未配置,跳过 SIP" app/gb28181/bootstrap.go`。
> 它们仍然没有 `event`（`(无 event)` 目标归零属 C08 的收尾），**本次故意不补** ——
> 补 `event` 会同时移动"唯一 event 数 / 无定位分母 / 登记册规模"三个指标，
> 把等级改动和事件补齐混成一笔账，正是 `README` 反复警告的"指标假动"。

---

## 五、机检：同一 event 的等级必须唯一

**为什么做成门禁而不是文档**：一个 event 名下挂两种等级时，任何"按事件名聚合"的统计
（按事件计数、按事件看趋势、告警规则）**都是假的**。C04 第一个撞上：
`play.reconcile.probe_failed` 两处 `WARN`、一处 `DEBUG`。

规则落在 `internal/loggingcatalog/`（C09 建的登记册门禁），**不动 `loggingcontract`**：

```
go test ./internal/loggingcatalog/     # 含 event_level_divergence
```

- **基线 0，零例外**。它不是"登记现状禁止退回"，而是一条纯规则：
  两个等级 → 拆成两个事件名，**永远不要在两个等级里挑一个赢家**。
- 报法：每个事件只报**一条** finding，描述里列出各等级的首个位置：

  ```
  app/utils/gormhelper/log.go:51 event_level_divergence event is logged at 3 levels
  (info@app/utils/gormhelper/log.go:51, warn@…:56, error@…:61); give each outcome its
  own event name so aggregation by name stays truthful
  ```

**盲区**（与所有其他规则共用）：事件名解析不出来的调用点看不见。
从运行时数据推等级的适配器（HTTP 访问日志 `ginhelper/logging.go`、
调度器 `schedulerhelper/logger.go`）把 `event` 攒在 `fields` 切片里，
本包读不到，也**不该**报 —— 它们一个事件对应多个等级是**设计如此**（等级跟着结果走）。
这类"读不到的"归 `loggingcontract` 的 `dynamic_event` 管（`reviewed_adapters.json` 有 SHA 封条）。

---

## 六、故意没做的事

1. **没把"启动期装配失败"升成 `ERROR`。** 按定义它们确实"功能缺一块"，
   但那是**带伤运行**（进程继续服务其他功能），而 `ERROR` 会被值班当成"服务挂了"。
   保留 `WARN`，理由写在这里而不是改代码 —— 这条边界如果要动，应该一次性重排
   全部 24 条启动期事件，而不是零散改几条。
2. **没做 C03.7 原本提的"消息文本不得是纯流程叙述"。** 那是自然语言判断，做不成门禁
   （"已运行"和"已重试成功"在正则眼里一样）。换成第五节的等级唯一性规则 ——
   它是同一诉求的**可机检**版本。
3. ~~没改 `ERROR` 的分布。~~ **C03.③ 已补做，见第九节。** 本轮（C03 主体）只校准
   `WARN`；当时分开做的理由现在仍然成立 —— 两种等级的判据不同（`WARN` 判"要不要人看一眼"，
   `ERROR` 判"哪块功能整个没了"），混在一轮里容易用同一把尺子量两件事。70 条的逐条复核
   放在第九节，与本节结论不冲突：第 1 条说的"启动期装配失败不升 ERROR"依然是边界。
4. **没给无字段调用补 `event`。** 见 4.3 的说明。
5. **没动口径外的框架适配器**（`schedulerhelper` 的 `任务被丢弃（策略）` 等）。
   它们的等级**由调用方传入**，是框架行为不是业务判断；且它们在脚本口径外，
   改了既不进指标也不进门禁。要治理它们得先决定"调度器任务丢弃"算不算事件，
   那是另一件事。

---

## 七、已知盲区

| 盲区 | 影响 | 归属 |
|---|---|---|
| **无 `zap.Field` 的调用不进扫描口径** | 约 24 条 `app.ZapLog.Warn(...)` 式的启动期日志（含 4.3 的三条）在指标外 | 口径本身的设计（`README` 第四节已记），不是 bug |
| **等级由运行时决定的事件** | HTTP 访问日志、调度器日志一个事件跨等级是设计如此 | `loggingcontract` 的 `dynamic_event` + SHA 封条 |
| ~~**`ERROR` 分布未复核**~~ | **C03.③ 已复核**（70 → 20），见第九节 | 第九节 |

---

## 八、改等级时的连带义务（本次踩过）

**改一个日志的等级，不只改那一行。** 至少两处会跟着红：

1. **`internal/loggingcontract/reviewed_legacy.json`** —— 豁免条目按
   `file + function + method + message` 匹配，且**要求恰好命中 1 个调用点**。
   把 `Warn` 改成 `Info`，那条豁免就"命中 0 个" → 报 `legacy_exception_mismatch`，
   同时该调用点会因为"没有静态 `event`"重新暴露成 `missing_event`。
   → 改等级时同步改豁免里的 `method`，并更新 `reason`。
2. **`internal/loggingcontract/reviewed_adapters.json`** —— 按**文件 SHA-256** 封条。
   碰了被封印的文件（本次是 `app/utils/gormhelper/log.go`），
   报 `legacy_exception_mismatch: reviewed adapter source changed`。
   → 重新读一遍该适配器的转发/归属契约，确认没变，再更新 SHA 与 `reason`。

> 这两步不是"绕过门禁"，是门禁设计的**复查触发点**：改等级这件事本身值得被看见一次。

---

## 九、`ERROR` 复核（C03.③，2026-09-15）

### 9.1 判据多了一条

`ERROR` = **功能已不可用** + 答得出"哪块功能没了" + **没有别的信号能暴露它**。

第三条是这一轮加的，不是修辞：`audit.operation_log.persist_failed`（审计日志落库失败）
就是靠它留下的 —— 它是全仓唯一一类"主流程成功、记录永久丢失、且没有任何下游会因此报错"
的失败（见 9.4）。

### 9.2 70 条的分布

| 分组 | 条数 | 处置 |
|---|---|---|
| **panic**（含 4 条被 `recover` 吞掉的事务回滚） | 7 | 留 `ERROR` |
| **进程/核心资源不可用**（启动依赖、DB 驱动 ×2、端口监听） | 4 | 留 `ERROR` |
| **子系统整块装配失败**（SIP 配置/启动/监听、播放鉴权、级联、调度器） | 6 | 留 `ERROR` |
| **整块功能装载失败**（定时任务装载） | 1 | 留 `ERROR` |
| **记录永久丢失**（审计） | 1 | 留 `ERROR` ← 见 9.4 |
| **关停阶段不完整**（`*.shutdown_incomplete`） | 8 | 降 `WARN` |
| **单次写库/查询/清理失败**（我方操作失败） | 37 | 降 `WARN` |
| **未认证请求 / token 失效被拒** | 4 | 降 `INFO` |
| **请求被拒出口**（4xx，见 9.3） | 2 个静态调用点 | 降 `INFO` + **拆事件名** |

合计 7 + 4 + 6 + 1 + 1 + 8 + 37 + 4 + 2 = **70**。

### 9.3 最大的一处：把 881 处调用共用的出口拆成两个事件名

`app/controllers/common.go` 的 `Common.Fail` / `FailAndAbort` 是**所有控制器的失败出口**。
实测（括号平衡解析第 4 个参数）：

| 走这个出口的调用 | 条数 | 占比 |
|---|---|---|
| 不传状态码（取默认 400） | 703 | 79.8% |
| 显式 4xx | 96 | 10.9% |
| **显式 5xx** | **82** | **9.3%** |
| 合计 | **881** | |

原来的写法是**一个** `logger.Error`，于是 **799 处输入校验（90.7%）顶着 `ERROR` 出现**
—— "设备不存在""预置位编号不合法""订阅有效期超范围"。`ERROR` 在这里已经不是告警，
是背景噪声；而真正的 82 处内部故障混在其中，看不出来。

**改法**：按状态码分两支（`failHTTPStatus` 复刻 `response.Fail` 取码规则，不改响应行为）：

- 5xx → `http.operation_failed`，`ERROR`（**保留原名**，语义收窄为"我方故障"）
- 其余 → `http.operation_rejected`，`INFO`（**新名**）

为什么不合成一条 `WARN`：两种语义的排障动作相反（"用户请求不对" vs "我们坏了"），
而 `event_level_divergence` 要求同一事件名等级唯一 —— 拆名字是唯一不违规的做法。
顺带的好处：**"我方 HTTP 故障"第一次可以按 event 名直接筛出来**。

> ⚠️ **口径提示**：`http.operation_rejected` 在扫描口径里只有 **1 个调用点**（函数里的定义处），
> 运行时它覆盖 799 处。**指标表上的 ±1 和运行时的 ±799 不是一回事**，
> 别用指标数字估这次改动的实际收益。

### 9.4 两条需要解释的判定

**① `audit.operation_log.persist_failed` 留 `ERROR`** —— 全仓唯一一条"留"得需要解释的。
它长得像其他"写库失败"（那些都降了 WARN），但性质不同：业务请求照样返回 200，
**没有任何下游会因此报错，也没有第二个信号能暴露它**。判据里"功能已不可用"说的是
审计这条链路本身，而它没有替代品。降成 WARN 等于把"操作没留痕"降成"又一个可看一眼的失败"，
而审计丢失不可追回。代码里已就地写了理由，防后人按"长得像"改掉。

**② `casbin.permission.check_failed` 降 `WARN`** —— 这一条同时**修掉了一个既有矛盾**：
上一轮（C03 主体）已经在这个文件里写下注释"权限**检查本身出错**才是 `WARN`，
两者别混"，但实现留在 `logger.Error` 没改（当时只校准 WARN）。本轮让它与注释一致：
- `casbin.permission.denied`（已认证主体权限不足，403）→ `INFO`：设计内行为，无人需动作
- `casbin.permission.check_failed`（enforcer 报错）→ `WARN`：本次请求被 500 拒，鉴权不可靠

**留 `ERROR` 的判定**（19 条）与**降级判定**的区别，看的是同一件事：**"没有这块功能，谁还能干活？"**
SIP 起不来 = 设备全掉线；DB 驱动没有 = 一切都没有；调度器装配失败 = 所有定时任务不跑 →
这些是 `ERROR`。一次设备信息回写失败 = 那台设备的这条信息旧了一次 → `WARN`。

### 9.5 结果

```
等级分布   Error 70 → 20      Warn 133 → 178      Info 121 → 126      Debug 27 不变
调用点总数 351（不变）
登记册     330 → 331 事件（新增 http.operation_rejected，定位字段 route/source_ip）
门禁       双门禁 0 findings（loggingcatalog / loggingcontract）
```

### 9.6 ⚠️ `Warn` 从 37% 涨回 50%，这不是回退

这是本轮最容易被误读的数字，三条都要一起看：

1. **没有新增日志。** 这 45 条一直在打，只是打在了 `ERROR` 桶里；调用点总数 351 一条没变。
   这是**转移，不是通胀**。
2. **它们本来就是 `WARN` 的定义** —— "我方操作失败，功能降级但仍在跑"（第二节那张表里的第一、二组）。
3. **本轮的目标不是压低 `WARN`，是让 `ERROR` 恢复语义**：70 → 20。在此之前，`ERROR` 桶里
   混着一个被 881 处调用共用的输入校验出口，`ERROR` 已经不再等于"值班立刻起来看"。

C03 已撤销比例目标（第二节），这里也一样：**`Warn` 占比不是指标，"每条 Warn 答得出动作"才是。**
第二、三条已机检（`event_level_divergence`，零例外）。


# 事件与字段登记册（C09 机制固化）

> **状态**：✅ 已接入自动流程（2026-09-15）
> **实现在**：`server/internal/loggingcatalog/`（`catalog.go` + `registry.json`）
> **验收**：`cd server && go test -count=1 ./internal/loggingcatalog/`
> 本文回答三件事：**登记册是什么**、**哪八条规则在读它**、**它故意不管什么**。

---

## 一、为什么单独开一个包

`internal/loggingcontract` 是**类型感知**的策略引擎：`go/packages` 全量装载、`policy.go`
1739 行、整包 4500+ 行。C09 要加的规则**只需要语法**（`zap.String("event", "x")` 长什么样、
字段 key 拼写是什么），不需要类型信息。

于是它们没有加进 `policy.go`，而是单独一个包：

| | `internal/loggingcontract` | `internal/loggingcatalog` |
|---|---|---|
| 看什么 | 类型（是不是真 zap logger、值安不安全） | 文本（事件名、字段名、定位字段） |
| 依赖 | `go/packages`、类型检查 | `go/ast` 解析 |
| 管什么 | 消息必须静态、值必须可安全表示、事件必须是常量 | **名字必须在登记册里、定位字段不许退回去** |

两个包**互不 import**，各自有一个门禁测试。理由写在 `content-plan.md` 的 C09 注意项里：
「`policy.go` 已 1739 行、契约包 4517 行。加规则前想清楚放哪，别再养出一个需要治理的对象。」

---

## 二、登记册里有什么

`server/internal/loggingcatalog/registry.json`（330 个事件 / 160 个字段 / 1 条例外）：

```json
{
  "events": {
    "gb28181.play.stop_requested": ["channel_id", "device_id"],
    "zlm.scheduler.switch_failed": []
  },
  "locating_free_exceptions": {
    "recording.catalog.reconcile_failed": "调度器整体失败（Enqueue 自己就失败了）时还没有任何具体节点：……"
  },
  "fields": ["device_id", "channel_id", "..."]
}
```

三段的含义：

1. **`events`** —— 事件名 → **该事件当前能定位到谁**（各调用点取并集，存原文拼写）。
   `[]` 表示这个事件在所有调用点上都不带定位字段。
2. **`locating_free_exceptions`** —— 允许"同一个事件有的调用点带定位字段、有的不带"的
   **人工判定**，值就是理由，**必填**。这是登记册里唯一由人写、不由生成器写的部分。
3. **`fields`** —— 字段 key 字典，**精确匹配**（改名就是新 key，不会被旧名字蒙过去）。

**它是基线，不是真理。** 它记的是"今天每个事件能定位到谁"，不是"今天每个事件**应该**
定位到谁"——后者要 C01 的四格表（`contracts/event-catalog.md`）逐条裁。登记册的职责是
**不让已经拿到的定位信息退回去**。

---

## 三、八条规则

| 规则（findings kind） | 判据 | 基线 |
|---|---|---|
| `unregistered_event` | 事件名不在 `events` 里 | 330 条 |
| `event_level_echo` | 事件名末段是等级词（`.warn` / `.error` / …） | 0（C08 已清零） |
| `field_not_in_dictionary` | 字段 key 拼写规范但不在 `fields` 里 | 160 条 |
| `field_name_not_snake_case` | 字段 key 不是 `^[a-z][a-z0-9_]*$` | 0（C08 已清零） |
| `locating_field_missing` | 调用点一个定位字段都没有，**而该事件在别处有** | 116 个事件豁免（见下） |
| `locating_field_lost` | 登记册记着某事件带某定位字段，现在**所有**调用点都不带了 | 214 个事件 |
| `stale_field_dictionary_entry` | 字典里的 key 在源码里一个都不剩 | 0 |
| `event_level_divergence`（**C03 加**） | 同一个 `event` 被打了两种以上等级 | 0，**零例外** |

### 3.0 `event_level_divergence` 是**规则**不是基线

前七条都是"登记现状 + 禁止退回"，这一条不同：它**不读登记册**，纯粹从源码算
（`Site` 多带一个 `Level` 字段即可）。理由是它没有需要豁免的情形 ——
一个事件名挂两种等级时，按事件名做的任何统计都是假的（C04 第一个撞上：
`play.reconcile.probe_failed` 两处 `WARN` 一处 `DEBUG`）。
**修法只有一种：拆事件名**，永远不要在两个等级里挑一个赢家。

**盲区**：事件名解析不出来的调用点看不见。等级由运行时数据决定的适配器
（HTTP 访问日志、调度器日志）一个事件跨多级是**设计如此**，本包读不到也不该报 ——
它们归 `loggingcontract` 的 `dynamic_event` 管（有 SHA 封条）。

### 3.1 「只报一条最可行动的」

- 字段 key 既不是 snake_case、又不在字典里时，**只报格式**——改名之后字典问题自然消失，
  一个根因报两条只会让人以为是两个问题。
- 事件名末段复述等级时，**只报命名**，不再报"未登记"——修的时候名字就变了。

### 3.2 `locating_field_missing` 与「条件字段」

**同一个事件在 if/else 两支里带不同字段是正常设计**（有值时带、取不到时让字段缺席），
C04 的停播链路就是这么写的。所以这条规则的豁免判定**放在事件级**：

- 该事件在**任何**调用点都不带定位字段（`events[x] == []`）→ 整个事件豁免，
  登记册已经如实记下"这个事件本来就定位不到谁"（116 个）。
- 该事件在别处带、这个调用点不带 → **报**，除非写进 `locating_free_exceptions`。

⚠️ 收录例外的门槛与 `IDENT_BASE` 同一条：**能不能唯一指向"出事时第一个要查的对象"**。
`locating_free_exceptions` 只减不增；`TestLoggingCatalogRegenerate` 会把它整段带过去，
**生成器永远不会替你把例外删掉或加上**。

### 3.3 「无定位字段」的口径与报表脚本共享

`locatingFields`（Go）与 `scan-logging.py` 的 `IDENT_BASE`（Python）是**两份手写清单**，
`TestLoggingCatalogMirrorsScanScript` 逐项比对它们，任何一边多一个或少一个都直接失败。
事件名末段等级词集合（`levelEchoSegments` ↔ `EVENT_LEVEL_ECHO_RE`）同样比对。

**为什么宁可写一个跨语言测试**：报表说"无定位 35%"、门禁按另一套字段集判"通过"，
这两个数字就会开始说两件不同的事，而没人能一眼发现。

---

## 四、怎么改登记册

```bash
cd server && export PATH=/opt/homebrew/Cellar/go/1.25.6/bin:$PATH

# ① 门禁（日常）
go test -count=1 ./internal/loggingcatalog/

# ② 重新生成（只在**读过 diff** 之后跑）
UVP_LOGGING_CATALOG_UPDATE=1 go test ./internal/loggingcatalog/ -run TestLoggingCatalogRegenerate -count=1
```

**生成器存在的唯一目的是让"新增"变成一次看得见的 diff。** 它不能让任何人少看那一眼：

- 新增事件 → diff 里多一行 `"新事件": [...]`；
- 新增字段 → diff 里多一个字典项；
- 删掉某事件的最后一个定位字段 → `locating_field_lost` 先红，你得二选一：
  把字段加回来，或者**故意**改登记册（改动的就是那行 diff）。

⚠️ **反过来做会毁掉这个门禁**：先跑生成器、再看测试是否变绿。那样"新增"和"批准"是同一个动作。

---

## 五、它故意不管什么（边界）

1. **事件值必须是编译期常量** —— 归 `loggingcontract` 的 `dynamic_event`。
   本包读不出来就**跳过**，不重复报：同一事实两个门禁各报一次，就会变成
   "改了一处、另一处变红"的假故障。
   两个合法派生事件名的适配器（`logging.RepeatKey.Event`、
   `schedulerhelper` 的 `ZapJobLogger.emit`）由 `reviewed_adapters.json` 的
   **SHA-256 源码封条**管——改文件即失效，逼重新审。
2. **字段值安不安全** —— 归 `loggingcontract` 的 `unknown_field`。本包只看 key 的名字。
3. **等级对不对（Warn 通胀）** —— 归 C03。登记册**故意不记等级**：记了就会和 C03 的
   等级校准互相顶牛（改等级不该被门禁挡）。
4. **「事件在目录里有没有四格登记」** —— 归 C01。登记册现在只记了名字和定位字段并集；
   等 C01 的四格表落地，`events` 段会长出 `requires` / `level` / `trigger` 字段，
   `locating_free_exceptions` 那套"事件级豁免"会被**声明的维度**取代。
5. **`fields` 只增不删的诱惑** —— 由 `stale_field_dictionary_entry` 挡住：源码里不用了，
   字典里也得删，否则旧名字会以"还在字典里"的身份悄悄回来。

---

## 六、C09 落地时被门禁翻出来的两件事

**基线不是 0。** 第一次跑 `Findings`，报 6 条：5 条 `locating_field_missing` + 1 条
`unresolved_event`（后者已判定归 `loggingcontract`，见 §五.1）。逐条判定后：

| 调用点 | 判定 | 处置 |
|---|---|---|
| `hook.go` `denyPlayback` / `ignoreFlowReport` / `denyAutoOnDemand` | `stream_id` 拿不到时字段**故意缺席**（C06 判定），但**对端地址一直拿得到** | **补 `source_ip`** —— 与 `gb28181.hook.auth.rejected`、`gb28181.hook.keepalive.*` 同一处置（那两处早就在这么写，唯独 hook 拒绝路径漏了） |
| `hook.go` `server_started_node_unresolved` 的 else 支 | 连 `mediaServerId` 都没有 | **补 `source_ip`**，理由同上 |
| `recording/catalog_scheduler.go` `recordSchedulerFailure` | 调度器整体失败时还没有任何节点；硬塞 `node_id=0` 会把"入队失败"谎报成"0 号节点失败" | **登记为例外**（唯一一条），理由取 `platform-support.md` §5.1 的原判定 |

→ 净结果：**4 处补字段、1 条登记例外、门禁 0 findings**。

> 这正是 C09 想要的效果：**它不是"把已有做法写进文档"，而是"把还没做的地方翻出来"**。
> 第一版就翻出 hook 拒绝路径缺 `source_ip` —— 同一份代码里 keepalive 处理已经有了，
> 只有拒绝路径漏了。

---

## 七、已知盲区（登记在案）

1. **`fields` 攒进变量再展开**（`fields := []zap.Field{...}` + `logger.Info(msg, fields...)`）
   —— 本包按语法读不到，测试用例 `fields assembled in a variable are out of reach`
   就是把这个盲区**钉住**，免得以后有人以为它被管着。
   这一族由 `loggingcontract` 的 `unresolved_logger` 负责（它报的是同一件事）。
   > 具体的数字差：`scan-logging.py` 报 **唯一字段名 161**，登记册是 **160**。
   > 差的这一个就是脚本对 `[]zap.Field` 变量做**保守文件级合并**时算进来的字段
   > （这就是同一个盲区的另一半）。两边的 `IDENT_BASE` / 等级词集合由一致性测试锁住，
   > **字段全集不锁**——它本来就该有这一处差。
2. **事件名由数据字段驱动**（`zap.String("event", key.Event)`）—— 见 §五.1，
   由 `reviewed_adapters.json` 的 SHA 封条管，目前 4 个调用点（`logging.RepeatKey` ×1 /
   `ZapJobLogger.emit` ×3，后者又覆盖 4 个等级）。这些调用点实际发出的事件名里，
   `zlm.node.offline_persist_failed` / `zlm.thread_load.net_failed` /
   `zlm.thread_load.work_failed` **不在登记册的 330 里** —— 它们在源码里从不以字面量出现
   （`zlm.node.offline` 恰好在别处被字面量用过，所以在册）。这三条是**登记册的已知缺口**，
   不是"已经管住了"。
3. **`_test.go` 不扫** —— 与 `scan-logging.py` 同口径（测试里的 `fixture.*` / `acceptance.*`
   事件不入册）。C01.5 要求把这类事件移出业务命名空间，届时口径再看。

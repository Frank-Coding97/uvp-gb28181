# 日志治理进度表

> **目标**：出了问题，用户/运维能**定位到具体对象**。不为打印而打印。
>
> 本文档是这套治理的**唯一进度表**，完成一项勾掉一项。跨会话可续。

---

## 一、判据：一条日志合不合格

不分等级、不分模块，任何一条日志都先过这三问：

| # | 问题 | 不合格的表现 | 处置 |
|---|---|---|---|
| ① | **定位谁？** 设备 / 通道 / 平台 / 流 / 请求 | 没带定位字段，或带了但值是空/占位 | 补字段 |
| ② | **看到它要做什么？** | 答不出运维该做什么动作 | 降级为 DEBUG 或删除 |
| ③ | **这个信息别处说过没有？** | 同一次流程里第二遍说同一件事 | 合并，同一事实只说一次 |

**等级只反映「是否需要人介入」**——需要就 WARN/ERROR，不需要就 INFO/DEBUG。
等级失真（什么都打 WARN）会让 WARN 彻底失去信号价值，等同于没有报警。

---

## 二、治理分两层，不能混着做

| 层 | 内容 | 状态 |
|---|---|---|
| **第一层 · 管道** | 让日志有结构、有关联、有边界（脱敏 / 截断 / 落盘 / 关闭排空 / 上下文传播） | ✅ T01–T16 已完成 |
| **第二层 · 内容** | 让日志能定位、不啰嗦（事件目录 / 字段字典 / 等级 / 定位字段） | 🔄 进行中，见下表 |

**管道修好 ≠ 每条日志有用。** 第一层解决的是"能可靠地打出来、且不会泄漏/丢/爆盘"，
第二层解决的是"打出来的东西能不能用"。本文档两层都记，但勾选独立。

---

## 三、进度

### 第一层：管道建设（T01–T16）

判定方式（可复跑，权威）：

```bash
cd <repo> && for b in $(git branch --list "codex/logging-*" | sed 's/^[* +]*//'); do
  printf "%-38s 未进develop=%s\n" "$b" "$(git cherry develop $b | grep -c '^+')"
done
```

全部输出 `未进develop=0` → 该项成果已等价进入 `develop`（patch-id 匹配，哈希不同是 cherry-pick 所致）。

- [x] **T01** HTTP 访问清单归入 API 迁移任务
- [x] **T02** 配置快照 + 模块感知运行时
- [x] **T03** 字段脱敏 + 旧日志桥接（凭据 / 任意对象 / 超长输出）
- [x] **T04** 文件保留上界 + sink 故障上报
- [x] **T05** 启动期与 SIP 日志桥接
- [x] **T06** HTTP 访问关联 + 安全恢复
- [x] **T07** GORM 日志结构化 + 保留 DB 上下文
- [x] **T08** HTTP 基线与审计上下文传播
- [x] **T09** 国标 HTTP 与业务结果关联
- [x] **T10** 调度器执行作用域关联
- [x] **T11** 重复失败限流 + 共享维护
- [x] **T12** 关闭前排空应用工作（10 个子任务）
- [x] **T13** 部署模板与持久化契约
- [x] **T14** 媒体与辅助业务事件命名（7 个子任务）
- [x] **T15** HTTP 日志预算与负载边界
- [x] **T16** 隔离 MySQL 验收辅助

逐项档案（目标 / 验收叙述 / 产物 / 证据）见 [`pipeline-T01-T16.md`](./pipeline-T01-T16.md)。

### 第二层：内容质量

| 编号 | 治理项 | 状态 | 详情 |
|---|---|---|---|
| C01 | 事件目录（Event Registry） | ✅ **已完成**（2026-09-15；登记处由 C09 的 `registry.json` + `unregistered_event` 落地，本项收尾：拆 2 个"一名多义"事件、统一 1 条等级、判定不做常量收拢） | [event-catalog.md](./contracts/event-catalog.md) · [registry.md](./contracts/registry.md) |
| C02 | 字段字典（统一命名） | ✅ **已完成**（2026-09-15；风格统一由 C08 完成、门禁由 C09 完成。本项裁决"同义不同名"：收敛 **1 处**、登记 **5 组"伪别名"不许合并**） | [field-naming.md](./contracts/field-naming.md) |
| C03 | 等级校准（Warn 通胀） | ✅ **已完成**（2026-09-15，含 C03.③ `ERROR` 复核 70→20、C03.④ 假阴性复核；撤销 <10% 目标，改为三条可判定目标） | [contracts/levels.md](./contracts/levels.md) |
| C04 | `play` 链路定位契约 | ✅ **已完成 + 已实机验收**（2026-09-15） | [contracts/play.md](./contracts/play.md) |
| C05 | `cascade` 链路定位契约 | ✅ **已完成**（2026-09-15，门禁 cascade 部分归零） | [contracts/cascade.md](./contracts/cascade.md) |
| C06 | `gb28181` 核心链路定位契约 | ✅ **已完成**（2026-09-15，**门禁 2 → 0 全绿**） | [contracts/gb28181-core.md](./contracts/gb28181-core.md) |
| C07 | 媒体节点（`zlm`）定位契约 | ✅ **已完成**（2026-09-15，豁免清单 92 → 77） | [contracts/zlm.md](./contracts/zlm.md) |
| C08 | 平台支撑域与命名空间归属 | ✅ **已完成**（2026-09-15，豁免清单 77 → 37；camelCase 58 → 0） | [contracts/platform-support.md](./contracts/platform-support.md) |
| C09 | 机制固化：事件/字段登记册 + 门禁接入 | ✅ **已完成**（2026-09-15，门禁基线 6 → 0；`hook.*` 拒绝路径补 `source_ip`） | [contracts/registry.md](./contracts/registry.md) |

---

## 四、实测基线（2026-09-15，脚本全量扫描生产代码）

扫描口径：`server/**/*.go`，排除 `_test.go` / `internal/loggingacceptance` / `internal/loggingcontract`
**以及所有隐藏目录**，共 **351 个日志调用点**。（337 → 343（C06）→ 349（C07）→ **351**（C08）。
两个方向都要小心：
条件分支会把一条调用点拆成两支 → 分母变大不等于日志变多；而**没有任何 `zap.Field` 参数的
调用根本不计入**（`calls_in()` 要求段内有 `zap.`）→ 给它补上 `event` 会让分母变大、
"无定位"一起变大，是**看得见的更多，不是变得更多**。见下面 C07 后记。）

| 指标 | 现状 | 目标 |
|---|---|---|
| 无任何定位字段 | **121 / 351 = 34%**（172 → 167 → 146 → 152 → 125 → **121**，末段是 C09 补的 4 处 `hook.go` 拒绝出口） | < 10%（组件级事件豁免） |
| 等级分布 | **Warn 178（50.7%）** > Info 126 > Error 20 > Debug 27 | 见 C03 后记（原「Warn < 10%」已撤销；**比例不是指标**） |
| 业务链路 Warn 占比 | **54%**（业务域 248 条里 135 条 WARN） | 见 C03 后记 |
| 字段名唯一值 | **160 个**（= 字典 **159** + `event` 本身）。⚠️ device/channel/node 的"多种写法"经核实是**不同对象**（`device_id` 主键 vs `device_code` 国标编码、`server_id` SIP 身份 vs `node_id` 媒体节点），**不是别名** | ✅ 一本字典（[field-naming.md](./contracts/field-naming.md)） |
| 唯一 event 值 | **331 个**（269 → 272 → 287 → 330 → **331**：末个是 C03.③ 把 881 处调用共用的失败出口按状态码拆名） | 全部进目录 |
| 登记册门禁（C09） | **0 findings**（331 事件 / 160 字段 / 1 条带理由的例外） | 保持 0（**不是**"基线为 0 才正常"，见 C09 后记） |
| 事件等级唯一性（C03） | **0 findings**（331 个事件里没有一个跨等级） | 保持 0 |

分命名空间（无定位字段占比，**C08 起改为三分组**）：**业务域** 74/248 = 29%
（`gb28181` 25%、`zlm` 53%、`play` 40%、`cascade` 16%、`ptz` 8%）·
**平台支撑域** 30/85 = 35%（`db` 100%、`sysgenservice` 80%、`casbin` 50%、`auth` 40%）·
**进程框架域** 17/17 = 100%（全豁免）· **未归类** `其他` **0**、`(无 event)` **1**。
（`其他` 与 `(无 event)` 里大量是启动期/生命周期日志——它们的定位对象是"进程/配置"而非设备，
判据应另设，不能一并算作不合格。）

> ⛔ **2026-09-15 修掉一个会让全部指标虚高的口径漏洞：隐藏目录没被排除。**
> 仓库约定是「跨分支做事另建 worktree」，而 worktree 默认落在
> `.claude/worktrees/<name>/` —— 那是**整份 `server/` 的拷贝**。
> 旧脚本的 `SKIP_DIRS` 只管 `node_modules`/`.git`/`vendor`/`third_party`，
> 于是副本里的日志调用点被一并计入：实测 **486 vs 真实 351**，
> 唯一 event 值 331 → 358、唯一字段名 161 → 200 ——
> **看着就像"前几轮的治理成果全部回流了"。**
> 现在 `SKIP_HIDDEN_DIRS` 一律跳过以 `.` 开头的目录（顺带覆盖 `.venv`/`.vscode` 等 IDE 缓存）。
> → **指标突然大幅变差时，先怀疑口径，再怀疑代码**；跑之前确认工作区没有未排除的副本。

> ⚠️ **口径变化（2026-09-15）**：脚本补上了**事件名常量化**的读取能力。
> C01 的方向就是把事件名收敛成常量（`zap.String("event", cascadeVideoEventFailed)`），
> 而旧脚本只认字面量，会把这类调用点整条判成 `(无 event)` 并踢出业务命名空间 ——
> **指标会随治理推进而假跌**。补上之后按新口径重测：唯一 event 值 250 → **269**、
> `(无 event)` 78 → **51**、`cascade` 7 → **18**、`zlm` 16 → 17、`gb28181` 97 → 112。
> **多出来的 event 不是新写的，是原来就在代码里、只是脚本读不出来。**
> 两处工具增强都带上"为什么"记在 `scan-logging.py` 的注释里，改回去要连带它们的理由一起推翻。

> ⛔ **C03 撤销了「Warn < 10%」这个目标，而且是本次最该被记住的一条。**
> 计划写这个目标时的前提是"Warn 通胀 = 什么都打 WARN"，但把 149 个 Warn 调用点
> 逐条摆开之后，看到的是另一回事：
>
> | 分组 | 条数 | 该是什么 |
> |---|---|---|
> | 我方操作失败（`*_failed` / `*_timeout` / `*_incomplete`…） | 约 110 | **WARN 本来就对** |
> | 启动期装配降级（`zlm.registry.db_unavailable` 等） | 约 24 | **WARN 本来就对** |
> | 外部输入被正确拒绝（坏报文、权限不足、入参非法、幂等重复） | **16** | 本次降 `INFO` |
> | 占位/无内容诊断（`db.diagnostic`） | **3** | 本次降 `DEBUG` |
> | 纯流程叙述（脚本口径**外**的 3 条） | **3** | 本次降 `INFO` |
>
> **前两组占 90%。** 要凑出 < 10% 只能把它们一起降 —— 那等于把"断流失败""级联点播失败"
> 藏进 `INFO`，是**信息错误**，比通胀更糟（判据①：定位不到对象就排不了障）。
> 所以目标换成三条**判得动**的：① `Warn` 里不存在答不出动作的条目；
> ② **同一 `event` 等级唯一**（机检，基线 0）；③ `ERROR` 只用于"功能已不可用"（本轮未展开）。
> 可达成的数量结果：Warn 43% → **37%**，业务域 52% → **47%**，平台支撑域 20% → **12%**
> （`casbin` 33%→0、`db` 20%→0）。逐条判定正本见 [`contracts/levels.md`](./contracts/levels.md)。
>
> ⚠️ **降级不是"把不确定的往低打"，判定表在 `levels.md` 第三节，两个刻意的例外也在那里**：
> **身份认证失败**（`register.digest_failed` / `hook.auth.rejected`）保留 `WARN`
> —— 那是"对方没通过认证"，可能是凭据被改/被冒充，需要人核对设备配置；
> 而 `casbin.permission.denied`（**已认证**主体权限不足）降 `INFO`，因为返回 403
> 是设计内行为，没人会去"修"一个用户缺权限。**这两条看着像同一类，判定不同，是故意的。**
>
> ⚠️ **本次唯一的降级依据来自既有代码的自我陈述**：`app/utils/logging/sanitize.go` 的注释里
> 早就把 `ptz.response.unmatched` 写成 **"the highest-volume WARN in the tree
> (~600 lines per log file)"** —— 最高频的 WARN 恰好就是"对方的错"，说明这个等级桶
> 早在 C03 之前就已经被自己的最高频条目污染了。
>
> ⛔ **改等级有连带义务**（本次踩了两次，都在 `loggingcontract`）：
> ① `reviewed_legacy.json` 的豁免按 `file + function + method + message` 匹配且
> **要求恰好命中 1 个调用点** —— `Warn` 改 `Info` 会让那条豁免"命中 0 个"，
> 同一个调用点又因为"没有静态 `event`"以 `missing_event` 重新冒出来；
> ② `reviewed_adapters.json` 按**文件 SHA-256** 封条 —— 碰了被封印的
> `app/utils/gormhelper/log.go` 就报 `reviewed adapter source changed`。
> 这两处**不是障碍，是复查触发点**：改等级这件事值得被门禁看见一次。
>
> ⚠️ **C03.7 原本提的"消息文本不得是纯流程叙述"没有做** —— 那是自然语言判断，
> 正则分不出"已运行"和"已重试成功"。换成 **`event_level_divergence`**：
> 同一 `event` 出现两个等级时，任何"按事件名聚合"的统计都是假的（C04 第一个撞上 ——
> `play.reconcile.probe_failed` 两处 `WARN` 一处 `DEBUG`）。规则落在 C09 建的
> `internal/loggingcatalog/`，**不动 `loggingcontract`**；落地前把全仓唯一的跨等级事件
> `db.diagnostic` 一并降 `DEBUG`，于是这条规则**零例外**。
>
> ⚠️ **C03.③ `ERROR` 复核已完成（70 → 20）。** 上面的目标③当时标着"本轮未展开"，现在补上了。
> 结论是：`ERROR` 桶被**一个出口**污染 —— `app/controllers/common.go` 的
> `Common.Fail` / `FailAndAbort` 是所有控制器的失败兜底，原本**一个** `logger.Error`
> 覆盖 **881 处调用**，其中 **799 处（90.7%）走默认 400**
> （"设备不存在""预置位编号不合法""订阅有效期超范围"）。`ERROR` 在这里不是告警，是背景噪声。
> 改法：按状态码拆两支 —— 5xx → `http.operation_failed`（`ERROR`，**保留原名、语义收窄**），
> 其余 → `http.operation_rejected`（`INFO`，新名）。**"我方 HTTP 故障"第一次能按 event 名直接筛。**
>
> ⛔ **这一次 `Warn` 从 37% 涨回 50.7%，那不是回退。** 45 条从 `ERROR` 移到 `WARN`
> （关停阶段不完整 8 条 + 单次写库/查询/清理失败 37 条），调用点总数 351 一条没变 ——
> **是转移，不是通胀**；它们本来就是 `WARN` 的定义（"我方操作失败，功能降级但仍在跑"）。
> 本轮的目标从来不是压低 `WARN`，而是让 `ERROR` 重新等于"值班立刻起来看"。
> **`Warn` 占比不是指标** —— 这条在 C03 主体就已撤销（见上）。
>
> ⚠️ **口径提示**：`http.operation_rejected` 在指标表上只贡献 **+1 个调用点**，
> 运行时它覆盖 **799 处**。**指标数字估不出这次改动的实际收益。**
>
> 留 `ERROR` 的 19 条里只有 1 条需要解释：`audit.operation_log.persist_failed`
> —— 全仓唯一"主流程成功、记录永久丢失、且没有任何下游会因此报错"的失败，
> 判据第三条（**没有别的信号能暴露它**）就是为它加的。逐条判定表见
> [`contracts/levels.md`](./contracts/levels.md) 第九节。

> ⚠️ **C03.④ 假阴性复核（2026-09-15）：从 178 条 `WARN` 里翻出 3 条该是 `ERROR` 的，另有 2 条反向错配。**
> 它是 C03.③ 的**反方向** —— 前两轮都在问"有没有被高估的"，这轮问"有没有被低估的"。
> 难点是"**没有的东西看不出形状**"，所以没有靠通读，而是用两条可机检的判据：
>
> 1. **同语义族跨等级**（`event_level_divergence` 管不到的那一半）。门禁只查**同一个 event 名**
>    跨等级；而 `auth.login_audit.persist_failed`(WARN) 与 `audit.operation_log.persist_failed`(ERROR)
>    是**不同名、同语义** —— 前者恰好是被后者那句"全仓唯一一类主流程成功、记录永久丢失"
>    的注释漏掉的。按事件名末段（结果词）分组后得 **15 个跨等级语义族**，逐族判定。
> 2. **完全静默的失败**（连日志都没有）。扫全仓装配/启动/后台函数里
>    「`err != nil` 分支既不打日志、也不向上传播 `error`」的分支 —— **8 条候选全是假候选**
>    （项目自封装的出口 `FailAndAbort`/`writeError`，或 `errors.Join` 向上返回）。
>    **否定性结论也算结论**：装配路径的记录在 C07/C08 补 event 那一轮已经补齐了。
>
> 升 `ERROR` 的 3 条（每处都就地写了"别改回去"的理由）：
>
> | 事件 | 原 | 为什么 |
> |---|---|---|
> | `gb28181.sip.uac_init_failed` | WARN | 原文写"创建失败仅警告，注册仍可工作"，但后果是 **Catalog 自动触发 / DeviceInfo 查询 / 点播全部不可用** —— 属 C03.③ 第三组"SIP 子系统装配失败"（同组 `sip.start_failed` / `sip.config_load_failed` 都是 ERROR） |
> | `auth.login_audit.persist_failed` | WARN | 与 `audit.operation_log.persist_failed` **完全同构**（登录照常成功、审计行永久丢失、无下游信号）。`SysLoginLog` 就是审计行（`CleanupBefore` 注释原话 "immutable audit rows"） |
> | `auth.login_audit.panic` | WARN | panic 在全仓统一是 ERROR（另外 3 条都是），唯独这条在 WARN —— recover 之后同样无人察觉 |
>
> **反方向（降 `INFO`）2 条**：`gb28181.playauth.key_init_failed_auth_off` 与
> `…reload_key_init_failed_auth_off` —— 它们在 `if CurrentPlayAuthSettings().Enabled` 的 **else 支**，
> 即**鉴权本来就关着**，这次失败没有牺牲任何东西，答不出判据②的"降了什么"。
> 同一函数另一支（鉴权开着却没密钥）才是 `ERROR`。留着等于**每台没开鉴权的机器启动都多一条告警**。
>
> ⛔ **故意没升的几条，理由写在代码注释里**，防止后人按"长得像"跟风：
> - `gb28181.sip.broadcast_client_init_failed` —— 广播是**子能力**，且缺失时
>   `handleBroadcastInvite` 回 **503 "Broadcast Service Unavailable"** → 有替代信号。
> - `zlm.registry.load_failed`（走 deprecated 单节点路径）、
>   `gb28181.traffic.repository_assemble_failed`（上游有链式 Info/Warn）、
>   `setup.sip_reload_failed`（**把 `reloadedOk:false` 返回给前端**）。
>   `setup.sip_reload_failed` 尤其容易被误判成"SIP 挂了"：它只是 HTTP 请求内的失败，前端当场收到原因。
>
> 结果：`Warn 178 → 173`、`Error 20 → 23`、`Info 126 → 128`；
> **调用点总数 351 与唯一 event 数 331 都没动** —— 与 C03.③ 一样是**转移，不是新增**。
> 双门禁 0 findings 且**这次零连带**：改的调用点全都带静态 `event`，不在
> `reviewed_legacy.json` 的豁免清单里 —— 正好印证 §八 那条"连带只在改被豁免的调用点时才触发"。

> ⚠️ **C01 + C02（2026-09-15）的共同结论：两项的目标都已被 C09 达成，只剩收尾 ——
> 而收尾时各自翻出一个"计划本身错了"的地方。**
>
> | | C01 事件目录 | C02 字段字典 |
> |---|---|---|
> | 原目标 | 唯一登记处 + 新增/改名可发现 | 一本字典、一套命名 |
> | 实际由谁达成 | **C09**：`registry.json` + `unregistered_event` | **C08**（camelCase 58 → 0）+ **C09**（三条字典规则） |
> | 本项做的 | 拆 **2 个"一名多义"事件**（`gb28181.message.ptz_failed` → `…ptz_control_failed` / `…ptz_query_failed`；`lifecycle.shutdown_incomplete` 的第 3 处 → `lifecycle.shutdown_work_active`）+ 统一 1 条等级 | 收敛 **1 处**真别名（`body_len` → `body_bytes`） |
> | 计划里被推翻的 | "把 50 个局部常量收拢成 Go 包" —— 会让事件名有两个来源 | 那张"别名映射表"：6 组里 **5 组是伪命题**（`server_id` 是 SIP ServerID 不是 `node_id`、`peer` 不是 `source_ip`、`device_code` 不是 `device_id`…） |
>
> ⛔ **两项一起给出的教训**：**计划里的"改名 / 合并 / 收拢"建议，先去代码里核对语义再执行。**
> C06 的 `is_first`、C02 的 `server_id`、C01 的"常量收拢"都是同一类 ——
> 计划写在通读代码**之前**，而语义只藏在代码里。
> **合并两个不同对象的名字，是把"信息不完整"变成"信息错误"，比两个名字更糟。**
>
> ⚠️ **两项都刻意"少建一个维护点"**：C01 不收拢事件常量（JSON 名册已是唯一来源），
> C02 不手写"核心 20 个字段"表（字典从源码扫出）。**一个名字只能有一个定义点。**
>
> ⚠️ **C01.4 的"一名多义"检测只做人工裁决、不做门禁**：同一事件在不同调用点写不同消息
> 并不都是错的（带不带原因、带不带上下文），做成门禁会大量误报。
> 门禁只留能零例外的那条（`event_level_divergence`，同一个 `event` 名跨等级）。
>
> 数字影响（接在 C03.④ 之后）：唯一 event **331 → 333**、唯一字段名 **160**
> （字典 **159** + `event` 本身）、`Warn 173 → 172`（`scheduler.example.canceled` 降 `INFO`）。
> ⚠️ **顺带修了一处门禁可读性**：`stale_field_dictionary_entry` 原先**报不出是哪个字段**
> —— 该 finding 没有 `File`/`Line`，而 `Finding.String()` 只打印「位置 + 类型 + 描述」。
> 收敛 `body_len` 时实测撞上，得靠读 diff 反推。已让 `String()` 带上 `event=` / `field=`。

> ⚠️ **另外修掉一个会让全部指标虚高的口径漏洞（2026-09-15）**：`scan-logging.py` 的
> `SKIP_DIRS` 原先不含隐藏目录，而仓库约定"跨分支另建 worktree"会落在
> `.claude/worktrees/<name>/` —— 那是**整份 `server/` 的拷贝**，被一并计入扫描：
> 实测 **486 vs 真实 351**（唯一 event 331 → 358、字段 161 → 200），
> 看着像"前几轮成果全部回流"。现在一律跳过 `.` 开头的目录。
> **指标突然大幅变差时，先怀疑口径，再怀疑代码。**

> ⚠️ **C09 加的是"门禁"，不是"文档"。它的第一批产出是 6 条基线 findings，不是 0 条。**
> 如果一项治理做完之后门禁直接是绿的，那说明**它只把已有做法写了下来**。
>
> 逐条判定后的净结果：**4 处补字段 + 1 条登记例外**。
> 补的是 `hook.go` 三条拒绝出口与 `server_started_node_unresolved` 的 else 支的 `source_ip` ——
> 同一份 `hook.go` 里 `keepalive.*` 早就在写 `source_ip` 并附了理由（"它是唯一能回答
> '是不是同一台机器在反复发'的线索"），**唯独拒绝路径漏了**。这就是"逐条判定"翻出来的东西：
> 不是新知识，是**同一份代码里两处不一致**，只有把调用点逐条摆出来才看得见。
>
> **两处刻意没做成"规则"**（做了会变成第二个需要治理的对象）：
>
> 1. **C09.3 从"必需定位字段"改成"定位字段只许增不许减"。** 计划说读 C01 的四格表，
>    但四格表只登记了 `play` / `cascade` / `gb28181` 核心约 90 类，330 个事件里绝大多数没有声明；
>    硬写"必需字段"会立刻被条件分支打脸 —— `play` 停播、`hook` 拒绝出口都是
>    **if/else 两支带不同字段**（有值时带、取不到时让字段缺席），这是 C04 明确设计过的。
>    改成"记现状 + 禁止退回"之后不需要人工裁决，而它挡的正是 C04–C08 的成果回流。
> 2. **`unresolved_event`（事件值读不出）没有做成规则。** 探针实测：`zap.String("event", name)`
>    → `loggingcontract` 的 `dynamic_event` 已经红；能合法派生事件名的两个适配器
>    （`logging.RepeatKey`、`ZapJobLogger`）也早有 SHA-256 封条。
>    **同一事实两个门禁各报一次，就会变成"改了一处、另一处变红"的假故障。**
>
> ⛔ **唯一能毁掉这个门禁的操作**：先跑生成器（`UVP_LOGGING_CATALOG_UPDATE=1`）、
> 再看测试绿没绿。生成器存在的目的是把"新增事件 / 新增字段 / 丢掉定位字段"
> 变成一次看得见的 diff；**diff 就是审查本身**。反过来做，等于"新增"和"批准"是同一个动作。
>
> ⚠️ **口径共享靠测试而不是靠自觉**：`locatingFields`（Go）与 `scan-logging.py` 的
> `IDENT_BASE`（Python）是两份手写清单，`TestLoggingCatalogMirrorsScanScript`
> 逐项比对，任何一边多一个少一个都直接失败。理由很直白：报表说"无定位 35%"、
> 门禁按另一套字段集说"通过"，这两个数字就开始说两件不同的事，而没人能一眼发现。

> ⚠️ **C08 后「其他」桶 97 → 0，这不是治理成果，是"分类补全"。**
> 旧版只有一个 `BUSINESS_NS` 白名单（11 个），命中不了的**一律进「其他」桶** ——
> 于是 97 条合法命名空间（`scheduler.*` `models.*` `casbin.*` `auth.*` `codegen.*` …）
> 全被算成"未归类"，看着像命名混乱。逐条判定后确认：**97 条里没有一条是裸前缀**。
> C08 把它拆成三组（业务域 / 平台支撑域 / 进程框架域）+ 「未归类」兜底，
> 于是问题从**一个 71% 的数字**变成**看得见的具体位置**：
> `db` 100% · `sysgenservice` 80% · `casbin` 50% · `auth` 40% · `codegen` 20%。
> `has_loc`（无定位判定）与命名空间分组完全无关 —— 补分组前 `scheduler.demo.started`
> （带 `job_id`）就已经是"有定位"，只是被错算进「其他」。
>
> ⚠️ **但补分组会盖住事件名问题**（`sysaffix.upload.warn` 归到 `sysaffix` 后不再显眼）
> → 所以 C08 另设**独立指标 ⑤「事件名末段复述等级」**：
> `.warn` / `.error` / `.info` 说的是等级、不是"发生了什么"。
> 判定口诀：**换掉它之后事件还说不说得清发生了什么** ——
> `sysaffix.upload.warn` 换掉后是 `sysaffix.upload`（说不清是成功还是失败）→ 算；
> `http.panic` 换掉后什么都不剩 → **不算**（`.panic` 是事件语义，不是等级）。
> C08 修 27 条，指标 ⑤ **28 → 0**。
>
> 本项其余成果：
>
> 1. **非设备类定位维度**（`IDENT_BASE` +10）：`username` / `role_id` / `parent_role_id` /
>    `menu_id` / `template_name` / `template_path` / `file_path` / `operator_id` /
>    `upload_id` / `table_name`。收录标准**没放宽** —— 仍问"能不能唯一指向出事时第一个
>    要查的对象"，所以 `phase` / `status` / `dialect` / 泛化的 `path` **一律不收**。
> 2. **`(无 event)` 40 → 1**：bootstrap 31 条（`gb28181.lifecycle.*` / `cleanup.*` /
>    启动期降级）+ `scheduler/register.go` 5 条 + `ginhelper` 4 条。
>    剩 1 条是 C07 已登记的动态事件名盲区。
> 3. **camelCase 58 → 0**（57 处 / 12 文件）。⚠️ 只匹配 `zap.<Method>("camelName"` ——
>    `nodeId`/`channelId`/`serverId` 等**同时也是 HTTP API 的 JSON 字段名**，
>    全局字符串替换会破坏接口契约。
> 4. **修掉 C06 遗留红 `TestLoggingGBHTTPContextWiring`**：
>    `cascade/controller/management.go:166` 把 `*gin.Context` 传给 `context.Context` 参数。
>    `gin.Context` 实现了该接口所以**编译通过**，但它丢掉了标准 request context 里挂着的
>    `request_id`/`execution_id` —— 日志会失去请求级关联。
>
> ⛔ **一条不能做的"优化"**：`scheduler.result.persist_failed` **不能补 `job_id`**。
> 它由 `logging.WithIdentity` 注入 core，而 `runtimeCore.Write` 里有
> `if ... || c.bound[f.Key] { continue }` —— 调用点重复写同名字段会被**丢弃**。
> 这是**第三族"静态不可见"**（前两族是 `[]zap.Field` 与 `traceFields ...`；
> **前两族能改写消掉，这一族连补都补不了**），只能登记为脚本已知盲区。

> ⚠️ **C07 后 `zlm` 桶"17 → 32、无定位 4 → 17"，这是口径变化，不是退步。**
> 原来 `app.ZapLog.Warn("消息")` 这类**没有任何 `zap.Field` 参数**的调用，
> 脚本的 `calls_in()` 根本不把它算作"日志调用点"（要求段内有 `zap.`）。
> C07 给这些启动期降级日志补上 `event` 之后，它们第一次进入统计 ——
> **脚本看见了原本就存在的东西**（与上面"事件名常量化导致指标假跌"是同一族现象，方向相反）。
> 同一批改动也让全局调用点 343 → 349、无定位 146 → 152。**判断"变好还是变坏"要看
> 补的是不是真信息，不能看数字。**
>
> 本项的实质成果有三件：
>
> 1. **`zlm.media_online.*` 三条补上 `node_id`** —— `c.node.ID` 一直在手上
>    （`Client` 持有绑定节点），却只打了 `endpoint`。同模块其他事件（`probe.*` /
>    `node.offline` / `scheduler.persist_failed`）全都带节点主键，唯独这三条不带，
>    导致"按节点聚合 API 失败率"做不到。
> 2. **发现并利用豁免清单的自我校验**：`reviewed_legacy.json` 要求每条豁免**恰好命中
>    1 个调用点**，所以给日志补 `event` 之后必须**同步删掉对应豁免条目**，
>    否则门禁报 `legacy_exception_mismatch`。本次 **92 → 77，只减不增**。
> 3. **17 条装配期降级事件从"裸消息"变成可检索事件**（`zlm.registry.*` /
>    `zlm.scheduler*.`* / `zlm.client.unavailable`）。DB 不可用会让整个媒体面退到
>    deprecated 单节点路径 —— 这是启动期最该被看见的降级，原来连事件名都没有。
>
> 一条要记住的反面结论：**`zlm.scheduler.buffer_full` 不能补 `node_id`**。
> 丢弃是**队列级**事件、每 100 条才采样一行，打出来的那个 `node_id` 只是"碰巧排第一"
> 的节点 —— 会把"所有节点都在丢"谎报成"这个节点在丢"，**从"信息不完整"降级成"信息错误"**。
> 判定已用契约测试锁住（断言 `NotContains(node_id)`，见 `contracts/zlm.md` §四）。

> ⚠️ **C06 后"无定位"从 167 掉到 146（−21），并且门禁第一次全绿。**
> 但**数字不是本项的主要成果**，两件事更值得记：
>
> 1. **`traceFields ...zap.Field` 变参传播被消除。** `catalog.go` / `deviceinfo.go` 原本
>    `logger.With(traceFields...)`：**运行时字段在、静态读不到** —— 与 C05 的
>    `fields []zap.Field` 容器是同一族反模式。改成显式参数后，`catalog` / `deviceinfo`
>    共 13 条日志一次性从"不可验证"变成逐条可验证。**"字段有没有"和"字段能不能被验证"
>    是两件事，这一族反模式专门破坏后者。**
> 2. **门禁 2 → 0。** 最后两条红是 `device_delete.go` 缺 `event` —— 那两条日志恰恰是
>    "幽灵设备"（上级平台目录里撤不下来的设备）排查的第一站：按事件名搜搜不到、
>    字段名是 camelCase 聚合不认。门禁转绿后，**CI 恢复才有前提**。
>
> 另一个要记住的教训：**C06 计划把 `is_first` 改名 `catalog_triggered` 是错的**。
> 核实代码后发现两者不等价（`catalog_triggered` 还受 `SyncChannelsOnOnline()` 约束），
> 改名会让日志在开关关闭时**谎报已同步** —— 从"信息不完整"变成"信息错误"。
> 处置改为**新增字段**。**计划里的"改名"建议一律要先去代码里核对语义再执行。**

> ⚠️ **`其他` 桶 71% 是当前最大的治理空白**：全仓仍有大量事件名不带命名空间前缀
> （`audit.*` / `auth.*` / `casbin.*` / `codegen.*` / `db.*` / `device.*` / `setup.*` …），
> 它们全部掉进"其他"，**在按命名空间看的任何指标里都看不见**。
> C06 只修了 `catalog.*`（因为对称文件 `deviceinfo_trigger.go` 已经带了前缀，属"同一模块
> 两个镜像文件必须一致"）。**"命名空间归属"应在 C08 当独立议题处理。**

> ⚠️ **C05 后"无定位"仍是 167，但这不等于没进展。** 变化在**结构**上：
> `cascade` 桶从 15 条"其中 9 条藏在一个 helper 里、静态不可见"变成 **18 条逐条可验证**，
> 其中只有 3 条没有定位字段，且全部是**进程级豁免**（详见
> [`contracts/cascade.md`](./contracts/cascade.md) §四）。指标数字相同是因为
> 脚本对"变量展开"的保守合并本来就把那 9 条算成了"有定位"——**假阳性掩盖了真问题**。

> ⚠️ **`play` 的 40% 是它的下限，不是欠账**：剩下 8 条 = 7 条**组件级事件**（豁免）
> + 1 条轮次级 `round_completed`。判据必须分层——给 `play.reconcile.stopped` 硬塞
> `device_id` 属于"为了指标好看而打印"（判据②）。详见
> [`contracts/event-catalog.md`](./contracts/event-catalog.md) §7.3。

> ⚠️ 167 里有 **1 条是扫描器盲区**：`gb28181.play.stop_requested` 的定位字段改由
> `stopLogFields()` 组装后，脚本的正则展不开（把函数名当成了变量名），运行时它**带**
> `device_id`/`channel_id`。判断"是否是回退"前先看 `contracts/play.md` §2.2.5。
> **别为了指标好看去放宽脚本判定** —— 那会把"条件省略"也算成达标。

> ⚠️ **另有一类"看不见的损耗"不在上表里**：被脱敏规则整个吞掉的字段。
> `[text omitted]` 在单个日志文件里出现 **600 次**，**全部来自 `reason` 一个键**——
> 扫描脚本与门禁都把这种字段算作"已带"，所以指标是绿的而排障是瞎的。
> 已于 2026-09-15 修复，见 [`contracts/sanitize-policy.md`](./contracts/sanitize-policy.md)；
> 复核命令见 [`testing-guide.md`](./testing-guide.md) 的 L3.2。

**字段命名风格**（按出现次数）：`snake_case` **496** + 单段小写 **591**（`event` / `stage` 这类）
—— **`camelCase` 与驼峰大写 ID 均已归零**。
（C04 把 `play` 链路的 18 处大写 ID 与 6 个 camelCase 统一后，大写 ID 23 → 5、唯一字段名 173 → 161；
**C08 后 camelCase 58 → 0**（57 处 / 12 文件）；**C09 用纯格式规则 `^[a-z][a-z0-9_]*$` 把它永久钉住** ——
目标"只剩 snake_case"**已达成**。）

**复跑（权威口径）**：

```bash
cd server
# 全量指标
python3 ../docs/logging-governance/scan-logging.py --root .
# 逐模块核对（--filter 子串 + --limit 0 看全量明细）
python3 ../docs/logging-governance/scan-logging.py --root . --filter play --limit 0
```

> ⚠️ **只看汇总数字会漏东西。** C04 收尾时就是靠 `--filter play --limit 0` 把 play 链路
> 13 条"无定位"**全部列出来逐条判定**，才发现契约清单里漏了 `play.attempt_begin_failed` /
> `attempt_finish_failed`（值就在作用域里、白白没打）。**逐条判定才是治理。**
同一个模块内部就不统一 —— `play/service.go` 写 `stream_id`，`play/reconciler/check.go` 写 `streamID`。

> **两个必须知道的口径问题**（它们直接决定你怎么读上面这些数字）：
>
> **1. 命名不统一会让统计本身失真。** 早期版本漏认驼峰大写 ID，一度把 `play` 误判为 **95%** 无定位
> （实为 50%）。**这个误判本身就是最强证据**——字段命名已经乱到连一个专门为它写的脚本都认不出
> 是同一个东西。工具认不出，门禁认不出，人 grep 同样会漏。这就是 C02 为什么必须做。
>
> **2. ~~有 9 条日志实际带了定位字段，但静态看不见。~~ → ✅ 2026-09-15 已解决（C05 + C06）。**
> 这是**一族反模式**，两个变体分别在 C05 / C06 处理：
>
> | 变体 | 位置 | 处置 |
> |---|---|---|
> | `fields []zap.Field` 容器 + `append(...)...` 展开 | `cascade_invite_handler.go` 的 `log()` helper | 拆成 **9 个具名方法**（C05） |
> | `traceFields ...zap.Field` 变参 + `logger.With(traceFields...)` | `handler/catalog.go` / `handler/deviceinfo.go` | 改成**显式参数**（C06） |
>
> 两者的共同点：**运行时输出是对的**，但字段名在**日志语句所在的函数里读不到** ——
> 它藏在调用点。于是脚本判"无定位"、门禁的字段可验证性检查看不到。
> 分别见 [`contracts/cascade.md`](./contracts/cascade.md) 与
> [`contracts/gb28181-core.md`](./contracts/gb28181-core.md)。
>
> 教训留在这里：**「字段有没有」和「字段能不能被验证」是两件事**，后者不做掉，门禁就永远红着，
> 而红着的门禁等于没有门禁。**看到"字段其实是有的"，要先问一句"能不能被验证"。**

---

## 五、怎么用这份文档

1. **每做一项，勾一项**，并在该项下面写一行日期 + 提交号 + 复跑命令的结果。
2. **完成判据必须可复跑**——这是从 T 系列学到的（它的每个提交都带 RED/GREEN 叙述，
   验收就是测试）。不接受"我看了觉得做完了"。
   **四层验收的具体命令与判据见 [`testing-guide.md`](./testing-guide.md)**：
   L1 静态门禁 → L2 自动化回归 → L3 日志形态 → L4 场景实机（含负面判据"防假达标"）。
3. **不要为了勾选而补字段**。回到判据 ②：答不出"看到它要做什么"的日志，
   正确处置是删掉，不是给它补个 `device_id` 让它看起来合格。
4. 约束：`internal/loggingcontract/policy.go` 已 1739 行、契约包 4517 行。
   新增规则前先想清楚放哪，别让它变成第二个需要治理的对象。
   ✅ **C09 遵守了这条**：规则放进新的 `internal/loggingcatalog`（纯语法、`go/ast`、
   与 `loggingcontract` 互不 import），`policy.go` 一行没加。
   ✅ **C03 也遵守了这条**：`event_level_divergence` 也进 `internal/loggingcatalog`
   （`Site` 多带一个 `Level` 字段即可），**没有**回到 `policy.go` 加规则。
5. **改等级不是改一行**。`Warn` ↔ `Info` 会连带破两处门禁基线
   （`reviewed_legacy.json` 按 `method` 匹配、`reviewed_adapters.json` 按文件 SHA 封条），
   详见 [`contracts/levels.md`](./contracts/levels.md) §八。看到这两处红，去核对，别去绕过。

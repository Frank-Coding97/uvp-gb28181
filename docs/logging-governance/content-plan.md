# 第二层 · 内容质量治理清单（C01–C09）

判据见 [README 第一节](./README.md#一判据一条日志合不合格)。
**每一项都必须有可复跑的完成判据**——这是从 T 系列学来的（它的每个提交都带 RED/GREEN 叙述，验收就是测试）。
统计口径与复跑方法见 [`scan-logging.py`](./scan-logging.py)。

---

## C01 · 事件目录（Event Registry）· ✅ 2026-09-15 完成

**目标**：250 个 event 有唯一登记处，新增/改名/删除有人能发现。

**实际结果：331 → 332 个事件进了 `internal/loggingcatalog/registry.json`，
`unregistered_event` 门禁负责"新增/改名有人能发现"。** 逐项判定见文末勾选区。

**好消息：基础已经有一半。** 仓库里**已经存在 50 个事件常量**，只是散落在 9 个文件里各自定义：

```
app/gb28181/play/service.go:121-132          playEvent* (12 个)
app/gb28181/snapshot/service.go:15-17        snapshotEvent* (3)
app/gb28181/zlm/heartbeat/watcher.go:15-18   *Event (4)
app/gb28181/cascade_runtime.go:36            cascadeCredentialKeyWarningEvent
app/gb28181/cascade_invite_handler.go:128    cascadeVideoEvent*
app/gb28181/playauth/signer.go
app/utils/logging/realtime.go / repeat.go    （框架自身）
```

所以这不是"引入新机制"，是**把已有的好做法收拢 + 补全 + 加校验**。

**做法**：

1. 建 `app/utils/logging/logevent/`（或 `internal/logevent`）作为唯一登记处。
2. 每个事件登记**四格**——不是只有名字：

   | 字段 | 含义 | 例 |
   |---|---|---|
   | event | 事件名 | `gb28181.play.media_ready` |
   | 定位维度 | 出问题要定位谁 | `stream` + `channel` + `node` |
   | 必需字段 | 该维度必备字段 | `stream_id, channel_id, node_id` |
   | 等级 + 触发条件 | 什么级别、什么时候打 | `INFO` / 成功拉起一路流时打一次 |

   **填不出"等级 + 触发条件"的事件，说明它本来就不该存在**——填表过程本身就是一次清理。
3. 用 `scan-logging.py` 从现网代码生成 250 条初稿，人工裁决（合并同义、删噪声、补缺失）。

**勾选** —— ✅ **2026-09-15 完成**。结论：本项的目标是"**250 个 event 有唯一登记处，
新增/改名/删除有人能发现**"，**这两点由 C09 落地**（`registry.json` + `unregistered_event` 门禁），
C01 剩下的只是收尾。逐项判定如下。

> ⚠️ **落地形态变了三处，都是"少一个必须同步的地方"**：登记处不是 Go 包而是 JSON 名册、
> 常量不收拢、四格表的另三格不重复登记。理由写在 C01.1/C01.2 里。 
> **教训：一个名字只能有一个定义点。** "Go 常量 + JSON 名册"看起来更规范，
> 实际是同一个事实两处存 —— 而门禁本来就**读得懂源码里的字面量和常量**
> （`scan-logging.py` 的 `CONST_EVENT_RE` + `const_strings()`），常量带来的增量只有"改名时多一道引用检查"。

- [x] C01.0（前置，2026-09-14）`play` 链路 **45 类事件**已按四格登记 →
      [`contracts/event-catalog.md`](./contracts/event-catalog.md)。
      这是 C01 的种子，同时产出了一个关键概念：**定位维度分层**
      （`device` / `stream` / `node` / `component`）——没有它，"组件级事件该不该补 `device_id`"就无据可依。
- [x] C01.1 确定登记处的包路径与数据结构 —— **形态变了**：用 C09 的
      `internal/loggingcatalog/registry.json`，**不是**把 `event-catalog.md` 迁成 Go 包。
      迁成 Go 包 = 同一份事件名在**常量**和**名册**里各存一次，
      而门禁 `unregistered_event` 已经能直接从源码比出"这个 `event` 值在不在名册里"。
- [x] ~~C01.2 迁移已有 50 个局部常量~~ —— **判定不做**，理由同上。那些常量
      （`playEvent*` / `snapshotEvent*` / `*Event`）留在原处没有问题：门禁读得懂它们，
      收拢反而让"事件名"有了第二个来源。
- [x] C01.3 生成初稿 —— 实测 **331 → 332 条**，生成器是
      `UVP_LOGGING_CATALOG_UPDATE=1 go test ./internal/loggingcatalog/ -run TestLoggingCatalogRegenerate`
      （**先判定、后跑生成器**，diff 就是审查）
- [x] C01.4 人工裁决：合并 / 删除 / 补全 —— 本次做了它的**可机检形态**：
      「**同一 `event` 名对应多条不同消息**」= 一名多义。全仓 332 个事件里只有 **5 个**，逐条判定：

      | 事件 | 判定 |
      |---|---|
      | `gb28181.message.ptz_failed` | **拆** → `…ptz_control_failed` + `…ptz_query_failed`（DeviceControl 应答 vs 一批查询应答，是两种报文） |
      | `lifecycle.shutdown_incomplete` | **拆**第 3 处 → `lifecycle.shutdown_work_active`（"还有这些组件的活没跑完" ≠ "Stop 返回了 error"；前者连 `error` 字段都没有） |
      | `gb28181.hook.keepalive.process_failed` ×2 | 保留（同一语义，第二条只是把原因写进消息） |
      | `gb28181.hook.rtp_timeout_stop_failed` ×2 | 保留（同一语义，措辞略异） |
      | `play.reconcile.probe_failed` ×3 | 保留（C04 已裁决：三条都降 `DEBUG`） |

      顺带统一 `scheduler.example.canceled` `WARN` → `INFO` —— 镜像文件 `demo_executor.go`
      同一支本来就是 `INFO`，而"任务被取消"是判据④"什么都没发生"。
      ⚠️ **这条规则只做人工裁决、不做门禁**：同一事件在不同调用点写不同消息并不都是错的
      （带不带原因、带不带上下文），做成门禁会大量误报 —— 门禁只做能零例外的那条
      （`event_level_divergence`，同名跨等级）。
- [x] C01.5 测试与示例事件移出业务命名空间 —— **实测基本已达成**：`_test.go` 本来就被扫描器排除
      （`calls_in()` 只在非测试文件跑）；`registry.json` 里带测试语义的只剩 `legacy.log` 一条，
      而 `legacy` 已在**进程框架域**；示例执行器的事件在 `scheduler.`（**平台支撑域**）下。
      没有需要迁移的。
- [x] C01.6 门禁校验：调用点的 `event` 值必须在目录中 —— `unregistered_event`（C09 建的）。
      本次拆分**立刻被它挡了一次**（3 条），这正是门禁该有的反应；
      连带还报了 2 条 `locating_field_lost`（旧名 `ptz_failed` 已无调用点）——
      两条一起在 diff 里被看见，然后才跑生成器。

---

## C02 · 字段字典（统一命名）· ✅ 2026-09-15 完成

**目标**：一本字典，一套命名。

**结论写在前**：**计划里那张"别名映射表"大半不成立。** 逐组去代码里核对后，
6 组假设里 **5 组是伪命题** —— 那两个名字指的是**不同的对象**（最典型：
`server_id` 是 **SIP ServerID**，不是 `node_id`；`peer` 是 **SIP 复合身份**，
不是 `source_ip`）。真正需要合并的"同义别名"只有 **1 处**
（`body_len` → `body_bytes`）。判定正本见
[`contracts/field-naming.md`](./contracts/field-naming.md)。

> ⚠️ 与 C06 的 `is_first` / `catalog_triggered` 是同一类教训：
> **计划里的"改名 / 合并"建议，一律先去代码里核对语义。**
> 合并两个不同对象的名字 = 把"信息不完整"变成"信息错误"。
>
> 风格统一那半边由 **C08** 做掉（camelCase 58 → 0），门禁那半边由 **C09** 做掉
> （`field_not_in_dictionary` / `field_name_not_snake_case` / `stale_field_dictionary_entry`）
> —— 本项只收尾"同义不同名"的裁决。

**原计划（留档）**：173 个唯一字段名；命名风格三套并存——`snake_case` 269 / `camelCase` 95 /
驼峰大写 ID（`streamID`）23；同一语义多写法：`device_id`(63) / `deviceId` / `deviceCode` / `deviceID`；
`channel` 5 种、`node` 5 种（含 `media_server_id`、`server_id`）。做法是
"先定核心 20 个字段 → 出别名映射表 → 按模块批量改"。

⚠️ **下面这三步是原计划，照做会犯错**（第 1 步表里的 5 组"别名"经核实是不同对象）。

**做法（原计划，仅留档）**：

1. 先定**核心 20 个**字段（覆盖 80% 调用点），别一上来搞 194 个。

   | 语义 | 规范名 | 已知别名（待合并） |
   |---|---|---|
   | 设备 | `device_id` | `deviceId` `deviceCode` `deviceID` |
   | 通道 | `channel_id` | `channelId` `channelCode` `channelID` |
   | 流 | `stream_id` | `streamId` `streamID` |
   | 节点 | `node_id` | `nodeId` `nodeID` `server_id` `media_server_id` |
   | 平台 | `platform_id` | `platformId` |
   | SIP 会话 | `call_id` | `callId` |

2. 出一张**别名映射表**（旧名 → 规范名），作为批量改名的输入，也是门禁的拒收清单。
3. 按模块批量改（脚本辅助），每改完一个模块复跑扫描。

**完成判据**：命名风格扫描只剩 `snake_case`；别名映射表中每个旧名的出现次数归零。

**勾选**：

- [x] C02.1 定核心规范字段 —— **形态变了**：不手写"核心 20 个"表，
      而是让 **C09 的字典**成为唯一来源（**159 条**，`registry.json` 的 `fields` 段）。
      手写 20 条等于同一个名字两处定义；而字典是**从源码扫出来的**，
      `field_not_in_dictionary` 保证"新名字必须显式登记"。
      规范本身（"一个名字一件事""**单位跟值类型走**"）写在 `field-naming.md` §一、§五。
- [x] C02.2 出别名映射表 —— **做了，结论是"表基本是空的"**：

      | 别名 | 规范名 | 现状 |
      |---|---|---|
      | `body_len` | `body_bytes` | ✅ **本次收敛**（2 处，`hook.go`） |
      | `deviceId` / `channelId` / `streamId` / `streamID` 等**大小写变体** | `device_id` / … | ✅ C08 已清零（58 → 0） |
      | `deviceCode` / `channelCode` / `server_id` / `media_server_id` / `peer` / `state` | ❌ **不是别名** | 登记在 `field-naming.md` §三，**不许合并**（各自指向不同对象） |

- [x] C02.3 改 `play`（见 C04）—— 2026-09-14：`reconciler` 18 处 + `snapshot` 4 处 → snake_case；
      `play` 链路 camelCase / 大写 ID **清零**。⚠️ 顺带登记的例外：`reuse_cleanup` 用
      `stale_stream_id`（语义确实不同，**不能并入 `stream_id`**）
- [x] C02.4 改 `cascade` —— **由 C08 完成**（`platformId` → `platform_id` 等在 C05 已改，
      其余 camelCase 归入 C08 的 57 处）
- [x] C02.5 改 `gb28181` 核心 —— **由 C08 完成**
- [x] C02.6 改 `zlm` / `ptz` / 其余 —— **由 C08 完成**。⚠️ 且**不能按名字硬扫**：
      `nodeId` / `channelId` / `serverId` 同时是 **HTTP API 的 JSON 字段名**，
      只能匹配 `zap.<Method>("camelName"`，全局替换会破坏接口契约
      （见 `contracts/platform-support.md` §五）
- [x] C02.7 门禁校验：字段名必须在字典中 —— **由 C09 完成**（三条规则：
      `field_not_in_dictionary` / `field_name_not_snake_case` / `stale_field_dictionary_entry`）

**顺带修的一处门禁可读性**：`stale_field_dictionary_entry` 原先**报不出是哪个字段** ——
这条 finding 没有 `File`/`Line`，而 `Finding.String()` 只打印「位置 + 类型 + 描述」。
收敛 `body_len` 时实测撞上，得靠读 diff 才能反推。已让 `String()` 带上 `event=` / `field=`。

**完成判据（复跑）**：命名风格扫描只剩 `snake_case` + 单段小写 `event`；
`grep -rn '"body_len"' app/` 无输出；`go test ./internal/loggingcatalog/` 0 findings。

---

## C03 · 等级校准（Warn 通胀）

**✅ 已完成（2026-09-15）** —— 判定正本见 [`contracts/levels.md`](./contracts/levels.md)。

**原计划**：Warn 占比 **54% → < 10%**，逐命名空间核对（`gb28181` 的 Warn 占 56% 是重灾区）。

**实测后的结论：< 10% 这个目标不成立，已撤销。** 把 149 个 Warn 调用点逐条摆开：

| 分组 | 条数 | 该是什么等级 |
|---|---|---|
| A 我方操作失败（`*_failed` / `*_timeout` / `*_incomplete`…） | 约 110 | **WARN 本来就对** |
| B 启动期装配降级（`zlm.registry.db_unavailable` 等） | 约 24 | **WARN 本来就对** |
| C 外部输入被正确拒绝（坏报文 / 权限不足 / 入参非法 / 幂等重复） | **16** | 降 `INFO` |
| D 占位无内容诊断（`db.diagnostic`） | **3** | 降 `DEBUG` |
| E 纯流程叙述（脚本口径**外**的无字段调用） | **3** | 降 `INFO` |

A + B 占 90%。凑出 < 10% 只能把它们一起降，那等于把"断流失败""级联点播失败"
藏进 `INFO` —— **信息错误比通胀更糟**（判据①：定位不到对象就排不了障）。

**新目标（三条，判得动）**：

- ① `Warn` 里不存在"答不出动作"的条目（人工复核 + `levels.md` 判据）✅
- ② **同一 `event` 等级唯一**（**机检**：`event_level_divergence`）✅ 基线 0
- ③ `ERROR` 只用于"功能已不可用"（71 条 `ERROR` 未逐条复核）🔄 未做

**可达成的数量结果**：

| 指标 | 前 | 后 |
|---|---|---|
| Warn 占比 | 150 / 351 = **43%** | 133 / 351 = **37%** |
| 业务域 Warn | 129 / 248 = **52%** | 118 / 248 = **47%** |
| 平台支撑域 Warn | 17 / 85 = **20%** | 11 / 85 = **12%** |
| `casbin` / `db` 桶 Warn | 33% / 20% | **0% / 0%** |
| `ptz` 桶 Warn | 91%（11/12） | **58%**（7/12） |
| 等级分布 | Warn 150 > Info 106 > Error 71 > Debug 24 | Warn 133 > **Info 121** > Error 70 > **Debug 27** |

**勾选**：

- [x] C03.1 等级判据写进 [`contracts/levels.md`](./contracts/levels.md)（**没有**写进 `CLAUDE.md`
      —— 那里是给所有人看的开发入口，等级判据属于"改日志时才需要翻"的契约，放 `docs/` 更准）
- [x] C03.2 `gb28181` 命名空间 Warn 校准（77 → **72**；降 5 条：4 条"对方报文坏了" + 1 条"应答缺 DeviceID"）
- [x] C03.3 `play`（含 `validation_succeeded` 降级，见 C04）—— 2026-09-14：
      `validation_succeeded` INFO → DEBUG（本链路唯一"减日志"）；
      `play.reconcile.probe_failed` 两处 WARN → DEBUG（同名事件跨等级会让聚合统计失真）
- [x] C03.4 `cascade`（13 → **12**：`cascade.video.ack_invalid` 降 `INFO` —— 上级平台的 ACK 不合规，
      我们继续跑，下游 `release_timeout` / `answer_failed` 仍留 `WARN`）
- [x] C03.5 `zlm` / `ptz` / 其余（`ptz` 11 → **7**；`casbin` 4 → **0**；`db` 1 → **0**；
      `auth` 4 → **3**；`play` 6 → **5**；`zlm` 19 **未动**，见下）
- [x] C03.6 复扫：Warn **43% → 37%**（业务域 52% → 47%）。⚠️ 判据**不是**"Warn < 10%"，
      换成了"每一条 Warn 都答得出动作"这一定性判据 —— 理由见上文与新目标 ①②
- [x] C03.7 门禁加规则 —— **换了形态**：原提的"`Warn` 的消息文本不得是纯流程叙述"
      是自然语言判断，做不成机检（正则分不出"已运行"和"已重试成功"）。
      改成 **`event_level_divergence`**（同一 `event` 跨等级 = 0），落在 C09 建的
      `internal/loggingcatalog`，**没有**回到 `policy.go`
- [x] C03.③ `ERROR` 复核（2026-09-15 补做）：70 条逐条判定 → **70 → 20**。
      最大一处是 `Common.Fail` / `FailAndAbort` 这个覆盖 **881 处调用**的失败出口
      （其中 **799 处 = 90.7%** 走默认 400），按状态码拆成
      `http.operation_rejected`（`INFO`）/ `http.operation_failed`（`ERROR`，保留原名）。
      另 45 条降 `WARN`（关停不完整 8 + 单次写库/查询/清理失败 37）、4 条降 `INFO`、
      19 条留 `ERROR`。逐条判定表见 [`contracts/levels.md`](./contracts/levels.md) 第九节
- [x] C03.④ **假阴性复核**（2026-09-15 补做，见 `levels.md` 第十节）：178 条 `WARN` 里
      翻出 **3 条该是 `ERROR`**（`sip.uac_init_failed` / `login_audit.persist_failed` /
      `login_audit.panic`）+ **2 条反向错配降 `INFO`**（`playauth.key_init_failed_auth_off` ×2）。
      两条可机检线索：① **同语义族跨等级**（按事件名末段分组，15 组，门禁只查同名跨等级、
      查不到"不同名同语义"）；② **完全静默的失败**（扫装配/后台函数里既不打日志也不
      向上传播 `err` 的分支 → 8 条候选**全是假候选**，是**否定性结论**：
      C07/C08 补 `event` 那一轮已顺带补齐了装配路径的留痕）。
      结果 `Warn 178 → 173` / `Error 20 → 23` / `Info 126 → 128`，**总数 351 与 event 331 不变**，
      双门禁 0 findings 且**零连带**（改的调用点都带静态 `event`，不在豁免清单里）

**`zlm` 桶 19 条 Warn 一条没动，这是判定不是遗漏**：17 条是 `zlm.registry.*` /
`zlm.scheduler*.*` 装配链降级（进程继续跑其他功能 = `WARN` 的标准情形），
2 条是 `zlm.scheduler.persist_failed` / `buffer_full`（我们的写入失败与丢条目）。

**⚠️ 本轮没做**（明确移交 / 留待决策）：

1. ~~**`ERROR` 分布未复核**。71 条 `ERROR` 里是否混着"还没到不可用"的，本轮没查
   —— 要另一轮同样量级的逐条判定。~~ **已于 2026-09-15 补做（见 C03.③）**：
   70 条逐条判定，20 条留 `ERROR`、45 条降 `WARN`、4 条降 `INFO`，另有 1 处出口拆成两个事件名。
   还没做的是**反方向**：本该 `ERROR` 却打在 `WARN` 里的子系统级失败（假阴性），
   要第三轮同样量级的判定 —— 那比这一轮难，因为"没有的东西"看不出形状。
   **→ 已于 2026-09-15 补做（C03.④）**：用"同语义族跨等级"（按事件名末段分组，15 组）
   与"完全静默的失败"（8 条候选全是假候选）两条机检线索把范围缩到个位数，再逐条读代码。
   3 条升 `ERROR`、2 条反向降 `INFO`，见 `levels.md` 第十节。
2. **启动期装配失败没有升 `ERROR`**。按定义它们"功能缺一块"，但那是"带伤运行"，
   升 `ERROR` 会被值班当成"服务挂了"。这条边界要动就得一次性重排全部 24 条，不能零散改。
3. **约 24 条无 `zap.Field` 的启动期 Warn 仍缺 `event`**（`(无 event)` 归零属 C08 收尾）。
   本次刻意不补：补 `event` 会同时移动"唯一 event 数 / 无定位分母 / 登记册规模"三个指标，
   把等级改动和事件补齐混成一笔账。
4. **框架适配器的等级**（`schedulerhelper` 的"任务被丢弃（策略）"等）未动
   —— 等级由调用方传入，且它们在脚本口径外，改了既不进指标也不进门禁。

---

## C04 · `play` 链路定位契约

**✅ 已完成（2026-09-14）** —— 全量清单见 [`contracts/play.md`](./contracts/play.md)，
事件登记见 [`contracts/event-catalog.md`](./contracts/event-catalog.md)（C01 的种子）。

- [x] 草案完成 → [`contracts/play.md`](./contracts/play.md)
- [x] 裁决：停播补字段用**改签名（A）** —— 已评估影响面，落地形态定为 **A′（改签名 + `Stop` 内部兜底）**，
      见 `contracts/play.md` §2.2.1~2.2.3（生产 9 处 + 测试 15 处 = 24 处改动）
- [x] 按草案执行改动 —— **2.2a~2.2g 已实施**（停播链路拿到 `device_id`/`channel_id`）
- [x] 2.1 字段命名统一 snake_case（reconciler 18 处 + snapshot 4 处）
- [x] 3.1 `validation_succeeded` 降级 DEBUG（本链路唯一一条"减日志"）
- [x] 3.3 `reuse_probe_failed` **判定不补 `node_id`**（单节点无节点维度，理由入档）
- [x] 3.6.1/3.6.2/3.6.3 `probe_failed` 等级统一 / `round_completed` 补 `sample_stream_id` /
      7 条组件级事件登记豁免
- [x] 3.2b 逐条核对时**新翻出** `play.attempt_begin_failed`/`attempt_finish_failed` 缺定位 → 已补
- [x] 验收：`play` 桶无定位 **10 → 8**（剩余 7 条组件级豁免 + 1 条轮次级），
      且 `play/**` 无 camelCase / 大写 ID；指标明细见 `play.md` §5.4

**未做（明确移交）**：2.3 `stage`/`outcome` 移位属**框架层**（`consoleFieldOrder`），随 C03 一起做；
`handler/hook.go` 的 `play.denied` 等属 **C06**（该模块需先统一 `stream` → `stream_id`）。

---

## C05 · `cascade` 链路定位契约

**✅ 已完成（2026-09-15）** —— 全量契约、逐事件登记、豁免与验收命令见
[`contracts/cascade.md`](./contracts/cascade.md)。

**现状（已核实代码）**：15 条中 2 条无定位字段（**13%**）—— **比初判好得多**。
初判曾报 86%，是统计失真（脚本看不到变量展开）。实际 `cascade_invite_handler.go` 的
`log()` helper 是带了 `platformId` 的。

**但真正的问题在另一处**：那个 helper 里 **9 个 `cascade.video.*` 事件**把字段攒进
`fields []zap.Field` 再 `append(...)...` 展开 —— **运行时是对的，门禁却读不到**，
于是产出 9 条 `unresolved_logger`。**本项的核心不是「补字段」，是「消除这个写法」**，
否则门禁永远红着，而红着的门禁等于没有门禁。

**具体问题**：

1. `cascade_catalog_handler.go` 用 `platformId` / `channelId`（**camelCase**），随 C02 改 `snake_case`。
2. `cascade.control.forward_failed` 出现两次（`:228` 用硬编码 `reason="PTZ service unavailable"`、
   `:234` 带 `err`）—— 建议合并为一个事件 + `reason_code`。
3. `cascade.catalog.query_failed`（`:116`）用的是 `app.ZapLog` 全局 logger，**没有请求上下文**，
   跨不上 HTTP 链路 —— 级联排查时无法和访问日志串起来。

**勾选**：

- [x] C05.1 消除 `cascadeVideoRuntime.log()` 的 `fields` 变量写法 → 拆成 9 个具名方法，
      门禁 9 条 `unresolved_logger` 归零
- [x] C05.2 `camelCase` → `snake_case`（`platformId`×4 / `channelId` / `configuredHost` 共 6 处）
- [x] C05.3 合并 `forward_failed` 的两处写法（`reason_code` + `error`，`error` 失败时才出现）
- [x] C05.4 `catalog.query_failed` 改用 `app.Log(ctx)`，并补 `call_id` + `peer`
      （**不补 `platform_id`**：该失败多数发生在认定平台之前，塞 0 就是占位）
- [x] C05.5 验收：`cascade_invite_handler.go` 的 9 条 finding 归零；
      连带 `cascade_catalog_push.go` 的 2 条 `missing_event` 也一并清掉（补 `event`）；
      门禁全仓 13 → **2**（剩余为 `device_delete.go`，属 C06）

**超出原计划的部分（同一次改动，都是"不这样做指标就是假的"）**：

- 会话级事件统一补 `call_id`（级联是纯 SIP 链路，Call-ID 是唯一能把它和
  `gb_sip_trace_message` 对上的键）；
- 新增 4 条日志契约测试（含"任意字段名含大写字母即失败"的回归断言）；
- `scan-logging.py` 补上**事件名常量化**的读取能力（C01 的方向就是常量化，
  脚本读不到常量会让指标随治理推进而假跌）；
- `scan-logging.py` 把 `peer` 登记为定位字段（依据与影响面见契约文档 §七）。

**明确移交**：等级校准（cascade 桶 Warn 72%）→ **C03.4**；
`cascade/controller/management.go` 的同类 `fields` 写法 → C06/C08；
门禁 `collectFieldFunctions()` 的**按文件收集**缺陷 → 独立修复（见契约文档 §六）。


---

## C06 · `gb28181` 核心链路

**✅ 已完成（2026-09-15）** —— 全量契约、逐事件登记、豁免与验收命令见
[`contracts/gb28181-core.md`](./contracts/gb28181-core.md)。
**门禁由 2 findings 转为 0（全绿）** —— 那 2 条 `missing_event` 就在 `device_delete.go`，属本项。

**原计划的四处缺口（逐条核实结果）**：

- ✅ `handler/register.go:352` 注册成功**有 `ip`/`port` 变量却没打** → 补 `peer`（用的是 `req.Source()`）
- ✅ `:298` 已解析 `expires` 未打 → 补 `expires`
- ❌ **`is_first` 改名 `catalog_triggered` 是错的，已改为新增字段**：
  两者**不等价**。`is_first` 是"本次注册让设备从离线转在线"（还决定订阅唤醒、DeviceInfo 查询），
  而 Catalog 查询额外受 `gbconfig.SyncChannelsOnOnline()` 约束。改名会让这条日志在
  "同步开关关掉"时**谎报已同步** —— 从"信息不完整"变成"信息错误"。裁决：保留 `is_first`，
  **新增** `catalog_triggered`。
- ✅ `:311` 注销日志同样缺 `peer` → 补上

**实际做的比原计划多**（都是核实代码时发现的同族问题）：

1. **`traceFields ...zap.Field` 变参传播**（`catalog.go` / `deviceinfo.go`）——
   和 C05 的 `fields []zap.Field` 容器是**同一族反模式**：运行时字段在、静态读不到。
   改成显式参数 `deviceID, callID, cseq string`，13 条日志一次性变成逐条可验证。**这是本项核心。**
2. `stream` → `stream_id`（hook 18 处 + zlm 3 处，与 play/cascade 统一）
3. hook 三个拒绝出口只有 `reason`（= "有人被拒了但不知道是谁"）→ `reason_code` + 条件性 `stream_id`
4. `hook_auth` 字段名规范化（`node`→`node_id`、`sourceIp`→`source_ip` 等）
5. `OnServerStarted` 解不出节点时**静默 return** → 新增事件 `server_started_node_unresolved`
6. `catalog_trigger.go` 事件名缺 `gb28181.` 前缀（对称文件 `deviceinfo_trigger.go` 是有的）
7. 16 个 kebab 短码 → snake_case；`reason="accepted"` 噪声字段删除

- [x] C06.1 `register` 补 `peer` / `expires` / `catalog_triggered`（`is_first` 保留）
- [x] C06.2 `catalog` / `deviceinfo` / `message` / `hook` 出契约（+ `traceFields` 消除、`stream_id` 统一、hook_auth 规范化）
- [x] C06.3 `device_delete.go` 补 `event` → **门禁全绿**
- [x] C06.4 验收：门禁 0 findings；build + handler/controllers 包测试；新增 4 条契约测试

---

## C07 · 媒体节点（`zlm`）· ✅ 2026-09-15 完成

计划里写的是"16 条里 4 条无定位（25%），目前最健康"——**逐条判定之后结论变了**。
`zlm` 命名空间实际有 **32 条**调用点（原口径只有 17 条：脚本不计"连一个 `zap.Field`
参数都没有"的调用），其中 17 条无定位但**全部是装配期豁免**；真正的欠账不在那 4 条里。

- [x] C07.1 核对全链路并补齐 `node_id`（`zlm.media_online.*` 三条：`c.node.ID` 一直在手上）
- [x] C07.2 补 17 条装配期降级日志的 `event`（`zlm.registry.*` / `zlm.scheduler*.`\* /
  `zlm.client.unavailable` / `zlm.node.config_converge*`）；`nodeId`、`checkInterval`
  → snake_case；`reg.Add` 返回值不再丢弃 → `zlm.node.default_seeded` 补 `node_id`
- [x] C07.3 删掉已失效的豁免条目 15 条（`reviewed_legacy.json` 92 → 77，只减不增）
- [x] C07.4 验收：门禁 0 findings；`go build ./...`；`./app/gb28181/zlm/...` 全绿；
  `./app/gb28181/` 唯一红灯为既有 T14 两条；新增 2 条契约测试
  （`zlm/scheduler/logging_scheduler_test.go`）

契约与判定表：[`contracts/zlm.md`](./contracts/zlm.md)。
⚠️ 桶指标 17 → 32 / 无定位 4 → 17 是**口径变化**（补 `event` 让原本隐形的日志进入统计），
理由写在 README 的 C07 后记里。

---

## C08 · 平台支撑域与命名空间归属 · ✅ 2026-09-15 完成

计划里写的是"`ptz` 9 条 / `recording`+`talk`+`security` 8 条 / `auth`+`audit`+`db` 20 条"——
**逐条判定后结论完全变了**：这三块加起来只占 C08 真实工作量的一小半，最大的空白是
**「其他」桶 97 条 / 71% 无定位**，而它的成因**不是命名混乱**：

> 97 条里**没有一条**是"裸前缀未归类"，全部是合法命名空间
> （`scheduler` / `models` / `casbin` / `auth` / `codegen` / `sysaffix` …）
> 只是不在 `BUSINESS_NS` 白名单里。

- [x] C08.1 扫描器拆**命名空间三分组**（业务域 / 平台支撑域 / 进程框架域 + 未归类），
  并另设**独立指标 ⑤ 事件名末段复述等级**（防止补白名单把劣质事件名盖住）；
  `--dump-ns` 支持组名。⚠️ 只增观测、**不改任何 `has_loc` 判定**
- [x] C08.2 修 **27 条劣质事件名**（末段复述等级 / 函数名横拼 / 同名不同事 / 名实不符）；
  指标 ⑤ **28 → 0**
- [x] C08.3 定**非设备类定位维度**（用户/请求/角色/任务/模板/上传会话六类），
  `IDENT_BASE` 新增 10 项；补 20 处定位字段
- [x] C08.4 给 `(无 event)` **补 40 条事件名**（bootstrap 31 + register 5 + ginhelper 4）；
  `(无 event)` **40 → 1**（剩 1 条为 C07 已登记的动态事件名盲区）
- [x] C08.5 camelCase 字段归一 **57 处 / 12 个文件**；camelCase **58 → 0**
- [x] C08.6 修掉 C06 遗留红 `TestLoggingGBHTTPContextWiring`
  （`cascade/controller/management.go:166` 把 `*gin.Context` 传给 `context.Context` 参数 →
  丢掉 request context 里的 `request_id`）；门禁 findings **保持 0**，
  豁免清单 **77 → 37**

契约与判定边界：[`contracts/platform-support.md`](./contracts/platform-support.md)。

**三条要记住的判定**（详见契约 §五）：

1. **`[omitted:zap.stringArray]`（计划里对 `ptz` 的疑问）不是问题** ——
   数组字段被清洗成占位符是有意的（`sanitize.go` 不支持第三方 ArrayMarshaler），
   要看列表内容就改**调用点**（`strings.Join` 单行），不是去"支持数组"。
2. **`scheduler.result.persist_failed` 不能补 `job_id`** —— 它由 `logging.WithIdentity`
   注入 core，`runtimeCore.Write` 里 `c.bound[f.Key]` 会**丢弃**调用点重复写的同名字段。
   这是**第三族"静态不可见"**（context 携带），**前两族能改写消掉，这一族连补都补不了**。
3. **别用全局字符串替换改日志字段名** —— `nodeId`/`channelId`/`serverId` 等同时也是
   HTTP API 的 JSON 字段名，只能匹配 `zap.<Method>("camelName"`。

**C07 移交已结清**：`bootstrap.go` 那批无 `event` 的启动日志**已全部补事件名**，
但其中 12 条"XX 已启动 / 已装配"的 Info 级日志**判据②答不出动作** ——
C08 选择"补 event 使其可检索"而不是删除，**删除权交给产品决策**，
待裁决清单见契约 §七。

---

## C09 · 机制固化 · ✅ 2026-09-15 完成

**目的：让新增的长不出来。** 不接门禁，前面的活儿会随时间回流。
契约、七条规则、边界与盲区：[`contracts/registry.md`](./contracts/registry.md)。

**计划里的五项，落地形态变了三处**：

| 计划 | 实际做法 | 为什么变 |
|---|---|---|
| C09.1 `event` 白名单 | 330 条冻结进 `registry.json` 的 `events` 段 | 计划写"存量一次性进 `reviewed_legacy.json`"——但那是一份**违规**清单；事件名不是违规，是资产，应该有一份**登记**清单。两者混在一起会让"新增事件"看起来像"新增违规" |
| C09.2 字段名白名单 | 字段字典（精确匹配）**+ 一条纯规则** `^[a-z][a-z0-9_]*$` | 字典只能挡住"字典里没有的新名字"；格式规则不依赖任何基线，把 C08 的 `camelCase 58 → 0` 永久钉住 |
| C09.3 必需定位字段（读 C01 四格表） | 改成**单调性**规则：登记册记下每个事件当前能定位到谁，**只许增不许减** | **前提不成立**：C01 的四格表只登记了 `play` 45 类 + `cascade` 17 类 + `gb28181` 核心约 30 类，330 个事件里绝大多数没有声明。硬写"必需字段"会立刻被条件分支打脸（C04 的停播链路就是 if/else 两支带不同字段）。**记现状 + 禁止退回**不需要人工裁决，也正好挡住"回流" |
| C09.4 存量入豁免清单 | 登记册本身就是基线；另设 `locating_free_exceptions`（带理由，**只减不增**） | 同上 |
| C09.5 验收套件接进自动流程 | 把不依赖 MySQL 的三条用例**移出 `//go:build logging_acceptance`** | 它们只是被标签挡在 `go test ./...` 之外，不是缺能力 |

**门禁基线不是 0** —— 第一版跑出 6 条，逐条判定后 **4 处补字段 + 1 条登记例外 + 1 条判定为归属别的门禁**：

- `hook.go` 三条拒绝出口 + `server_started_node_unresolved` 的 else 支**补 `source_ip`**：
  同一份代码里 `keepalive.*` 早就在这么写，唯独拒绝路径漏了 —— 这是 C09 第一个翻出来的真问题；
- `recording/catalog_scheduler.go` 的 `recordSchedulerFailure` 登记为**唯一一条**例外
  （调度器整体失败时还没有任何节点，硬塞 `node_id=0` 会把"入队失败"谎报成"0 号节点失败"）。

**故意没做的**：`unresolved_event` 规则（"事件值必须是编译期常量"）。
探针实测确认 `loggingcontract` 的 `dynamic_event` 已覆盖它（`zap.String("event", name)` → 红），
两个合法派生事件名的适配器也已有 SHA 封条。**同一事实两个门禁各报一次 = 改一处另一处变红**。

- [x] C09.1 `event` 登记册 + `unregistered_event` 规则（330 条基线）
- [x] C09.2 字段字典（160 条）+ `field_not_in_dictionary` / `field_name_not_snake_case` / `stale_field_dictionary_entry`
- [x] C09.3 定位字段单调性：`locating_field_missing` / `locating_field_lost`（214 个事件记了定位字段并集）
- [x] C09.4 `locating_free_exceptions` 带理由、只减不增；生成器不代改
- [x] C09.5 `loggingacceptance` 的三条 MySQL 无关用例移出构建标签，进 `go test ./...`
- [x] C09.6 验收：`go test ./internal/loggingcatalog/ ./internal/loggingcontract/` 双门禁 0 findings；
  `go build ./...` 干净；`go build -tags logging_acceptance ./...` 仍然可编；
  新增 1 个跨语言一致性测试（`IDENT_BASE` / 等级词集合两边逐项比对）

**⚠️ 约束遵守情况**：没有往 `policy.go`（1739 行）里加一行。新包 = `catalog.go` 703 行 +
`catalog_test.go` 381 行 + 数据 `registry.json` 1076 行，只做一件事：**把源码和登记册比一遍**。

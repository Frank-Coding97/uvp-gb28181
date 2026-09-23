# 媒体节点退役：让「被移除的节点」不再刷屏（设计提案 · 2026-09-21）

> 现场触发：接了两个 ZLM 后移除一个，被移除的那个继续每 10s 打 `on_server_keepalive`，
> 控制台以 **8640 行/天** 重复同一句 `gb28181.hook.auth.rejected reason_code=node_unknown`。
> 本提案回答的不是"怎么把这一条日志关掉"，而是**这类场景以后怎么不再形成干扰**。
>
> **状态（2026-09-21）**：L1 ✅、L4 ✅ 已交付；L2 / L3 未做。详见 §4 / §6。

## 1. 现场与根因（全部实测）

| 事实 | 证据 |
|---|---|
| 被移除的节点行已**硬删** | `meta_node` 只剩 `zlm-220`(`f36d056a…`)；`Registry.Delete` = 删 DB 行 + 删内存索引，**没有任何一步通知对端**（`zlm/node/registry.go:259`） |
| 对端毫不知情，继续回调 | 幽灵身份定位到 220 上容器 `eeyelog-zlm`（host 网络，ZLM API `:21080`，容器内 `/opt/media/conf/config.ini`）：`general.mediaServerId=56e37a2a…` + 9 条 hook 指向 `http://192.168.10.106:8280/…` |
| 平台只能把它当陌生人 | `hook_auth.go:76` 的 `resolver.GetByUUID(nodeID)` miss ⇒ `node_unknown` |
| 平台还在"礼貌地骗它" | `on_server_keepalive` 用 `HookRejectNotification`（`routes.go:1098`）⇒ 回 `{"code":0,"msg":"success"}`，对端以为一切正常，**会永远打下去** |
| 既有抗噪机制对"慢性重复"无效 | `HookAuthenticator.limiter` = 全局令牌桶 8/s burst 16 ⇒ 0.1/s 永不触发 |

⚠️ 反查时的陷阱：对端配置文件与运行时都写 `hook.alive_interval=30.0`，平台侧实测却是 **10s 一条**
⇒ **认"是不是它"要拿 `node_id` 对上，别拿周期对上**。

## 2. 一般形态：这是两个错误的叠加，不是一次事故

1. **生命周期不对称** —— 平台可以单方面忘记一个节点，节点不会单方面忘记平台。
   任何"移除/换实例/换平台/重装"都会留下仍在回调的遗骸。ZLM 侧唯一的杠杆是它的配置，
   而配置恰恰是**平台自己写进去的**。
2. **用日志承载了一个"持续状态"** —— `hook.auth.rejected` 表达的是"有一次被拒"（事件），
   而现场是"有一个来源**一直**在被拒"（状态）。事件级日志去承载状态级事实，只能靠重复播报，
   于是行数 = 频率 × 持续时间，与信息量完全脱钩。

## 3. 判据（先定对错，再选做法）

- **日志记"变化"，"持续"归聚合**：持续的稳态事实应有一次可查的落点（聚合视图/指标），
  日志只留"出现 / 消失 / 量级突变"。
- **消除重复来源 > 折叠重复播报**：能在源头让对方停下来，就不要留着靠日志滤波。
- **静默必须可见、可撤销**：任何"以后不打了"的决定都要留下"谁被静默、谁批的、什么时候复审"，
  否则静默等于遗忘。
- **平台自身故障不许被折叠**：`auth_runtime_unavailable` 是平台坏了，与"对方被拒"不是一类。

## 4. 四层方案

### L1 折叠同源重复（✅ 已实现，2026-09-21）

- **落点**：`app/gb28181/handler/hook_rejection_log.go`（折叠器）+ `hook_auth.go:152` `logHookRejection`
- **规则**：键 = `reason_code` + 自称 `node_id` + `source_ip` + `hook_event`；
  30 分钟窗口内**只留首条** `gb28181.hook.auth.rejected`（WARN 不变），窗口到期补一条
  `gb28181.hook.auth.rejected_summary`（带 `suppressed_count` / `window_seconds`）。
- **不折**：`auth_runtime_unavailable` 与未判过的新 reason（显式 allowlist）。
- **有界**：表上限 512 + 过期清理 + 按 `lastSeen` 淘汰最旧 1/4（键由对方自报，防伪造刷表）。
- **效果**：8640 行/天 → **约 48 行/天**。
- **代价**：无（进程内状态）。**局限**：仍是"周期性地提醒"，对**长期残留**的遗骸只是降噪。

### L2 把"持续"搬到可查聚合（建议，成本低）

- **落点**：折叠器已有的 `firstSeen / lastSeen / suppressed` 直接暴露成一个只读接口
  `GET /api/gb28181/zlm/hook-rejections`（权限复用现有节点查看权限码，避免新权限点迁移），
  返回：`node_id / source_ip / reason_code / hook_event / first_seen / last_seen / suppressed_count`。
- **为什么不做"消失结算"**：那需要后台定时器 + 生命周期（`logging.Repeater` 的 `maintain()/Close()`
  那一套），而"某来源不再被拒"的信息量很低 —— 用查询出口代替定时结算，日志更少、风险更小。
- **效果**：值班从"翻日志"变成"看一张表"；由此 L1 的窗口可以放心取更长（如 6 小时）。

### L3 已确认遗骸的"忽略名单"（可选，兜底）

- **场景**：对端根本关不掉（别人的机器、已失联、密钥被改），或运维判断"这是已知历史遗留"。
- **落点**：新表（uuid/node_id + source_ip + reason_code + 说明 + 操作人 + 复审日期）
  ⇒ 命中后**只计数、不打日志**；撤销后立即恢复。
- **硬约束**：① 说明必填（写清"为什么已知"）；② 默认 90 天后重新提醒一次；
  ③ 前端要能看到"当前被忽略的来源"（否则等同于遗忘）。
- **代价**：三方言迁移 + 三份全量快照 + 前端入口（本仓铁律，工作量最大的一层）。

### L4 对端解约 + 自愈（✅ 已实现，2026-09-21）

> 核心原则：**要能撤销，就必须留凭据**。硬删把 `api_secret` 一起删掉后，平台既不再知道
> "这是谁"，也没有任何能用的凭据 —— 于是只能靠 L1/L3 忍它。

**已选定 (b) 独立表**，理由与当时的判断一致：软删会让一张"活跃节点表"里混进每个读者都要
重新判一次的退休行，而撤销凭据是独立职责。

- **表 `meta_node_retired`**（迁移 `2026-09-21-retired-media-node-credentials.sql`，三方言六件套）：
  `media_server_uuid`(UNIQUE) / `name` / `host` / `api_port` / `api_secret` / `retire_reason` /
  `unprovision_state`(pending|done|unreachable) / `unprovision_attempts` / `last_attempt_at` /
  `retired_at` / `updated_at`。⛔ 建表显式 `COLLATE=utf8mb4_general_ci`（本仓复发性 1267）。
- **三个动作**（落点全部已实现）：

  1. **删行后**尝试解约：`zlm.Client.UnprovisionHooks` 清空该节点 managed events 的 hook URL
     —— **只清平台自己写的那些**（判据 = URL 的 `node` 查询参数等于本节点 uuid），
     不动对方 `on_send_rtp_stopped` 之类别人的 hook，也不动 `hook.enable`（那是全局总开关，
     置 0 会连别人的 hook 一起关）+ 回读校验。失败**不阻断删除**。
     - 接口是独立窄接口 `service.HookUnprovisioner`（**没有并进 `ZLMProbe`**：可选能力，
       未装配时退役照走，只是不自愈）。
  2. **此后**若仍收到该 uuid 的回调 ⇒ 命中退休凭据 ⇒ **退避重试**解约
     （1m → 2m → … → 30m 封顶）。成功后打 INFO「已撤销对端 Hook 回调」并停止重试。
     **这一步才是"自愈"**：触发源是对端自己的回调，所以重试次数天然有界。
  3. 始终解约不掉（不可达 / 密钥已改）⇒ `unprovision_state=unreachable`（**显式失配记录，
     绝不当作成功**），事件 `zlm.node.hook_revoke_failed`(WARN)，日志回落到 L1 折叠兜底。

⛔⛔ **顺序修正（与初稿相反）**：初稿写的是"删行**前**解约"。实现时改成了**删行之后**：
解约成功而删行失败时，节点还活着但已经不再回调平台 —— 心跳停了、Watcher 会把它判离线，
运维看到的是"删不掉还变成离线的僵尸"。删行先做，失败就什么都没发生，是唯一无副作用的方向。
凭据也不会因此丢：调用方此刻手里就有 `cur` 的完整内存快照（`Registry.Delete` 删的是 DB 行与索引，
不是这个副本）。同理，**被拒的删除**（影响预检冲突 / 强制移除发现节点其实可达）不在退休表里
留任何记录 —— 那是常态而非异常，留了就会让这张表的语义失真。

- **`reason_code` 分出新的一类**：认证 miss 时先问退休索引，命中记 `node_retired`，未命中才是
  `node_unknown`。两者都进 L1 折叠白名单、都是 WARN，但**事实不同**：一个是"平台自己删过它、
  还没撤干净"，一个是"平台根本不认识它"。混成一种，值班的人没法据此判断该不该紧张。
- **重新退役同一个 uuid**（重新加入平台又被删掉）：`RetainCredentials` 把状态推回 `pending`、
  尝试计数归零 —— 一条退休记录对应**一次**退役事件；否则第二次删除会继承 `done` 从此不再尝试，
  而重新加入时 `ApplyConfigForNode` 可能已经把 hook 又写回去了。
- **落点清单**：`node/retirement.go`（凭据模型 + 仓库接口 + 进程内索引）、
  `zlm/repo/node_retired_repo.go`（持久化）、`zlm/unprovision.go`（对端逆操作 + 纯函数
  `ManagedHookRevocation`）、`zlm/service/retired_node.go`（协调器 + 三处删除路径接入）、
  `handler/hook_auth.go`（`HookRetiredObserver` 热路径）、`bootstrap.go` / `routes.go`（装配）。
- **残留风险**（已写进实现注释）：退休表保留 `api_secret`，与 `meta_node` 同等级（都是明文列）；
  解约请求发向"曾经是节点"的地址，**若该 IP 易主**，会把 secret 交给新占用者并可能清掉他的 hook 配置
  ⇒ 缓解：只清列表、只对退休表内已知 uuid 触发（天然 allowlist，不会变成放大面）、
  可选 90 天后清除凭据并在清除时说明"此后只能人工处理"。
- **撤约后对端会开始刷本地异常（2026-09-21 实测，已写进 `unprovision.go` 注释）**：
  清空 hook URL 之后，对端那个**已经在运行**的 keepalive Timer 不会消失 ——
  ZLM 只在启动时建它、且只在那时判一次 URL 是否为空 ⇒ 此后每周期
  `do_http_hook("")` 抛一次 `非法的http url`（实测每 10s 一条，`Timer.cpp:31`）。
  这是对端本地日志、不影响平台，对端重启或重新接入即消失；共用 ZLM 上无法用
  `hook.enable=0` 规避。选择接受，理由见代码注释。
- **`hook.alive_interval` 下发不生效**：`ApplyConfigForNode` 每次都写 30.0，但这个值
  只被读**一次**（喂给 Timer 构造函数），运行中的 ZLM 不会因此改变心跳周期 ⇒
  现场 config.ini 写 30.0 而实测仍是 10s。要改必须重启对端。

## 5. 与既有机制的关系（别重复造）

| 机制 | 管什么 | 与本提案的关系 |
|---|---|---|
| `HookAuthenticator.limiter`（8/s 令牌桶） | 秒级风暴（伪造一批随机 node_id） | **保留**，与 L1 互补：一个防"快而多"，一个防"慢而久" |
| `app/utils/logging.Repeater`（T11） | 后台操作的失败重复（`NodeID int64` + `JobID` + `Recovered(key)`） | **不复用**：身份维度与"恢复"语义都不匹配（未知节点没有恢复信号）；只对齐字段名 `suppressed_count` |
| `levels.md` C 类"外部输入被正确拒绝" | 等级校准 | hook 认证拒绝属"身份认证失败、需人去核对凭据"，**WARN 不能降** ⇒ 本提案只改频率，不改等级 |

## 6. 推荐路线

1. ~~L1 已完成~~（2026-09-21 交付）。
2. ~~L4 进行中~~ ✅ **L4 已完成**（2026-09-21）：唯一能"从源头让日志归零"的一层。
3. **L2 紧随**（未做）：没有查询出口，L1 的长窗口就是"把问题藏进日志稀疏处"；
   而且 L4 的 `unprovision_state=unreachable` 现在只能从日志看，正是 L2 该接的东西。
4. **L3 最后**，只在确实存在"关不掉的遗骸"时做 —— 它是兜底，不该成为首选。
5. **本次不做**：不改 ZLM 侧别人的容器（对外部资源的改动要走运维确认）；
   不降 `hook.auth.rejected` 的等级。

## 7. 待定决策点

- [x] L4 的凭据保留选 (a) 软删还是 (b) 独立表？ ⇒ **(b) 独立表**（已实现）
- [ ] 退休凭据是否设 90 天有效期？（**仍未做**：当前只增不删。倾向设，且在到期时把状态
      明确降级为"此后只能人工处理"，而不是悄悄失去凭据）
- [ ] 220 上那个幽灵（容器 `eeyelog-zlm`）何时清掉它那 10 条指向平台的 hook？
      ⚠️ 它已被**硬删**、不在退休表里 ⇒ L4 无法自愈它（凭据在删行那一刻才留下，
      而它是在本功能之前被删的）。目前只能靠 L1 折叠（约 48 行/天）或人工去对端清。
- [ ] L2 的查询出口放哪个权限码？（倾向复用节点查看权限，避免新权限点迁移）
- [ ] L3 是否要做（取决于是否真的存在关不掉的遗骸）

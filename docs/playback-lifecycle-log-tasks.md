# 播放生命周期日志任务清单

- 状态：待评审
- 对应规格：[playback-lifecycle-log-spec.md](./playback-lifecycle-log-spec.md)
- 对应方案：[playback-lifecycle-log-plan.md](./playback-lifecycle-log-plan.md)
- 首期范围：GB28181 实时点播

## 执行规则

- 严格按任务依赖顺序实施；每个任务先提交 RED 测试并确认失败原因正确，再写最小实现至 GREEN，最后只重构本任务引入的代码。
- 单元测试、迁移契约、本地构建、浏览器验收、真实 SIP/ZLM/客户端证据分别报告，不互相替代。
- 不改变点播、调度、SIP 或媒体控制行为；生命周期记录失败只能产生诊断，不能改变播放结果。
- 每完成一个任务先运行该任务聚焦测试；跨模块接入完成后再运行 GB28181 后端测试和前端 Vitest 回归。

## Task 1：冻结生命周期领域契约

**目标**：定义阶段、事实状态、来源、稳定原因码、汇总状态和允许的状态转换，明确服务端媒体事实与客户端事实互不覆盖。

**候选位置**：`server/app/gb28181/play/`、`server/app/gb28181/models/`。

**RED 测试**：

| 用例 | 输入 | 期望输出 |
| --- | --- | --- |
| 完整成功路径 | request → validation → node/RTP → INVITE accepted → media ready → URL issued → first frame | 各阶段均为已证实；媒体与客户端状态分别成功 |
| SIP 成功但无媒体 | INVITE accepted → media timeout | INVITE 保持已证实，媒体为失败，客户端为未知 |
| 复用路径 | validation → reuse media ready → URL issued | RTP、INVITE 为不适用，不生成伪造事件 |
| 客户端错误 | media ready → player error | 媒体仍为已就绪，客户端独立为失败 |
| 终态竞争 | failure 后追加 success，或重复 finish | 首个合法终态保持不变，重复操作幂等 |
| 错误脱敏 | 底层错误含 URL、token、Authorization、超长文本 | 仅保留稳定原因码和限长安全说明 |

**GREEN 完成标准**：领域契约测试全部通过，原因码和转换表成为后续模块唯一依据。

## Task 2：落地三数据库模型与迁移

**依赖**：Task 1。

**目标**：扩展 `gb_play_attempt`，新增 `gb_play_lifecycle_event`，提供 MySQL、PostgreSQL、SQL Server 的 up/down migration 及必要索引、唯一约束。

**候选位置**：`server/app/gb28181/models/`、`server/resource/database/gb28181/migrations/`、`server/app/gb28181/migration/`。

**RED 测试**：

| 用例 | 输入 | 期望输出 |
| --- | --- | --- |
| 三方 DDL 契约 | 读取三种 up migration | 列、类型、默认值、索引和 `event_id` 唯一约束语义一致 |
| 迁移兼容 | 迁移前已有 `gb_play_attempt` 行 | 迁移成功；新增状态为未知，不伪造阶段 |
| 回滚契约 | 执行对应 down migration | 仅移除本功能对象，不破坏原汇总表既有字段 |
| 模型映射 | GORM AutoMigrate/SQLite 测试模型 | 表名、JSON 字段、可空字段和时间字段符合契约 |

**GREEN 完成标准**：三种 migration 合约及模型测试通过，初始化/迁移清单能够发现新文件。

## Task 3：实现汇总仓储与不可变事件 recorder

**依赖**：Task 1、Task 2。

**目标**：提供 Begin、Append、Finish、MarkStale 能力；同一事务内追加事件并推进汇总快照，保留现有首页媒体就绪率统计语义。

**候选位置**：`server/app/gb28181/dashboard/`、`server/app/gb28181/play/`。

**RED 测试**：

| 用例 | 输入 | 期望输出 |
| --- | --- | --- |
| 首事件写入 | Begin 用户/设备/通道 | 创建唯一生命周期并追加 `request_received` |
| 幂等追加 | 两次提交相同 `event_id` | 仅一条有效事件，汇总只推进一次 |
| 并发顺序 | 并发追加多个不同事件 | sequence 唯一递增，详情排序稳定 |
| 原子失败 | 事件 insert 或汇总 update 注入失败 | 事务回滚，不出现事件与快照互相矛盾 |
| 旧指标兼容 | media ready、终止失败、客户端错误混合样本 | 首页 success/failure 统计结果与改造前语义一致 |

**GREEN 完成标准**：仓储测试通过；旧 `play_attempt_store` 和 `play_history` 回归不变。

## Task 4：实现有界异步记录与运行时诊断

**依赖**：Task 3。

**目标**：在不阻塞播放控制的前提下异步持久化；队列满、数据库失败和序列化失败可观测；正常退出限时排空。

**候选位置**：`server/app/gb28181/play/`、`server/app/gb28181/bootstrap.go`。

**RED 测试**：

| 用例 | 输入 | 期望输出 |
| --- | --- | --- |
| 主链路隔离 | recorder 持久化被阻塞 | 播放调用不等待数据库完成 |
| 队列满 | 容量为 1 并连续提交 | 不阻塞；增加 dropped/persist_failed 诊断 |
| 数据库失败 | recorder 返回错误 | 播放结果不被改写；结构化诊断含生命周期和阶段 |
| 正常关闭 | 队列中仍有事件后 shutdown | 限定时间内排空，拒绝关闭后的新事件 |
| 超时关闭 | writer 永久阻塞 | 到期返回，不拖死进程关闭 |

**GREEN 完成标准**：异步、诊断和 shutdown 测试通过，bootstrap 生命周期完成装配。

## Task 5：接入实时点播服务端阶段事实

**依赖**：Task 4。

**目标**：生命周期 ID 贯穿 HTTP 入口、`play.Request`、播放服务和 `InviteOutcome`；记录校验、复用、选点、RTP、INVITE、媒体就绪和 URL 签发事实。

**候选位置**：`server/app/gb28181/controllers/play.go`、`server/app/gb28181/play/`、`server/app/gb28181/uac/`。

**RED 测试**：

| 用例 | 输入 | 期望输出 |
| --- | --- | --- |
| 新流成功 | 在线设备、可用节点、INVITE 200、媒体 ready | 事件顺序完整，Call-ID/CSeq、stream/node/SSRC 已关联 |
| 校验失败 | 设备离线或通道不存在 | 终止在 validation，使用不同稳定原因码 |
| 资源失败 | 无节点或 RTP 分配失败 | 终止在对应阶段，不发送 INVITE |
| SIP 失败 | INVITE 拒绝或超时 | 分别记录 rejected/timeout，不声明媒体就绪 |
| 媒体超时 | INVITE 接受但 `WaitReadyRef` 超时 | SIP 已接受、媒体失败、客户端未知 |
| 复用成功 | 已有流且探测可用 | 标记 reused；不出现新 RTP/INVITE 事件 |
| 授权失败 | 媒体 ready 后 URL/token 签发失败 | 媒体事实保留，失败阶段为 play authorization |

**GREEN 完成标准**：播放服务聚焦测试全部通过，原点播返回和控制行为回归通过。

## Task 6：接入 Hook、停止清理、异常未收口与 retention

**依赖**：Task 5。

**目标**：关联 ZLM Hook 和停止清理事实；无法关联时不制造生命周期；实现 stale 标记及事件 7 天、汇总 30 天清理。

**候选位置**：`server/app/gb28181/handler/hook.go`、`server/app/gb28181/play/`、`server/app/gb28181/dashboard/retention.go`。

**RED 测试**：

| 用例 | 输入 | 期望输出 |
| --- | --- | --- |
| Hook 关联 | 相同 stream/node 的流注册、流量、无人观看事件 | 追加到活动生命周期且不重复 |
| Hook 无关联 | 未知 stream/node | 只写通用诊断，不创建孤儿生命周期 |
| 正常停播 | stop、BYE、关闭 RTP、清状态均成功 | stop requested 和 cleanup completed 已记录 |
| 部分清理失败 | BYE 或关闭 RTP 失败 | cleanup partial failure，列出安全原因码 |
| stale 收口 | 超过窗口仍进行中的生命周期 | 标记异常未收口，保留原阶段，不计业务失败 |
| 保留清理 | 8 天事件、31 天终态汇总、正常进行中、31 天 stale | 删除过期事件/汇总；进行中保留；stale 按标记时间清理 |

**GREEN 完成标准**：Hook、stop、stale、retention 和 bootstrap retention 回归全部通过。

## Task 7：实现客户端反馈凭据与写入 API

**依赖**：Task 3、Task 5。

**目标**：签发短期反馈凭据，并安全接收 `first_frame`、`player_error`；绑定用户、设备、通道、生命周期和允许事件。

**候选位置**：`server/app/gb28181/playauth/`、`server/app/gb28181/controllers/`、`server/app/gb28181/play/`。

**RED 测试**：

| 用例 | 输入 | 期望输出 |
| --- | --- | --- |
| 合法首帧 | 当前用户、有效 token、匹配生命周期 | 首帧只记录一次，客户端状态已确认 |
| 合法播放器错误 | 媒体已就绪后提交安全错误码 | 客户端失败，媒体仍为已就绪 |
| 凭据失效 | 过期、篡改、事件类型不允许 | 统一不可用响应，生命周期不变 |
| 越权反馈 | 用户或设备/通道不匹配 | 与不存在同形响应，不泄露记录存在性 |
| 重放与旧会话 | 重复 event key 或旧生命周期 token | 重复幂等；旧会话不能污染新生命周期 |
| 敏感字段 | 请求携带 URL、token 或超长原始错误 | 拒绝或清洗，不写入敏感内容 |

**GREEN 完成标准**：签发、验证、权限、幂等和 controller 测试通过；播放响应返回 lifecycle ID 与反馈凭据。

## Task 8：实现数据权限受控的查询 API

**依赖**：Task 3、Task 6。

**目标**：提供分页列表和详情时间线，支持规格中的筛选项，并在所有查询路径重用设备可见性 scope。

**候选位置**：`server/app/gb28181/dashboard/`、`server/app/gb28181/controllers/`、`server/app/gb28181/bootstrap.go`。

**RED 测试**：

| 用例 | 输入 | 期望输出 |
| --- | --- | --- |
| 默认分页 | 无分页参数，存在 15 条记录 | 返回最近事件倒序的 10 条及正确 total |
| 组合筛选 | 时间、设备、通道、节点、状态、失败阶段 | 仅返回全部条件同时匹配的记录 |
| 详情排序 | sequence 与接收时间存在并列 | 按 sequence/event_at 稳定返回完整时间线 |
| 权限隔离 | 可见和不可见设备各有记录 | 列表、详情都只返回可见设备 |
| 旁路尝试 | 用不可见 lifecycle/stream/node 精确筛选 | 仍不可见，不因精确标识绕过 scope |
| 安全输出 | 事件扩展字段含内部诊断 | 响应只包含白名单字段和脱敏原因 |

**GREEN 完成标准**：repository、controller、路由和数据权限测试通过，错误响应不泄露存在性。

## Task 9：接入前端播放事实反馈

**依赖**：Task 7。

**目标**：扩展前端点播类型与 API；`PlayWindow` 首次有效视频尺寸上报首帧，EasyPlayer error/timeout 上报播放器错误。

**候选位置**：`web/src/api/gb28181.ts`、`web/src/views/gb28181/components/PlayWindow.vue` 及宿主组件。

**RED 测试**：

| 用例 | 输入 | 期望输出 |
| --- | --- | --- |
| 首帧去重 | 多次轮询得到有效视频尺寸 | 当前播放会话只上报一次 first_frame |
| 错误上报 | EasyPlayer error/timeout | 上报白名单错误码，不含播放 URL/token |
| 会话切换 | URL/lifecycle 改变后旧回调到达 | 旧回调被忽略；新会话可独立上报 |
| 销毁与重试 | 组件卸载或播放器重建 | 清理轮询和监听，去重键按新会话生成 |
| 上报失败 | API 超时或返回不可用 | 不影响播放器状态，不无限重试 |

**GREEN 完成标准**：API 类型、`PlayWindow` 和宿主组件 Vitest 通过，现有播放协议回归不变。

## Task 10：建设独立播放日志页面与权限入口

**依赖**：Task 8。

**目标**：新增播放日志列表和详情时间线；默认 10 条、表格内部滚动、外部容器无垂直滚动；明确展示媒体与客户端状态。

**候选位置**：`web/src/views/gb28181/`、`web/src/api/gb28181.ts`、路由与菜单 permission migration。

**RED 测试**：

| 用例 | 输入 | 期望输出 |
| --- | --- | --- |
| 初始查询 | 首次进入页面 | pageSize=10，按最近事件倒序请求 |
| 滚动布局 | 超过视口高度的 10 条记录 | 滚动条位于表格区域，页面外层不产生垂直滚动 |
| 状态文案 | 媒体 ready、客户端 unknown/error、reused | 分栏显示准确，不出现无限定“播放成功” |
| 时间线语义 | 已证实、失败、进行中、未知、不适用 | 五类状态视觉和文案可区分 |
| 查询失败 | 首次失败或刷新失败 | 显示数据不可用或保留旧样本，不伪装成空数据 |
| 权限入口 | 无权限/有权限用户 | 菜单、路由、接口权限一致；down migration 可撤销入口 |

**GREEN 完成标准**：页面、布局、API、路由和菜单 migration 测试通过，调度日志语义与页面不受影响。

## Task 11：全量回归与真实链路验收

**依赖**：Task 1 至 Task 10。

**目标**：验证源码契约、构建和真实点播事实，形成可复核证据，不以单元测试或 HTTP 200 代替真实播放结论。

**自动化与构建清单**：

| 验证 | 输入 | 期望输出 |
| --- | --- | --- |
| 后端聚焦测试 | 生命周期、dashboard、controller、Hook、playauth、migration 包 | 全部通过，无竞态终态 |
| 后端回归 | GB28181 相关 Go 测试和 race 可执行范围 | 无新增失败；基线失败单独列出 |
| 前端回归 | 相关 Vitest、类型检查和生产构建 | 全部通过，无布局/类型回归 |
| Migration 校验 | 三种方言 up/down 契约 | 文件齐全、注册可发现、回滚边界正确 |

**真实链路用例**：

| 场景 | 必须保留的输入/证据 | 期望结果 |
| --- | --- | --- |
| 成功首帧 | HTTP 响应、SIP Call-ID/CSeq、ZLM ready、浏览器首帧、数据库事件 | 同一 lifecycle 串联全部事实 |
| INVITE 失败 | SIP 拒绝或超时证据、数据库时间线 | 停在 SIP 阶段，不出现媒体/首帧成功 |
| 媒体超时 | SIP 200、无 ZLM ready、数据库时间线 | INVITE 已接受、媒体失败、客户端未知 |
| 客户端失败 | ZLM ready、播放器错误上报、数据库时间线 | 媒体已就绪与客户端失败同时保留 |
| 正常停播 | DELETE/停播请求、BYE/RTP/状态清理证据 | cleanup completed；无残留会话和 RTP 资源 |

**GREEN 完成标准**：分别报告源码测试、migration、本地构建、浏览器和真实设备链路结果；未具备的环境验收明确标为未验证。

## 依赖顺序

`Task 1 → Task 2 → Task 3 → Task 4 → Task 5 → Task 6 → Task 8 → Task 10 → Task 11`

`Task 3 + Task 5 → Task 7 → Task 9 → Task 11`


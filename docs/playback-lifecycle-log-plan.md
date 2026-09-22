# 播放生命周期日志技术方案

- 状态：已确认
- 对应规格：[playback-lifecycle-log-spec.md](./playback-lifecycle-log-spec.md)
- 首期范围：GB28181 实时点播

## 1. 方案结论

采用“汇总快照 + 不可变阶段事件”双层模型：

- 继续使用 `gb_play_attempt`，扩展为一次播放生命周期的当前快照和 30 天汇总来源，保留现有首页媒体就绪率兼容语义。
- 新增 `gb_play_lifecycle_event`，每个阶段写一行，保留 7 天，用于详情时间线和阶段筛选；事件只追加、不更新历史事实。
- `gb_play_attempt.correlation_id` 作为生命周期 ID。HTTP 点播入口创建它，并通过播放请求上下文传到播放服务；没有 HTTP 入口的内部实时点播由播放服务创建生命周期。
- 播放服务是服务端事实的唯一编排点；SIP、ZLM Hook 和客户端反馈只能追加对应来源的事件，不能把较晚事实倒写成较早阶段成功。
- 客户端反馈使用服务端签发的短期反馈凭据，凭据绑定用户、设备、通道、生命周期和过期时间；不复用播放 URL 中的 bearer token，也不把非 bearer 的审计 correlation ID 当作鉴权凭据。

## 2. 分层结构

### 2.1 生命周期汇总

扩展 `GbPlayAttempt`：

- 标识：`correlation_id`、`user_id`、`device_code`、`channel_code`。
- 媒体定位：`stream_id`、`node_id`、`reused`、`ssrc`。
- SIP 关联：`call_id`、`cseq`。
- 当前事实：`current_stage`、`media_state`、`client_state`、`lifecycle_state`。
- 终止信息：`outcome`（兼容既有 `started/success/failure`）、`failure_stage`、`reason_code`、`reason_message`、`finished_at`。
- 客户端事实：`client_first_frame_at`、`client_error_at`、`client_error_code`。
- 时间：`started_at`、`last_event_at`、`created_at`、`updated_at`。

旧首页指标继续只把服务端媒体就绪的终态计入 `success`，不把客户端首帧缺失计为失败；新页面分别展示媒体状态和客户端状态。

### 2.2 阶段事件

新增事件表保存：

- `event_id`（幂等键）、`lifecycle_id`、递增 `sequence`、`event_at`、`elapsed_ms`。
- `stage`、`event_name`、`fact_state`、`source`。
- 设备、通道、流、节点、SSRC、复用标记，以及已知的 `call_id`/`cseq`。
- `reason_code`、受限且脱敏的 `reason_message`。
- 受控扩展字段 JSON，仅允许白名单键，不保存完整 SIP/SDP、播放 URL、令牌、Secret 或原始 Hook 对象。

事件唯一性由 `event_id` 保证；同一 Hook 重试、客户端重复上报和服务重试使用同一幂等键，不产生重复有效事件。事件时间按服务端接收时间记录，客户端原始时间只作为受限元数据，不参与服务端排序。

## 3. 服务端数据流

1. `POST /play/{device}/{channel}` 在权限校验后创建生命周期汇总，写入 `request_received`，将生命周期 ID 放入 `play.Request`/`AuthorizedRequest`。
2. 播放服务在校验、复用探测、节点选择、RTP 分配、INVITE、媒体等待和清理边界调用统一 recorder；每次调用同时更新汇总快照和追加阶段事件。
3. `InviteOutcome` 提供 `RequestSent`、最终 SIP 状态、ACK 和 Call-ID/CSeq；服务层据此追加 `invite_sent`、`invite_accepted`、`invite_rejected` 或 `invite_timeout`，不能用函数返回 `err == nil` 单独推断媒体成功。
4. `WaitReadyRef` 成功后追加 `media_ready`；只有这一事实继续驱动现有首页“媒体流就绪”统计。
5. URL/播放授权完成后追加 `play_url_issued`，响应返回生命周期 ID和一次性客户端反馈凭据。
6. ZLM `on_stream_changed`/`on_flow_report`/`on_stream_none_reader`/`on_rtp_server_timeout` 通过 `stream_id + node_id` 反查活动生命周期，追加流注册、下行数据、无人观看和媒体终止事实；反查不到时只写通用 Hook 日志，不制造孤儿生命周期。
7. `DELETE /play/{stream}` 和 Hook 停流路径追加 `stop_requested`、`cleanup_completed` 或 `cleanup_partial_failure`，并更新汇总终态。
8. 每个生命周期超过允许窗口仍为进行中时，由收口任务标记为 `stale_in_progress`，保留原始阶段和“证据不足”语义，不伪造业务失败。

事件写入采用独立 recorder 和有界队列，不能阻塞 SIP/媒体控制；服务正常退出时先停止接收新事件并在限定时间内排空队列。队列满、数据库不可用和序列化失败必须产生计数器及结构化 `persist_failed` 诊断。汇总更新失败不能改变播放控制结果，也不能把响应改报成功。

## 4. 客户端反馈

### 4.1 凭据

播放响应增加 `lifecycleId` 和 `clientFeedbackToken`。凭据为短期签名值，绑定：用户、设备、通道、生命周期、允许事件集合和过期时间；服务端只接受当前用户可见且仍处于有效窗口的生命周期。

### 4.2 API

新增 `POST /api/gb28181/play/lifecycles/{lifecycleId}/client-events`：

- 请求：`clientFeedbackToken`、`event`（`first_frame`/`player_error`）、播放器类型、协议、可选安全错误码和客户端耗时。
- 服务端验证登录、数据权限、凭据签名、生命周期绑定、事件允许性和幂等键。
- 首帧只允许从“未确认”转为“已确认”；错误不得覆盖已经确认的服务端 `media_ready`，而是更新独立 `client_state`。
- 失败响应不泄露生命周期是否属于其他用户；过期、越权、签名错误和不存在统一返回不可用。

`PlayWindow` 在首次取得有效视频尺寸时发出一次 `first-frame`；EasyPlayer `error`/`timeout` 事件发出一次 `player-error`。URL 切换、组件销毁和重试都生成新的客户端去重键，不把旧会话事件写入新生命周期。

## 5. 查询 API 与页面

新增只读查询：

- `GET /api/gb28181/play/lifecycles`：分页、时间范围、设备、通道、流、节点、生命周期状态、失败阶段、生命周期 ID。
- `GET /api/gb28181/play/lifecycles/{id}`：返回汇总快照和按 `sequence/event_at` 排序的阶段事件。

查询统一应用设备可见性 scope；生命周期 ID、流 ID 和节点筛选都不能绕过 scope。错误原因通过现有安全展示规则脱敏。

前端新增独立“播放日志”入口，不修改“调度日志”的命中语义：

- 列表和时间线复用现有表格内部滚动、默认 10 条和数据不可用状态模式。
- 列表展示媒体状态、客户端状态、当前/最终阶段；不使用裸“播放成功”。
- 详情时间线明确显示未发生、未知、不适用、已证实和失败。
- 复用流展示 `reused` 和 `reuse_media_ready`，不补造 RTP/INVITE 事件。

## 6. 数据库与保留

为 MySQL、PostgreSQL、SQL Server 各新增同名 migration 及 down migration：

- `gb_play_attempt` 增量列和必要索引，兼容已有行；历史行的新增状态保持未知，不回填虚假阶段。
- `gb_play_lifecycle_event` 表、生命周期/时间/设备/节点/阶段/来源索引，以及 `event_id` 唯一约束。
- 客户端反馈凭据不落明文；如需防重放，只保存凭据指纹和最后接受的客户端事件键。

复用现有 retention runtime：汇总终态保留 30 天，阶段事件保留 7 天；正常进行中的记录不清理，`stale_in_progress` 按标记异常未收口的时间单独保留 30 天，避免永久积压。清理结果进入 retention 诊断，不影响播放请求。

## 7. 模块边界

- `server/app/gb28181/play/`：生命周期上下文、阶段转换、事件 recorder 接口和播放事实映射。
- `server/app/gb28181/dashboard/`：汇总/事件持久化、查询和保留实现；不把 dashboard 指标反向当作播放事实。
- `server/app/gb28181/controllers/`：生命周期创建、查询、客户端反馈和权限入口。
- `server/app/gb28181/handler/`：Hook 事件关联及无法关联时的降级日志。
- `server/app/gb28181/playauth/`：客户端反馈凭据签发/验证，不暴露播放授权 Secret。
- `web/src/api/`：类型、查询和反馈 API。
- `web/src/views/gb28181/components/PlayWindow.vue` 及其宿主：首帧/播放器错误反馈。
- `web/src/views/gb28181/`：独立播放日志页面、时间线和状态文案。

## 8. 测试与验收策略

### 自动化

- 阶段转换：每个成功、拒绝、超时、媒体未就绪、授权失败和清理失败均有正确 stage/state/reason_code。
- 事件幂等、序列、并发追加和队列持久化失败可观测性。
- `gb_play_attempt` 旧统计兼容，复用流不重复生成 INVITE 阶段。
- 客户端凭据签名、过期、越权、重放、错误覆盖规则和数据权限。
- 三种数据库 migration/DDL 契约、索引和 30/7 天 retention；进行中记录受保护。
- 前端首帧只上报一次、旧会话反馈被拒绝、错误与媒体状态分离、列表分页和时间线语义。

### 真实链路

按规格分别保留 HTTP、SIP、ZLM、浏览器和数据库证据：成功首帧、INVITE 拒绝、媒体超时、客户端错误和正常停播各一例。服务端媒体就绪但客户端无反馈必须单独验收为“媒体已就绪、客户端未知”。

## 9. 风险与取舍

1. **阶段事件异步写入可能丢失**：用有界队列避免影响播放，正常退出时限时排空，并以持久化失败计数和诊断事件暴露缺口；进程被强制终止时仍可能丢失队尾事件。若要求硬崩溃场景零丢失，需要再引入事务 outbox，会扩大本期范围。
2. **客户端反馈不是 GB28181 标准事实**：明确标记 `source=client`，只代表授权客户端观测，不改变 SIP/RTP 服务端事实。
3. **Hook 反查可能晚于清理**：关联不到时不补造业务事件，保留原始 Hook 证据和关联失败诊断。
4. **旧汇总记录缺少阶段细节**：全部新增字段保持未知，不回填；页面须区分历史不完整与当前无数据。

## 10. 实施顺序

1. 先落领域状态、事件契约和持久化迁移，再接入播放服务阶段事实。
2. 再接入 Hook、停止清理和客户端反馈，补齐安全与幂等。
3. 最后建设查询 API、独立前端页面和菜单权限。
4. 完成聚焦测试后，再进行真实设备/浏览器链路验收；不把单元测试或 HTTP 200 当作播放成功证据。

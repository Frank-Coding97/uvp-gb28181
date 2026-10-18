---
title: 国标级联 P2 缺陷修复技术方案
status: implemented-with-limitations
spec: ../specs/cascade-p2-fixes.md
date: 2026-10-16
---

## 方案边界

本方案覆盖静态审查 D06–D16。实现只针对当前工作区已有级联代码、协议解析和测试边界，保留其他任务的暂存、未暂存和未跟踪改动；不重置、不清理、不提交、不推送、不部署。

验证顺序固定为：先新增可复现缺陷的 RED 测试，再做最小实现使其 GREEN，最后执行相关包的回归和需要的竞态检查。测试仅使用本地 fake、隔离数据库和模拟 SIP/媒体对象，不操作浏览器、真实上级平台、真实设备或当前开发环境的级联台账。

标准依据采用 GB/T 28181-2022；2016 的兼容行为单独保留。标准的“应/宜/可”和本产品的目录投影、租约、重试及 UI 结果策略分开记录，不能用产品默认代替标准要求。

## 总体设计

### 结果契约

管理操作统一返回两层结果：

1. 持久化结果：配置是否已经写入、平台是否已经软删除、修订号是什么。
2. 运行时结果：actor 是否已经加载、注册/注销是否已经完成、目录推送是否已经完成。

HTTP 成功表示持久化成功时，响应体必须携带可判断的 `runtimeSynced`（或等价字段）；运行时未同步时保留已保存资源并使用稳定错误码，禁止把异步发起、HTTP 200 或软删除单独表述为“已注册/已注销/全部完成”。前端只依据响应契约提示，不自行推断。

### 每平台生命周期

每个启用上级平台继续由独立 runtime actor 管理。actor 内维护一个串行维护事务和有限的意图标记，避免 REGISTER、续期、心跳、重注册重叠。注册响应先解析授予期限和版本，再更新持久化状态与定时器；心跳连续超时达到阈值时进入失效状态并按退避重新注册。停止、删除和配置重载都通过同一 actor 收尾，旧定时器用代际或取消机制失效。

### 入口与资源隔离

所有 Catalog、信息、PTZ、INVITE、RecordInfo 和已建立会话请求，先解析实际网络来源，再匹配唯一启用上级和其投影。域名配置通过受控解析得到合法地址集合，不能退化为只比较 From 身份。查询目标只在该上级的已发布投影内展开；媒体源、发送器、播放会话和业务目录始终按 platform ID 与来源会话隔离。

## D06/D07：管理 API 与页面结果一致

### D06 选择

在管理 service/controller 层建立统一结果映射：

- 创建或更新成功且 Reload 成功：返回资源、`runtimeSynced:true`。
- 持久化成功但 Reload 失败：返回资源、`runtimeSynced:false`，稳定的运行时同步错误码；HTTP 状态沿用现有“已提交但需重试”的契约。
- 持久化失败：不返回成功资源，保留原错误。
- 停用/启用返回的是配置状态，不把 REGISTER 或注销完成提前承诺。

删除操作复用同一契约，并区分“未删除”“已软删除但运行时未同步”“已删除且运行时已同步”。页面保存、启停、删除的 toast 和刷新策略只读取该结果；成功 HTTP 但同步失败时显示“已保存/已删除，运行时待同步”，刷新后以服务端状态为准。

### D07 删除顺序

服务层先检查活动媒体会话。启用平台删除时：

1. 在持久化事务中停用/软删除，形成唯一可重试的配置状态。
2. 调用 runtime Reload。
3. Reload 失败时返回明确的已提交状态和运行时错误，不伪造删除成功。删除结果使用显式 
   `deleted`/`runtimeSynced` 字段：首次停用同步失败为 `deleted:false`，软删除已提交但
   二次同步失败为 `deleted:true`、`runtimeSynced:false`。
4. 活动会话存在时拒绝删除，不能先释放配置再遗留媒体租约。
5. 持久化失败时不调用 Reload。

当前实现先持久化停用再 Reload，因此保留这一顺序：首次同步失败时平台保持可见但已停用，下一次删除请求继续完成软删除；软删除后的同步失败则平台已删除但运行时待同步。service 返回结构化 DeleteResult，controller 只透传 `deleted`、`runtimeSynced` 和可选平台视图，不能让 controller 自己猜测 service 状态。

## D08：媒体结束与发送器监测

扩展 `waitCascadeVideo` 的监测抽象，而不是在 ticker 中直接把查询错误当作结束：

- 播放会话终态、源流不存在、租约失效分别记录原因。
- UDP 开启/关闭 RTCP、TCP active/passive 都走统一 sender 活跃检查；RTCP 是附加探针，关闭 RTCP 不能关闭源流消失检测。
- 查询暂时失败只记录并保留会话，连续失败达到受控阈值才进入收尾；阈值和时间由可控时钟测试覆盖。
- 收尾必须先停止上级 sender，停止失败时保留资源并重试，不能提前释放共享源；成功后再 BYE、停止播放租约、删除会话状态。
- 每个平台和每个源的 sender key 继续隔离，不能因一个消费者结束停止其他上级消费者。

将源流探测、sender 停止和会话收尾拆成可注入接口，允许 RED 测试覆盖各传输模式和失败分支。

## D09：心跳失败后的可恢复注册

在 runtime actor 中新增明确状态：

- `registered`、`heartbeatHealthy`、`reRegisterPending` 与 `maintenanceInFlight` 只在 actor 串行上下文变更。
- 连续心跳超时达到配置次数（默认 3 次）后记录离线、停止旧续期计时器，并排队一次 Register；拒绝响应与超时分别记录，不把一次瞬时错误判为离线。
- Register/Keepalive 事务完成后只消费合并后的意图；退避重试不少于标准要求的注册失败间隔，并在测试中使用 fake scheduler，不真实等待。
- 重注册成功后恢复心跳和过期时间；失败后按退避重试，禁止忙循环。
- 停止、删除和重载清空 pending 标记并取消旧 timer；同一 actor 至多一个维护事务。

持久化的注册、心跳和错误码由状态转移统一写入，避免页面看到在线但运行时已经判离线。

## D10/D16：版本协商与注册期限

### 版本

扩展 SIP transaction 结果，解析最终 REGISTER 响应中的 `X-GB-Ver`，并与本次绑定的 Contact 一起返回：

- `2.0` 映射 2016，`3.0` 映射 2022；缺失、格式无效和未知值分别建模。
- 摘要挑战后的最终 2xx 响应才更新版本；失败响应不覆盖上次有效版本。
- auto profile 首次注册使用兼容默认（2016），收到有效版本后持久化协商结果，并让后续 REGISTER、Catalog、SDP 和 MANSRTSP 选择同一 profile。
- 固定 2016/2022 的配置不被协商响应改写；未知或缺失版本保留当前有效 profile并记录能力不确定，不发送对端不能识别的高版本字段。
- 持久化版本来源（override/negotiated/default）与有效版本，重启后恢复一致。

### 期限

服务端和前端对正注册期限统一执行最小值 3600 秒；0 仅用于注销，不与正常注册混淆。边界 3599 拒绝、3600 接受；auto 首次注册和协商切换都复用同一校验。上级响应授予期限由 D03 既有逻辑负责，D16 只约束本地请求与配置；授予期限不足时按 D03 的实际值安排续期，不把本地请求值冒充对端授予值。

## D11/D12：目录目标展开

在 catalog responder 的查询解析层增加统一目标解析：

- 物理设备目标先在当前上级投影中查找，再展开该设备下属于当前上级的共享子通道；未发布设备、其他设备和无权限通道返回空或明确未知错误，不回退到全局设备目录。
- To 为本平台、行政区划、已发布业务根/虚拟组织以及 XML 指定节点时，统一解析成当前平台投影中的目标集合；To、Request-URI 和 XML 目标冲突时以最小权限交集为准，冲突请求拒绝。
- 保持 SN、SumNum、分批和 2016/2022 BusinessGroupID 序列化差异；不把直接寻址的兼容入口声明成标准强制要求。
- 出站推送和查询复用同一快照，避免查询能看到而推送不存在。

## D13：域名来源匹配

将平台 Host 规范化为 IP 或域名端点：

- IP/IPv6 直接做规范化比较；域名通过注入的 resolver 解析为地址集合，并记录解析版本/过期时间。
- 入站实际源 IP 必须命中当前解析集合，同时端口、传输协议、From 上级 ID 和启用状态都匹配；解析失败默认拒绝，不回退 identity-only。
- 地址变化在缓存过期或受控刷新后生效；已有会话仍绑定建连时实际 peer，后续请求不因 DNS 变化跨会话迁移。
- 目录、信息、PTZ、INVITE、RecordInfo 以及 CANCEL/ACK/BYE/INFO 全部复用同一 matcher。测试使用 fake resolver 覆盖多地址、IPv6、失败和地址变化。

实际采用逐次调用可注入 resolver 的简单实现，没有新增应用层 DNS 缓存和过期状态；新请求使用本次解析集合，解析失败拒绝。已建会话及待处理视频事务继续比较原始实际 peer，DNS 更新不改变其来源绑定。

## D14：媒体源标识与安全替换

解析 INVITE Subject 中发送者 ID、发送方媒体流序列号、接收者 ID 和接收方序列号，形成媒体源标识。内存会话保留完整标识；持久化复用既有 `SDPSummary` 保存四字段 JSON，不新增列或迁移：

- 同一 SIP 事务重传由 Call-ID/CSeq/From tag/Via branch 幂等处理，不触发替换；最终响应重传由 SIP 事务层负责。
- 工程替换槽按 platform、发送者 ID、发送方序列号及 SDP SSRC 隔离；同槽新事务先停止旧 sender，再取消旧会话并等待其租约、sender claim 释放，之后建立新链路。
- 停止失败保留旧链路并返回受控错误，不误删其他消费者；不同上级、不同源序列或不同 SSRC 不互相停止。
- 媒体源标识缺失时沿用现有严格拒绝或独立 key 策略，不能把空序列全部视为同源。
- 用 fake sender/source lease 测试替换成功、停止失败、并发新 INVITE 和多消费者隔离。

发送方序列号按字符串校验：非空、不超过 20 字符、实时首位 0/历史首位 1，不强制为十进制或等于 SSRC。接收方序列号为非空字符串，不套用发送方的首位和长度规则。不同媒体源共享底层 source 且 SSRC 冲突时仍返回 486，不误拆其他消费者。CANCEL 直接绑定原 INVITE 的实际 peer，不重新查询数据库或 DNS 决定是否验证。

## D15：MANSRTSP 范围控制与 RTP-Info

先扩展 `mansrtsp.Request` 和 playback `ActionRequest`，保留原有 npt 起点、暂停、继续及正向倍速；增加时间格式、Range 终点和非法范围校验。npt 与 smpte 的解析规则独立实现，不能把 smpte 秒值直接当作 npt。

控制链路要求：

1. ParseRequest 得到规范化范围。
2. `playback.Action` 校验会话 owner、范围和设备能力，将起点/终点/倍速传到下接设备。
3. 指定终点必须在会话或设备控制层产生实际停止条件；只有解析字段不算完成。
4. PLAY 响应的 RTP-Info 只能来自重新发流后的真实首个 RTP 包序号和时间戳。需要在 sender/媒体探针处捕获首包元数据，并按请求的 URL、seq、rtptime 生成响应；数据尚未捕获时省略可选 seq/rtptime 或延迟响应，绝不填模拟值。
5. 设备失败、越界、权限变化返回对应错误，不把 `Action` 调用成功当作设备已定位。

如果当前下接设备控制接口没有终点或首包元数据能力，则本阶段先完成解析、拒绝非法输入和数据通路接口，留下明确的端到端阻塞项；不宣称 D15 全部完成，也不伪造 RTP-Info。

实施结果：`PlaybackRange` 保留原始 npt/smpte 值及起终点，SMPTE 按默认 30 fps 和百分之一子帧换算，仅用于边界检查。`ActionRequest.RangeValue` 经 service、普通 UAC adapter 或 durable SIP INFO wire 传至设备；缺少完整范围能力返回明确错误。级联 SIP 200 仅确认 INFO 传输，业务错误置于 RTSP 响应（457/501/454/500）。本机 UDP 测试证明完整 Range 报文发出，不证明真实设备按终点停止；现有 ZLM sender 接口只返回 LocalPort，未提供重新发流后的首包 seq/rtptime，因此尚未实现真实上行 RTP-Info。

## 文件与依赖

- D06/D07：`server/app/gb28181/cascade/service/management.go`、`cascade/controller/management.go`、`web/src/views/gb28181/cascade/index.vue` 及其 API/测试。
- D09/D10/D16：`cascade/runtime/runtime.go`、`cascade/sipclient/transaction.go`、`cascade_runtime.go`、模型/仓储和相关测试。
- D11/D12：`cascade/catalog/responder.go`、catalog snapshot/查询处理和 Catalog handler 测试。
- D13：`cascade_invite_handler.go`、`cascade_catalog_handler.go`、信息/PTZ/录像入口、来源匹配辅助及测试。
- D08/D14：`cascade_recording_playback.go`、`cascade_invite_handler.go`、cascade media sender/source、播放清理和 SIP 会话测试。
- D15：`mansrtsp/request.go`、`playback/types.go`、播放服务/设备控制适配器、sender RTP 元数据接口及协议测试。

依赖顺序为：D06/D07/D16 的结果和输入校验 → D10 版本协商 → D09 生命周期 → D11/D12/D13 入口投影 → D08/D14 媒体资源 → D15 回放控制。D03 已有授予期限实现作为 D16/D09 的前置回归，不在本轮重复改造。

## 风险与回退

- 域名解析会让原先依赖 identity-only 的配置失效；这是来源边界收紧的预期兼容影响，测试和错误信息必须说明配置端点。
- 版本缺失或未知的上级继续按当前有效 profile 工作，不能因为一次异常响应切换到高版本。
- 媒体监测探针若无法区分临时查询错误与源流终止，宁可延迟收尾并记录错误，也不能误停共享源。
- D15 的真实 RTP 首包元数据、终点控制可能依赖 ZLM/下接设备能力；缺少能力时保留明确未完成项，禁止伪造协议响应。
- 所有新增状态必须有可控时钟、取消和幂等测试，防止旧 timer、重载和停用产生重复注册或资源泄漏。

## 阶段状态

老板已确认并授权无人值守执行；本地实施完成，D15 保留端到端阻塞。逐项测试结果与未覆盖范围见 `docs/tasks/cascade-p2-fixes.md`。


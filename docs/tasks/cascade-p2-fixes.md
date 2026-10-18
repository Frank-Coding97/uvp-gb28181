---
title: 国标级联 P2 缺陷修复任务与测试清单
status: implemented-with-limitations
spec: ../specs/cascade-p2-fixes.md
plan: ../plans/cascade-p2-fixes.md
date: 2026-10-16
---

## 执行约束

- D06–D16 每项按 RED → GREEN → REFACTOR 执行：先加入能复现当前缺陷的测试，确认修复前失败，再做最小实现，最后跑针对性回归。
- 只修改任务列出的文件；保留工作区其他任务的暂存、未暂存和未跟踪改动，不执行 reset、stash、清理、提交或推送。
- 测试只使用本地 fake、隔离数据库、模拟 SIP/媒体对象和可控调度器；不操作浏览器、真实上级平台、真实设备、开发环境级联台账或部署环境。
- 每项 GREEN 只证明列出的代码行为；真实互操作、设备动作、RTP/PS 媒体包和浏览器呈现另行报告。
- D15 如果当前下接控制或 RTP 首包元数据没有可用来源，允许完成输入校验和数据接口，但必须保留阻塞证据，不能标记为全部完成。

## 任务总览

| 任务 | 缺陷 | 责任范围 | 主要 RED 证据 | GREEN 完成标准 |
|---|---|---|---|---|
| T06 | D06 管理页面误报运行时成功 | cascade management service/controller、级联页面与 API 测试 | Reload 返回错误但 HTTP 200 或页面仍提示注册/删除完成 | 响应明确持久化与运行时结果，页面按 `runtimeSynced` 提示 |
| T07 | D07 删除顺序和结果不准确 | management service/controller、删除测试 | 首次 Reload 失败后仍软删除或返回 `ok:true` | 活动会话拒绝；删除状态和同步失败结果可重试且 API/UI 一致 |
| T08 | D08 媒体结束监测覆盖不足 | playback monitor、sender/source fake、会话清理测试 | TCP、关闭 RTCP、源流消失或停止失败时会话不收尾/误释放 | 统一监测和幂等收尾，暂时探测错误不误判终止 |
| T09 | D09 心跳失败不触发恢复注册 | runtime actor、可控 scheduler/clock、状态持久化 fake | 连续超时只记错，未注销旧周期或重注册 | 达到阈值后退避重注册，串行、可取消、持久化一致 |
| T10 | D10 auto 未协商注册版本 | transaction、runtime/profile、repository 测试 | REGISTER 响应的 `X-GB-Ver` 不影响后续 profile/持久化 | 2.0/3.0 正确协商，缺失/未知不污染固定 profile |
| T11 | D11 物理设备目标无法查询共享子通道 | catalog snapshot/responder、Catalog handler | 目标为已发布设备时返回未知目标或全局资源 | 仅返回当前上级该设备的共享子通道，批次/SN/隔离正确 |
| T12 | D12 业务组/虚拟组织入口受限 | catalog handler/responder、目标冲突测试 | To 直接指向已发布业务节点被拒，或冲突目标扩大权限 | 本平台、组织目标和 XML 目标按最小权限交集解析 |
| T13 | D13 域名 Host 匹配错误 | peer matcher、Catalog/Info/PTZ/INVITE/RecordInfo 入口 | 合法域名解析地址被拒，或 identity-only 来源被放行 | 解析地址集合、端口、协议、From、启用状态全部校验 |
| T14 | D14 同媒体源重新请求无法安全替换 | Subject parser、sender/session registry、INVITE 测试 | 同源新 INVITE 486；替换时误停其他消费者或泄漏旧 sender | 按源标识幂等替换，停止失败保留旧链路 |
| T15 | D15 回放 Range/RTP-Info 不完整 | mansrtsp、playback Action、sender 元数据 | smpte/终点请求被拒或只解析不约束；响应伪造/缺失真实 RTP 信息 | 合法范围贯通控制；RTP-Info 只使用真实首包值 |
| T16 | D16 注册期限允许小于标准最小值 | management validation、前端校验、注册请求测试 | 60 秒或 3599 秒配置成功并发 REGISTER | 正值最小 3600 秒，0 仅注销，前后端边界一致 |

## 任务明细

### T06：管理操作结果契约

责任文件：`server/app/gb28181/cascade/service/management.go`、`cascade/controller/management.go`、`web/src/views/gb28181/cascade/index.vue`、对应 service/controller/Vitest 测试。

RED：

1. fake runtime 的 Reload 返回错误，Create/Update/SetEnabled/Delete 的持久化已经成功；断言 API/UI 当前不应显示“已注册/已注销/删除完成”，并验证响应包含已保存资源和同步失败标记。
2. Reload 成功但 REGISTER 只是异步发起；断言页面不能把请求发出当作已注册完成。
3. 持久化失败时断言不返回成功资源、不触发运行时 Reload，保留原错误反馈。

GREEN：

- controller 对成功同步、已持久化但未同步、持久化失败分别输出稳定结构和状态。
- 前端 toast、刷新和行状态只读取结果契约；同步失败提示“已保存/已删除，运行时待同步”。
- 普通成功路径和请求失败路径保持当前用户可见语义。

回归：

`go test ./app/gb28181/cascade/service ./app/gb28181/cascade/controller ./app/gb28181/routes -count=1`

`pnpm exec vitest run src/views/gb28181/cascade src/api/gb28181-cascade.test.ts`

### T07：删除顺序与软删除结果

责任文件：`cascade/service/management.go`、`cascade/controller/management.go`、repository fake/测试、删除 API 测试。

RED：

1. 启用平台第一次 Reload 失败：断言不能返回 `ok:true`，并返回 `deleted:false`、`runtimeSynced:false` 和已停用的平台视图；下一次删除请求可以继续软删除。
2. 软删除完成后第二次 Reload 失败：断言 API 返回 `deleted:true`、`runtimeSynced:false`，不能把运行时同步说成完整成功。
3. 存在非终态媒体会话：断言删除被拒，Update/SoftDelete/Reload 均不调用。
4. 持久化失败：断言不调用 Reload。

GREEN：

- service 通过结构化 DeleteResult 返回 `deleted` 和 `runtimeSynced`，controller 不再自行把任意 `ErrRuntimeSyncFailed` 映射成 `ok:true`。
- 页面删除提示、刷新和列表状态与 API 结果一致。
- 重试不会因前一次已停用/软删除产生唯一键或状态冲突。

回归：

`go test ./app/gb28181/cascade/service ./app/gb28181/cascade/controller -run 'Delete|Enabled|RuntimeSync' -count=1`

### T08：媒体源消失和发送器收尾

责任文件：`cascade_recording_playback.go`、`cascade_invite_handler.go`、cascade media sender/source fake、播放清理测试。

RED：

1. RTCPKeepalive=false 的 UDP 会话源播放进入终态或源流消失：当前监测不能只依赖 RTCP，断言会话最终 BYE、停止 sender、释放播放租约。
2. TCP active/passive 发送器源流消失：断言进入同一收尾路径。
3. 探测暂时返回错误：断言不会立即 BYE/释放；连续错误达到阈值后才按策略收尾。
4. stopSender 返回错误：断言旧源仍被占用、不会先释放共享源，并可重试。
5. 两个上级或两个消费者共享同一源：一个结束不能停止另一个 sender。

GREEN：

- 探测、停止和收尾接口可注入；UDP/TCP、RTCP 开关行为一致。
- 收尾顺序为停止 sender → BYE/停止播放 → 删除会话；每一步幂等且记录原因。
- 失败不泄漏 session、sender key 或播放租约。

回归：

`go test ./app/gb28181 -run 'Cascade.*(Playback|Video|Sender|Cleanup)' -count=1`

`go test -race ./app/gb28181 -run 'Cascade.*(Playback|Video)' -count=1`

### T09：心跳故障恢复注册

责任文件：`cascade/runtime/runtime.go`、`runtime_test.go`、必要的 model/repository 状态字段。

RED：

1. fake Keepalive 连续 1–2 次超时：断言不触发重注册。
2. 连续达到默认 3 次超时：断言记录离线、停止旧 refresh/keepalive timer，并只排队一次 Register。
3. 重注册事务阻塞期间重复 timer/心跳事件：断言无并发维护事务，释放后只执行合并意图。
4. 重注册失败：断言按 RetryDelay 退避，不忙循环；成功后恢复 refresh、首个 keepalive 和持久化在线状态。
5. actor Stop/Reload 期间旧 timer 回调：断言不再发起注册或心跳。

GREEN：

- 每个 actor 维护状态、pending 意图和事务均串行。
- 超时、明确拒绝、成功和停止状态均有不同错误码/持久化结果。
- scheduler/clock 可控，race 测试无数据竞争和重复任务。

回归：

`go test ./app/gb28181/cascade/runtime -count=1`

`go test -race ./app/gb28181/cascade/runtime -count=1`

### T10：auto 注册版本协商

责任文件：`cascade/sipclient/transaction.go`、`transaction_test.go`、`cascade_runtime.go`、model/repository/profile 测试。

RED：

1. 200 REGISTER 最终响应带 `X-GB-Ver: 2.0`/ `3.0`：断言 transaction 结果当前没有版本信息，auto 后续请求仍固定默认。
2. 摘要 401 后的最终 200 带版本：断言只以最终成功响应更新。
3. 缺失、空白、未知值和失败响应：断言不覆盖已有有效 profile。
4. 固定 2016/2022 override：断言任何响应都不改写有效版本。
5. 重启/重新加载平台：断言 negotiated version 和来源持久化后仍选择一致 profile。

GREEN：

- transaction 返回受支持版本、缺失、无效、未知的区分结果。
- auto 首次注册使用兼容默认，协商成功后持久化并用于后续 REGISTER/Catalog/SDP/MANSRTSP。
- 不向声明较低版本的上级发送其不能识别的高版本字段。

回归：

`go test ./app/gb28181/cascade/sipclient ./app/gb28181/cascade/runtime ./app/gb28181/cascade -run 'Version|Profile|Register' -count=1`

### T11：物理设备 Catalog 目标

责任文件：`cascade/catalog/snapshot.go`、`responder.go`、Catalog handler/query 测试。

RED：

1. snapshot 含已发布物理设备及其两个共享子通道，请求 DeviceID 为物理设备：断言当前返回未知目标或不含子通道。
2. 同一设备存在未共享通道、另一上级同源通道：断言查询只返回当前平台的已共享子通道。
3. 无子通道、设备未发布、设备 ID 冲突：断言结果为空或稳定未知错误，不泄露全局目录。
4. 分批查询：断言 SN、SumNum、Num 和批次顺序保持。

GREEN：

- 物理设备目标先解析为当前 platform snapshot，再按 SourceDeviceID 展开已发布 channel。
- 与业务组/行政区划目标共用快照和序列化路径，不改变 2016/2022 字段位置。

回归：

`go test ./app/gb28181/cascade/catalog ./app/gb28181 -run 'Catalog.*(Device|Query|Batch)' -count=1`

### T12：业务组/虚拟组织实际入口

责任文件：`cascade_catalog_handler.go`、`cascade/catalog/responder.go`、入口和业务目录测试。

RED：

1. To 为本平台、XML DeviceID 为已发布业务根/虚拟组织：断言当前入口拒绝或查询不到组织后代。
2. To 直接指向当前上级已发布业务节点：断言当前不支持的路径被拒。
3. To、Request-URI 与 XML 目标互相冲突，或节点属于另一上级/未发布：断言不能扩大查询范围。
4. 已发布组织含嵌套组织和频道：断言后代筛选、BusinessGroupID 和 ParentID 正确。

GREEN：

- 目标解析同时支持本平台入口和已发布业务节点入口，但始终限制在当前 platform snapshot。
- 冲突目标取最小权限交集或明确拒绝；不回退到全局目录。
- 保持 2016 Info 与 2022 Item 的 BusinessGroupID 差异。

回归：

`go test ./app/gb28181/cascade/catalog ./app/gb28181 -run 'Catalog.*(Business|Organization|Target)' -count=1`

### T13：域名来源匹配

责任文件：`cascade_invite_handler.go`、`cascade_catalog_handler.go`、信息/PTZ/录像入口、matcher 测试。

RED：

1. 平台 Host 为域名，fake resolver 返回 IPv4/IPv6 多地址：断言任一合法解析地址可通过，端口/传输协议/From 不匹配仍拒绝。
2. resolver 失败或结果过期：断言拒绝，不走 identity-only 回退。
3. DNS 地址变化：新地址在刷新后生效，旧地址按缓存策略失效；已建会话仍绑定建立时 peer。
4. 对 Catalog、Info、PTZ、INVITE、RecordInfo、CANCEL/ACK/BYE/INFO 分别输入合法和异源报文，断言全部使用同一 matcher。

GREEN：

- matcher 接受规范化 IP/IPv6 和注入 resolver 地址集合。
- 来源判断同时要求实际源地址、端口、transport、From 上级 ID、启用状态和必要的共享授权。
- 会话后续请求不因 DNS 变化跨来源迁移。

回归：

`go test ./app/gb28181 -run 'Cascade.*(Peer|Source|Catalog|Info|PTZ|Record)' -count=1`

### T14：Subject 媒体源替换

责任文件：`cascade_invite_handler.go`、Subject 解析辅助、media sender/source、INVITE 和清理测试。

RED：

1. 同一 platform、同一发送者 ID+发送方媒体流序列号、不同 Call-ID 的新 INVITE：断言当前返回 486 或未释放旧链路。
2. 同一 SIP 事务重传：断言不重复停止/重建。
3. 不同 platform、不同源序列或不同 SSRC：断言互不影响。
4. 停止旧 sender 失败：断言新链路不覆盖旧状态，资源可重试；并发新 INVITE 不产生两个活动 sender。
5. Subject 缺少序列或格式非法：断言不把所有空值归为同源。

GREEN：

- 解析并保存发送者 ID、发送方媒体流序列号、接收者 ID、接收方序列号。
- 同源替换在源级锁内完成：停止成功后建立新 sender；失败返回受控 SIP 错误并保留旧链路。
- 会话表、sender key、数据库租约和清理路径保持 platform/consumer 隔离。

回归：

`go test ./app/gb28181 -run 'Cascade.*(Invite|Subject|Sender|Cleanup)' -count=1`

`go test -race ./app/gb28181 -run 'Cascade.*(Invite|Sender)' -count=1`

### T15：MANSRTSP Range、终点和 RTP-Info

责任文件：`mansrtsp/request.go`、`mansrtsp/request_test.go`、`playback/types.go`、playback service/适配器、`cascade_recording_playback.go`、sender RTP 元数据测试。

RED：

1. 解析 `npt=start-end`、`smpte=start-end`、暂停/继续和既有倍速：断言当前不支持终点或 smpte。
2. 终点小于起点、负值、NaN、无效帧格式、越过会话时长：断言当前可能只按起点成功，修复前测试失败。
3. 合法终点进入 playback Action：断言下接控制命令没有终点字段或不会在终点收尾。
4. PLAY 响应：断言当前没有与首个 RTP 包对应的真实 seq/rtptime；禁止用固定或随机值通过测试。
5. 设备控制失败、owner 变化或会话权限变化：断言不返回 200 成功。

GREEN：

- Request/ActionRequest 携带时间格式、起点、可选终点和 scale；解析与范围校验独立于 npt/smpte。
- playback service 和下接适配器执行实际终点约束；无法执行时返回明确能力/设备错误。
- sender/探针捕获重新发流后的真实首个 RTP seq/rtptime；RTP-Info 携带时必须是真实值，未捕获时省略可选字段或延迟响应，不伪造。
- 保留 owner 隔离、暂停/继续、正向倍速和停止语义。

回归：

`go test ./app/gb28181/mansrtsp ./app/gb28181/playback ./app/gb28181 -run 'Playback|MANSRTSP|RTPInfo|Range' -count=1`

如新增 sender/registry 并发状态，再执行对应包的 `go test -race`。

### T16：注册期限最小值

责任文件：`cascade/service/management.go`、平台请求 DTO、`web/src/views/gb28181/cascade/index.vue`、`sipclient/request_factory.go` 及测试。

RED：

1. HTTP/API 输入 60、3599：断言当前后端接受并可生成 REGISTER。
2. 3600：断言前端和后端边界行为不一致（前端可能提示可用，后端仅检查大于 0）。
3. 0：在普通注册配置中断言当前未明确区分注销语义。
4. auto 首次注册、协商切换和存量短周期平台：断言可能继续携带不合规期限。

GREEN：

- 正值统一要求 `>=3600`；0 只由注销事务使用，普通平台配置拒绝。
- 前端最小值、后端校验、默认值和 REGISTER 请求工厂一致。
- 存量短周期配置在读取/更新/启动时有明确迁移或拒绝策略，不静默发送小于 3600 的正期限。
- 固定 2016、2022、auto 都满足同一最小值；上级实际授予期限仍由 D03 结果单独处理。

回归：

`go test ./app/gb28181/cascade/service ./app/gb28181/cascade/sipclient ./app/gb28181/cascade -run 'Expires|Register' -count=1`

`pnpm exec vitest run src/views/gb28181/cascade src/api/gb28181-cascade.test.ts`

## 任务依赖和执行顺序

1. T06、T07、T16：先稳定 API 结果和输入边界。
2. T10：版本结果结构和 profile 选择。
3. T09：在期限与版本结果稳定后修复心跳失效重注册。
4. T11、T12、T13：统一 Catalog 目标与来源入口。
5. T08、T14：媒体生命周期和同源替换。
6. T15：最后贯通回放控制和真实 RTP 元数据。

任务之间共享 runtime、入口 matcher 和媒体 session 状态；同一文件只在对应任务执行时修改，避免并行写冲突。

## 完成定义

- 每个任务至少有一个修复前失败的 RED 测试；GREEN 后通过新增测试和相关回归。
- T09、T14、T15 的并发或时序路径在需要时通过 race 检查。
- D15 缺少真实首包/终点能力时，任务状态必须标注“接口完成、端到端阻塞”，不能报告全项通过。
- 实现阶段结束后把实际测试命令、结果和未覆盖的真实互操作风险回填本文件；不另建测试或验收文档。

## 阶段状态

老板已确认规格、方案和任务并授权无人值守执行。已完成本地修复与测试；T15 只完成范围控制通路，保留真实终点与 RTP-Info 阻塞，不能整体标为完成。

## 2026-10-16 实施结果

| 任务 | 当前状态 | 实际结果及证明边界 |
|---|---|---|
| T06 | 本地完成 | 保存、启停、删除读取结构化 `runtimeSynced`，区分配置已保存和运行时待同步；163 项前端级联/API 回归通过，不代表浏览器呈现已验收。 |
| T07 | 本地完成 | 删除返回 `deleted` 与同步结果；首次停用同步失败保留可重试平台，活动媒体会话仍拒绝删除；service/controller 回归通过。 |
| T08 | 本地完成 | UDP（RTCP 开/关）及 TCP 主/被动统一 sender 检查；成功探测清零、连续 3 次错误收尾；停止成功后才 BYE、释放源。停止失败保留租约；播放清理和替换共享幂等停止入口。 |
| T09 | 本地完成 | 连续心跳超时达到阈值触发离线及退避重注册；维护事务、合并意图和取消路径通过 runtime 竞态回归。 |
| T10 | 本地完成 | 最终 REGISTER 响应携带版本进入 auto 协商及持久化；固定版本不受覆盖，缺失/未知不污染已有版本；sipclient/runtime 回归通过。 |
| T11 | 本地完成 | 物理设备目标只展开当前上级已发布子通道；目录查询、空结果、分批和权限隔离回归通过。 |
| T12 | 本地完成 | 支持已发布业务节点入口，校验 To/Request-URI/XML 目标，拒绝冲突；2016/2022 业务组字段位置回归通过。 |
| T13 | 本地完成 | 域名解析集合参与严格来源匹配，后续会话绑定实际 peer；补复现并修复地址配置变化后异源 CANCEL 被放行的问题。 |
| T14 | 本地完成 | 完整 Subject 保存到内存和既有 SDPSummary；同槽替换先停旧 sender、等待旧资源释放后重建；重传幂等、停止失败、并发请求及源序列隔离通过竞态测试。 |
| T15 | 部分完成 | npt/smpte 起终点解析、非法/越界/NaN 校验、service 与普通/durable INFO 下接均贯通；本机 UDP 校验完整报文。真实设备终点行为未验收，真实上行 RTP-Info 尚未实现。 |
| T16 | 本地完成 | 正注册期限最小 3600 秒，普通配置拒绝 0，注销才使用 0；前后端边界及请求生成回归通过。 |

### 本轮明确观察的 RED 与修复

- T14：缺失/非法发送序列原先被接受；同源新请求原先返回 486；旧 sender 停止失败原先返回 486，修复后返回 503 并保留旧会话和源。
- T15：原 parser 拒绝合法 npt 终点和 smpte；原 service 丢弃完整 Range，且允许 NaN 起点进入设备控制。修复后拒绝非法值，保留时间制和终点下发。
- T13 补充：`TestCascadeCancelKeepsOriginalPeerAfterPlatformEndpointChanges` 修复前失败。原逻辑按当前配置重新识别原 INVITE，未匹配就跳过验证；修复后直接比较原事务实际 peer，配置/DNS/数据库变化不再降低来源约束。
- 本轮追加测试曾因测试辅助调用误用未定义的 `stopFailures` 编译失败，已修正；此编译错误不作为业务缺陷 RED 证据。

### D15 未完成项与阻塞证据

1. 指定终点已进入下接 INFO 的实际 Range，缺能力的旧 adapter 返回 `ErrPlaybackRangeUnsupported`，设备拒绝不会更新本地位置；但当前没有真实设备媒体包证明其按终点停止。没有用本机墙钟定时器冒充设备播放时间。
2. `zlm.GBSendRTPResult` 当前只有 `LocalPort`，`IsGBSendRTPActive` 仅返回 sender 存在状态；没有上行重新发流后首包 seq/rtptime 数据来源。下接 RTP 或 MANSRTSP 信息不能证明上行重新发流后的首包值，因此尚未生成 RTP-Info。
3. SIP 200 与 RTSP 业务成功分别处理；现有设备兼容路径可能接受无 RTSP 消息体的 SIP 2xx，这仍不能证明实际定位或终点执行。fake 接受及本机 UDP 报文测试不等于真实设备动作或完整国标互操作验收。

### 已确认的回归结果

- `go test -race ./app/gb28181 ./app/gb28181/cascade/... ./app/gb28181/playback ./app/gb28181/mansrtsp ./app/gb28181/uac ./app/gb28181/playauth ./app/gb28181/sdp ./app/gb28181/sip ./app/gb28181/recordquery -count=1`：20 个包全部通过；其中 UAC 全包含隔离子进程及本机 SIP 测试，耗时约 307 秒。随后追加的回放替换测试由下一条专项覆盖。
- `go test -race ./app/gb28181 ./app/gb28181/playback ./app/gb28181/mansrtsp ./app/gb28181/uac ./app/gb28181/playauth -run 'Cascade|PlaybackRange|ParseRequestPlayback|DeviceSIPINFORange|TestPlaybackIntentINFOActualUDP' -count=1`：5 个包通过。
- `go test -race ./app/gb28181 -run 'TestCascadeSubjectPlaybackReplacement|TestCascadeCancelKeepsOriginalPeer' -count=1`：通过；补充验证回放同源替换先释放旧源、停止失败不建立新源，以及配置变化后的 CANCEL 来源绑定。
- `go test ./app/gb28181/zlm -run 'Test(StartGBSendRTP|IsGBSendRTPActive)' -count=1`：通过；仅证明 fake HTTP 参数、sender 状态与错误行为。
- `go test ./app/gb28181/routes -run Cascade -count=1`：通过；`go test ./app/gb28181/config -count=1`：通过（与 routes 全包联合执行时 config 的独立结果）。
- `pnpm exec vitest run src/views/gb28181/cascade src/api/gb28181-cascade.test.ts`：7 个文件、163 项通过。
- `git diff --check -- server/app/gb28181 docs/specs/cascade-p2-fixes.md docs/plans/cascade-p2-fixes.md docs/tasks/cascade-p2-fixes.md`：通过。

### 回归限制

- routes 全包回归中，4 个无关权限迁移测试引用已不存在的历史 SQL 文件：alarm-management、custom-group-permissions、device-config-family、playback-scheme-permissions；没有为此恢复旧迁移或修改无关测试。config 全包通过。
- 此前 ZLM 全包的日志断言 `TestLoggingGBHTTPZLMIsMediaOnlineTransportFailureUsesSafeError` 失败，本轮只重跑 GB sender 专项，不把日志断言差异认定为发流功能失败。
- 没有浏览器、真实上级、真实设备、开发台账、RTP/PS 实流验收；没有部署、提交或推送。

## 2026-10-17 T06 复核补充

- 新增服务层测试覆盖 Create/Update/SetEnabled 在 Reload 成功时标记 `runtimeSynced=true`，失败时返回已提交对象并标记 `false`；RED 发现 Create 失败对象未带明确状态，已修复，三类操作专项通过。
- 新增 Controller + 隔离 SQLite HTTP 测试验证 Create/Update/SetEnabled 的降级响应、配置持久化及状态标记。Controller 现在返回固定中文 `warning`，不把底层运行时错误详情直接暴露给前端。专项通过。
- 前端新增 `cascadeRuntimeSyncPending` 响应判定及 direct/wrapped payload 测试；新增级联管理页面使用该判定后，成功仍描述为“正在向上级注册”，失败提示运行时待同步。级联/API Vitest 7 个文件、164 项通过。
- `go test ./app/gb28181/cascade/service ./app/gb28181/cascade/controller ./app/gb28181/cascade/... -count=1`：级联 service/controller 及 12 个 cascade 子包通过。
- `pnpm exec vue-tsc --noEmit` 未通过，报错集中在既存 DeviceFirmwareUpgradePanel、firmware-repo 和 ZLM NodeForm 测试/类型；本轮 T06 文件不在错误列表中，未修改这些无关模块。
- `git diff --check` 对 T06 涉及文件通过。仍未做浏览器实机呈现验收；该结论只覆盖本地服务、HTTP 和 Vitest 行为。


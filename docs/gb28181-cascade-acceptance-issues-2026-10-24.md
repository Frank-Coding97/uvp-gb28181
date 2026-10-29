# 国标级联验收 — 问题清单

- 起始日期：2026-10-24
- 被测版本：`develop` @ `6c0b76df`（1.1.13），220 实例
- 对应计划：[gb28181-cascade-acceptance-plan-2026-10-24.md](./gb28181-cascade-acceptance-plan-2026-10-24.md)
- 规则：**阻断类立即修复并回测；非阻断类先记录，本阶段跑完统一修复后一次性回测**

## 状态说明

| 状态 | 含义 |
|---|---|
| 待复现 | 现象已出现，但尚未稳定复现或尚未定性 |
| 待定性 | 行为已确认，正在判断是缺陷 / 设计如此 / 对端差异 |
| 待修复 | 已确认为缺陷，尚未修复 |
| 修复中 | 已改代码，未部署 |
| 待回测 | 已部署，等待回测 |
| 已关闭 | 回测通过 |
| 非缺陷 | 定性为设计如此 / 对端限制 / 环境问题，附依据 |

## 清单索引

| ID | 标题 | 阻断 | 状态 | 关联用例 |
|---|---|---|---|---|
| P-001 | 打开"发布行政区划/平台自身"后，对端丢弃全部真实通道 | 否（非默认开关） | 待修复 | A-CAT-08 |
| P-002 | 共享 `scope=all` 不生成投影与发布编号 | 否 | 待定性 | A-SHR-02 |
| P-003 | `push-catalog` 超时 30s 才返回 | 否 | 待定性 | A-CAT-03 |
| P-004 | WVP 曾把我方判为设备离线 | 否 | 待复现 | A-REG-02/03 |
| P-005 | 我方与 WVP 之间 SIP 源地址不对称（VPN 回包） | 否 | 待定性 | A-LIVE-01 |
| **P-006** | **级联 SIP 请求的实际源端口与 Via/Contact 声明不一致，WVP 丢弃目录消息** | **是** | **已关闭** | A-CAT-01/03 |
| P-007 | WVP 收到分批目录时报 `[缓存-Catalog] 未找到缓存对象` | 否 | 非缺陷 | A-CAT-01 |
| P-008 | 上级对「平台自身编码」发 ConfigDownload / DeviceControl 时我方回 403 | 否 | 待决策 | A-CFG/A-CTL |
| P-009 | 上级用 `Expires: 0` 首次订阅时，我方回 481 | 否 | 非缺陷 | A-EVT |
| P-010 | 我方 REGISTER 应答的 `Date` 头不是 RFC 1123 格式 | 否 | 待修复 | A-REG |
| P-011 | ZLM 残留 RTP 收流端口（52044）与僵尸流，重启后无人收尾 | 否 | 待定性 | A-LIVE-05 |
| P-012 | `stopSendRtp` 调用失败率高（14 次里 9 次 failed） | 否 | 待定性 | A-LIVE-05 |
| P-014 | 点播失败会在设备侧留下会话，之后该通道约 90 秒内点播全部超时 | 否 | 待定性 | A-LIVE-05/08 |
| P-013 | 录像条目多的日子查询必然失败：设备 `SumNum` 与实际条目不符 ⇒ 判「不完整」⇒ 一条都不回 | **是（回放主干）** | **已关闭** | A-REC-01/03 |
| P-015 | 级联目录不下发 `<Info><PTZType>`，EasyGBS 据此判定「云台类型未知」并**完全不给云台入口** | **是（云台用例）** | **已关闭** | A-PTZ-01 |
| P-016 | 级联语音广播整条链路不通：应答判身份不符 → 音频 INVITE 匹配不上 → 认领与应答抢同一行 | **是（广播/对讲用例）** | **已关闭** | A-AUD-01/02 |
| P-017 | 目录订阅被接受后的首个 NOTIFY 只带差量，目录无变化时 `SumNum=0`（等于告诉上级「没有任何资源」） | 否 | 待决策 | A-SUB-01 |
| P-018 | 同一对话框刷新/取消时若对端轮换了 `Event: …;id=`，我方按新会话找不到而回 481 | 否 | 待定性 | A-SUB-03 |
| **P-019** | **「下级注销后级联列表仍显示在线」= 误判**（该行是「上级方向」的注册事实，与下级无关） | 否 | **非缺陷** | B-REG-04 |
| P-020 | 播放控制台「拉流超时」后反复重试永远超时（失败实例未被销毁） | 否 | **已关闭** | B-LIVE-01/03/04 |
| P-021 | 停播收尾告警被当成失败 ⇒ 整个 DELETE 返回 400（提示「停止流失败」），随后该通道无法正常重播 | 否 | **已修复** | B-LIVE-02 |
| P-022 | 下级不提供 `DeviceName` 时设备名为空串，列表把同一个编码显示两遍（名称列 + 编码列） | 否 | 待修复 | B-REG-01 |
| P-023 | 对端不支持的 `Event: PTZPosition` 订阅会按 1s/2s/4s… 无限重传，无退避上限 | 否 | 待定性 | B-CTRL-01 |

---

## P-001 打开"发布行政区划/平台自身"后，对端丢弃全部真实通道

- **发现时间**：2026-10-24 08:5x
- **阶段 / 用例**：A-CAT / A-CAT-08
- **对端**：LiveGBS（WVP 表现待复核）
- **现象**：
  1. 创建上级平台时把 `publishCivil`（发布行政区划）与 `publishPlatform`（发布平台自身）设为 `true`；
  2. 配置 2 个设备 + 2 个通道共享，发布编号 `34020000011320000001/2`、`34020000011310000001/2`；
  3. 推送目录后，LiveGBS 日志里能看到两个通道的条目，但数据库 `t_channels` 一条都没入库，只落地了"平台自身"那一条；
  4. 把两个开关改成 `false` 重新推送，两个通道立刻正常入库。
- **影响**：打开这两个开关的用户，目录推送过去但对端没有通道。**前端新建默认值是关闭**（`cascadeState.ts:280-281`，`publishPlatform:false`、`publishCivil:false`），所以默认路径不受影响，非阻断。
- **初步判断**：区划条目（行政区划节点）的编号可能不是 20 位国标编码，对端解析到非法条目后**整包丢弃**或提前终止。
- **待办**：
  1. 抓一次 `publishCivil=true` 时的目录 XML，核对每个 `<DeviceID>` 位数与 `<ParentID>`；
  2. 若确认为非法编号，修法二选一：区划条目改用合法 20 位编号；或发布区划时把区划节点与真实通道**分包发送**，避免互相污染；
  3. 补一条回归用例：开启区划发布后，对端必须仍能收到真实通道。
- **复核（2026-10-24 12:23，WVP）**：单独把 `publish_platform` 打开（`publish_civil` 保持关闭）后，
  目录里多出「平台自身」条目 `34020000012000000001 / UVP GB28181`，两个通道的条目**带上
  `<ParentID>34020000012000000001</ParentID>`**，WVP 侧一条不少（通道 1042/1043 仍在，另加一条节点行 1049）。
  并且此前被卡住的两件事同时通了：`subscribe/alarm?id=35` 与 `subscribe/mobile-position?id=35` 都能订阅成功。
  ⇒ **问题更可能出在「区划（civil）条目」而不是「平台自身条目」**，本项修法应收窄到区划节点编号/分包；
  至于默认是否把 `publish_platform` 打开，属产品选择（默认关闭时报警与位置订阅对 WVP 不可用）。
- **状态**：待修复

## P-002 共享 `scope=all` 不生成投影与发布编号

- **发现时间**：2026-10-24 08:4x
- **阶段 / 用例**：A-SHR / A-SHR-02
- **现象**：
  - `PUT /api/gb28181/cascade/platforms/{id}/shares`，body `{"scope":"all","devices":[],"channels":[],"expectedProjectionRevision":0}`；
  - 接口返回成功，但 `gb_cascade_device_projection` / `gb_cascade_channel_projection` 为空，对端拿不到任何通道；
  - 必须显式给出 `devices[].publishedDeviceId` 与 `channels[].publishedChannelId` 才有投影。
- **影响**：非阻断（可显式指定绕过）；但如果前端"共享全部"按钮走的就是 `scope=all`，则用户点"全部"等于没共享。
- **待办**：确认前端"全部"入口实际提交的 body；确认 `scope=all` 是否本就要求调用方补发布编号（若是，接口应报参数错误而不是静默成功）。
- **状态**：待定性

## P-003 `push-catalog` 超时 30s 才返回

- **发现时间**：2026-10-24 08:5x
- **阶段 / 用例**：A-CAT / A-CAT-03
- **现象**：
  - 第一次对 WVP 调 `POST /platforms/1/push-catalog`，请求挂到 30s 超时；
  - 当时 WVP 里另有一条"我方作为上级"的反向平台配置（`UVP-220`），与本平台的注册形成互相指向；
  - 移除反向配置后，同一接口秒级返回成功。
- **影响**：非阻断，但"失败要 30 秒才知道"体验差，且易被误判成目录功能坏了。
- **待办**：确认超时是否来自等待对端应答（同步等 200 OK）；建议目录推送改为异步 + 超时可配（如 10s），并对"无应答"给出明确错误码。
- **状态**：待定性

## P-004 WVP 曾把我方判为设备离线

- **发现时间**：2026-10-24 09:09
- **阶段 / 用例**：A-REG / A-REG-02、A-REG-03
- **现象**：WVP 容器日志出现把 `34020000012000000001` 判为离线的记录，而同一时段我方 `registration=registered`、`heartbeat=healthy`；移除环配置后 WVP 侧恢复在线。
- **影响**：非阻断（已自愈），但若复现则是"我方以为在线、对端以为离线"，会导致上级点播失败。
- **待办**：在干净配置下连续观察 10 分钟，比对两侧心跳计数；若复现，抓 `MESSAGE Keepalive` 与 WVP 侧接收日志。
- **状态**：待复现

## P-005 我方与 WVP 之间 SIP 源地址不对称

- **发现时间**：2026-10-24 08:3x
- **阶段 / 用例**：A-LIVE / A-LIVE-01（预判风险）
- **现象**：我方发往 `192.168.10.220:8160`，WVP 回包源地址为 `10.8.0.2:8160`（VPN 网段）。信令目前可通。
- **影响**：潜在阻断点播 —— 若 SDP 里的媒体地址按对端回包源地址推导，会填成 VPN 地址，导致上级收不到流（"注册成功、点播黑屏"的典型成因）。
- **待办**：点播时抓我方发出的 SDP，确认 `c=` 与 `m=` 地址为 `192.168.10.220`（媒体服务地址），而非回包源地址。
- **状态**：待定性

## P-006 级联 SIP 请求的实际源端口与 Via/Contact 声明不一致，WVP 丢弃目录消息

- **发现时间**：2026-10-24 09:30–09:55
- **阶段 / 用例**：A-CAT / A-CAT-01、A-CAT-03（阻断主干）
- **对端**：WVP（LiveGBS、EasyGBS 均正常，见下）
- **现象**：
  1. 我方 → WVP 注册成功、心跳 `healthy`、WVP 侧设备 `onLine=true`，但该设备下**通道数恒为 0**；
  2. 同一份目录（同一个共享配置、同一份快照）推给 LiveGBS 与 EasyGBS，**两家都入库 2 条通道，中文名正常**（EasyGBS `channels` 表可见"水星开发测试机器01"）；
  3. WVP 主动发起目录查询后，我方日志显示 `cascade.catalog.response_completed items=2 batches=2`，即我方确实回了响应；WVP 侧仍无通道；
  4. WVP 容器日志（含 ERROR/WARN）中**没有任何目录处理记录**，只有 SIP 层自动应答。
- **已取到的证据**：
  - 抓包（假上级 sink）：我方 MESSAGE 的 `Via` 声明 `192.168.10.220:52064`，但**实际源端口是 54542**（每次推送重新分配）；
  - WVP 数据库中该设备 `port=48845`、`host_address=192.168.10.220:48845`，即 WVP 按注册来源记录了另一个端口；
  - `ss` 显示 SIP 服务端确实监听 `*:52064`（UDP+TCP），但发送侧另开了随机端口；
  - 手工从随机端口（45110）向 WVP 发一份同样格式、带 `ParentID` 的目录消息，WVP **回了 `200 OK`**（SIP 层收到），但**通道仍为 0 条**（业务层丢弃）。
- **代码定位**：
  - 发送入口 `server/app/gb28181/cascade_runtime.go:337` → `sip/server.go:306` `sipgo.NewClient(s.ua, ...)`（未指定本地绑定地址，UDP 发送时新建 socket，源端口随机）；
  - `Via`/`Contact` 由 `server/app/gb28181/cascade/sipclient/request_factory.go:87-95`、`124-131` 用配置的 `LocalIP:LocalPort` **手工写入**，与真实 socket 不一致。
- **影响**：WVP 认为消息来源与注册地址不是同一设备，业务层静默丢弃 → 目录推不进去 → 上级无从点播，A-LIVE 之后的用例全部无法执行。**阻断。**
- **修复方向**（待实施）：
  1. 首选：让级联发送复用已绑定 `localSipIp:localSipPort` 的传输通道（或让 UA 的 transport 以该地址为本机地址发送），使 **实际源端口 = Via/Contact = 配置端口**；
  2. 备选：不手工写 `Via`/`Contact`，交由 sipgo 按真实 socket 填充（代价是对端记录的端口会随进程重启变化，不利于稳定对接，因此只作兜底）；
  3. 修完必须回归：WVP 目录入库、我方 `register/ heartbeat` 不受影响、LiveGBS/EasyGBS 仍正常、TCP（被动/主动）点播可用。
- **附带**：P-004（WVP 判离线）很可能与本条同源（心跳 MESSAGE 同样从随机端口发出），修复后一并回测。
- **结论：已修复，两个根因**（2026-10-24 10:30 部署回测通过）
  1. **源端口漂移**：`sipgo` 的连接池按「地址字符串」索引，通配监听时键为 `[::]:52064`，与查询键 `192.168.10.220:52064` 永不相等 ⇒ 找不到共享 socket 就 dial 临时端口。
     修法：`transport.ListenerConnectionByPort` 可选接口 + `TransportUDP.GetListenerConnection(port)`（`server/third_party/sipgo/sip/transport_udp.go`、`transport_connection_pool.go`），
     `TransportLayer.ClientRequestConnection` 在字符串匹配失败时**按端口回退**取监听 socket（`transport_layer.go`）。
  2. **Via 非法（同源第二处）**：401 鉴权重试时 `clientRequestCreateVia` 用 `Client.host/port` 重建 Via，为空则由 `overrideSentBy` 用连接本地地址填补 ⇒ 产生 `Via: SIP/2.0/UDP [::]:52064`。
     EasyGBS 对这种包回 `400 Bad Request` 且应答**不带 Via**，客户端事务永远匹配不上（重传到超时）。
     修法：`clientRequestCreateVia` 优先取 `Request.Laddr`（`third_party/sipgo/client.go`）；`NewCascadeClient` 补 `WithClientHostname`（`app/gb28181/sip/server.go`）。
  3. **钉本地地址只在 UDP 生效**：UDP/TCP 共用同一个客户端，TCP 无法从监听端口发起连接，故不做客户端级绑定，改由 `sipclient.RequestFactory` 在 `Transport == "UDP"` 时给请求写 `Laddr`（`request_factory.go` / `notify.go`）。
- **回归证据**：
  - 新增单测全绿：`sip/transport_udp_listener_port_test.go`、`owned_client_via_pinned_laddr_test.go`、`sip/cascade_client_source_port_test.go`、`sip/cascade_register_digest_test.go`（首次包与 401 重试包都断言源端口 == 声明端口、Via 非通配）、`sipclient/request_factory_test.go`（UDP 钉 / TCP 不钉）。
  - 两次反向验证确认测试能捕捉缺陷：改回原实现时报「源端口 57401 != 59754」、报 `Via host 127.0.0.1 != ::`。
  - 220 实测（`192.168.10.220`）：启动日志 SIP 监听 `0.0.0.0:52064` UDP+TCP；三平台目录推送 `items=2 batches=2`；抓包中我方 MESSAGE 源端口恒为 `52064`、`Via: SIP/2.0/UDP 192.168.10.220:52064`；`make key failed: 'Via' header not found` 与 EasyGBS 400 均消失。
  - **WVP**：`wvp.wvp_device` 我方 `on_line=1`，`wvp.wvp_device_channel` 已入库两条（`...310000001` 水星开发测试机器01、`...310000002` IP PTZ Camera），WVP 已自行点播成功（`[点播成功] 通道编号:340200000113100000002`，我方 `级联点播已建立 platform_id=1`）。
  - **LiveGBS**：`device/list` 我方 `Online=true`、`ChannelCount=2`。
  - **EasyGBS**：注册/心跳/DeviceInfo/DeviceStatus 正常，400 归零。
- **状态**：已修复（后续只留 WVP 的目录日志噪音 P-007 待判）

## P-007 WVP 收到分批目录时报 `[缓存-Catalog] 未找到缓存对象`

- **发现时间**：2026-10-24 10:00
- **阶段 / 用例**：A-CAT-01（目录，非阻断）
- **对端**：WVP
- **现象**：我方把 2 条通道分两批推给 WVP（每条 `SumNum=2`、`DeviceList Num="1"`，这是国标允许的多响应），WVP 日志出现
  `[缓存-Catalog] 未找到缓存对象，可能已经结束` 与 `[收到通道] -> 1个，0/2`；但通道**最终都入库了**，且 WVP 随后点播成功。
- **判断**：功能无影响，属 WVP 自身的缓存竞态日志；`0/2` 这一批是「本条消息里的通道数 1、本批匹配到 0」的统计口径，不是丢数据。
- **待办（可选）**：若要消除噪音，可把两批合并为一条 `DeviceList Num="2"` 的响应（需确认大目录时是否仍要分批，不能为消日志牺牲大目录场景）。
- **状态**：不改（记录备查）

## P-008 上级对「平台自身编码」发 ConfigDownload / DeviceControl 时我方回 403

- **发现时间**：2026-10-24 10:10（EasyGBS 抓包 `/tmp/egbs.pcap`）
- **阶段 / 用例**：A-CFG / A-CTL（非阻断）
- **对端**：EasyGBS（LiveGBS、WVP 暂未观察到）
- **现象**：
  - `MESSAGE` `Query/ConfigDownload`、`ConfigType=BasicParam`、`DeviceID=34020000012000000001`（我方下级平台编码）→ 我方 `403 Configuration query target is not published`；
  - `MESSAGE` `Control/DeviceControl`、`GuardCmd=ResetGuard`、同 `DeviceID` → 我方 `403 Device control target is not published`；
  - 同一编码的 `Query/Catalog` 正常 200 + 目录响应。
- **代码定位**：`cascade_config_query.go:83-86`、`cascade_advanced_control.go:98-101` 均在 `resolveCascadeDeviceStatusTarget` 返回 `IsPlatform=true` 时直接 403。
- **关键判断（按项目元规则先查反向断言）**：`cascade_config_query_test.go:214` 有
  `{name: "platform is not a device config", target: "platform", configType: "BasicParam", status: 403}` ⇒**这是既有设计契约，不是疏漏**：平台自身是虚拟节点，没有可下发给真实设备的配置源，宁可 403 也不编造。
- **影响**：EasyGBS 侧对这两个查询会记一条失败，但不影响注册、目录、点播。国标里 `DeviceID` 指设备/通道时这两种查询合法，所以严格讲这是「互操作性折衷」而非缺陷。
- **待老板拍板**：是否补一个「平台级 BasicParam 本地应答」（用我方自身配置回 `Response/BasicParam`），把 403 变成有内容的 200。**这会翻转上面那条测试的语义，属新增能力，不在非阻断修复范围内。**
- **状态**：待决策（暂不改）

## P-009 上级用 `Expires: 0` 首次订阅时，我方回 481

- **发现时间**：2026-10-24 10:10
- **阶段 / 用例**：A-EVT（非阻断）
- **对端**：EasyGBS
- **现象**：EasyGBS 连发三条 `SUBSCRIBE`（`Event: presence` + `Alarm`、`Event: Catalog;id=...` + `Catalog`、`Event: presence` + `MobilePosition`），**全部带 `Expires: 0`、无 To tag**，我方均回 `481 Subscription Does Not Exist`。
- **判断**：按 RFC 3265 §3.1.1，`Expires: 0` 即「取消订阅」，对不存在的订阅回 481 **是标准行为**；`cascade_event_subscriptions.go:176` 的实现与此一致，且 `Expires: 0` 走 cancel 分支已有测试覆盖（`cascade_event_subscriptions_test.go:275-283`）。
  抓包中 EasyGBS 收到 481 后**没有重传**，且它本身是走 `MESSAGE` 查询目录（已成功），说明它并不依赖订阅。
- **风险提示**：若某家上级**只**靠订阅收告警，我方会推不出告警。届时再考虑「`Expires: 0` 且无既有订阅时也回 200」的宽松兼容（需新增配置开关，不默认开）。
- **状态**：不改（记录备查）

## P-010 我方 REGISTER 应答的 `Date` 头不是 RFC 1123 格式

- **发现时间**：2026-10-24 10:00
- **阶段 / 用例**：A-REG（非阻断）
- **现象**：我方日志 WARN `cascade.registration.date_invalid`（platform_id=1/3/4），抓包中上级应答含
  `Date: 2026-10-24T09:59:28.639`（ISO 8601 带毫秒），而 RFC 3261 要求 SIP-date 为 `Sun, 24 Oct 2026 09:59:28 GMT`。
- **影响**：仅日志告警与对外协议规范性，注册本身成功。
- **修复方向**：我方**发送**的应答里 `Date` 由 sipgo 生成（本应合规），需确认是我方手工写了 `Date`，还是解析上级应答时把合法格式判成非法（`cascade.registration.date_invalid` 是解析侧告警）。**先定位再改，不要把解析器放宽当成修好。**
- **状态**：待修（非阻断，最后统一处理）

## P-011 ZLM 残留 RTP 收流端口（52044）与僵尸流

- **发现时间**：2026-10-24 10:25
- **阶段 / 用例**：A-LIVE-05（停止后回收）
- **现象**：
  1. `listRtpServer` 始终有一个未关闭的收流端口：`port=52044`、`stream=37010301021320000018_37010301021320000006`、`tcp_mode=1`、`ssrc=0200000000`；
  2. `getMediaList` 里该流常驻（0 个观看者），`bytesSpeed` 稳定 ~480–510 KB/s，但**抓包证实既没有设备发来的 RTP（`host 192.168.10.204` 0 包），也没有任何 RTP 进入我方收流端口段（52014-52063 共 0 包）** ⇒ 那个速率是**陈旧值**，流实际不流动；
  3. ZLM 日志显示该端口是 **10:10:36**（应用 10:10:29 重启后 7 秒）打开的，之后再无对应 `closeRtpServer`。
- **判断**：属「重启时未收尾的会话残影」——重启后重新拉起了设备流（tcp_mode=1 的被动收流端口），但对应会话在库里已是 `closed`，收尾逻辑没覆盖这条恢复路径。功能上不阻断（点播/回放都正常复用该流名），但会**永久占住 1 个 RTP 端口**，重复发生会耗尽 52014-52063 这 50 个端口。
- **复现（2026-10-24 10:43，干净路径）**：LiveGBS 正常点播 → 正常停止（BYE 成功、设备侧 RTP 已停、抓包 `host 192.168.10.204` 0 包）之后，
  `listRtpServer` 仍然留着 `port=52018 / stream=37010301021320000018_37010301021320000006`，`getMediaList` 里该流也还在。
  ⇒ **不是重启残留，而是「停止点播时不关 RTP 收流端口」的常规路径缺陷**。每次点播漏 1 个端口，52014-52063 只有 50 个，累积起来必然耗尽。
- **待办**：在级联会话收尾（BYE / 超时清理）里补 `closeRtpServer`，并让巡检兜底关掉「没有会话引用的 RtpServer」；不要用「定期重启」规避。
- **状态**：待修复（非阻断，但会累积成阻断）

## P-014 点播失败会在设备侧留下会话，之后该通道约 90 秒内点播全部超时

- **发现时间**：2026-10-24 10:41
- **阶段 / 用例**：A-LIVE-05 / A-LIVE-08
- **对端**：LiveGBS 触发，源设备为大华 `192.168.10.204`
- **现象**：部署新版本后第一次点播失败，日志为
  `cascade.video.failed detail="live start cleanup pending / 点播总超时 / BYE 失败 / ZLM 请求失败 closeRtpServer: ... context deadline exceeded"`；
  此后连续两次点播都是 `发 INVITE 失败: 等待 INVITE 应答超时`。抓包确认：我方 INVITE 连发（重传），**设备一个字节都不回**；
  但同一时刻设备对 `RecordInfo` 的 MESSAGE 查询正常应答 ⇒ **不是设备掉线，是设备侧并发会话被占满/卡死**。
- **自愈情况**：静置约 90 秒后再点播即恢复（`级联点播已建立 platform_id=3`），说明设备会自己超时释放。
- **影响**：上级连续重试时，越试越糟，且这 90 秒内该通道完全不可用；用户看到的是「点播失败 / 黑屏」。如果现场设备并发上限低（常见 1~4 路），叠加 P-011 的端口不回收，很容易变成长期不可用。
- **待办**：
  1. 查 `closeRtpServer` 为什么会 `context deadline exceeded`（ZLM 刚起 56 秒，是否开局未就绪 / 客户端没设超时）；
  2. 清理失败时要有**明确的补偿**（重试队列 + 会话状态标记），不能只留一句 `cleanup pending`；
  3. 点播失败后对同一通道加**短退避**，避免上级秒级重试把设备越占越满。
- **状态**：待定性

## P-012 `stopSendRtp` 调用失败率偏高

- **发现时间**：2026-10-24 10:26
- **阶段 / 用例**：A-LIVE-05
- **现象**：ZLM 日志中 `stopSendRtp` 共调用 14 次，其中 **9 次返回 `code:-1 stopSendRtp failed`**（`ssrc=0200000102 / 0200002924 / 0200000546 / 0200005407 / 0200004105` 等）；同一 `ssrc=0200000002` 被连续停三次。
- **初步判断**：大概率是「发送器已被对端 BYE 或前一次清理关掉，我方再停一次必然 failed」，属无害噪音；**但同一 ssrc 连停三次**像是重试/多路径清理撞车，也可能是真实发送器没被关掉。
- **待办**：抓一次「上级 BYE → 我方清理」的完整流程，核对 `stopSendRtp` 的返回与实际 RTP 是否停止；若确认无害则把 failed 降级成 debug 级日志，避免淹没真实故障。
- **状态**：待定性

## P-013 录像条目多的日子查询必然失败（设备 `SumNum` 与实际条目不符 → 判不完整 → 一条都不回）

- **发现时间**：2026-10-24 10:28
- **阶段 / 用例**：A-REC-01 / A-REC-03（回放主干，**阻断级**）
- **对端**：LiveGBS（上级）→ 我方 → 大华设备 `37010301021320000018`（通道 `37010301021320000006`，192.168.10.204:5060）
- **现象（同一条通道，三种结果）**：

  | 查询日期 | 设备侧 | 我方结果 | 上级看到 |
  |---|---|---|---|
  | 2026-10-24（约 1 条录像） | 正常 | 正常返回 | ✅ 列表有 00:00–00:30、724MB |
  | 2026-10-22（0 条录像） | 回空 | 正常返回空 | ✅ `RecordList: []`、`SumNum: 0` |
  | **2026-10-23（录像很多）** | `SumNum=199`，**实际推 130 条 MESSAGE、386 个条目**，2 秒内发完 | 判 `incomplete recording query`，**一条都不返回** | ❌ `wait for recordinfo timeout[15s]` |

- **抓包证据**（`/tmp/rec3.pcap`，过滤 `host 192.168.10.204`）：
  - 我方 10:28:53.09 发 `RecordInfo`（`SN=5`、`Type=all`、`Secrecy=0`）；
  - 设备 10:28:53.79 起连发 130 条 `Response/RecordInfo`，每条 `RecordList Num="3"`，**每条都声明 `SumNum=199`**，`</Item>` 合计 **386** 个；
  - 设备从未发送 `SumNum=0 && Num=0` 的结束包。
- **代码定位**：
  - `app/gb28181/recordquery/registry.go:135-158` `add()`：按 `recordDigest` 去重、`DeviceID != channelCode` 计为 rejected；
  - `app/gb28181/recordquery/service.go:169` 完成判据 = `declaredTotal > 0 && len(records) >= declaredTotal`；
  - `service.go:256-262` `finishDeadline`：**有记录但未达完成判据 ⇒ `QueryStatusPartial` ⇒ `Incomplete=true`**；
  - `app/gb28181/cascade_recording.go:162-165`：`if err != nil || result.Incomplete { 记日志; return }` ⇒ **已收到的 386 条全部丢弃，一条不复用**。
- **影响**：上级在"录像多"的日子点回放，看到的是"没有录像"，用户会认定平台丢了录像。这是**回放主干**的可用性缺陷，应按阻断处理。
- **判断依据**：`grep Incomplete --glob '*_test.go'` 只命中下载路径的用例，**没有**任何测试断言"级联录像查询不完整时不得返回"，所以"整批丢弃"是实现选择而非设计契约，可以改。
- **修复方向（按优先级）**：
  1. **不因 `Incomplete` 整批丢弃**：把已拿到的记录按标准 `Response/RecordInfo` 回给上级（`SumNum` 写成实际返回条数），让上级至少有东西可播；
  2. **修完成判据**：设备 `SumNum` 与实际条目常年不一致（本例 199 vs 386），完成判据不能只靠与 `SumNum` 比大小 —— 至少要加「设备在 N 秒内无新消息」的静默收尾，并容忍 `received > declared`；
  3. 补测试：`SumNum` 小于实际条目、设备不发结束包、只收到部分批次 三种情况都要有用例锁行为。
- **待办**：先在后端把 `ReceivedCount / DeclaredTotal / RejectedCount` 加到这次告警日志里，便于下次一眼定位是哪一项没达标（当前日志只有 `incomplete recording query`，信息不足）。

### 修复结论（2026-10-24 10:40 部署回测通过）

三处改动，均在 `app/gb28181/`：

1. **不完整也回**（`cascade_recording.go`）：`Incomplete` 且 `len(Records) > 0` 时照样按实际条目回响应（`SumNum` 由 `planCascadeRecordResponses` 按 `len(records)` 计算，不会虚报）；只有「一条都没有」才维持原样不回，避免把「查询失败」伪装成「确实没录像」。
2. **压聚合预算**（`cascade_recording.go`）：新增 `cascadeRecordQueryBudget = 8s`，`QueryForCascade` 跑在这个预算下。原因：LiveGBS 只等 15 秒，而我方原来自带的聚合超时正好 15 秒 —— 即使拿到数据也已经晚 1 秒，上级照样报 `wait for recordinfo timeout[15s]`。
3. **日志补字段**：新增 `event=cascade.record.partial_returned`，带 `status / received / declared_total / rejected`，下次不用再靠猜。

**回测证据**：

| 场景 | 修复前 | 修复后 |
|---|---|---|
| 2026-10-23（录像多） | 15s 超时、**0 条** | **8.5s 返回 197 条**（`SumNum=197`，首条 `15:05:56–15:06:17`、8983531 字节，与设备抓包一致） |
| 2026-10-22（无录像） | 空列表（正常） | 空列表（不变） |
| 2026-10-24（47 条） | 未完整验证 | **47 条全回**（`SumNum=47`） |

日志实测：`partial_returned status=partial received=197 declared_total=199 rejected=2`
—— 说明「凑不齐 199」的直接原因就是设备把 2 条条目的 `DeviceID` 写成了父设备编码而非通道编码，被 `registry.go` 的 `DeviceID != channelCode` 判掉。**这 2 条丢了但不再影响整体返回**；是否要放宽这条匹配（很多设备都这么写）留作后续单独决策。

**新增测试**（都做了反向验证）：
- `TestCascadeRecordQueryReturnsPartialResultWhenDeviceUnderDelivers`（设备少给 ⇒ 仍要回；改回旧逻辑时断言失败 0 != 2）
- `TestCascadeRecordQueryAnswersWithinBudgetWhenDeviceNeverCompletes`（设备永远凑不齐 ⇒ 必须在预算内应答，聚合超时故意设 30s 以排除「被超时兜住」）

- **状态**：已关闭

---

## P-015 级联目录不下发 `<Info><PTZType>`，EasyGBS 据此判定「云台类型未知」并完全不给云台入口

- **发现时间**：2026-10-24 11:0x
- **阶段 / 用例**：A-PTZ-01（云台控制）
- **对端**：EasyGBS 3.7.451（LiveGBS 3.6.10 / WVP 不受影响）
- **现象**：三平台注册、目录、点播都正常，但 EasyGBS 的「视频调阅」里选中我方通道后
  **完全没有云台面板**；其通道列表显示「云台类型未知」。
- **根因**：`cascade/catalog` 的 `CatalogItem` 结构体里**根本没有 PTZType 字段**，
  `catalogItemXML` 也只发 `<Info><BusinessGroupID>`。EasyGBS 收到目录后把通道写成
  `ptz_type=0` / `ptz=0`，据此隐藏云台控制。
  我们自己的库里 `gb_channel.ptz_type` 是对的（`IP PTZ Camera` = 1 球机），**纯属级联侧漏发**。
- **修复**（4 处 + 2 个测试）：
  1. `ChannelMetadata` / `CatalogItem` 新增 `PTZType`；
  2. `cascade_catalog_handler.go` 的通道查询补 `ptz_type` 列并透传；
  3. `publication.go` **只在该通道对本上级 `PTZAllowed` 时才带上**（云台是授权而非设备事实，
     免得界面给出一个必然被拒的入口）；
  4. `responder.go` / `subscription.go` 把 `PTZType` 放进 `<Info>`（2016/2022 两版都在 Info 内），
     值为 0 时**保持缺省、不伪造 0**（那等于谎报「固定枪机」）。
- **回测证据**：
  - 部署后 EasyGBS `data.db` 的 `channels.ptz_type` 由 **0 → 1**（`IP PTZ Camera`，更新时间即推送时刻）；
    水星 MIPC3286W 仍是 0（设备本身没报 PTZType，符合「不伪造」）；
  - 页面「视频调阅 → 实时预览」选中该通道后出现完整云台面板（八方向 + 变倍/变焦/光圈/速度），
    修复前该区域完全不存在；同时画面正常出图。
  - 新增测试 `TestCatalogPublishesPTZTypeOnlyWhenKnown`（两版都验证 Info 内、0 时不发）、
    `TestPublicationOmitsPTZTypeForChannelsWithoutPTZPermission`，均做反向验证
    （把 PTZType 去掉后两条测试同时失败）。
- **遗留**：EasyGBS 界面上的方向键在本机自动化里没能触发其前端事件（点击落点与视口存在缩放比，
  且其云台疑似按下持续型），因此**「EasyGBS 云台指令到达设备」这条未取到抓包**；
  但同一条转发链路（`cascade/control` → `PTZCmd` 原样下发 → 设备 200 OK）已在 LiveGBS 上抓包证实。
- **状态**：已关闭（平台侧入口已具备；指令转发由 LiveGBS 用例覆盖）


## P-016 级联语音广播整条链路不通（三处不兼容叠加）

- **发现时间**：2026-10-24 11:2x
- **阶段 / 用例**：A-AUD-01（语音广播）/ A-AUD-02（广播结束）
- **对端**：WVP-Pro 2.7.4 + 大华球机 `37010301021320000006`（父设备 `37010301021320000018`）
- **现象**：WVP 点「语音广播」后，页面上广播流建起来了，但**一点声音也没有**；设备侧无任何播报。
- **复现步骤**（可脚本化，见 `docs` 附带的抓包脚本思路）：
  1. WVP 调 `GET /api/play/broadcast/{deviceId}/{channelId}?broadcastMode=1` 拿到 `app=broadcast` 的流；
  2. 往 WVP 的 ZLM 推一路音频（`rtmp://<wvp-zlm>:10001/broadcast/<device>_<channel>`）——
     WVP 只有在**媒体真正到达**时才会给下级发 `MESSAGE/CmdType=Broadcast`；
  3. 我方收到 `Notify/Broadcast` → 转发给源设备 → 设备回 `Response` → 设备再发音频 `INVITE`（`s=Play`/`a=recvonly`）。
- **根因（三处，任一处都足以让广播失败）**：
  1. **应答身份判据过窄**：设备回的 `Response` 里 `<DeviceID>` 填的是**通道编码**、且不带 `TargetID`，
     而我们要求它必须等于**父设备编码** ⇒ 判「identity mismatch」，广播结果永远回不到上级。
  2. **音频 INVITE 收件人判据过窄**：设备把 INVITE 打给
     `sip:37010301021320000006@<平台>:52064`（通道编码），而我们要求收件人必须是**平台编码**
     ⇒ 匹配不到会话，落到本地对讲逻辑并回 **403「没有匹配的 Broadcast 会话」**。
     另外设备 `Subject` 里的 SSRC 段是 **19 位十六进制**（`03d7a8ef3276a8ef327`），
     我们只认十进制 ⇒ 即使收件人放宽也会在 Subject 处被拒（**488**）。
  3. **认领与应答抢同一行**：设备「先回应答、紧接着发 INVITE」，而应答处理（`recordBroadcastResult`）
     会改写同一行的 `sdp_summary`；音频认领用的是 resolve 时的**旧快照**做 CAS ⇒ `RowsAffected=0`
     ⇒ 设备收到 **486「Cascade Audio Reservation Changed」**。
- **修复**（`cascade_broadcast.go` / `cascade_audio_binding.go` / `cascade_broadcast_audio_runtime.go`）：
  1. 应答身份改为「父设备编码 **或** 通道编码」都认（`broadcastResponseIDMatches`）；
  2. INVITE 收件人同样两个编码都认（`cascadeAudioCalleeMatches`），
     Subject 的 SSRC 段十进制/十六进制都认（`mediaSequenceToken`）；
  3. `Claim` 改为**事务内重读当前行**，只比对「授权相关字段」（`cascadeBroadcastBindingStable`），
     结果类字段（`DeviceResult`/`NotifyStatus`/`ResultDelivered`/`ResultAttempt*`）与音频认领字段
     不参与比较，其余字段原样保留 ⇒ 并发写不再互相打架。
  4. 顺手补了一条可诊断日志 `cascade.broadcast.audio_offer_rejected`
     （此前 488 只有设备看得到，服务端无任何线索）。
- **回测证据（WVP + 大华，2026-10-24 11:52）**：
  - 设备 `INVITE sip:37010301021320000006@...` → 我方 `100 Trying` → 我方主动向上级
    `INVITE sip:35020000002000000001@3502000000`（`Subject: ...`，`cascade-audio-<uuid>`）→ WVP `200 OK`
    → 我方回设备 `200 OK`；
  - **媒体真的到了设备**：抓包 **153 个 RTP 包 `192.168.10.220:42038 → 192.168.10.204:9740`**；
  - 停止：WVP 发 `BYE` → 我方 `200 OK` 并给设备发 `BYE`，媒体立即停止（抓包里 RTP 到此为止）；
  - 新增 4 条测试并做反向验证（去掉任一处修复，对应用例立刻失败）：
    `TestCascadeBroadcastAcceptsDeviceResponseEchoingChannelCode`、
    `TestCascadeAudioPullAcceptsInviteAddressedToChannelCode`、
    `TestCascadeAudioPullAcceptsDahuaBroadcastEnvelope`、
    `TestCascadeAudioClaimSurvivesConcurrentBroadcastResultWrite`、
    `TestMediaSequenceTokenRejectsMalformedValues`。
    其中 `TestCascadeAudioPullClaimFailsWithoutLeakingLease/binding_changes` 原先用「往 JSON 末尾加空格」
    代表「绑定被改过」，新语义下必须改成**改授权字段**（改成改通道编码）——契约本身没变，
    只是把"格式变化"和"授权变化"区分开了。
- **遗留（非阻断）**：
  - 广播结束后会话以 `failure_code=broadcast_ended / audio reservation expired` 收尾，
    而不是「正常结束」；租约要等 30s 过期才释放（设备侧声音已停，不影响听感，属状态语义不精确）。
  - 我方给设备发 `BYE` 后设备**没有回 200 OK**，触发了 3 次重传；对端行为，非我方缺陷。
- **状态**：已关闭（WVP 侧 A-AUD-01/02 通过；LiveGBS/EasyGBS 的广播入口需要各自页面推流，本轮未做）

---

## P-017 目录订阅的首个 NOTIFY 只带差量，目录没变化时 `SumNum=0`

- **发现时间**：2026-10-24 12:10
- **阶段 / 用例**：A-SUB / A-SUB-01（订阅成功但首推内容为空）
- **对端**：WVP（`GET /api/device/query/subscribe/catalog?id=35&cycle=60`）
- **现象**：
  - 我方 200 OK（`Expires: 60`、Contact 正确），随即 NOTIFY：
    `<Notify><CmdType>Catalog</CmdType><SN>344001</SN><DeviceID>34020000012000000001</DeviceID><SumNum>0</SumNum></Notify>`；
  - 也就是说上级订阅成功，但**订阅推送里没有任何通道**；同一时刻 WVP 自己查目录是 2 条通道。
- **根因**：`cascade_subscription.go` 的首推沿用「差量」语义——
  `baseline` 取的是订阅被接受那一刻的目录快照，`DiffCatalog(baseline, current)` 必然为空，
  `force` 只跳过了「空差量不发送」的早退分支，最终发出去的是一个 `SumNum=0` 的空通知。
- **为什么这次不改**（重要）：
  1. 现行设计假定上级在注册时已用 `Catalog` 查询拿过全量目录，订阅只承载变化；
     实测三家（WVP/LiveGBS/EasyGBS）**确实都在注册后查过一次目录**，所以今天没有功能性失败。
  2. 正确改法是「首推带全量、`Item` 不带 `Event` 属性」（GB 语义的目录快照，且与我方**上级侧解析器**
     `subscribe/catalog.go:117` 完全一致：无 `Event` 视为全量、有 `Event` 视为增量）。
     但这会改变**所有上级**在订阅时收到的首个报文内容，且会推翻多条把「首推=差量」写进断言的既有测试
     （`TestCascadeCatalogSubscriptionLifecycle` / `…ProjectionChangeBetweenBatches…` /
     `…ChangeImmediatelyAfterAcceptanceIsNotInitialBaseline` 等），属产品级语义变更，需先拍板。
- **建议**：若要改，走「首推 = `Item` 无 `Event` 的全量快照」这一种，不要用 `Event=ADD` 全量（部分上级会把
  `ADD` 当成新增通道而与已有通道重复）。落改时需同步调整上述 3 条用例的断言口径（补维度，不翻原意）。
- **状态**：待决策

---

## P-018 对话框刷新/取消时对端轮换了 `Event: …;id=`，我方回 481

- **发现时间**：2026-10-24 12:16
- **阶段 / 用例**：A-SUB / A-SUB-03（订阅刷新）
- **对端**：WVP
- **现象**（同一 Call-ID、同一对 From/To tag，仅 `Event` 的 id 变了）：
  - 取消：`Call-ID: 2fe8774f…`、`Expires: 0`、`Event: Catalog;id=5059` → 我方 **481**；
  - 随即重订：同 Call-ID、同 tag、`Event: Catalog;id=6180`、`Expires: 60` → 我方仍 **481**；
  - WVP 只好换一个新 Call-ID（`9294515a…`）重建对话框 → 200 + 首推。
  - 位置订阅同样复现（`pos2_1224*.pcap`：`Event: Catalog;id=981` 刷新 → 481 → 新建 `id=6599` → 200）。
- **根因**：会话键是 `平台|Call-ID|对端tag|Event全值`，而 `catalogSubscriptionEvent()` 把 `;id=<n>` 拼进了
  `Event` 值（`cascade_subscription.go:118-140`）。WVP 每次重订都重新生成 id，于是同一个对话框永远命中不了。
- **影响**：功能不中断（WVP 会重建对话框），但每轮刷新都会多一个 481、我方旧会话要等过期才回收，
  且给对端留下「订阅不存在」的错误印象。GB/RFC 口径上订阅方确实应沿用同一 id，**对端不规范**。
- **待办**：决定是「维持严格（回 481）」还是「按对话框（平台+Call-ID+对端 tag）绑定，id 变化只更新不拒」。
  后者更利于互通，但需要同步改 `TestCascadeCatalogSubscriptionRefreshPreservesDialogURIs` 一类的断言。
- **状态**：待定性

---

## P-019 「下级注销后级联列表仍显示在线」= 误判，非缺陷

- **发现时间**：2026-10-26 18:5x（方向 B，LiveGBS 作下级）；复核 19:1x
- **原始记录**：9.5 节记为「LiveGBS 关开关会真的发注销，`gb_device.status` 立刻变 0，但级联列表一直显示在线」，
  并据此推断 `gb_cascade_platform.register_at/register_expires_at` 缺写入。
- **复核结论：该推断不成立。** 两个列表说的不是同一条关系：

  | 界面 | 数据来源 | 表示的关系 |
  |---|---|---|
  | **设备列表 / 通道列表** | `gb_device.status`、`gb_channel.status` | 下级平台注册到**我方**（我方作上级） |
  | **国标级联页（上级平台列表）** | `gb_cascade_platform.register_at / register_expires_at / heartbeat_at` | **我方**向上级注册（我方作下级） |

  `gb_cascade_platform` 整张表是「上级方向」的：页面只有「新增上级」、列头是「上级平台 / 本平台身份」。
  它的两个时间字段由 `cascade/runtime` 在**我方发出的** REGISTER / MESSAGE Keepalive 成功后写入，
  与「别人注册到我方」无关。因此下级注销后该行仍在线是**正确**的 —— 我方对上级的注册还活着。
- **现场证据（2026-10-26 19:1x）**：
  1. 页面关掉 `LiveGBS-220` 行的启用开关（我方主动向上级注销）→ 3 秒内
     `register_at/register_expires_at` 变 NULL、行显示「已停用」；重新打开 → 6 秒内「在线」，
     「最近注册」刷新为 `19:13:11`。⇒ **上级方向的注销/重注册本来就是通的**。
  2. 低位（下级）方向另行实测：LiveGBS 侧关级联 → 我方 19:19:55 收到
     `event=gb28181.register.unregistered device_id=34020000002000000001 cseq=140`，
     `gb_device.status` 立即 0（`offline_at=19:19:55`）、通道 `gb_channel.status` 同一毫秒一并置 0，
     记录**未删除**；重新启用后 19:22:30 重注册，设备与通道双双回到在线。
     ⇒ 方向 B 的注销链路（设备 + 通道 + 不删记录）**本来就是对的**。
- **顺带排除的怀疑**：现场这台 220 存在**互相指向的环配置**（我方把 LiveGBS 配成上级、LiveGBS 又把我方配成上级），
  所以「我方 级联页在线」与「下级设备离线」会同时出现。这是两台机器各配了一条，不是状态算错。
- **不做的事（重要）**：**不要**为了「让下级注销影响级联行」往 handler 里加回写。
  这样会在环配置下用「别人的注册」覆盖「我方自己的注册事实」，把一行真实在线覆盖成离线。
- **状态**：非缺陷（误判），无需改代码。

---

## P-020 播放控制台「拉流超时」后反复重试永远超时

- **发现时间**：2026-10-26 18:0x–18:4x（方向 B 点播阶段）
- **现象**：同一通道第一次「拉流超时」后，点「重试」只会继续超时；把整个浏览器会话关掉重开才恢复。
  控制台同时报 `TypeError: Cannot read properties of null (reading 'appendChild')`。
- **根因**：`PlayWindow.vue` 的 `p.on("error")` / `p.on("timeout")` 只把 `errorMsg` 写出来，
  **不销毁播放器实例**。EasyPlayer 同族（flv↔flv）切换按设计复用实例，于是重试等于在**一个已经坏掉的实例**上重放
  ⇒ 永远不会自愈。
- **修复**（`web/src/views/gb28181/components/PlayWindow.vue`）：新增 `discardBrokenInstance()`，
  在 error / timeout / 初始化 catch 三条失败路径上 `destroy()`（仅当实例仍是当前实例时，避免误杀新实例）。
- **验证**：
  - 单测 `PlayWindow.test.ts` 新增「销毁超时的播放器实例，让重试能拿到全新实例」，
    断言 timeout 后首个实例 `destroy` 被调用、再次 `play` 拿到的是**第 2 个**实例；该文件 22 条全绿；
  - 现场（220，部署后）：`Camera 01` 在「点播未完成 / 拉流超时」状态下点一次「重试」→ 12 秒内转「播放中」并出图。
- **状态**：已关闭（前端已部署回测）。

---

## P-021 停播收尾告警被当成失败 ⇒ 整个 DELETE 返回 400

- **现象**（EasyGBS 作下级，`Camera 01`）：点「关闭」后前端顶部弹「停止流失败」，播放区停在
  「点播未完成 / 拉流超时」；此后点「重试」仍然超时，**只有刷新页面**才能重新点播。
- **日志**（220，2026-10-26 19:58:16）：
  ```
  WARN play  停播清理失败  reason_code=cleanup_failed duration_ms=105
  INFO app   请求被拒  error="BYE 失败: Invite failed with response: SIP/2.0 400 pusher not exits"
             route=/api/gb28181/play/:streamId method=DELETE http_status=400
  ```
  同一时刻 ZLM 的 5 个 schema 全部 `on_stream_changed regist=false` —— **媒体确实已经停了**。
- **根因（2026-10-26 20:4x 现场复核后修正）**：最初怀疑的是 `recovery.go` 里
  `if closeErr != nil { return errors.Join(byeErr, closeErr) }` 这条裸错误分支，**复核后排除**：
  - `zlm.Client.CloseRtpServer`（`client.go:265`）已经把 `msg` 含 `not exist` / `不存在`
    的返回码归一到 `nil`，所以「ZLM 早已拆掉收流服务」这一情形**根本不会**产生 `closeErr`；
  - 现场 20:28:17 那次日志里只有一条 BYE 错误（`errors.Join` 多错误会用换行拼），
    且 `CloseRtpServer` 之后确实解绑/清库成功，进一步证实 `closeErr == nil`；
  - 真正的问题在**下游没人读标记**：`stopCurrentResult` 确实对 BYE 失败做了
    `completedMediaStop(...)` 包装，但那个标记只被协调器 `finishStop` 用来判断
    「要不要把会话留在待清理」，**HTTP 停播口只看 `err != nil`** ⇒ 媒体已停、BYE 告警，
    照样 `FailAndAbort("停止流失败")` 回 400。
  - ⛔ 同时确认 `recovery_stop_failure_test.go` 里已有反向断言：`closeErr` 失败必须
    **保留 generation / SSRC / 授权**以便重试（`TestServiceStopCloseFailureKeepsGenerationTrackableAndRetryable`）。
    所以正确修法是「补一个对外的判定入口」，而不是把 `closeErr` 一律包成已完成。
- **为什么影响重播**：前端拿到 400 后认为停播没成功，本地会话状态不清理；
  下次点播复用坏状态（后端其实点播成功，日志 `play.completed outcome=succeeded`，前端仍超时）。
- **修法（已实施）**：
  1. `play/coordinator.go`：把内部标记对外暴露成一对 —— `CompletedMediaStop(err)` 打标记、
     `IsMediaStopCompleted(err)` 读标记（nil 视为已完成）；
  2. `controllers/play.go` 停播口：`err != nil` 且**不是**收尾告警才回 400，
     收尾告警一律按成功返回 `{released:true}`；真失败（ZLM 关流没走通）路径完全不变；
  3. `play/service.go` 的 `stopDirect`（无协调器条目的老路径）同一处补齐：
     `closeErr == nil` 时把 BYE/落库收尾失败也打上标记；`closeErr != nil` 仍然返回裸错误
     （媒体状态未知，必须保持可重试）；
  4. `openapi/play/dispatcher.go` 第三方停播口同源修正（同一个标记，不再回 `unavailable`，
     否则客户端重试只会拿到 not-found）；
  5. `play/service.go` 停播收尾的 WARN 补 `error` 字段，并把收尾告警的文案改成
     「停播已完成，收尾告警」（事件名/等级不变）。**以前这句 WARN 不带任何错误细节**，
     排查只能翻 SIP 抓包。
- **验证**：
  - 单测：`play` 包补「BYE 失败 ⇒ 标记成立」「关流失败 ⇒ 标记不成立」两条断言；
    `controllers` 包补 `TestPlayControllerStopTreatsCompletedMediaStopAsSuccess`（200 + released）；
    三个包（play / controllers / openapi-play）全量绿；
  - 现场（220，2026-10-26 20:28:17）：点「停止当前直播」**正好命中原始条件**
    （ZLM 5 个 schema `regist=false`、`停播清理失败` WARN），HTTP 已经是
    `method=DELETE http_status=200`，前端弹「已停止直播」；随后再点「点播」11 秒转「播放中」并出图。
- **状态**：已修复（2026-10-26，前端无需改动 —— 弹窗来自 axios 全局拦截器对非 2xx 的处理）。

## P-022 下级不提供 `DeviceName` 时设备名为空，列表重复显示编码

- **现象**（EasyGBS 作下级）：设备列表里该设备显示成
  `34020000002000000003` / `34020000002000000003` 两行完全一样的编码，没有可读名称。
- **证据**：
  - 库里 `gb_device.name` 是**空串**；
  - 后端日志 `DeviceInfo 回写成功 updated_field_count=3 name_updated=false
    manufacturer_updated=true model_updated=true firmware_updated=true`
    ⇒ EasyGBS 的 DeviceInfo 应答里确实没给 `DeviceName`（厂商/型号/固件都有）。
- **定性**：不是解析 bug，属**对端不提供字段 + 前端兜底不当**。
  期望兜底显示「未命名设备」之类，而不是拿编码顶两次。
- **状态**：待修复（前端兜底）。

## P-023 `Event: PTZPosition` 订阅被对端忽略时无限重传

- **现象**（EasyGBS 作下级，我方注册成功后自动订阅）：我方对 `34020000002000000003` 连发
  `SUBSCRIBE Event: PTZPosition`，EasyGBS **从头到尾没回过 200**，于是我方按
  1s → 2s → 4s … 持续重发，`Call-ID: subscription-1793014955720587046`、`CSeq: 1` 完全不变。
  抓包时间点：`19:42:35.720 / 36.221 / 37.222 / 39.222 / 43.223`。
- **对照**：同一批 `Event: Catalog`、`Event: presence`（`CmdType=MobilePosition` / `Alarm`）
  都拿到了 200，不发散 ⇒ 只有对端不支持的事件被无限重传。
- **风险**：长期挂着一个不响应的订阅会周期性打空包；若多台下级都这样，信令链路会被无谓占用。
- **建议**：给订阅重传设上限（如 64×T1/超时后放弃并记 WARN），别无限循环。
- **状态**：待定性（是否属"对端不响应该放弃"还是"必须一直重试"，需产品口径）。

---

## 附：方向 B 在 EasyGBS 上的关键更正（推翻上一轮一条结论）

上一轮（2026-10-26 17:5x）曾按「`cascade_event_subscriptions.go` 只实现响应、没有主动发
SUBSCRIBE 的代码」判定 **B-ALM / B-SUB 链路缺失**。本轮抓包证明**该结论错误**：

我方在下级注册成功后会主动发出 4 类订阅，EasyGBS 均正常应答：

| 订阅 | 报文 | 对端应答 |
|---|---|---|
| 目录 | `SUBSCRIBE Event: Catalog` + `<CmdType>Catalog</CmdType>` | 200 + 两包 `NOTIFY`（`SumNum=7`） |
| 位置 | `SUBSCRIBE Event: presence` + `<CmdType>MobilePosition</CmdType><Interval>30</Interval>` | 200 + `<Response><Result>ERROR</Result><Reason>Cann't get Position</Reason>`（对端无 GPS，属正常） |
| 报警 | `SUBSCRIBE Event: presence` + `<CmdType>Alarm</CmdType>` | 200 |
| 云台位置 | `SUBSCRIBE Event: PTZPosition` | **无应答**（见 P-023） |

⇒ B-SUB-01（订阅下级目录）实际**已具备**；B-ALM-03（报警订阅）链路**通**，
只是本轮没有报警源可触发（EasyGBS 侧无报警设备配置），无法验证推送内容。

---

## 变更记录

| 日期 | 变更 |
|---|---|
| 2026-10-24 | 建立清单，录入 P-001 ~ P-005（均来自方向 A 的注册与目录阶段摸底） |
| 2026-10-24 | P-001 定性为缺陷（目录里区划条目 `DeviceID` 只有 2~6 位，如 `34`／`3402`／`372928`，非 20 位国标编码）；补记前端默认关闭该开关 |
| 2026-10-24 | 新增 P-006（阻断）：级联 SIP 发送源端口与 Via/Contact 不一致，WVP 丢弃目录；LiveGBS/EasyGBS 不受影响 |
| 2026-10-24 | P-006 修复完成并部署回测通过（源端口按端口回退取监听 socket + Via 取 `Laddr`），新增 5 个单测/端到端测试；三平台注册+目录全部正常，WVP 已能自行点播。状态转「已关闭」 |
| 2026-10-24 | 新增 P-007~P-010：WVP 分批目录缓存日志（不改）、平台级 ConfigDownload/DeviceControl 403（待决策，有测试契约）、`Expires: 0` 订阅回 481（符合 RFC，不改）、`Date` 头格式（待修） |
| 2026-10-24 | 新增 P-015（阻断-云台用例）：级联目录漏发 `<Info><PTZType>`，EasyGBS 完全不给云台入口；已修复（含授权门控与「不伪造 0」）并回测 `ptz_type` 0→1、云台面板出现，状态转「已关闭」 |
| 2026-10-24 | 新增 P-016（阻断-语音广播）：应答/INVITE 身份判据过窄 + 认领与应答抢同一行，三处叠加导致 WVP 语音广播无声；已修复并回测（153 个 RTP 包到达设备端 9740 端口），状态转「已关闭」 |
| 2026-10-24 | A-SUB/A-POS/A-ALM 实机验证：目录订阅 200 + 变更推 `Event=UPDATE`、位置订阅 200 + 经纬度 NOTIFY、报警订阅 200 均拿到抓包；新增 P-017（首推 `SumNum=0`，待决策）、P-018（`Event` id 轮换导致 481，待定性）；P-001 补复核（单独开 `publish_platform` 时 WVP 侧正常，问题指向区划条目） |
| 2026-10-26 | 方向 B 补测：B-CAT-02/03/04、B-CTRL-04、B-LIVE-03/04、B-REG-03/04、B-ST-02 通过；新增 P-019（「注销后仍显示在线」复核为误判，附两方向分别实测）、P-020（播放器失败实例不销毁，已修复并回测） |
| 2026-10-26 | 方向 B 第三家 **EasyGBS** 跑完：注册/注销/目录（含手工刷新 7/7）/中文名/实时点播/停播回收/云台八方向全部通过；回放因对端无设备录像入口而明确提示空；新增 P-021（停播 400）、P-022（设备名为空）、P-023（PTZPosition 无限重传）；**更正上一轮「我方不主动订阅下级」的错误结论**（抓包见我方主动发 Catalog / MobilePosition / Alarm / PTZPosition 四类 SUBSCRIBE） |
| 2026-10-26 | P-021 修复并现场回测：**根因修正**（不是 `CloseRtpServer` 报错 —— 那个已归一为 nil，而是「媒体已停」标记只在协调器内部生效、HTTP 停播口只看 `err != nil`）。新增对外判定 `CompletedMediaStop` / `IsMediaStopCompleted`，停播口与 OpenAPI 停播口都按标记区分真失败；`stopDirect` 同源补齐；停播收尾 WARN 补 `error` 字段。20:28:17 现场命中原始条件，DELETE 由 400 转 200，随后 11 秒重播成功。状态转「已修复」 |

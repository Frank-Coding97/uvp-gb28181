---
title: 国标下级级联静态审查
aliases:
  - 国标级联功能审视
---

审查对象：2026-10-15 当前完整工作区（含暂存、未暂存和未跟踪源码），HEAD `c109ac95152e6eb81e692ed2b122521a453c64c8`。

本轮交付是静态审查。覆盖级联前后端、管理 API、注册运行时、SIP 装配、目录投影、媒体发送、录像与回放，以及相关控制、订阅、报警、语音、抓拍和升级模块的上下游调用。没有修改业务实现、部署或变更已有级联台账。老板明确静态审查后，停止浏览器和实机联调。

判断口径：存在设备接入侧能力、XML 构造函数或模拟测试，不等于已经接通“上级→本平台→下接设备→上级”的级联流程。以下是代码路径结论，不是实机复现或完整国标认证。标准以 GB/T 28181-2022 为基准；推荐能力缺失与既有功能缺陷分开列示。优先级表示工程处理顺序，不表示标准条款的应、宜、可强度。

## 已有主干能力

注册与摘要挑战、注册续期/注销骨架、心跳、目录分批和共享投影、平台/行政区划/业务目录发布、DeviceInfo、实时点播、UDP/TCP 主被动媒体适配、ACK/BYE/CANCEL、并发及多上级隔离、设备录像检索、设备录像回放、基本 INFO 控制、基础 PTZ、启动清理遗留发送器均已有代码路径。它们仍受下表问题影响，不能整体标为已验收。

## 已实现但存在问题

| 编号 | 优先级 | 功能 | 静态确认的问题及触发条件 | 关键代码 |
|---|---|---|---|---|
| D01 | P1 | SDP 解析 | `m=video` 前出现合法会话级 `a=recvonly` 时，`current` 和 `video` 均为 nil，`case video` 先匹配并解引用空指针；`f=` 等分支也有同类路径。解析发生于上级身份校验前，存在服务稳定性风险。 | [cascade_video.go:126](/Users/menglulu/code/uvp/UVP-GB28181/server/app/gb28181/sdp/cascade_video.go:126)、[入口:251](/Users/menglulu/code/uvp/UVP-GB28181/server/app/gb28181/cascade_invite_handler.go:251) |
| D02 | P1 | 续期及心跳调度 | 任一事务进行中时 `startTransaction` 直接返回；恰好到期的续期或心跳事件被消费后没有排队或补调度。心跳期间丢失续期可导致后续不再续期。 | [runtime.go:437](/Users/menglulu/code/uvp/UVP-GB28181/server/app/gb28181/cascade/runtime/runtime.go:437)、[事件处理:498](/Users/menglulu/code/uvp/UVP-GB28181/server/app/gb28181/cascade/runtime/runtime.go:498) |
| D03 | P1 | 注册有效期 | 成功后按本地请求的有效期记录状态和计算续期，不读取上级响应授予的 Contact expires / Expires；授予时间更短时会错过续期。 | [runtime.go:530](/Users/menglulu/code/uvp/UVP-GB28181/server/app/gb28181/cascade/runtime/runtime.go:530) |
| D04 | P1 | 入站上级身份验证 | INVITE 严格匹配失败后允许只凭 From、源端口、传输协议认定上级；RecordInfo 也用该识别。不见对应请求的摘要校验、可信连接绑定或有效注册门禁。SIP 可达且已知上级和共享编码时，存在伪造上级请求并指定接收地址的风险。该结论是代码风险，未进行攻击验证。 | [identify:73](/Users/menglulu/code/uvp/UVP-GB28181/server/app/gb28181/cascade_invite_handler.go:73)、[回退:286](/Users/menglulu/code/uvp/UVP-GB28181/server/app/gb28181/cascade_invite_handler.go:286)、[录像入口:110](/Users/menglulu/code/uvp/UVP-GB28181/server/app/gb28181/cascade_recording.go:110) |
| D05 | P1 | 新共享通道云台权限 | 通道选择器把新通道 `ptzAllowed` 固定为 false；保存时显式覆盖后端从平台总开关继承的默认值。逐通道开关已移除，平台允许 PTZ 也无法使这条新共享路径获得 PTZ 权限。 | [BusinessChannelPicker.vue:545](/Users/menglulu/code/uvp/UVP-GB28181/web/src/views/gb28181/cascade/BusinessChannelPicker.vue:545)、[提交权限:750](/Users/menglulu/code/uvp/UVP-GB28181/web/src/views/gb28181/cascade/BusinessGroupManager.vue:750)、[覆盖:278](/Users/menglulu/code/uvp/UVP-GB28181/server/app/gb28181/cascade/repository/business_group.go:278) |
| D06 | P2 | 配置保存及启停反馈 | 后端可返回 HTTP200 + `runtimeSynced:false`，平台页面忽略返回体，仍提示正在注册、已注销或删除成功。数据库配置与实际运行态未同步时，用户收到错误成功提示。 | [index.vue:901](/Users/menglulu/code/uvp/UVP-GB28181/web/src/views/gb28181/cascade/index.vue:901)、[启停:940](/Users/menglulu/code/uvp/UVP-GB28181/web/src/views/gb28181/cascade/index.vue:940) |
| D07 | P2 | 删除 API 结果 | 删除启用平台时，先停用再 Reload；若第一次 Reload 失败，尚未执行 SoftDelete 就返回错误。Controller 却统一返回 `ok:true`，实际仅停用而未删除。 | [management.go:357](/Users/menglulu/code/uvp/UVP-GB28181/server/app/gb28181/cascade/service/management.go:357)、[Controller:123](/Users/menglulu/code/uvp/UVP-GB28181/server/app/gb28181/cascade/controller/management.go:123) |
| D08 | P2 | 实时流异常回收 | 监测只覆盖回放终态及“开启 RTCP 保活的 UDP sender 失活”。默认关闭 RTCP 或使用 TCP 的实时流缺少源流/sender 消失检测，可能遗留上级会话和租约。 | [cascade_recording_playback.go:179](/Users/menglulu/code/uvp/UVP-GB28181/server/app/gb28181/cascade_recording_playback.go:179) |
| D09 | P2 | 心跳故障恢复 | 心跳失败只记录错误并安排下次心跳，不将注册状态失效或触发重新注册；上级重启丢失注册时，恢复可能一直等到后续注册续期。 | [runtime.go:545](/Users/menglulu/code/uvp/UVP-GB28181/server/app/gb28181/cascade/runtime/runtime.go:545) |
| D10 | P2 | 自动版本协商 | auto 仅读取已持久化 EffectiveVersion，默认2016；注册响应未采集 `X-GB-Ver` 并更新有效版本。“自动协商”缺少真正的版本发现闭环。 | [cascade_runtime.go:161](/Users/menglulu/code/uvp/UVP-GB28181/server/app/gb28181/cascade_runtime.go:161)、[事务结果:163](/Users/menglulu/code/uvp/UVP-GB28181/server/app/gb28181/cascade/sipclient/transaction.go:163) |
| D11 | P2 | 按设备查目录 | 已发布物理设备节点，但查询选择器只支持本平台、行政区划、业务组和虚拟组织；设备节点查询直接返回未知目标，不能展开其子通道。 | [responder.go:132](/Users/menglulu/code/uvp/UVP-GB28181/server/app/gb28181/cascade/catalog/responder.go:132) |
| D12 | P2 | 业务目录寻址兼容 | SIP To 为已发布业务组/虚拟组织编码时，入口拒绝；只有 To 仍为本平台、XML DeviceID 为业务节点时能进入底层查询。属于真实入口兼容缺口，具体直寻址验收报文需结合对接规范确认。 | [cascade_catalog_handler.go:63](/Users/menglulu/code/uvp/UVP-GB28181/server/app/gb28181/cascade_catalog_handler.go:63) |
| D13 | P2 | 上级域名配置 | API 允许 Host 为域名，但目录/信息/PTZ 请求用实际源 IP 与域名文本直接比较；可注册却无法匹配这些请求。前端只允许 IP，前后端支持范围也不一致。 | [validHost:507](/Users/menglulu/code/uvp/UVP-GB28181/server/app/gb28181/cascade/service/management.go:507)、[匹配:70](/Users/menglulu/code/uvp/UVP-GB28181/server/app/gb28181/cascade_catalog_handler.go:70) |
| D14 | P2 | 同媒体源重新请求 | Subject 只核对两个 ID，不消费媒体流序列号；同源同 SSRC 新请求返回486，未按2022附录K的媒体源标识处理释放旧链路并重建。附录K属性标注冲突需保留，不能据此将全部重复请求都判为非法。 | [Subject:312](/Users/menglulu/code/uvp/UVP-GB28181/server/app/gb28181/cascade_invite_handler.go:312)、[SSRC冲突:423](/Users/menglulu/code/uvp/UVP-GB28181/server/app/gb28181/cascade_invite_handler.go:423) |
| D15 | P2 | 回放控制完整性 | 已支持暂停、继续、npt起点拖动及正向基本倍速；不支持smpte/指定终点Range，响应仅状态/CSeq，缺少PLAY响应的RTP-Info。倒放属于推荐能力另行确定范围。 | [request.go:54](/Users/menglulu/code/uvp/UVP-GB28181/server/app/gb28181/mansrtsp/request.go:54)、[响应:84](/Users/menglulu/code/uvp/UVP-GB28181/server/app/gb28181/mansrtsp/request.go:84) |
| D16 | P2 | 注册参数规范校验 | 页面允许60秒注册周期，服务端只检查大于0，没有2022最短3600秒的版本校验。配置可以落入所选标准版约束之外。 | [index.vue:352](/Users/menglulu/code/uvp/UVP-GB28181/web/src/views/gb28181/cascade/index.vue:352)、[management.go:470](/Users/menglulu/code/uvp/UVP-GB28181/server/app/gb28181/cascade/service/management.go:470) |

## 尚未贯通的级联功能

| 编号 | 功能 | 结论与代码证据 | 范围 |
|---|---|---|---|
| M01 | DeviceStatus 状态查询 | 有底层 `PlanDeviceStatusResponse`，但 [cascade_info_handler.go:25](/Users/menglulu/code/uvp/UVP-GB28181/server/app/gb28181/cascade_info_handler.go:25) 仅接受DeviceInfo；级联总入口没有DeviceStatus分支。 | 基础能力缺口 |
| M02 | 上级目录订阅与增删改/上下线通知 | [sip/server.go:477](/Users/menglulu/code/uvp/UVP-GB28181/server/app/gb28181/sip/server.go:477) 未安装OnSubscribe；未注册方法由 [sipgo/server.go:484](/Users/menglulu/code/uvp/UVP-GB28181/server/third_party/sipgo/server.go:484) 返回405。已有主动目录MESSAGE推送不能替代SUBSCRIBE/NOTIFY。 | 推荐能力，建议作为产品基础能力 |
| M03 | 报警向上级分发 | [subscribe/alarm.go:32](/Users/menglulu/code/uvp/UVP-GB28181/server/app/gb28181/subscribe/alarm.go:32) 处理设备报警并落库，没有按上级共享投影改写编码和发送报警的链路。 | 有报警资源时适用 |
| M04 | 上级报警、移动位置等订阅 | 同M02，无上级SUBSCRIBE入口及其生命周期；已有向设备订阅和通知入库只属于接入侧。 | 推荐、按资源能力适用 |
| M05 | 预置位、巡航、扫描等扩展PTZ | [ptz_command.go:53](/Users/menglulu/code/uvp/UVP-GB28181/server/app/gb28181/manscdp/ptz_command.go:53) 明确限定基础级联；高位扩展指令在81行拒绝，现有测试也明确断言不支持。 | 按设备能力适用 |
| M06 | 录像启停、布撤防等控制 | [control/service.go:119](/Users/menglulu/code/uvp/UVP-GB28181/server/app/gb28181/cascade/control/service.go:119) 统一解析为PTZCmd，无法处理RecordCmd、GuardCmd等。收到SIP200不代表执行。 | 按设备能力适用 |
| M07 | 配置、预置位、看守位、巡航、精准状态、SD卡查询 | [级联入口:277](/Users/menglulu/code/uvp/UVP-GB28181/server/app/gb28181/cascade_catalog_handler.go:277) 没有对应分支；普通处理器将相关消息当设备查询应答，不能向上级提供结果。 | 按设备能力适用 |
| M08 | 录像下载 | [SDP解析:140](/Users/menglulu/code/uvp/UVP-GB28181/server/app/gb28181/sdp/cascade_video.go:140) 只接受Play和Playback，Download被拒；回放适配器固定ModePlayback。 | 有录像资源时适用 |
| M09 | 上级语音广播与对讲 | [视频入口:179](/Users/menglulu/code/uvp/UVP-GB28181/server/app/gb28181/cascade_invite_handler.go:179) 将音频INVITE交原设备广播链路；该链路依赖本地已发起任务，没有上下级音频会话绑定。 | 推荐、按资源能力适用 |
| M10 | 上级图像抓拍 | 无上级抓拍命令接入、任务映射及结果回传；已有设备抓拍命令/结果sink是本地业务能力，级联DeviceControl仅进入基础PTZ解析。 | 推荐、按资源能力适用 |
| M11 | 上级设备软件升级 | 无上级升级任务接入及完成结果回传；本地firmwareUpgradeService及设备结果处理没有级联任务关联。 | 推荐、按资源能力适用 |
| M12 | 消费上级注册Date授时 | [注册结果处理:523](/Users/menglulu/code/uvp/UVP-GB28181/server/app/gb28181/cascade/runtime/runtime.go:523) 只用本地clock，没有消费响应Date的路径。不能以向下接设备发送Date代替下级接收授时。 | 时间同步能力缺口；不应未经授权修改宿主机时钟 |
| M13 | 注册302重定向 | [transaction.go:139](/Users/menglulu/code/uvp/UVP-GB28181/server/app/gb28181/cascade/sipclient/transaction.go:139) 只特殊处理摘要挑战，302作为失败，继续重试原目标。 | 推荐能力，按上级对接需要安排 |

## 其他范围边界与后续验收重点

- 基础PTZ在2016版属于无业务响应控制，先回SIP200本身不能直接判为协议错误；当前缺口是其他DeviceControl也被统一走PTZ，并且上级无法据200确认设备执行。任何验收都需要看后续业务结果或设备动作。
- 录像检索/回放目前贯通的是下接设备录像。未见把平台本机录像提供给上级的分支；是否要求该录像来源属于产品范围选择，不等于每个下级平台必须本机录像。
- 设备录像查询失败或结果不完整后只记本地日志，不再发送结果，上级会等到业务超时（cascade_recording.go:161）。应明确失败收尾策略，但不能自行发明标准没有定义的错误XML字段。
- RetryPolicy字段能保存，但actor实际使用统一retry时长，没有消费该JSON策略；它当前不是完整可用的配置能力。前端编辑又会从默认空值重建该隐藏字段，后续若开放该能力需同时完善读写。
- TLS、证书认证、GB 35114和多路径路由应按适用安全等级、部署和对接范围确认。未实现这些增强项，不自动等价于普通级联全部不合格；D04针对的是现有识别逻辑本身的身份信任缺口。
- 2022附录K正文与目录对附录属性的标注存在冲突，D14保留该标准解释边界。

## 已执行的本地辅助检查

以下检查发生在老板明确本轮只做静态审查之前或是已启动命令的结果收集，没有增加浏览器/实机验收结论。执行均使用 `-count=1`；数据库类测试使用测试库/临时SQLite，不是开发台账联调。

| 检查 | 结果 | 说明 |
|---|---|---|
| `go test ./app/gb28181/cascade/...` | 12个包、177个顶层测试函数通过 | 包含目录、权限、服务、运行时、SIP客户端、媒体适配器等 |
| 根包级联、SIP入口和路由筛选测试 | 49个顶层测试函数通过 | 根包46、SIP1、routes2；有模拟依赖及本地SIP套接字测试 |
| 扩展Go检查（含上述、SDP、MANSRTSP、录像、回放、协议、配置、ZLM、迁移） | 合计548通过、6失败，按顶层函数计 | 包含子用例时984通过、16失败；两种计数不能相加 |
| `go test -race ./app/gb28181/cascade/...` | 12个包、177个顶层测试函数通过 | 是已有覆盖的竞态检查，不证明未覆盖时序正确；不能因此排除D02 |
| ZLM发流API定向检查 | 8个顶层测试函数通过 | 使用mock HTTP验证参数、身份匹配和错误处理，不证明实际RTP/PS |
| 前端级联/API/路由相关Vitest | 159项通过 | 不证明D05/D06的前后端跨模块场景已覆盖 |

6个失败的准确范围：

1. ZLM日志断言1个：`TestLoggingGBHTTPZLMIsMediaOnlineTransportFailureUsesSafeError`。当前保留错误描述后，旧断言仍要求整个原始错误文本消失；日志URL中的secret已经脱敏。仅凭此失败不能认定真实凭据泄露，也不是发流测试失败。
2. 迁移相关5个顶层测试：3个reconnect退役迁移/快照契约检查、1个目录source列快照检查、1个位置来源迁移检查。存在测试引用旧迁移文件缺失及快照文本断言差异。
3. SQLServer快照已有 `source_device_id` 和 `idx_owner_source_type_code`；对应失败是测试预期具名DEFAULT约束，而当前SQL为无显式约束名的DEFAULT，不能直接认定字段或索引缺失。

## 可复查证据

完整测试事件、摘要和87个级联相关源码文件SHA-256保存在本地目录：[/tmp/uvp-cascade-audit-20261015.kiZRc9](/tmp/uvp-cascade-audit-20261015.kiZRc9)。这是临时目录，系统清理后可能失效；本报告保留功能结论、代码位置和测试范围。

- [测试汇总](/tmp/uvp-cascade-audit-20261015.kiZRc9/summary.json)
- [核心Go测试事件](/tmp/uvp-cascade-audit-20261015.kiZRc9/go-core.jsonl)
- [集成范围Go测试事件](/tmp/uvp-cascade-audit-20261015.kiZRc9/go-integration.jsonl)
- [竞态测试事件](/tmp/uvp-cascade-audit-20261015.kiZRc9/go-race.jsonl)
- [前端测试报告](/tmp/uvp-cascade-audit-20261015.kiZRc9/web-tests.json)
- [源码指纹](/tmp/uvp-cascade-audit-20261015.kiZRc9/source-sha256.json)

本轮没有实施任何修复。后续需先确定修复范围，针对每个缺陷补能复现问题的测试，再实现修复；本报告不能替代真实上级、真实设备及媒体包验收。

## 2026-10-16 P1 修复进展

老板确认修复 D01–D05 后，已完成对应代码修改和针对性测试：

- D01：修复首个 `m=` 前媒体属性导致的 SDP 空指针。
- D02：维护事务忙碌时合并并补执行续期/心跳意图，停止时清理待办。
- D03：解析匹配 Contact 的授予期限及响应级 `Expires`，运行时按授予期限记录和续期。
- D04：INVITE、RecordInfo 取消 identity-only 来源回退；会话后续请求和事务层级联 CANCEL 绑定实际来源。
- D05：新增共享通道不再提交选择器默认 PTZ 值作为显式权限，由后端平台默认规则决定。

验证结果：级联核心、SDP 和根级联包通过；runtime/sipclient race 通过；sipgo 子模块通过；前端级联相关 117 项测试通过。修复仍未进行浏览器、真实上级平台、真实设备或媒体包联调，因此不代表完整国标互操作验收。

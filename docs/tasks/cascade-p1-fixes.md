---
title: 国标级联 P1 缺陷修复任务与测试清单
status: implemented-pending-review
spec: ../specs/cascade-p1-fixes.md
plan: ../plans/cascade-p1-fixes.md
date: 2026-10-16
---

## 执行约束

- 每个任务按 RED → GREEN → REFACTOR 执行：先新增能复现问题的测试并确认失败，再修改实现，最后跑针对性回归和必要的竞态检查。
- 只修改任务列出的源码和测试；保留工作区其他暂存、未暂存和未跟踪改动，不执行 reset、stash 或提交。
- 测试使用现有 fake、临时数据库和本地 SIP 测试，不操作浏览器、不修改现有级联台账、不部署。

## 任务清单

| 任务 | 责任文件 | RED 测试与输入 | GREEN 验收 | 回归 |
|---|---|---|---|---|
| T01 D01 修复 SDP 会话级属性空指针 | `server/app/gb28181/sdp/cascade_video.go`、`cascade_video_test.go` | 在首个 `m=` 前加入 `a=recvonly`、`f=`、`a=setup`、`a=rtpmap`；输入含音频段后再含视频段；期望返回合法视频 offer 且不 panic。 | 会话级属性保存到 session；未选媒体时所有媒体属性只保存会话字段；合法 Play/Playback 结果不变，非法输入返回 error。 | `go test ./app/gb28181/sdp/... ./app/gb28181` |
| T02 D02 修复维护事务丢事件 | `server/app/gb28181/cascade/runtime/runtime.go`、`runtime_test.go` | fake client 阻塞 REGISTER 或 Keepalive；期间触发 Refresh/Keepalive；释放阻塞后断言每类待办最终各执行一次、无并发事务。 | 维护意图合并且最终执行；停止时清理待办；重复 timer 事件不无限补发；平台隔离和关闭行为保持。 | `go test ./app/gb28181/cascade/runtime/...`; `go test -race ./app/gb28181/cascade/runtime/...` |
| T03 D03 使用上级授予注册期限 | `cascade/sipclient/transaction.go`、期限辅助测试、`runtime.go`、相应测试 | 构造 200 REGISTER 响应：Contact expires 短于请求、仅 Expires、无关 Contact、零值、非法值；期望解析结果区分授予期限、缺失和非法。 | 匹配本地 Contact；Contact expires 优先 Expires；actor 的 expiresAt 与 RefreshDelay 使用授予值；零/非法值不记录长期在线。 | `go test ./app/gb28181/cascade/sipclient/... ./app/gb28181/cascade/runtime/...` |
| T04 D04 收紧 INVITE 来源 | `cascade_invite_handler.go`、`cascade_recording.go`、入口测试 | 已知上级 From/端口/协议但源 IP 不等于配置 Host；期望 INVITE 与 RecordInfo 均 403 且 source loader/media 不被调用；启用平台严格匹配仍成功。 | 删除 identity-only 回退；IP 规范化后严格匹配唯一启用平台；会话 ACK/BYE/INFO 继续绑定原始 peer。 | `go test ./app/gb28181 -run 'Cascade(Invite|Video|Record)'` |
| T05 D04 收紧 CANCEL/事务入口 | `server/third_party/sipgo/sip/transaction_layer.go`、`transaction_server_tx.go`、级联 SIP 装配和测试 | 为级联视频事务注入不同源 CANCEL；期望不会提前 200/487 或终止原 INVITE；同源 CANCEL 仍正常终止；非级联事务行为不变。 | 可选来源校验在事务消费前执行；仅级联视频启用；重传、非 2xx ACK 和合法同源 CANCEL 保持 RFC 事务流程。 | `go test ./third_party/sipgo/...`; `go test ./app/gb28181/sip/... ./app/gb28181` |
| T06 D05 修复新增共享 PTZ 默认值覆盖 | `web/src/views/gb28181/cascade/BusinessGroupManager.vue`、相关 Vitest | 平台 PTZ 开启，选择器新增通道草稿 `ptzAllowed:false`；期望提交只有 shareChannelIds，不能带该通道的 channelPermissions；已有显式权限更新仍发送。 | 后端按保存时平台 PTZEnabled 设置新增投影默认值；前端不把无编辑入口的草稿 false 当显式变更；存量权限不变。 | `pnpm vitest run src/views/gb28181/cascade/BusinessGroupManager.test.ts src/views/gb28181/cascade/BusinessChannelPicker.test.ts`; 后端 repository 测试 |

## 测试用例明细

### T01

1. `a=recvonly`、`f=` 出现在首个 `m=` 之前：解析成功，方向/格式进入 session 字段。
2. `a=rtpmap`、`a=setup` 出现在首个 `m=` 之前：解析不 panic，不解引用 nil。
3. 先出现 `m=audio` 再出现 `m=video`：只选择 video，audio 属性不污染 video。
4. 原有 Play、Playback、TCP/UDP 正常样例全部保持通过。

### T02

1. REGISTER 阻塞时触发 Refresh：释放后恰执行一次 REGISTER，不丢续期。
2. REGISTER 阻塞时触发 Keepalive：释放后恰执行一次 Keepalive。
3. 同一事件重复触发：只保留一个待办，不产生并发请求。
4. 收到 Stop 后触发旧 timer：不再发起维护事务，已注册状态按现有流程注销。

### T03

1. 响应 Contact 与本地绑定一致且 `expires=1200`：记录 1200 秒并按 80% 安排续期。
2. Contact 无 expires、响应 `Expires: 900`：使用 900 秒。
3. 有关 Contact 不匹配本地绑定：不使用其期限，按兼容回退或报缺失结果。
4. `expires=0`、负数、溢出或非数字：视为未生效/非法，不记录为长期在线。

### T04/T05

1. 严格匹配的 INVITE、RecordInfo：继续进入投影和媒体/录像链路。
2. 仅 identity 命中的不同源 IP：在资源 loader、source lease、sender 之前返回 403。
3. 已建立会话收到不同源 BYE/INFO：返回拒绝，不释放、不控制。
4. 同源 CANCEL：仍按事务层正常响应并结束挂起 INVITE；异源 CANCEL 不影响原事务。
5. 非级联 SIP 事务：不受可选校验影响。

### T06

1. 新增共享且平台 PTZ 开启：请求不含 channelPermissions，保存后读取为 true。
2. 新增共享且平台 PTZ 关闭：保存后读取为 false。
3. 已共享通道明确修改权限：仍提交并更新该权限。
4. 取消再重新共享：由后端当前平台默认值决定，不继承前端 false 草稿。

## 完成定义

- 每项 RED 测试在修复前确实失败，并在 GREEN 后通过。
- T02、T05 的竞态/事务测试通过；D01、D03、D04、D05 的既有回归通过。
- 不以浏览器、实机上级、真实设备或媒体包结果替代上述代码证据；这些留作后续联调验收。
- 实现阶段结束后更新本文件测试结果，并单独报告未覆盖的真实互通风险。

## 本轮执行结果（2026-10-16）

| 任务 | 结果 | 证据 |
|---|---|---|
| T01 | 通过 | 首个 `m=` 前的方向、格式、rtpmap、setup 输入在修复前触发 panic；修复后 `go test ./app/gb28181/sdp/... -count=1` 通过。 |
| T02 | 通过 | 阻塞 REGISTER 期间触发 Refresh/Keepalive 的测试修复前失败、修复后通过；runtime race 通过。 |
| T03 | 通过 | Contact expires、响应 Expires、非法期限和运行时授予期限测试通过；sipclient/runtime 测试及 race 通过。 |
| T04 | 通过 | 异源 INVITE/RecordInfo 严格拒绝，资源 loader 不被触发；级联根包测试通过。 |
| T05 | 通过 | 新增 sipgo 可选 CANCEL validator；底层事务测试确认拒绝发生在内置 200/487 处理前，异源级联视频 CANCEL 被拒绝，同源请求交回原事务处理；sipgo 子模块测试通过。 |
| T06 | 通过 | 新增共享通道请求不再提交伪造的 `channelPermissions`；前端 3 个级联测试文件共 117 项通过，后端 repository 回归通过。 |

实现阶段没有操作浏览器、真实上级平台、设备或开发环境级联台账；尚未提交、推送或部署。

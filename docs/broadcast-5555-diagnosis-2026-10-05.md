# 真实设备广播故障排查

日期：2026-10-05。输入：`/Users/menglulu/tmp/5555.pcapng`。

## 结论与范围

已确认水星广播在平台匹配会话时被拒绝，尚未进入设备侧媒体传输。当前源码存在可复现的匹配缺陷：把 Subject 首段当作通道 ID，随后又把 From 中的设备 ID 放进通道 ID 查询参数，没有真正按设备 ID 兜底。

抓包时长约 13 秒，只有水星这一条广播建会话链路，没有大华建会话或独立对讲失败的 SIP 报文。因此不能据此认定大华和双向对讲具有同一个根因。抓包没有注册版本协商信息，本报告不作具体国标版本合规结论。

## 抓包证据

| 帧 | 相对时间 | 发送方 → 接收方 | 内容 |
| --- | --- | --- | --- |
| 5696 | 5.232781 秒 | 平台 192.168.10.106 → 水星 192.168.10.205 | MESSAGE / Broadcast Notify |
| 5711 | 5.238566 秒 | 水星 → 平台 | Broadcast Response，Result=OK |
| 6320 | 5.742586 秒 | 水星 → 平台 | INVITE，PCMA/8000，TCP/RTP/AVP，setup:active |
| 6353 | 5.749062 秒 | 平台 → 水星 | 403 没有匹配的 Broadcast 会话 |
| 7048、8197、10359 | 后续 | 平台 → 水星 | 同一个 403 的重传 |

平台 Notify 中的 SourceID 为 `34020000002000000002`，TargetID 为通道 `37292800001320003005`。设备 INVITE 的 From 为设备 `37010301021180000010`，Subject 为：

```text
34020000002000000002:1,37010301021180000010:2
```

设备没有在这个 Subject 中回显 Notify 的通道 ID。平台在收到 INVITE 后约 6.5 毫秒就返回 403；这条失败链路还没进行 SDP 解析或启动 RTP 发送，不能归因于麦克风波形、音量或设备扬声器。

## 源码根因

`server/app/gb28181/sip/server.go` 的 `broadcastSubjectTarget` 只取 Subject 首段，得到平台 ID `34020000002000000002`。

`server/app/gb28181/talk/service.go` 创建会话时保存设备 ID 到 `device_id`、通道 ID 到 `target_id`。`repo.go` 的查询签名为 `FindPendingBroadcast(ctx, deviceID, targetID)`。

`broadcast_activation.go` 的三次查找实际变成：

1. `device_id=37010301021180000010 AND target_id=34020000002000000002`。
2. `target_id=34020000002000000002`。
3. `target_id=37010301021180000010`。

三次均匹配不到 `target_id=37292800001320003005` 的会话。第三次代码传的是 `("", invite.PeerID)`，不是 `(invite.PeerID, "")`。之前认为已有“按设备兜底”的判断已纠正。

## 本地验证

使用 Go overlay 在临时文件中加入诊断用例，未修改业务源码。用真实设备/通道/平台 ID 和完整抓包 SDP 构造 `inviting` 状态、PCMA 音源就绪的会话：

| 实验 | 结果 |
| --- | --- |
| 当前源码，TargetID=通道 ID | 通过，进入媒体发送协商 |
| 当前源码，TargetID=抓包 Subject 首段平台 ID | 失败，复现“没有匹配的 Broadcast 会话” |
| 临时 overlay 补充真正的设备 ID 兜底查询 | 两种输入均通过，生成 TCP passive SDP 应答 |

这些验证使用本地数据库测试夹具和媒体客户端替身，证明匹配缺陷及修复方向，不代表真实设备已出声。

临时实验目录：`/var/folders/fq/t7kcbyf96kl18vpdslng8_cw0000gn/T/uvp-broadcast-5555-s7a116_y`。

```sh
cd server
go test -overlay /var/folders/fq/t7kcbyf96kl18vpdslng8_cw0000gn/T/uvp-broadcast-5555-s7a116_y/repro.json ./app/gb28181/talk -run '^TestMercuryCaptureBroadcastMatch$' -count=1 -v
go test -overlay /var/folders/fq/t7kcbyf96kl18vpdslng8_cw0000gn/T/uvp-broadcast-5555-s7a116_y/hypothesis.json ./app/gb28181/talk -run '^TestMercuryCaptureBroadcastMatch$' -count=1 -v
```

原有聚焦测试：`sip`、`sdp` 包通过；`talk` 包有既存失败，分别是 `TestTalkMigrationMatchesModelAndHasDown`、`TestTalkModeMigrationsAreIdempotentAcrossDialects`，原因是引用的四个迁移 SQL 文件不存在，与本次临时匹配实验无关。

## 修复及后续取证方向

广播：明确区分平台、设备和通道身份；设备没有回显通道 ID 时，按该设备的唯一可协商广播会话匹配；同设备多个候选必须明确拒绝歧义，不能任取一条。永久回归测试应覆盖真实水星字段、原有通道格式、多个候选和无候选。

对讲：`talk/activation.go` 的 `OnPublished` 有独立分支，平台主动发 INVITE；当前实现使用 PCMA + TCP 被动模式。需补充真实设备对讲的完整 INVITE、响应、ACK/BYE 和媒体连接抓包后确认兼容性，不能把固定 TCP 模式直接认定为本次根因。

大华：需要该设备从点击开始到失败的信令、设备 ID 与抓包时间段，才能核对身份格式和媒体协商。

以上为初次诊断与隔离实验的结果。

## 正式修复与回归结果

老板确认本次处理范围为 broadcast，并授权开始修复。修复直接落在 `talk/broadcast_activation.go`：保留已有的通道 Subject / 通道 From 匹配，再增加 `FindPendingBroadcast(ctx, invite.PeerID, "")`，用设备 ID 查找候选。无候选仍返回 403，多个候选仍返回 486；只有唯一候选才进入媒体协商。

永久测试位于 `talk/broadcast_matching_test.go`，使用水星抓包 SDP，覆盖 9 个场景：平台 Subject、通道 Subject、通道 From、缺少 Subject、身份前后空白、其他设备候选、同设备候选歧义、明确通道消除歧义、未知设备。成功场景还检查 TCP passive 应答、INVITE 重传不重复启动媒体、ACK 后会话 active 和媒体地址落库。

RED：原代码在真实 Subject、空白身份、其他设备并存场景返回 403；同设备歧义场景错误返回 403 而非 486。GREEN：补充设备 ID 查询后 9 个场景全部通过。

```sh
cd server
go test ./app/gb28181/talk -run '^TestPrepareBroadcastInviteMatchesDeviceSubject$' -count=1 -v
go test -json -skip '^(TestTalkMigrationMatchesModelAndHasDown|TestTalkModeMigrationsAreIdempotentAcrossDialects)$' ./app/gb28181/talk ./app/gb28181/sip ./app/gb28181/sdp ./app/gb28181/uac ./app/gb28181/zlm ./app/gb28181/handler
```

结果：6 个包全部通过，共 583 个顶层测试通过（顶层数不含子场景）。仅排除初次诊断已确认因缺少迁移 SQL 文件失败的两个既有测试组；没有修改迁移代码来规避问题。`git diff --check` 通过。

源码修复与回归测试已完成；未推送或部署，尚未进行真实大华/水星设备声音验收。大华采用同类 Subject / From 格式时将走同一个修复路径，仍需部署后实测确认媒体连接和出声。

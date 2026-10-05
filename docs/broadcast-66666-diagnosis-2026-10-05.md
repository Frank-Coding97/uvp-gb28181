# 大华广播故障排查

日期：2026-10-05。输入：`/Users/menglulu/tmp/66666.pcapng`。

## 结论

本次大华广播已经通过广播会话匹配，失败点在平台解析设备 INVITE 的 SDP。平台返回 488，原因是按完整字符串查找 `a=rtpmap:8 PCMA/8000`，而大华发送了合法等价的 `a=rtpmap:8 PCMA/8000/1`，显式声明单声道。平台因此误判为缺少 PCMA/8000 音频，未进入 RTP 发送。

## 抓包证据

| 帧 | 相对时间 | 发送方 → 接收方 | 内容 |
| --- | ---: | --- | --- |
| 6442 | 5.421355 秒 | 平台 `192.168.10.106` → 大华 `192.168.10.204` | Broadcast Notify |
| 6538 | 5.494502 秒 | 大华 → 平台 | Broadcast Response，`Result=OK` |
| 6734 | 5.601725 秒 | 大华 → 平台 | INVITE，携带 UDP/PCMA 音频 SDP |
| 6882 | 5.607824 秒 | 平台 → 大华 | `488 broadcast SDP 缺少 PCMA/8000 audio` |
| 6938 | 5.652559 秒 | 大华 → 平台 | ACK 失败应答 |

大华 INVITE 的关键 SDP 为：

```sdp
m=audio 9732 RTP/AVP 8 96
a=recvonly
a=rtpmap:8 PCMA/8000/1
a=rtpmap:96 PS/90000
```

设备提供 UDP、PCMA payload 8，并同时列出可选 PS payload 96。平台广播发送继续使用已有的 PCMA/8 路径，不需要切换到 PS。

## 修复

`server/app/gb28181/sdp/broadcast.go` 的 SDP 解析改为按字段解析 `a=rtpmap`：精确确认 payload 8，接受 `PCMA/8000` 和显式单声道 `PCMA/8000/1`，同时继续拒绝错误采样率、错误编码、非 8 payload、立体声和多余编码参数。大小写与属性字段间空白也按 SDP 字段规则处理。

## 回归验证

- `server/app/gb28181/sdp/testdata/dahua-broadcast-offer.sdp` 保存了本次大华 INVITE 的脱敏 SDP 夹具。
- `TestParseBroadcastOfferDahuaCapture` 验证媒体地址 `192.168.10.204:9732`、UDP、PCMA 和 SSRC。
- `TestParseBroadcastOfferPCMAMappings` 覆盖显式/隐式单声道及拒绝场景。
- `TestPrepareBroadcastInviteDahuaCapture` 验证广播会话匹配、PCMA 应答、向 `192.168.10.204:9732` 发 UDP、重传不重复启动媒体，以及 ACK 后会话状态落库。
- 相关 6 个 Go 包共 586 个顶层测试通过：`talk`、`sip`、`sdp`、`uac`、`zlm`、`handler`。
- `git diff --check` 通过。

回归命令跳过了两个已知的迁移测试组：它们引用当前仓库不存在的四个迁移 SQL 文件，与本次广播修复无关。

源码和自动化回归已完成；本次尚未推送、部署或在真实大华设备上复测出声。部署后仍需确认设备实际收到 RTP、扬声器出声，以及停止广播时 BYE/媒体清理链路正常。

# 语音对讲对外接口能力评估

> 评估对象：`/api/gb28181/device-mgmt/channel/:id/talk-sessions` 三端点 + 其上行通道设计
> 代码基线：`develop`，含 2026-09-17 广播修复（503 / 406 / 空 SDP 三处）
> 结论：**内部前端自用够用；作为对外接口不合格。** 见第三节问题清单与第五节推荐形态。

---

## 一、现状契约（实测）

### 1.1 端点

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/gb28181/device-mgmt/channel/:id/talk-sessions` | body `{mode}`，建会话 |
| GET | `/api/gb28181/device-mgmt/channel/:id/talk-sessions/:sessionId` | 查状态（`active` 时顺带续租） |
| DELETE | `/api/gb28181/device-mgmt/channel/:id/talk-sessions/:sessionId` | 停止 |

鉴权链路：挂在 `protected.Group("/gb28181")` 下（`routes.go:584`），走**平台登录态**；无 API Key / OAuth2 / scope。
`swagger.yaml` **未覆盖**对讲端点。

> 📌 **改造后新增一个端点**：`POST /api/gb28181/device-mgmt/talk-sessions/:sessionId/uplink`（上行入口，由平台转发到媒体节点）。见 §6.7。

### 1.2 返回结构（POST，`talk.CreateResult`）

> ⚠️ 本节记录的是**改造前**的对外结构（评估依据）。**新契约见 §6.7** —— `nodeId`/`nodeName`/`sourceStream`/`recvStream`/`ssrc`/`publishUrl`/`publishToken` 已全部收回内部。

```json
{
  "sessionId": "...",
  "mode": "broadcast",
  "state": "reserved",
  "nodeId": 3,
  "nodeName": "192.168.10.220:21080",
  "sourceStream": "talk_<compact-id>",
  "recvStream": "talk_recv_<compact-id>",
  "ssrc": "0123456789",
  "publishUrl": "https://192.168.10.220:21080/index/api/whip?app=talk&stream=...&token=...",
  "publishToken": "<一次性发布凭证>",
  "expiresAt": "2026-09-17T09:xx:xxZ"
}
```

### 1.3 两种 mode 的差异与共性

| mode | SIP 信令方向 | 实现落点 |
|---|---|---|
| `broadcast` | **设备主动** INVITE 平台（GB/T 28181-2022 广播流程） | `talk/broadcast_activation.go` |
| `talk` | **平台主动** INVITE 设备 | `talk/activation.go:OnPublished` → `deps.Inviter.InviteTalk` |

**共性（关键）**：两者的上行媒体通道**完全相同** —— 都是同一个 `publishUrl`（WHIP 到 ZLM）。
即 `mode` 只影响 SIP 信令方向，**不影响媒体上行方式**。上行通道全平台只有一种。

### 1.4 媒体拓扑

```
上行：调用方 --WebRTC(PCMA/8000, SRTP)--> ZLM --StartSendRtpPassive/StartBroadcastSendRtp--> 设备
下行：设备 --RTP--> ZLM --(播放侧 EasyPlayer ZLM WebRTC)--> 浏览器
```

平台在媒体面只做「ZLM 的指令下发」，本身**不承载媒体流**（纯信令控制面）。

---

## 二、调用方实际负担（前端实测）

`web/src/views/gb28181/components/PlayConsoleLinked.vue:3381-3440` 是当前唯一参考实现，共 11 个必要步骤：

1. `navigator.mediaDevices.getUserMedia({audio: {channelCount:1, echoCancellation:true, noiseSuppression:true, autoGainControl:true}})`
2. `new RTCPeerConnection()`
3. `addTransceiver(audioTrack, {direction:"sendonly"})`
4. `setCodecPreferences` 强锁 `audio/PCMA` clockRate=8000
5. `createOffer()` + `setLocalDescription()`
6. **等 ICE 收集完成**（`iceGatheringState === 'complete'`，上限 **2s**）—— 非 trickle。⚠️ **超时不是失败**：按已收集到的候选继续发（同网段靠 host candidate 就能建连），仅记一条告警 —— 这块原本是 reject，会让整个对讲失败（§6.10）
7. `assertPCMA8000(offerSdp)` 断言
8. `fetch(publishUrl, {method:"POST", headers:{"Content-Type":"application/sdp"}, body: offerSdp})`
9. `assertPCMA8000(answerSdp)` 断言
10. `setRemoteDescription(answer)`
11. 轮询 `GET` 直到 `active`，**然后才** `track.enabled = true`（先静音避免设备听空噪）

**两条约束没有任何接口级表达**（均须在第 8 步 POST 之前满足）：

| 约束 | 性质 | 不满足的症状 |
|---|---|---|
| **PCMA / 8000 强制** | **业务约束**：GB28181 设备只吃 G.711A。平台在 `activation.go:136`、`broadcast_activation.go:76` 硬检查 `hasReadyPCMA`（音频轨 && Ready && `CodecIDName == "PCMA"` && SampleRate ∈ {0, 8000}），否则 `ErrTalkCodecUnsupported`「对讲发布源不是 PCMA/8000」 | 标准 WHIP 客户端默认发 Opus（浏览器默认、OBS 默认）→ 会话激活失败 |
| **候选必须进 POST 的 offer** | **ZLM 实现限制**：WHIP 协议（RFC 9725 §4.3.2）中 Trickle ICE 是 RECOMMENDED，通过 `HTTP PATCH` + `application/trickle-ice-sdpfrag` 补候选；**ZLM 的 WHIP 端点不支持 PATCH**，客户端若按 trickle 模式发空候选 offer，后续没有通道补候选 | ICE 建不起来，媒体流推不上来 |

> ⚠️ 措辞修正（2026-09-17）：**「候选必须进 offer」≠「必须等到 `iceGatheringState === complete`」**。
> STUN 挂掉时收集永远到不了 complete，但 host candidate 早已在手 —— 此时应当带上已有候选直接发。
> 早前把这条写成「必须先完成 gathering」，导致前端把超时当成硬失败（§6.10）。

> 二者**都不是 WHIP 协议对客户端的强制要求** —— 前者是设备编解码约束向上传导的结果，后者是所选媒体服务器实现导致的。
> 结论：**只要知道这两条，任何标准 WHIP 客户端都能接；不知道就一定失败。** 这正是「SDK 或等价参考代码」的真实价值所在。

---

## 三、问题清单

### P0 — 对外阻塞

| # | 问题 | 证据 | 影响 |
|---|---|---|---|
| P0-1 | **契约泄漏实现** | 响应含 `publishUrl`(ZLM 地址) / `nodeId` / `nodeName` / `sourceStream` / `recvStream` / `ssrc` | 内部拓扑与媒体服务器实现被写进对外契约；换媒体服务器或换上行协议即破坏性变更 |
| P0-2 | **绕过平台入口** | `publishUrl = https://<node.Host>:<httpsPort>/...` 实测为内网 IP + ZLM 自签证书 | 第三方需能路由到内网、信任自签证书、防火墙放行 ZLM 端口；平台鉴权/审计/限流全部绕过 |
| P0-3 | **无开放 API 通道** | 路由在 `protected` 组；无 API Key/OAuth2/scope；swagger 未覆盖 | 第三方无合法接入途径，需先补开放接口体系。⏸️ **暂缓（2026-09-17）**：该体系的载体 openapi 分支**代码尚未合并进 develop** |

### P1 — 易用性与可维护性

| # | 问题 | 说明 |
|---|---|---|
| P1-1 | 调用方须手写 WebRTC 客户端 | 11 步 + 2 条未文档化约束（PCMA 强制、必须先 gather 完 ICE），见第二节 |
| P1-2 | 上行通道单一只 | 全平台仅 WHIP 一种；无 WebRTC 栈的调用方（Java / Python / 硬件对讲台）无法接入 |
| P1-3 | `publishToken` 明文置于 URL query | 一次性核销（`token_consumed_at`），但泄漏窗口内可被抢发流 |
| P1-4 | 会话生命周期未在契约中明示 | 实际有 30s 租约 + 10s 轮询续租 + 到期清理，对外文档缺失 |
| P1-5 | WHIP 协议本身是开放标准，但平台未声明兼容边界 | 上行为标准 WHIP（RFC 9725），未说明支持/不支持的扩展（PATCH、ICE restart、simulcast 均不支持），第三方按完整规范实现反而会踩空 |

---

## 四、WS + 后端转媒体方案评估

### 4.1 成立的部分

- **方向正确**：把复杂度收到后端、对外收敛，是对外接口设计的正解。
- **媒体面单入口是真实需求**：WHIP 反向代理只能收敛**信令**（SDP 交换走 HTTP），媒体面 SRTP 按 ICE 候选地址直连 ZLM。因此当**客户端网络无法直达 ZLM 的 RTC 端口**（UDP 被封、TCP 回退也不可用，或客户只肯复用现有单一端口、不新增任何监听）时，**WS 是唯一能把媒体面也收敛到平台单入口的方案**（TCP 单端口可穿透绝大多数网络策略）。
  - ⚠️ **「跨公网」≠「不可达」**，两者必须分开。公网 WebRTC 是行业常态，只要 ZLM 有公网可达地址、UDP 可通、前端配了 `iceServers`，公网场景 WHIP 完全可用（见 §5.1 末「公网 / 跨网段场景」）。
  - ⭐ **判据是「能开哪些端口」，不是「内网还是公网」**：RTC 媒体面只占**一个 UDP 端口**（`rtc.port`，默认 8000），并有 TCP 回退（`rtc.tcpPort`），ZLM 还自带 STUN/TURN。真正的死角比想象中窄得多。

### 4.2 修正 1：后端应对接 RTP，不是 WebRTC

平台**已有 `openRtpServer` 完整封装**：

- `zlm/management/rtp_service.go` — `OpenRtpServer` / `CloseRtpServer`
- `zlm/management/rtp_adapter.go` — `NodeRTPClientAdapter.OpenRtpServer`
- `zlm/client.go:161` — `OpenRtpServerRequest{VHost, App, StreamID, SSRC, Port, TCPMode, OnlyTrack, LocalIP, Reuse}`

故「平台 → ZLM」这段用**裸 RTP（PCMA）**即可，无需在后端实现 WebRTC 栈（DTLS / SRTP / ICE / pion 全免）。
基础设施已就绪，改造量远小于「后端做 WHIP 客户端」。

### 4.3 修正 2：浏览器场景下 WS 不会更简单

| 环节 | WebRTC 直连 | WS + 裸 PCM |
|---|---|---|
| 采样率转换 | 浏览器内部完成（带抗混叠） | 自己降采样，或让 `AudioContext` 代劳 |
| 编码 | 浏览器内部打包 PCMA | **可整体放后端** —— 前端只发 PCM（LiveGBS 即如此，见 §4.5） |
| 抖动缓冲 / 丢包隐藏 | 自带 NetEQ（抗抖动 + 丢包补偿） | 无（但 TCP 保证可靠有序，代价转为队头阻塞） |
| 回声消除 / 降噪 | 浏览器默认开启 | **同样是浏览器默认开启** —— 见下方澄清 |
| 加密 | DTLS-SRTP | TLS（WS） |

**关键澄清：AEC / ANS / AGC 属于 `getUserMedia` 的采集处理链，与后续用 WebRTC 还是 WS 传输无关。** 只要经过 `getUserMedia`，浏览器默认就会开启这三项 —— W3C Media Capture 规范中 `echoCancellation` 默认值为 `true`，Chrome / Firefox / Safari 实测均为 on by default。

因此 WS 路径**并不会丢掉回声消除**。本节初稿在此判断错误，已修正。

**麦克风在调用方手里，采集这一步平台永远替不了**；能省的只有「协商 + 编码」。
WS 与 WebRTC 的真实差异因此收敛为两点：**传输层**（UDP 直连 vs TCP 经平台，影响延迟与队头阻塞）与**音频前端的实现质量**（重采样是否抗混叠、帧长短）。

> 注：本节初稿把「G.711 编码」列为前端负担、把「回声消除」列为 WS 的代价，§4.5 的竞品实测与文档核实显示两处判断分别过重／有误 —— 编码可整体放后端；AEC 由浏览器默认提供，与传输方式无关。

### 4.4 权衡：延迟与平台定位

| 维度 | 现状 | WS 方案 |
|---|---|---|
| 网络跳数 | 1（浏览器 ↔ ZLM） | 2（浏览器 ↔ 平台 ↔ ZLM） |
| 传输层 | SRTP/UDP，无队头阻塞 | WS/TCP，抖动时队头阻塞累积 |
| 延迟预算 | 充裕 | 对讲通常需端到端 < 300–400ms；内网/专网无碍，跨公网需实测 |
| 平台定位 | 纯信令控制面 | **变为媒体面**：每会话一个 RTP 发送循环 + G.711 编码；CPU/带宽/扩缩容需重算，且新增单点 |

G.711A 编码本身极轻（查表级，单核可支撑数百至上千路），**瓶颈不在编码，在传输链路质量与平台角色变更**。

### 4.5 竞品实测：LiveGBS 的 WS 对讲（前端开源）

LiveGBS 是商用 GB28181 平台，其 Web 前端源码开源（`github.com/livegbs/GB28181-Server`，Vue2 + webpack3）。实测其语音对讲**确实走 WebSocket 推音频**，是 WS 路径最直接的先例。

> ⚠️ **版本说明**：该开源前端最后提交于 **2021-03-04**，商用版可能已演进；以下结论仅对这份开源快照成立。采集库为 `@liveqing/liveplayer@2.3.0`（npm 可获取，实读其 `liveplayer-lib.min.js`）。后端闭源，`?format=pcm|g711` 的服务端行为由前端参数与调用方式推断，**未实测**。

**链路形态**

```
浏览器 ──WS 文本帧(base64 PCM16LE@8kHz)──> 平台
  /api/v1/control/ws-talk/{serial}/{code}?format=pcm ──> 设备
```

**前端代码（`DeviceTree.vue:459` / `VideoDlg.vue:269` / `Screen.vue:701`，三处同构）**

```js
var ws = new WebSocket(`ws://${location.host}/api/v1/control/ws-talk/${serial}/${code}?format=pcm`);
LiveRecorder.get((rec, err) => { /* ... */ this.recorder = rec; rec.start(); }, {
  sampleBits: 16,
  sampleRate: 8000,
  pcmCallback: pcm => {
    var reader = new FileReader();
    reader.onloadend = () => { if (this.ws) this.ws.send(reader.result.split(',')[1]); };
    reader.readAsDataURL(pcm);          // → base64 文本帧，非二进制帧
  }
});
```

**采集库内部（`@liveqing/liveplayer@2.3.0` → `liveplayer-lib.min.js`）**

```js
e.getUserMedia = navigator.getUserMedia || navigator.webkitGetUserMedia || /* moz, ms */;
navigator.getUserMedia({ audio: !0 }, ...);                 // ① 无任何音频约束
var i = new (e.webkitAudioContext || e.AudioContext),       // ② 默认采样率（通常 48k）
    r = i.createMediaStreamSource(t),
    a = (i.createScriptProcessor || i.createJavaScriptNode).apply(i, [4096, 1, 1]);

compress: function() {                                      // ③ 降采样＝直接抽取
  var i = parseInt(this.inputSampleRate / this.outputSampleRate);  // 48000/8000 = 6
  for (; s < r;) a[s] = e[o], o += i, s++;                  //    每 6 个样本取 1 个，无低通
}
encodePCM: function() { /* float32 → int16 小端，不做 G.711 */ }
```

**逐项判读**

| 环节 | LiveGBS 实现 | 判读 |
|---|---|---|
| 采集 API | `navigator.getUserMedia({audio:true})` | 旧式 API，但库内 `adapter.js` 已 shim 到 `mediaDevices.getUserMedia`，现代浏览器仍可运行 |
| 音频约束 | **未声明任何约束**（`audio: true`） | 注意：这**不等于**「无 AEC」——浏览器默认开启 `echoCancellation` / `noiseSuppression` / `autoGainControl` |
| 降采样 | 48k → 8k **每 6 取 1**，无抗混叠滤波 | 3.4–4kHz 以上频率折叠回带内 |
| 采集块 | `createScriptProcessor(4096,1,1)` → 4096/48000 ≈ **85 ms** | 比 WebRTC 的 20 ms 帧粗 4 倍 |
| 编码 | float32 → int16 LE，**不做 G.711** | 交给后端（`?format=pcm`） |
| 传输 | `ws.send(base64)` **文本帧** | 相比二进制膨胀 33%；8k×16bit ≈ 171 kbps |
| 交互 | `@mousedown.prevent="talkStart"` + `$(document).on("mouseup touchend", ctrlStop)` | **按住说话（PTT）**，非点击切换 |
| 下行音频 | **无 `ws.onmessage`** | 纯上行喊话；设备侧声音走视频播放器 |
| 回声处理 | `// this.$refs["player"].setMuted(true)` —— **被注释掉** | 曾考虑「对讲时静音播放器」，最终未启用；不能据此推断无 AEC（见下） |

**对本方案的三点印证 / 修正**

1. **印证「编码可放后端」**：`format=pcm|g711` 这一参数说明 G.711 转换在后端完成，前端只做「降采样 + int16」。§4.3 表格初稿把「G.711 编码」列为前端负担，**判断过重** —— 实测只需 6 行抽取代码。
2. **印证「复杂度可被 SDK 吸收」**：把 Recorder.js 封装进 `@liveqing/liveplayer`（npm 开源）之后，业务代码仅剩约 40 行。这正是 §5.5「选项 A」的**现成样板** —— LiveGBS 已演示了 SDK 应该长什么样。
3. **实现层的四处粗糙点**（架构选择与实现精度是两件事，此处只评后者）：

   | # | 位置 | 实现 | 后果 |
   |---|---|---|---|
   | ① | `compress()` 降采样 | 48k→8k **每 6 个样本取 1 个**，无抗混叠滤波 | 3.4kHz 以上折叠回带内 → 语音发闷；G.711 的 3.4k 带宽限制掩盖部分折损，故「能用」。补 `OfflineAudioContext` 或 4 阶 IIR 即可 |
   | ② | `createScriptProcessor(4096,1,1)` | @48k ≈ **85 ms** 采集块；该 API 已废弃且跑主线程 | 比 WebRTC 的 20 ms 帧粗 4 倍；换 `AudioWorklet` 可降到 10–20 ms |
   | ③ | `talkStart` / `talkStop` 生命周期 | `talkStop` 做 `ws.close(); ws=null`，但 `this.recorder` **刻意不置空**（`// this.recorder = null` 被注释）；下次按下时 `if(!this.ws)` 重建连接（`onopen` **异步**赋值），而 `if(this.recorder)` 立即 `recorder.start()` | **第二次起的每一次按下，开头几十至数百 ms 音频被丢** —— `pcmCallback` 里 `if(this.ws)` 此刻仍为 null。PTT 场景下「按下瞬间」恰是要说话的位置 |
   | ④ | `onerror` / `onclose` | 只 `console.log`，无重连、无 UI 状态回退 | 连接异常时按钮仍显 active，用户以为在说话 |

   （次要）**base64 文本帧**：带宽 +33%（8k×16bit ≈ 171 kbps → 约 228 kbps），绝对量小可忽略，但改二进制帧是零成本优化。

   ⚠️ **不要把「缺 AEC」算作它的代价**：`getUserMedia` 虽未声明约束，但浏览器默认开启 `echoCancellation` / `noiseSuppression` / `autoGainControl`（规范默认值 `true`，Chrome / Firefox / Safari 一致），且 AEC 作用在 `createMediaStreamSource` 之前 —— `ScriptProcessor` 取到的已是处理后的信号。**AEC 属于采集处理链，与传输方式无关**；这是本节初稿的错误判断，已在 §4.3 修正。代码里被注释掉的 `setMuted(true)` 只能说明作者曾考虑「对讲时静音播放器」，不能反证无 AEC。

**产品定位透露出的事实**：喊话按钮被 `VersionType == '旗舰版'` 门禁保护（`DeviceTree.vue` / `VideoDlg.vue` / `Screen.vue` / `Play.vue` / `ChannelList.vue` 等 10+ 处引用），即 **LiveGBS 把 WS 对讲当作旗舰版的差异化卖点**，不是所有版本都有的基础能力。同时它**纯上行** —— 全仓既无 `ws.onmessage` 也无 `addEventListener('message')`，设备端回传的声音走视频播放器。它是**单向喊话**，不是双向对讲。两点合起来说明：该实现的服务目标是「能演示、能卖、能穿网」，而非「高保真双向音频」—— 这也解释了上表 ①②③④ 为何都停在「能用」而非「好用」。

**反过来值得抄的一处**：`Vue.prototype.canTalk = () => location.protocol.indexOf("https") == 0 || hostname === 'localhost' || hostname === '127.0.0.1'`，且在 `!canTalk()` 时渲染「麦克风划掉」图标，tooltip 原文写明「由于浏览器安全策略, 非 HTTPS 或 localhost 访问, 对讲不可用」。它把 `getUserMedia` 的 secure-context 约束做成了**显式 UI 状态**，而不是让用户猜「为什么点了没反应」。

**结论**：LiveGBS 证明 WS 路径**工程上成立、部署上无对手、实现上并不精细** —— 用「牺牲双向 + 牺牲重采样质量 + 牺牲采集块时延」换「穿一切网络 + 平台单入口」。**架构选择值得借鉴，实现细节不值得照抄**。这与 §5.1 的分场景路由判断一致 —— 不是路线对错，是场景取舍，且取舍点应当明确写进契约，而不是留给实现去凑。

### 4.6 协议事实：标准里的「对讲」本来就是两路组合

这一条直接决定对外接口该长什么样，必须先说清 —— 它不是我们的实现选择，是标准的定义。

> **语音对讲并不是一个独立的命令，而是通过组合实时音视频点播和语音广播的方式来实现的。**
> 语音对讲功能由下述两个独立的流程组合实现：
> a) 通过 **9.2 的实时视音频点播**功能，中心用户获得前端设备的实时视音频媒体流；
> b) 通过 **9.12 的语音广播**功能，中心用户向前端对讲设备发送实时音频媒体流。
>
> —— 业务上是单一的，但底层的两个方向的媒体传输是通过不同的命令实现的，**相互独立、互不影响**。

（GB/T 28181-2016 与 -2022 口径一致；2016 版 9.12 为语音广播、9.2 为实时视音频点播。）

**三点推论，直接约束接口设计**：

1. **不存在「一条双向会话」可以对外暴露。** 所谓「对讲」在平台内部必然是**两个会话对象**。如果契约只给一个 `POST /talk` 而不让调用方区分两路状态，就等于把「设备听不到」和「用户听不到」这两类完全不同的故障压成一个模糊的「对讲失败」—— 调用方无法定位，只能来问我们。
2. **两路解耦是标准规定的，不是实现瑕疵。** 任何一个方向的会话都可能先于另一个终结（设备发了 Broadcast BYE，点播那路毫发无伤）。因此契约必须允许**单路重试/重建**，不能只提供「整体重启」。
3. **设备能力要在发起前就判定。** 语音输出设备类型编码 **137**（语音输入 **136**）；设备不在目录里上报 137 子设备 = **没有语音输出能力**，广播必然不响。这应当在发起前判定并返回明确错误码，而不是等到 30s 超时。

**2022 版没有改变这一点**：它把广播流程改成经**媒体服务器**中转（SIP 服务器以 B2BUA 代理建立「接收者 ↔ 媒体服务器」，以三方呼叫控制建立「媒体服务器 ↔ 发送者」），并新增注册重定向、图像抓拍、H.265 / AAC —— **未新增独立双向对讲能力**。

**回声提示**：组合方案下 AEC 的参考信号（本地播放的、从设备绕回来的声音）带着网络绕行延迟（几百 ms），远超 AEC 尾部长度（100–200 ms），对这类**远端回声**基本失效。因此「说话时静音本地播放器」是必要措施，而不是可选优化。

---

## 五、推荐形态

### 5.1 分场景路由

| 调用方 | 推荐上行 | 理由 |
|---|---|---|
| 浏览器 / Web 前端，且与 ZLM 网络可达 | **反代 WHIP** + 提供 JS SDK | 延迟最低；编码与抖动缓冲由浏览器内置完成，前端无需自己实现 |
| 服务端 / 异构系统（Java / Python / 硬件对讲台） | **WS + PCM** | 这些环境无 WebRTC 栈，且往往只能访问平台开放的那一个端口 |
| 客户端无法直达 ZLM 的 RTC 端口（UDP 封且 TCP 回退不可用 / 只能复用现有单一端口） | **WS + PCM** | 唯一能把媒体面收敛到单入口的路径 |
| 调用方已有流媒体能力 | **给音频流 URL，平台拉流** | 真正零媒体代码，仅一条 HTTP |

**⚠️ 关于「反代」的两点澄清（最容易被误解，先看这里）**

1. **反代不需要 nginx。** 平台后端加一个转发 handler 即可（Go `httputil.ReverseProxy`），而且比让客户配 nginx 更好：零外部依赖、跟着平台一起部署、能在这层直接做鉴权/审计/限流，ZLM 的内网地址与自签证书**完全不暴露给前端**。
2. **反代只覆盖信令，覆盖不了媒体。**

   | 通道 | 协议 | 反代能否覆盖 |
   |---|---|---|
   | WHIP 信令（`POST` SDP / `DELETE` 停发） | HTTPS | ✅ 能 |
   | 音频媒体 | **UDP（SRTP）** | ❌ **不能** |

   所以反代解决的是**信任与暴露面**，不是**可达性**。浏览器仍须能直达 ZLM 的 RTC 端口（UDP `rtc.port`，或退一步的 TCP `rtc.tcpPort`）—— 这一点任何 HTTP 反代都改变不了，也正是上表最后一行必须走 WS 的原因。

**为什么反代是「必须」而非「优化」**：当前实现是后端把 ZLM 内网地址拼进 `publishUrl`（`talk/service.go:155` → `https://<ZLM_IP>:<HTTPSPort>/index/api/whip`），前端 `fetch` 直连（`PlayConsoleLinked.vue:3425`）。而 **`fetch` 遇到自签证书的 origin 会直接失败，浏览器不会给出「继续前往」的交互**（那是地址栏导航才有的）。结果是：每台客户端**首次**使用对讲前，必须先手动在地址栏访问该地址并接受证书例外 —— 开发机上早已接受过所以无感，换干净机器/浏览器则首次必失败，且报错只是一句笼统的网络错误。后端代理可一并消除这个问题。

**实现要点**（若做后端代理）：

- **必须重写 `Location` 响应头**：WHIP 返回 resource URL，客户端后续 `DELETE`/`PATCH` 依赖它；不重写前端会拿到 ZLM 的内网地址（**最易漏**）
- `DELETE` 也要代理（停止发布）
- 上游是自签证书 → 转发时 `InsecureSkipVerify: true`（内网常规做法）
- 上游不支持 `PATCH`（trickle），故客户端必须在 `POST` 前把候选收进 offer（前端用 `waitForIceGatheringComplete`；⚠️ 超时只降级不失败，见 §6.10）
- 对外路径用 `/api/gb28181/talk/uplink` 这类业务路径，不要暴露 `/index/api/whip` 的内部形状

**公网 / 跨网段场景：不是「不可达」，是缺三样配置**

WebRTC 公网通话是行业常态（各类视频会议都是公网 WebRTC），所以「跨公网就不能用」是错误结论。本实现当前**确实跨不了网段，但根因是配置缺失，不是协议限制**。核查代码后的三项缺口：

| 缺失项 | 现状 | 后果 | 补法 |
|---|---|---|---|
| 前端 `iceServers` | ✅ **已下发**（`PlayConsoleLinked.vue`：`new RTCPeerConnection({ iceServers: uplink?.iceServers \|\| [] })`） | 为空数组时等价于旧行为：只产 **host candidate** | ⛔ **门禁**：只有节点声明了 `rtc.externIP` 才下发（`talk/ice.go`）。同网段不需要 srflx，且下发一个不可达的 STUN 会把发布拖死（§6.10） |
| ZLM `rtc.externIP` | ⏳ **两个节点实测均为空** | ① ZLM 写进 SDP 的候选仍是内网地址 ② 平台因此**不下发 STUN** → 跨网段仍不可用 | 填对端可达 IP（**可多个、逗号分隔**）。平台可用已有的 `SetServerConfig` 通道代为下发（见 §6.6 A1）。⭐ 它同时是**跨网段开关** |
| 防火墙放行 | 未纳入部署要求 | 信令通、媒体不通（ICE 一直 pending 后失败） | 放行 **`rtc.port`（单个 UDP 端口；实测 node 2 = 18001 / node 3 = 21800）**，**不是端口段**；用内置 STUN 时**还要放行 `rtc.icePort`**（node 2 = 3478 / node 3 = 21812）。⛔ **容器化部署下「放行」= docker 端口映射**：node 2 的 `uvp-zlm` 容器漏了 `3478` 与 `8000`（§6.10） |

**⚠️ 三点与直觉不符、但属 ZLM 事实（已核对官方 `config.ini` 原文注释）**

1. **WebRTC 媒体面只占一个 UDP 端口。** `[rtc] port` 注释原文：「rtc udp 服务器监听端口号，**所有 rtc 客户端将通过该端口**传输 stun/dtls/srtp/srtcp 数据」。
   ⛔ 别和 GB28181 的 RTP 收流混淆 —— 后者是 `rtp_proxy.port_range`（默认 30000–35000 那**一段**），是给**设备**推流用的。放行成本是「一个端口」，不是「一段」。
2. **不需要自建 TURN。** ZLM 内置 STUN/TURN（`rtc.enableTurn=1` 默认开）。⚠️ **`icePort` 的「默认 3478」只是默认值，生产里通常被端口规划改掉**：实测 node 2 = `3478`（沿用默认）、node 3 = `21812`。两个节点**都真的在听**（各 16 个 UDP socket，同一形态），差别只在宿主有没有放行/映射。对称 NAT 场景**优先用 ZLM 自带的**，确认能力不足再考虑外部 coturn。
3. **UDP 被封时还有第三个选项**：`rtc.tcpPort`（默认 8000）是 **WebRTC over TCP** 的退路，注释原文「在 udp 不通的情况下，会使用 tcp 传输数据」。即「UDP 封但能开一个 TCP 端口」**未必**要退到 WS —— 但这条**待实测**（ZLM 是否在候选里给出 TCP candidate、浏览器能否建连、`preferred_tcp` 是否需置 1），见 §七。

于是分档改为（**判据是「能开哪些端口」，不是「内网还是公网」**）：

| 客户端网络条件 | 可用上行 | 说明 |
|---|---|---|
| UDP 直达 `rtc.port`（内网 / 专网 / 公网） | **WHIP over UDP** | 最优路径 |
| 对称 NAT，打洞失败 | **WHIP + ZLM 内置 TURN** | 中继，仍优于 WS（SRTP/UDP） |
| UDP 被封，但可开一个 TCP 端口 | **WHIP over TCP**（`rtc.tcpPort`）| **待实测**；不可行再退 WS |
| 只能复用现有单一端口、不新增任何监听 | **WS + PCM** | **唯一的真死角** |

**弹性公网 IP 场景**（云主机无法 bind 公网 IP）：ZLM 提供 `rtc.interfaces`，注释原文「指定网卡 bind socket，以解决公网 IP 使用弹性公网 IP 配置实现（部署机器无法 bind 该公网 ip 的问题）」，需与 `externIP` 配合使用。

**公网暴露 WHIP 端点会放大鉴权问题**：当前 token 放在 **query 参数**里（`talk/service.go:155` 把 `token` 拼进 publishURL）→ 会进访问日志、`Referer`、浏览器历史。内网部署无所谓，**公网暴露前应改为请求头**（WHIP 标准用 `Authorization: Bearer`，ZLM 端点是否支持需实测，见 §七）。

### 5.2 核心设计：把上行方式做成可扩展字段

**不要**在契约里写死 `publishUrl`。用 `uplink.protocol` 做多态，新增协议不动既有调用方：

> 📌 **路径前缀待定**：下例的 `/openapi/v1` 依赖**开放 API 网关**，而网关⏸️**暂缓**（openapi 分支代码尚未合并）。
> 已核实该分支网关的命名空间**就是 `/openapi/v1`**（`server/app/openapi/routes/public.go`：`gin.New()` 起独立子路由树，
> 靠 `root.Use` 按 `/openapi` 前缀劫持，与 `/api/**` 的 JWT 树完全隔离，AK/SK 签名 + scope），所以合并后前缀大概率不用改。
> **已实现落点是 `POST /api/gb28181/device-mgmt/talk-sessions`**（§6.7）；字段结构已按本设计落地（`uplink` 多态 + `iceServers`）。

```json
POST /openapi/v1/talk-sessions
{ "channelId": 123, "mode": "broadcast" }

200 OK
{
  "sessionId": "ts_01H...",
  "mode": "broadcast",
  "state": "reserved",
  "expiresAt": "2026-09-17T09:30:00Z",
  "uplink": {
    "protocol": "whip",
    "url": "https://platform.example.com/openapi/v1/talk/uplink?token=<一次性>",
    "contentType": "application/sdp"
  }
}
```

其余两种形态仅在 `uplink` 上不同：

```json
{ "uplink": { "protocol": "ws", "url": "wss://platform.example.com/openapi/v1/talk/uplink?token=<一次性>",
              "audio": { "codec": "PCMA", "sampleRate": 8000, "channels": 1, "frameMs": 20 } } }

{ "uplink": { "protocol": "pull", "audioSource": { "url": "rtsp://...", "codec": "PCMA" } } }
```

配套两个端点（**必须有，否则调用方不知道怎么停**）：

```
GET    /openapi/v1/talk-sessions/{id}   → 查状态 + 续租
DELETE /openapi/v1/talk-sessions/{id}   → 停止
```

> 「两个接口」的骨架是对的，但**不能砍成两个**：会话有 30s 租约 + 续租 + 到期清理，
> 没有 `GET`（续租/探活）和 `DELETE`（显式停止），调用方无法正确管理生命周期。

### 5.3 WS 帧格式（若采用）

**主推格式** —— 沿用设备侧约束，调用方零编码工作量的目标才成立：

| 项 | 值 |
|---|---|
| 编码 | G.711A（PCMA） |
| 采样率 / 声道 | 8000 Hz / 单声道 |
| 帧长 | 20 ms / 160 样本 |
| 帧字节 | 160 B（1 B/样本），二进制帧，无额外头 |

**建议同时提供 LiveGBS 兼容模式**（`?format=pcm`）—— 接受 `base64(PCM16LE@8kHz)` 文本帧，行为对齐 §4.5 实测的竞品端点：

- 成本极低：后端多一个「base64 文本帧 → PCM → G.711A」的解码分支，与主路径共用同一套 RTP 打包管线
- 收益明确：**已对接过 LiveGBS 对讲的集成商，改一个 URL 即可接入本平台**，无需重写采集代码
- 这也顺带覆盖了「调用方不愿引入浏览器 SDK」的场景 —— 服务端直接发 base64 PCM 帧即可推流

### 5.4 契约瘦身

对外响应**只保留**：`sessionId` / `mode` / `state` / `expiresAt` / `uplink`。
`nodeId`、`nodeName`、`sourceStream`、`recvStream`、`ssrc` 降级为内部字段（或仅在管理员接口返回）。
这是让「换媒体服务器/换上行协议」不成为破坏性变更的前提。

### 5.5 关于 SDK：不是技术门槛，是「约束可发现性」

**调用方并非必须使用平台 SDK。** 上行走的是标准 WHIP（RFC 9725），一个公开 IETF 标准，平台不校验客户端身份。任何能在 POST 前完成 ICE gathering 的 WHIP 客户端都能接（OBS、GStreamer、mediasoup-client、各语言 WebRTC 库）。

但**没有 SDK 或等价参考代码时，第三方会必然踩第二节那两条约束**，且症状（「没声音」「卡在正在建立中」）与网络问题难以区分。所以 SDK 的定位是：

> **把约束封装掉 + 让契约可发现**，而不是技术垄断。

三个可选的取舍：

| 选项 | 做法 | 调用方负担 | 平台代价 |
|---|---|---|---|
| **A. SDK + 文档写明约束** | 浏览器：一个 npm 包（≈10 行调用）；其他语言：文档 + 可复制参考代码 | 仍需「会 WebRTC」，但不必读源码 | **无**，平台保持纯信令 |
| **B. 平台做编解码归一化** | 平台把 Opus → PCMA 转一次，契约即可接受任何编解码 | **零编解码约束**，可用任意标准 WHIP 客户端（OBS 可直接推） | 媒体流进平台（拉流 + 转码 + 推回 ZLM），平台从纯信令变为媒体面，多一跳 |
| **C. 调用方给音频流 URL** | 调用方提供 RTSP/RTMP/HTTP 音频流地址，平台 `addStreamProxy` 拉流 | **零媒体代码**，一个 HTTP 请求；不需要任何 SDK | 同 B（媒体流进平台） |

**选项 A 已有现成样板**：§4.5 实测的 LiveGBS 正是把采集封装成 `@liveqing/liveplayer` 中的 `LiveRecorder`（npm 开源），业务代码只剩约 40 行。注意它封装的是 **WS 而非 WHIP** —— 说明 SDK 的具体形态随上行方式变化，但「把约束藏进库里、对调用方只暴露几行」这个作用是共通的。

**关键联动**：选项 B 的「归一化到 PCMA/RTP」组件与第四节 WS 方案的后端媒体处理是**同一个组件**。若将来要做 WS 上行，先做 B 属于顺路投资——两者共用一套媒体归一化管线。

**建议次序**：先做 A（零风险、立即解掉 P0-1/2）；P0-3 的载体 openapi 分支**尚未合并** → 暂缓（2026-09-17）。把两条约束写进 openapi 与接入文档；B / C 与 WS 方案合并评估，按调用方实际网络条件与类型再排期。

---

## 六、最终方案

### 6.1 形态收敛

一句话：**对外只暴露平台自己的域名和「一个会话对象」；上行方式做成 `uplink.protocol` 多态，默认 WHIP 走平台反代；平台内部始终保留两个方向独立的会话对象与两路状态。**

| 项 | 结论 | 细节 |
|---|---|---|
| 端点 | `POST` 创建 / `GET` 查状态续租 / `DELETE` 停止 —— **三个，不能砍成两个** | §5.2 |
| 对外字段 | 仅 `sessionId` / `mode` / `state` / `expiresAt` / `uplink`；`nodeId`/`sourceStream`/`ssrc` 收回内部 | §5.4 |
| 上行 | `uplink.protocol` ∈ `whip`（默认）/ `ws` / `pull`，三方共用后端一套媒体管线 | §5.2、§5.3 |
| 反代 | **后端 handler 做，不用 nginx**；`url` 指向平台域名，ZLM 内网地址与自签证书不露面 | §5.1 |
| 不可省的边界 | 反代只搬信令；媒体面 SRTP 是浏览器↔ZLM 的 UDP，**可达性由网络决定**，任何 HTTP 反代都改不了 | §5.1 |

### 6.2 平台侧需要新增的组件

| 组件 | 作用 | 服务于 | 状态 |
|---|---|---|---|
| **WHIP 反向代理 handler** | 平台 → ZLM：重写 `Location`、上游 `InsecureSkipVerify`、后端代持令牌 | `whip` | ✅ 已实现（§6.7） |
| ~~**开放 API 网关**~~ | 已在 openapi 分支实现：`/openapi/v1` 独立路由树 + **AK/SK 签名（v1 canonical）** + scope 粒度 + 客户端密钥管理 + 拒绝审计（模块 `server/app/openapi/`） | 全部 | ⏸️ **暂缓（2026-09-17）**：**代码在 openapi 分支上，尚未合并进 develop**（`codex/aksk-openapi*` 逐条 `merge-base --is-ancestor` 验过） |
| **媒体归一化管线** | 任意输入（PCM16 / base64 PCM / Opus）→ **G.711A @8000Hz 单声道** → 20ms RTP（160B）→ 会话 SSRC `pushRtp` | `ws`、`pull` | ⏳ 二期 |
| **WS 上行端点** | 收帧 → 会话 SSRC；含 `?format=pcm` 兼容模式 | `ws` | ⏳ 二期 |

> 媒体归一化管线与 §5.5 选项 B 是**同一个组件**，做一次同时满足「WS 上行」与「编解码归一化」。
> ⚠️ 反代与契约瘦身已落地，所以**当前对外形态已经是新契约**；但路由仍在 `protected` 下（无 API Key）——
> 网关**暂缓**（openapi 分支未合并），第三方正式接入前必须先补上。

### 6.3 三期路线

| 期 | 内容 | 解决 | 完成后的对外可用性 |
|---|---|---|---|
| **一期** | WHIP 后端反代 + 契约瘦身 + ICE 下发（跨网段另按 **§6.6** 补配置）；~~开放 API 网关~~ **暂缓，待 openapi 分支合并** | P0-1/2、P1-3/4/5（P0-3 暂缓） | **浏览器/Web 前端 + 能到 ZLM 的 RTC 端口 → 即可用**（当前主要场景） |
| **二期** | 媒体归一化管线 + WS 上行 + `?format=pcm` 兼容 + `pull` 模式 | P1-2 | 覆盖「无 WebRTC 栈」「只能复用现有单一端口、不新增监听」「调用方已有流」 |
| **三期** | 浏览器 SDK（npm，约 10 行调用）+ 各语言可复制参考代码 | P1-1 | 调用方负担下降；**非阻塞**，一期即可不用它 |

### 6.4 实施步骤（按优先级）

1. ⏸️ **补开放 API 通道**（P0-3）**暂缓（2026-09-17 用户决定）** —— 网关实现已在 openapi 分支上、**尚未合并进 develop**，等合并后再做。合并后待办（备查）：① 把对讲 4 个端点加进 `server/app/openapi/routes/public.go` 那张**显式发布清单**（发布面是白名单，不是从后端路由推导的）；② 定 scope 名，对齐既有 `device:list` / `channel:list` / `play:live:apply` 的冒号风格（别沿用早前草稿的 `talk:control`）；③ 核 **WHIP 成功响应**（`201` + `Content-Type: application/sdp` + 裸 SDP body + `Location` 头）在网关下是否原样透出。
2. **平台反代 WHIP 信令**（P0-1 / P0-2）✅ **已做（2026-09-17）**：**后端加转发 handler**，对外路径 `POST /api/gb28181/device-mgmt/talk-sessions/:sessionId/uplink` → ZLM WHIP，透传 `Content-Type: application/sdp`、重写 `Location`、后端代持令牌。**不需要 nginx，也不该要求客户装 nginx**（理由见 §5.1）。
   - ⚠️ 只收敛信令。媒体面 SRTP 仍按 ICE 候选直连 ZLM；若需媒体面也收敛，必须走第 3 项或部署 TURN 中继。
   - **跨网段 / 公网接入**：按 **§6.6 实施清单**执行（代码 3 处 + ZLM 配置项 + 放行矩阵 + 验收判据）。三项当前全部缺失，内网客户不受影响，但接入方一旦跨网段就会表现为「信令通、听不到」。
   - 📌 A2（`publishURL` 改用对外地址）已随反代**自然消除**，无需单独改。
3. **WS 上行通道**（P1-2，按调用方实际需要排期）：
   - 新增 WS 端点 + 会话内 `openRtpServer(SSRC, Port, TCPMode)` 接流
   - 平台侧 `PCM → RTP(PCMA)` 打包，按会话 SSRC 推入
   - 帧边界、丢帧策略、抖动缓冲需明确（建议服务端做 20ms 重采样对齐）
   - **同时实现 `?format=pcm` 兼容模式**（base64 PCM 文本帧），对齐 LiveGBS 端点形状（§4.5、§5.3）—— 成本低，且让已对接竞品的集成商改 URL 即可迁移
4. **契约瘦身**（P0-1）✅ **已做（2026-09-17）**：按 5.4 收窄对外字段，内部字段移出（`nodeId`/`nodeName`/`sourceStream`/`recvStream`/`ssrc`/`publishToken` 全部收回内部）。
5. **错误语义对齐** ⚠️ 部分完成：上行端点已给出稳定语义（`404` 会话不存在 / `410` 已过期 / `409` 已开始发布）；其余「设备离线 / 节点不可用 / 租约冲突 / 编码不支持」仍是中文文案。

### 6.5 已定的决策与遗留问题

**① 下行（听设备侧声音）进不进契约？—— 已定：不进。**

按 §4.6，标准里的「对讲」本就是两路组合：上行 9.12 广播（本契约覆盖）+ 下行 9.2 点播（设备侧声音）。决定**契约只保留上行**：

- 契约保持最窄 —— 只有「把本地音频送进设备」一件事，不多维护一个方向的状态
- 下行的能力本来就有（平台播放 / 拉流接口），调用方需要时直接调，不必在对讲契约里再包一层

⚠️ **代价要写进接入文档**：调用方遇到「设备不响」时无法从对讲接口自证方向。所以接入文档必须明写 **「本接口只保证上行；下行请使用平台的播放 / 拉流接口」**，并给出对应接口指路 —— 否则现场会把「下行没接」报成「对讲坏了」。

**② 二期的对外端口前提（已澄清：端口是活端口）**

「客户只能开单个对外端口」里的端口号**不是写死的 443**，是现场决定的活端口（可能被改成任意值，也可能由前置设备随机映射）。两条约束随之而来：

- 平台侧**不得在任何地方硬编码对外端口**：契约里的 `uplink.url` 必须按**请求实际到达的 host / 端口**拼（或用配置项），不能写死协议或端口
- WS 上行的价值来自**只占一个 TCP 端口**，不来自「443 能穿透」—— 判断某客户该走哪条路时，问的是「浏览器能不能 UDP 直达 ZLM」，而不是「443 通不通」

**③ 二期做不做？**

二期的真实成本不在 WS 那几十行，而在**平台从纯信令变成媒体面**：多一跳（延迟 + 抖动缓冲）、CPU / 带宽上升、并发容量未压测（§七）。判断依据是客户构成：

- 客户都是浏览器/Web 前端，且浏览器能到 ZLM 的 RTC 端口 → **一期就够，二期可缓**
- 存在服务端 / 硬件台接入（**无 WebRTC 栈**）→ 二期不可省
- ⚠️ 但「UDP 被封」**不再自动等于**「必须上二期」：ZLM 有 `rtc.tcpPort` 回退、且自带 STUN/TURN，应先按 §6.6 E 步逐级验证。只有「**只能复用现有单一端口、不新增任何监听**」才是二期不可省的情形

**④ 视觉反馈不进契约（边界声明）**

前端对讲按钮上的音频波形（采集电平驱动的起伏）属于**平台自身的 UI 反馈**，不进对外契约：

- 不提供电平 / 采集状态接口，不把「麦克风是否在动」做成可查询字段
- 第三方拿到的是**会话 + 上行入口**；怎么展现、展不展现、要不要自己做波形，**是调用方的事**

反向也成立：**调用方不应从 `state=active` 推断音频内容**。它只表示信令与推流已建立，不表示音频有效、更不表示音质合格 —— 这条要写进接入文档，避免被当成「平台声称音频正常」的证据。

### 6.6 跨网段 / 公网接入实施清单

**第 0 步（前置）：先探测 ZLM 实际能力，不改任何东西**

调 `GetServerConfig`（平台已有通道），看返回里 `rtc.*` 有哪些键：

- 有 `rtc.icePort` / `rtc.enableTurn` / `rtc.port_range` → **新版 ZLM**，走「内置 STUN/TURN」路线，**不需要外部 coturn**
- 只有 `rtc.port` / `rtc.externIP` → 旧版，STUN 需外部提供

⭐ **这一步决定后面所有配置项，先做。**

⚠️ 两个已知情况：平台的 ZLM 配置白名单 `configCatalog`（`zlm/service/config_service.go:82`）**目前完全没有 `rtc.*` 段**，UI 里配不到；但 `SetServerConfig` 是**直通 ZLM API**（`zlm/client.go:143`），**不受白名单限制** —— 所以平台可以代码代配，只是 UI 需另加 catalog 项。

**A. 平台侧代码改动（3 处，全部复用已有字段）**

| # | 位置 | 现状 | 改法 |
|---|---|---|---|
| A1 | `zlm/apply.go` → `ApplyConfigForNode` | `params` 只写 hook / general 键 | 追加 `rtc.externIP`（取 `node.EffectiveReceiveHost()`）；并把 `rtc.port` / `rtc.tcpPort` / `rtc.icePort` 加入 `readbackKeys` 做回读校验 |
| A2 | `talk/service.go:155` | 拼 `https://<mediaNode.Host>:<HTTPSPort>` —— 用的是**内网 API host** | 改用 `mediaNode.EffectivePlaybackHost()`（该字段注释即「播放访问地址,返回给浏览器/客户端」） |
| A3 | `talk.CreateResult` 及对外响应 | 无 `iceServers` | ✅ 已落地：`ICEServers []ICEServer`（`talk/ice.go: BuildICEServers`）。⛔ **只在 `rtc.externIP` 非空时下发**，否则空数组（原方案「派生 `stun:<EffectivePlaybackHost()>:<icePort>`」已废弃，原因见 §6.10） |

⭐ **A1 / A2 是"复用已有字段"，不是新增配置** —— `node.Node` 早有 `ReceiveHost` / `PlaybackHost` 及 `Effective*` 兜底（`zlm/node/node.go:42-58`），且是 **per-node 存 DB、管理界面可改**，无需重启平台。

⚠️ 语义细差：`ReceiveHost` 是「**设备**收流地址」，`rtc.externIP` 是「**RTC 客户端**（浏览器）可见 IP」。同机部署通常相同，先复用；若出现「设备在专网、浏览器在公网」这类两面地址不同的部署，再拆独立字段。

**B. 前端改动（2 处）**

| # | 位置 | 改法 |
|---|---|---|
| B1 | `PlayConsoleLinked.vue` | ✅ 已改为 `new RTCPeerConnection({ iceServers: uplink?.iceServers \|\| [] })`；⚠️ 同时把 `waitForIceGatheringComplete` 改成**超时降级不失败**（§6.10） |
| B2 | `talkPublisher.ts` | 抽 `buildPeerConnection(iceServers)`，便于单测覆盖 |

⚠️ **降级安全**：`iceServers` 为空时行为与今天**完全一致**（只产 host candidate），内网部署零影响 —— 已有测试守住：`talk/ice_test.go` 的 `TestBuildICEServersReturnsNilWhenNotDeclaredOrUnusable` + `talkPublisher.test.ts` 的「收集超时只降级返回 false，不抛错」。

**C. ZLM 侧配置项**

| 键 | 默认 | 用途 | 何时需要 |
|---|---|---|---|
| `rtc.externIP` | 空 | 对 RTC 客户端可见 IP，可多个（逗号分隔）；**置空会自动取网卡 IP** | **跨网段必填** |
| `rtc.port` | 8000 | RTC **UDP** 监听（所有客户端共用这一个） | 永远 |
| `rtc.tcpPort` | 8000 | RTC **TCP** 监听，UDP 不通时回退 | UDP 被封时 |
| `rtc.icePort` / `iceTcpPort` | 3478 | 内置 STUN/TURN（UDP/TCP） | 用内置 STUN/TURN 时。⛔ 容器部署**必须把这两个端口映射出来**，否则会出现「ZLM 配置里有、宿主上不可达」（§6.10 实测） |
| `rtc.enableTurn` | 1 | TURN 开关 | 对称 NAT 时 |
| `rtc.port_range` | 49152-65535 | **TURN 分配端口池** | 用 TURN 时 |
| `rtc.interfaces` | 空 | 指定网卡 bind，解决弹性公网 IP 无法 bind 的问题 | 云主机 EIP 场景 |

**D. 网络放行矩阵（按能力递进，越往下要求越高）**

| 需放行 | 协议 | 覆盖场景 |
|---|---|---|
| `rtc.port` | **UDP** | 内网 / 专网 / 公网 UDP 可达 |
| ＋`rtc.tcpPort` | TCP | UDP 被封 |
| ＋`rtc.icePort`（＋`iceTcpPort`） | UDP / TCP | 使用 ZLM 内置 STUN/TURN。⛔ 容器部署最常漏这两个端口（§6.10） |
| ＋`rtc.port_range` | UDP | 使用 TURN 中继（对称 NAT） |

**E. 实施顺序（每步都有可验证产出）**

1. `GetServerConfig` 探测 `rtc.*` → **确定路线**
2. 配 `rtc.externIP` + 放行 `rtc.port`(UDP) → 抓 answer SDP，**候选 IP 必须变成公网 IP**
   ⛔ 回读配置值只证明 ZLM 的 mINI 改了，**不证明 WebRTC 模块用上了新值** —— 唯一可信判据是 SDP 里的候选地址
3. 前端加 `iceServers` → 抓 `getStats()`，应出现 **srflx** 候选且 `candidate-pair.state = succeeded`
4. UDP 不通 → 试 `rtc.tcpPort`，看候选里有无 TCP candidate
5. 对称 NAT → 开 `enableTurn` + 放行 `port_range`
6. **以上都不可行** → 才立二期 WS

**F. 验收判据**

| 项 | 判据 |
|---|---|
| 候选正确 | answer SDP 候选 IP = 公网 IP（非 `192.168.` / `10.`） |
| ICE 成功 | `connectionState === "connected"`，且 `candidate-pair.state = succeeded` |
| 媒体上行 | ZLM `getMediaList` 中 talk 流 `readerCount > 0` |
| 端到端 | 真机对讲出声 + 设备侧日志（四路对照，同 §0.6 口径） |

**G. 与一期的关系**

本清单**属于一期**（服务跨网段客户），与一期主线（反代 + 契约瘦身 + ICE 下发；开放 API 网关⏸️暂缓）无依赖冲突，可并行。
若一期客户全在内网，A1 / B1 可延后；但 **A2 必须在一期做** —— 它同时是 P0-2「不暴露内网 IP」的修复。
> 📌 **实施后记（2026-09-17）**：A2 被**反代取代**了 —— 前端不再拿到 `publishUrl`，`uplink.url` 指向平台自己，媒体节点地址从信令面消失（见 §6.7）。A1 / B / C / D 仍待做。

### 6.7 实施进展（2026-09-17）

一期落了**反代 + 契约瘦身 + ICE 下发**三件；开放 API 网关**暂缓**（openapi 分支代码尚未合并，2026-09-17 用户决定）。

#### 已实现

| 项 | 落点 | 判据（自动化） |
|---|---|---|
| WHIP 后端反代 | `controllers/talk.go: Uplink`，路由 `POST /api/gb28181/device-mgmt/talk-sessions/:sessionId/uplink` | 上游用 `httptest.NewTLSServer`（自签证书）起假节点：offer 原样到达、状态码与 SDP 回传、`Location` 被换成平台地址 |
| **后端代持发布令牌** | `talk/uplink.go: PrepareUplink` + `repo.ReissuePublishToken` | 响应 JSON 里不含 token；令牌只存在于「后端内存 → 上游 URL」这一跳 |
| 契约瘦身 | `talk.CreateResult` | 序列化后断言不含 `nodeId`/`nodeName`/`sourceStream`/`recvStream`/`ssrc`/`18443`/`token` |
| ICE 下发 | `node.ParseServerConfig` 补 `rtc.*`；`talk/ice.go: BuildICEServers` | `uplink.iceServers = ["stun:<host>:<rtc.icePort>"]`，`rtc.externIP` 优先、带端口的 host 会被剥离 |
| 对外地址不硬编码 | `controllers/talk.go: talkPublicOrigin` | 用 `c.FullPath()` 裁出 API 前缀（路由模式串与 `routes` 共用常量）+ `X-Forwarded-*` 优先；测试把路由挂在 `/api/gb28181/device-mgmt` 下验证前缀跟着走 |

**新契约**（实测形状）：

```json
{
  "sessionId": "…",
  "mode": "talk",
  "state": "reserved",
  "expiresAt": "2026-09-17T…Z",
  "uplink": {
    "protocol": "whip",
    "url": "https://<请求实际到达的 host>/api/gb28181/device-mgmt/talk-sessions/<id>/uplink",
    "contentType": "application/sdp",
    "iceServers": []   // 同网段：节点未声明 rtc.externIP，不下发；跨网段时是 [{ "urls": ["stun:<externIP>:<icePort>"] }]
  }
}
```

对外只多了一个端点（上行入口）；调用方拿到的地址**永远是平台自己**，媒体节点地址只出现在 `iceServers` 里 —— 那是媒体面直连所需，藏不住也不该藏。

> ⚠️ `iceServers` **允许为空数组**，客户端必须按「只产 host candidate」处理（同网段就是这样）。
> 拿到非空值时，先用 STUN 探一次那个端口（§6.10 末的探活命令）——**它是媒体面直连地址，不可达时会把发布拖死**。

#### 三个必须知道的实现约束

1. **令牌轮换只在 `reserved` 态生效**（`repo.ReissuePublishToken`）。进入 `publishing` 后再签也过不了 `AuthorizePublish` 的「非 reserved 即拒绝」分支，所以第二次 `POST uplink` 得到 **409**；重连必须走「DELETE 旧会话 + 新建」，不要指望重发 offer。
2. **上游走 https 且跳过证书校验**（内网自签）。实测 `http://<host>:18080/index/api/whip` **同样提供该端点**，将来若嫌 TLS 开销可切 http（本次未改，行为保持与旧实现一致）。
3. **令牌不再进浏览器**，于是 §七 里「公网暴露前必须把 token 从 query 改成请求头」这条**不再阻塞公网**：token 只在平台后端到 ZLM 这一跳出现，不进浏览器历史 / Referer / 前端日志。若将来要支持第三方**按 WHIP 标准**自带 Bearer 头直连 ZLM，那才需要实测 ZLM 是否收头鉴权。

#### 未做（仍待办）

- ⏸️ **开放 API 网关**（P0-3）**暂缓，不计入待办**：openapi 分支代码尚未合并进 develop（2026-09-17 用户决定）。现状：路由仍在 `protected` 下，无 API Key / scope，`swagger.yaml` 未覆盖（§6.4 第 1 条）。
- **§6.6 A1 / B / C / D**：`zlm/apply.go` 下发 `rtc.externIP` 与回读校验未做（需要 ZLM 配置变更窗口 + 放行矩阵）。
- **端到端真机验证**：反代后的完整链路（浏览器 → 平台 → ZLM → 设备出声）**尚未在真机/浏览器验证** —— 改的是 SIP 装配期之外的代码，但后端仍需重启才生效。

### 6.8 可用性验收清单（首要目标：系统可用）

> 2026-09-17 加上。目的：把「改完了」变成「**证明可用**」。按层跑，**任一层红就地停**，别跳层。

**前置 0（唯一阻塞项）：重启后端。**
运行中的实例是**旧构建** —— 实测 `POST /api/gb28181/device-mgmt/talk-sessions/<id>/uplink` 返回 **404**（路由未注册），
而 `POST .../channel/1/talk-sessions` 返回 **401**（路由在、被 JWT 挡住）。**404 vs 401 就是「新代码生效与否」的判据**。
重启方式：VSCode 调试配置「启动后端 (server)」（`.vscode/launch.json`，cwd `server/`）。

| 层 | 动作 | 期望（判据） | 红了先看 |
|---|---|---|---|
| 0 编译 | `go build ./...` | 无输出 | 未提交改动互相冲突 |
| 1 单测 | `go test -p 1 ./app/gb28181/talk/... ./app/gb28181/controllers/... ./app/gb28181/routes/... ./app/gb28181/zlm/node/...` | 4 个包全 `ok` | 他人未提交改动 |
| 2 路由在线 | `POST /api/gb28181/device-mgmt/talk-sessions/x/uplink`（不带 JWT） | **401**（不是 404） | 后端没重启 |
| 3 会话可用 | 平台页面发起对讲 | 返回**新契约**：有 `uplink.iceServers`，**无** `token`/`nodeId`/`ssrc` | `loggingcatalog` 门禁 |
| 4 上行反代 | 浏览器 `POST uplink`（WHIP offer） | **201** + `Location` 指回平台自己；后端日志**无** `uplink_prepare_failed` | `uplink_forward_failed`（上游证书/端口） |
| **4.5 ICE 收集**（在 4 之前） | 前端等 `iceGatheringState === 'complete'` | **2s 内完成**（或 `iceServers` 为空时立即完成）；后端**能看到 uplink 请求** | 报「等待 WebRTC ICE 收集完成超时」且后端**没有** uplink 请求 → **§6.10** |
| 5 设备出声 | 模拟器侧 | 收到 INVITE 并出声 | 技能 `uvp-gb28181-talk-broadcast-triage` |
| 6 停止/重连 | `DELETE` 会话；对同一会话**再 POST 一次 uplink** | DELETE 停成功；重复 POST = **409** | 409 是**预期行为**（§6.7 约束 1），重连须「删旧建新」 |

- ⛔ **第 2 层的 401/404 是本清单的关键设计**：401 说明路由已注册且鉴权生效，404 说明跑的是旧二进制 —— 不用翻日志就能判。
- ⛔ 第 4 层是**本次改动的主战场**：`uplink` 是新增端点，前 3 层绿也证明不了它；必须真的发一次 offer。
- ⚠️ **跨网段（§6.6）不在本清单内**：`rtc.externIP` 仍为空，同网段验收通过≠跨网段可用；接入方跨网段前先做 §6.6。

#### 首次验收结果（2026-09-17 21:30，后端已重启）

| 层 | 结果 | 证据 |
|---|---|---|
| 0 编译 | ✅ | `go build ./...` 无输出 |
| 1 单测 | ✅ | `go test -p 1` talk / controllers / routes / zlm-node 四包全 `ok` |
| 2 路由在线 | ✅ | `uplink` **404 → 401**；日志出现新路由模板 `route=…/talk-sessions/:sessionId/uplink` |
| 3 会话契约 | ✅ | `POST …/<假ID>/uplink` → **404「对讲会话不存在」**，日志落 `gb28181.talk.uplink_prepare_failed error_code=session_not_found`（新日志入册运行态生效）；`POST …/channel/3538/talk-sessions` → **409「设备或通道离线」**（create 链路通） |
| 4 上行反代 | ✅ | 真机点击对讲：建会话 **200** → `uplink` **201**；随后 ZLM `on_stream_changed regist=true` ×5（rtsp/rtmp/ts/fmp4/hls）→ **流真的进了 ZLM** |
| 4.5 ICE 收集 | ❌ → ✅ **已修** | 21:37 那次报 `等待 WebRTC ICE 收集完成超时`，后端**一条 uplink 请求都没有**。根因：STUN 被下发到一个容器未映射的端口，见 **§6.10** |
| 5 设备出声 | ✅（会话侧） | SIP 报文库确认设备**主动发起广播 INVITE** 并被平台 `200 + ACK` 接受 → 广播会话建立；**是否真出声仍需现场听** |
| 6 停止 / 重连 | ⚠️ **红并已修** | `DELETE` → **504**（`duration_ms=15048`）。根因见 **§6.9**，已修 |

- ⭐ **第 3 层不需要浏览器**：dev 配置 `token.iscache: false` + `server.notcheckuser: [1]` → 从 `sys_user_sessions`
  取一条活 `sid` 自签 HS256 JWT 即可直调受保护接口（签法与注意见技能 `uvp-gb28181-talk-broadcast-triage` §0.8）。
- ⛔ **结论**：平台侧 0–5 层全绿；**第 6 层红是既有缺陷**（§6.9），非本次改动引入。平台重启**不会**让设备自动回在线，
  必须让模拟器/设备重新 register + 心跳。
- ⚠️ 顺带发现（与本次改动无关）：`meta_node` id=4 是**影子节点** —— 与 id=3 同 host+api_port，
  但 `media_server_uuid` 与 ZLM 自报不符 → 探活 `identity_mismatch` 跳过、`recording.catalog.reconcile_failed node_id=4`
  每轮刷 WARN。node 3 顶着同一端点且 `active`，未断服务，但建议清理该行。
- 前端 `iceServers` 在真实跨网段环境下的效果未测 —— ⚠️ 更新（2026-09-17）：当前所有节点 `rtc.externIP` 为空，平台**根本不下发 STUN**（§6.10），所以同网段验的是「纯 host candidate」那条路；跨网段那条（下发 STUN + srflx）**一行都还没跑过**。

#### 第二次验收（2026-09-17 21:28，真机点击对讲）

设备 `37010301021180000007` 于 21:28:22 恢复心跳上线后，页面上点了**一次**对讲，全过程可追：

| 时间 | 事件 | 判定 |
|---|---|---|
| 21:28:40.018 | `POST …/channel/3538/talk-sessions` → **200** | 建会话通 |
| 21:28:40.234 | `POST …/talk-sessions/<id>/uplink` → **201** | **WHIP 反代成功** |
| 21:28:40.33~42 | ZLM `on_stream_changed regist=true` ×5 | **上行流真的进了 ZLM** |
| 21:28:40.416→.545 | 设备 `INVITE` → 平台 `200` → 设备 `ACK` | **设备侧广播会话建立** |
| 21:28:52.383 | ZLM `regist=false` | 前端停了推流 |
| 21:28:52.391 | 平台 `outbound BYE`（CSeq 2） | 拆会话 |
| 21:28:52.402 | 设备 `inbound BYE`（CSeq 2） | **交叉 BYE** |
| 21:28:52.9→21:29:03.9 | 平台 BYE **重传 5 次**，始终没有 200 | 对端不回 |
| 21:29:07.439 | `DELETE` → **504**，`duration_ms=15048` | ❌ 用户可见故障 |

### 6.9 停止返回 504 的根因与修法（2026-09-17）

**一句话**：**交叉 BYE** 让我方拆除事务拿不到 200，而清理的 15s 预算是**共享**的 —— 这一步把它吃光，
后面的 ZLM 释放步骤全被饿死；DELETE 拿到超时错误就回了 504。

**三种 BYE 时序，只有第三种会坏**（`gb_sip_trace_message` 实测）：

| 时序 | 报文 | 结果 |
|---|---|---|
| 设备先 BYE | `inbound BYE` → `outbound 200` | ✅ 我方 cleanup 时 map 已空，`ByeBroadcast` 直接返回 nil |
| 我方先 BYE | `outbound BYE` → `inbound 200` → `inbound BYE` → `outbound 481` | ✅ 正常 |
| **交叉 BYE** | 双方在 **11ms** 内各发一个 BYE（**CSeq 都是 2**），我方**永远收不到 200** | ❌ 重传到超时 |

**⛔ 这是既有缺陷，不是本次改动引入**：同日 **17:31:57** 已出现**完全一样**的报文序列与同一条错误
（`talk source unpublished; Broadcast BYE: context deadline exceeded`）；且 **17:32:13** 那次 `DELETE`
的接入日志就是 `ERROR … duration_ms=15093` —— **当时跑的还是旧二进制**（新代码 21:19 才重启生效）。
对照组：**17:32:25** 那次是「设备先 BYE」，`DELETE` 只花 **121ms** 就 200。

**两个后果**（第二个比第一个更严重）：

1. `DELETE` → **504**，用户以为没停掉会去重试；
2. `stopSendRtp` / `close source` 因为父 ctx 已到期而**全部被跳过** → ZLM 侧发送会话没释放。

**修法三处（已落地）**：

| 位置 | 改动 |
|---|---|
| `talk/cleanup.go` | 新增 `stepBudget()`：`context.WithoutCancel` + 独立超时。SIP BYE **3s**、每个 ZLM 释放步骤 **5s**，任何一步都不许吃掉整个 15s 预算，且父 ctx 到期不再短路后续步骤 |
| `controllers/talk.go`（DELETE） | 停止是**幂等**操作：只要会话确实落到终态，即使某些拆解步骤降级也返回 200（失败细节仍写进 `gb_talk_session.error` + 告警日志） |
| `PlayConsoleLinked.vue` | **停止顺序**：先静音（`track.enabled=false`，RTP 仍送静音，不会触发设备端 BYE）→ 调 DELETE → 最后才停流关 `pc`。原来先 `cleanupTalkLocally()` 才 DELETE，等于自己制造交叉 BYE |

**验证**：新增 `TestCleanupSIPTerminationTimeoutDoesNotStarveMediaRelease`，
把两条注入打回去（BYE 用父预算 + 去掉 `WithoutCancel`）→ 用例变红，且打在
`stopSendRtp 必须拿到未过期的 ctx` 这条断言上（忠实复现「步骤被调用但 ctx 已死 = 等于没释放」）；
撤销注入 → 全绿。

**仍待现场确认**：设备是否**真的出声**（第 5 层只验到「设备侧广播会话建立」）；
以及改完顺序后**停止是否不再 504** —— 需要重启后端再点一次。

---

### 6.10 发布前 ICE 收集超时：STUN 被下发到不可达端口（2026-09-17）

**现象**：页面上点对讲 → 前端报 **`等待 WebRTC ICE 收集完成超时`**。
关键旁证：后端日志里 **`uplink` 请求一条都没有** —— 只有建会话 `200`，5s 后 `DELETE 200`。
也就是说死点在「发 offer 之前」，**跟反代、跟 ZLM、跟设备都还没关系**。

**根因**：平台给浏览器下发了 `iceServers = [{ "urls": ["stun:192.168.10.220:3478"] }]`，
而这一个 UDP 端口**宿主上不可达** —— 浏览器会一直等 STUN 重传，`iceGatheringState` 到不了 `complete`，
前端的 5s 超时把整个发布判死。

| 节点 | 形态 | `rtc.icePort` | STUN 探活 |
|---|---|---|---|
| node 2 `zlm-220` | **docker 容器** `uvp-zlm`（只映射 18080/18443 tcp、18001 udp、40000-40200） | 3478 | ❌ UDP 3s 无响应 / TCP refused |
| node 3 `:21080` | 宿主网络进程 | 21812 | ✅ **1ms** 响应 |

⭐ **两边 ZLM 都真的在听**（容器内 `/proc/net/udp6` 查到 `0D96` = 3478，16 个 socket，与 node 3 的 21812 同一形态）——
**差别只在宿主有没有把端口映射出去**。这也解释了为什么 21:28 那次成功、21:37 这次失败：
**调度到哪个节点是随机的，于是对讲时好时坏** —— 最难受的一类 bug。

⛔ **踩过的坑**：第一次读容器内 `/proc/net/udp`（IPv4 表）没看到 3478，差点误判成「ZLM 没绑」。
**ZLM 的 socket 全在 IPv6 表（`/proc/net/udp6`）里** —— 容器化排查必须两张表都读。

**两条修法（已落地）**

| 层 | 改动 | 为什么 |
|---|---|---|
| 后端 `talk/ice.go` | **只在 `rtc.externIP` 非空时才下发 STUN**；不填 → `iceServers` 为空数组 = 只产 host candidate | STUN 唯一作用是拿 NAT 映射地址（srflx），**只在跨网段有意义**；同网段 host candidate 直连可达，多下发一个不可达的 STUN 是**净损失**。于是 `externIP` 顺理成章兼任「跨网段开关」 |
| 前端 `talkPublisher.ts` | `waitForIceGatheringComplete` 超时**从 reject 改为 `resolve(false)`**，上限 5s → **2s**；调用方只记一条 `console.warn` | 兜底：将来即使有人填了 `externIP` 但端口没放行，也只是**降级为 host-only 发布**，不再整个会话失败。这是「STUN 是优化、不是依赖」的语义修正 |

**验证（双向）**

- 回归：`go build ./...` OK；talk / controllers / routes / zlm-node 四包 `ok`；前端 `talkPublisher` 8 例 + `PlayConsoleLinked` 139 例全绿；gofmt 干净、无注入残留。
- **故障注入 ①（后端）**：把「未声明 `externIP` 也用节点地址下发 STUN」打回去 →
  `TestBuildICEServersReturnsNilWhenNotDeclaredOrUnusable` 的「未声明 externIP」「externIP 只有空白」两子例**变红**，
  报错内容正是生产上那条假地址 `stun:192.168.10.220:3478`。
- **故障注入 ②（前端）**：把 `reject` 打回去 → 「收集超时只降级返回 false，不抛错」**变红**，报
  `promise rejected "Error: 等待 WebRTC ICE 收集完成超时"` —— 与用户看到的报错**一字不差**。

**⚠️ 跨网段的前置条件（仍未做）**：容器化部署要让 STUN 真能用，必须把 `rtc.icePort` / `rtc.iceTcpPort` **映射出来**
（node 2 缺 `-p 3478:3478/udp -p 3478:3478/tcp`；顺带 node 2 的 `rtc.tcpPort` = 8000 **也没映射** = WebRTC over TCP 退路同样不可用）。
本次**没有动容器**：一是要重建容器的窗口，二是同网段已不再依赖 STUN。
**在填 `rtc.externIP` 之前，必须先探一遍 `icePort`**：

```bash
# STUN 探活：收到 0x0101（success response）即活；3s 无响应即死
/Users/menglulu/.workbuddy/binaries/python/versions/3.13.12/bin/python3 -c \
"import socket,os,struct;s=socket.socket(2,2);s.settimeout(3);s.sendto(struct.pack('!HHI',1,0,0x2112A442)+os.urandom(12),('<节点IP>',<icePort>));print(s.recvfrom(2048)[0][:2].hex())"
```

---

## 七、未验证事项

- 跨公网 WS 上行的**端到端延迟实测**（20ms 帧 + TCP 队头阻塞的累积量），建议以 300–400ms 为验收线。
- ~~**目标 ZLM 的版本能力**~~ —— **已探测（2026-09-17，220 节点）**：`rtc.icePort` / `rtc.iceTcpPort` = 3478、`rtc.enableTurn` = 1、`rtc.port_range` = 40101-40200 **齐全** → 内置 STUN/TURN 可用，**不需要外挂 coturn**；`rtc.externIP` 为**空**（唯一缺口）；另有 `http.sslport` = 18443、`rtc.port` = 18001、`rtc.tcpPort` = 8000。⛔ 这只说明**这一台**，其余节点仍需按 §6.6 第 0 步逐台探测。
- **`rtc.externIP` 是否支持热重载**：`setServerConfig` 下发后，WebRTC 模块是否**即时**采用新候选地址，还是必须重启 ZLM。⛔ 判据只能是 **SDP 候选是否变化**，配置值回读不算证据。
- **`rtc.tcpPort`（WebRTC over TCP）在本环境是否真的可用**：ZLM 是否在候选里给出 TCP、浏览器能否建连、`preferred_tcp` 是否必须置 1。
  ⭐ 这条结论直接决定 **「UDP 被封」是否真的必须退到 WS** —— 若可用，二期 WS 的适用面将大幅缩小。
- **公网 WHIP 的 ICE 打通率**：需在真实公网 / 跨网段环境实测（含对称 NAT 场景），确认直连成功率与何时回落到 TURN。当前所有验证都发生在同网段，**跨网段一档完全未测**。
  ⛔ 开测前必须先做两件事：① 探活 `rtc.icePort`（容器部署十有八九没映射，§6.10）；② 确认 `rtc.externIP` 已填 —— 不填平台不下发 `iceServers`。
- **ZLM WHIP 端点是否接受请求头鉴权**（`Authorization: Bearer`）：**已不阻塞** —— 令牌改由后端代持后不再出平台（§6.7）。只有将来要支持第三方**按 WHIP 标准自带 Bearer 头直连 ZLM** 时，才需要实测 ZLM 收不收头。
- 平台作为媒体转发面后的**并发容量**（CPU / 带宽 / GC）尚未压测。
- `mode=talk` 路径目前**无真机验收证据**（现有日志全部为 `broadcast` 会话）。
- ~~WHIP 反代在 `https` + 自签证书下的浏览器行为~~ —— **已由反代消除**：浏览器只看平台自己的证书，不再碰媒体节点的自签证书（§6.7）。已用自签证书上游的自动化用例守住「上游证书校验确实跳过了」。

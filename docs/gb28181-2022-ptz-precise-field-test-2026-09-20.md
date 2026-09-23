# GB/T 28181-2022 PTZ 精准控制（`PTZPreciseCtrl`）真机校验报告

- **日期**：2026-09-20
- **被测对象**：海康 `DS-2DC2C40MY-DE`，设备编码 `37010301021320000002`，平台通道主键 `3748`，
  `effective_version=2022`（设备 SIP 版本报 3.0）
- **触发**：用户反馈「操作精准定位后设备没有反应，怀疑平台信令做得不对」
- **一句话结论**：**平台信令是对的（逐字节合规）；这台设备本身不执行这条命令；真正该改的是平台侧的「不可观测」**

---

## 1. 结论（三层，互相独立）

| # | 结论 | 判据强度 |
|---|---|---|
| ① | **平台出向报文与标准一字不差**，SIP 层设备正常受理（9ms 回 `200 OK`） | 真机 wire 逐字节核对 |
| ② | **该设备不执行 `PTZPreciseCtrl`** —— 是设备能力边界，不是平台 bug | 画面客观对比 + 同族查询被拒 |
| ③ | **平台侧真缺口是「不可观测」**：拿不到执行反馈、也无负向能力证据，UI 却报「已受理」 | 代码 + 标准原文 |

> 用户原假设「我们的信令做得不太对」**不成立**；但「设备没反应」这个现象是**真实且合规的**，
> 平台目前**无法把它和「平台 bug」区分开** —— 这才是要修的地方。

---

## 2. 证据链①：平台报文逐字节合规

触发（真实运行中的后端，8280 / SIP 5062）：

```bash
curl -s -X POST "http://127.0.0.1:8280/api/gb28181/device-mgmt/channel/3748/ptz/precise" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"pan":45,"tilt":5,"zoom":1}'
# → {"code":0,"data":{"operationId":"26428b8c-...","sn":10110,"status":"sent"}}
```

从 `gb_sip_trace_message` 解密出的**实际发出去的帧**：

```xml
<?xml version="1.0" encoding="GB18030"?>
<Control><CmdType>DeviceControl</CmdType><SN>10110</SN><DeviceID>37010301021320000002</DeviceID>
  <PTZPreciseCtrl><Pan>45</Pan><Tilt>5</Tilt><Zoom>1</Zoom></PTZPreciseCtrl></Control>
```

对照标准（OCR 原文，非二手解读）：

| 出处 | 要求 | 我们的帧 |
|---|---|---|
| **A.2.3.1.11** | `PTZPreciseCtrl` 为 `Control` 下的可选元素 | ✅ 位置一致 |
| **A.2.1.11 `PTZPreciseCtrlType`** | 只有 `Pan`(0~360.00) / `Tilt`(-30~90) / `Zoom`(>1.00)，全部 `minOccurs=0` | ✅ 三个字段，无多余元素 |
| **表 1 序号 10** | 「PTZ 精准控制 → 应答命令：**（无）**」 | ✅ 平台 `PrecisePTZ = noBusinessResponse` 正确 |

设备收到后 **9ms 回 `SIP/2.0 200 OK`（Content-Length: 0）** —— 传输层认可，标准也**不要求**业务应答。

---

## 3. 证据链②：设备不执行，且这是它的能力边界

### 3.1 硬证据：设备拒答同族的 `PTZPosition` 查询

平台发 `?refresh=true` 查精准状态：

```xml
<Query><CmdType>PTZPosition</CmdType><SN>10109</SN><DeviceID>37010301021320000002</DeviceID></Query>
```

设备回：

```xml
<Response><CmdType>PTZPosition</CmdType><SN>10109</SN><DeviceID>37010301021320000002</DeviceID>
  <Result>ERROR</Result><Reason>Cann't get ptz position</Reason></Response>
```

`PTZPosition` 与 `PTZPreciseCtrl` 属**同一个 2022 精准定位特性族**（§9.11），设备对前者明确「做不到」。

### 3.2 客观画面判据（含正向对照）

用 ffmpeg 直连 ZLM 的 RTSP 取帧逐帧比对：

| 步骤 | 操作 | 画面 |
|---|---|---|
| A → B | 精准定位 `pan=120 tilt=-30 zoom=4` | **完全不变**（连变倍都没发生） |
| B → C | `PTZCmd left`，速度 120，持续 6s | **明显转动**（天花板视角 ↔ 墙面视角） |

⇒ **链路是通的**（方向控制生效），**只有精准控制命令被忽略**。

### 3.3 原因：机型规格本身

`DS-2DC2C40MY-DE` 是**室内 PT-定焦**球机（公开规格书）：

- 2.8 mm **定焦**镜头 —— **无光学变倍** → `Zoom` 物理上无从执行
- 水平 `0°~350°`、垂直 `0°~100°`、水平速度 ≤20°/s
- 定位是入门商用（小型商业 / 办公室 / 仓库），巡视能力仅「2 条巡航 × 16 预置点」

而标准 **7.3 b)** 把「…看守位控制、**PTZ 精准控制**等」列为「**宜**」（推荐，非「应」）
⇒ 入门机型不做完全合规。

> ⚠️ **该机型不适合当「精准定位 / 扫描」的对标设备**（扫描此前也已实测不生效）。

---

## 4. 证据链③：平台侧的缺口 —— 不可观测

| 缺口 | 现状 | 后果 |
|---|---|---|
| **能力门禁只按版本** | `device_ptz.go:331` 用 `profile.SupportsPrecisePTZ()`，该能力位由 `effective_version` 推导 | 本机 report 3.0 → 判 2022 → 门禁放行、按钮可用、**看不出设备其实不行** |
| **没有负向证据通道** | 设备回 `Result=ERROR / Cann't get ptz position` 只写了 `gb_ptz_state.sourceSn`，`freshness` 仍记 `fresh` | 平台**永远学不会**「这台设备不支持精准定位」（对比 `ResolveHomePositionCapabilities` 有历史证据机制） |
| **UI 文案过强** | 前端无条件弹「精准定位请求已受理」 | 把「**已发出**」说成「**已受理**」，操作员会以为设备动了 |

**建议（待排期）**：

1. 把 `PTZPosition` 应答的 `Result=ERROR` + `Reason` **落库成负向能力证据**，
   在精准定位卡片上区分三态：`未验证 / 设备不支持 / 已下发（未确认）`。
2. 文案改为 **「已下发（设备无应答命令，须以画面为准）」** ——
   与 `FormatSDCard` 已确立的「无应答命令只能叫『已下发』」纪律一致。

---

## 5. 复现配方（下次一条条照跑）

```bash
# 0. 登录（captcha 已关）
TOKEN=$(curl -s --noproxy '*' -X POST http://127.0.0.1:8280/api/login \
  -H 'Content-Type: application/json' -d '{"username":"admin","password":"admin123"}' \
  | python3 -c "import sys,json;print(json.load(sys.stdin)['data']['accessToken'])")
B=http://127.0.0.1:8280/api/gb28181/device-mgmt/channel/3748/ptz   # :id 是 gb_channel 数值主键

# 1. 下发精准定位并拿到 SN
curl -s -X POST "$B/precise" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"pan":45,"tilt":5,"zoom":1}'

# 2. 看 wire（解密真实报文）
PY=/Users/menglulu/.workbuddy/binaries/python/envs/default/bin/python
$PY ~/.workbuddy/skills/uvp-sip-trace-triage/scripts/decrypt_trace.py \
    --grep MESSAGE --since "2026-09-20 16:27:00" --until "2026-09-20 16:28:00" --dump

# 3. 正向对照：方向控制（证明观测管道 + 链路可用）
curl -s -X POST "$B" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"action":"left","speed":120}'; sleep 6
curl -s -X POST "$B" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"action":"stop","speed":120}'
```

### ⛔ 观测设备的三个坑（本次踩到，别再踩）

1. **ZLM `getSnap` 返回缓存帧**：同一 `url` 键在 `expire_sec=0/1` 下仍返回同一张 jpg
   （连 OSD 时钟都不走）——**差点得出「设备没动」的错误结论**。
   改用 **ffmpeg 直连 RTSP 取帧**：
   `/Users/menglulu/.workbuddy/binaries/python/envs/default/lib/python3.13/site-packages/imageio_ffmpeg/binaries/ffmpeg-macos-aarch64-v7.1`
2. **ZLM 无人拉流会回收流**，`streamId` 递增漂移（012→013→…→020），RTSP 抓帧随即
   `Invalid data found`。抓帧前先 `getMediaList` **现取 streamId**，并用后台长持有拉流器保活。
3. ⭐ **先做正向对照**（发一条 `left/right` 确认画面会变）—— 否则「画面没变」什么都证明不了。

---

## 6. 附带发现（与精准定位无关，单列）

- `ptzRequest.Speed` 带 `binding:"required"`（`controllers/device_ptz.go:20`）
  ⇒ `POST …/ptz -d '{"action":"stop"}'` 恒 400「请求体不合法」。
  `stop` 本是 speedless 动作，却被要求凭空带一个非 0 速度；前端目前靠「停止时也带 speed」绕过。

---

## 7. 回填去向

- `docs/gb28181-2022-master-backlog.md` §1.3 与 §5 两行已更新为实测口径；
- 细节与可复用判据进项目记忆 `topics/ptz-linkage.md`；
- 操作配方进技能 `uvp-gb28181-ptz-linkage` §1.5。

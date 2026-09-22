# 看守位「设备回了、平台超时」排查记录（2026-09-22）

> ⛔⛔ **当前状态（2026-09-22 12:30 追加）：本轮的兼容实现已全部回滚，本文档不对应任何在库代码。**
> 用户拍板「不兼容这种设备了」，故第四节 A 的 `CmdType` 归一、第六节的分派层落点、以及连带的
> 空 `DeviceID` 关联放宽 与 `trace` 归类归一**全部已删除**；`handler/message.go`、`ptz/handler.go`、
> `manscdp/ptz_precise.go`、`manscdp/storage_card.go`、`trace/business.go`、
> `internal/loggingcatalog/registry.json` 均已还原到改动前状态（新增文件已删除）。
> ⇒ **平台对该设备的行为回到「按规范 CmdType 精确匹配」**：设备把应答写成 `HomePosition` 时
> 仍会在报文分派层被丢弃、重试后落 `APPLICATION_TIMEOUT`，前端显示「查询设备超时，请重试」。
> 这是**已知且被接受**的现状，不是回归缺陷。
> 本文件保留的是**归因过程与证据**；第四节 A 方案、第六节落点结论**不再对应实现**。
> 第四节 B（补 `Result` 检查）与 C（观测口径）**未实施**，是否要做另行决策。
>
> 触发：现场反馈「不是国标规范实现的，抓包看到设备回复了信令，平台仍显示请求超时」。
> 取证源：`uvp_gb28181.gb_sip_trace_message`（AES 解密原文）+ `gb_ptz_operation`；标准原文为
> GB/T 28181-2022 附录 A（OCR）+ GB/T 28181-2016 正文/附录 A（文本层）。

## 一、结论摘要

平台的**报文形态**是照 2022 抄的，逐字段可对回标准原文，**不存在「自创协议」**。
但**关联与解析这两层有两处真缺陷**，其中第 1 条正是「设备明明回了、平台报超时」的直接原因。

| # | 现象 | 根因 | 位置 |
|---|---|---|---|
| **A** | 设备回复 `CmdType=HomePosition`，平台按「无关报文」丢弃 → 走满重试 → `APPLICATION_TIMEOUT` → 前端「查询设备超时，请重试」 | 平台对 `CmdType` **逐字符精确匹配** `HomePositionQuery` | `ptz/handler.go:186`（SQL `cmd_type = ?`）<br>`manscdp/ptz_precise.go:437`（解析硬校验） |
| **B** | 设备回 `Result=ERROR`（明确失败），平台记为 `accepted / device_result=OK` | 应答解析结构体**没有 `Result` 字段**，失败信号被整体丢弃 | `manscdp/ptz_precise.go:384-390` |

## 二、实测证据

### 2.1 复现：2026-09-22 10:11「设备回了、平台超时」

通道 `34020000001310000001`（设备 `34020000001320000009`，库内登记 Dahua DH-3H3405-ADG-L，
User-Agent `SIP UAS V.2016.xxxx`）。平台连发 3 次查询（SN 10323），三次全部 `APPLICATION_TIMEOUT`。

**平台发出**（`gb_sip_trace_message` event_id `9c913eff`，10:11:11.479，CSeq 145）：

```xml
<?xml version="1.0" encoding="GB18030"?>
<Query><CmdType>HomePositionQuery</CmdType><SN>10323</SN><DeviceID>34020000001310000001</DeviceID></Query>
```

**设备回复**（event_id `43d5f673`，10:11:11.580，CSeq 476）：

```xml
<?xml version="1.0" encoding="GB2312" standalone="yes" ?>
<Response><CmdType>HomePosition</CmdType><SN>10323</SN><DeviceID>34020000001310000001</DeviceID>
<Result>OK</Result><HomePosition><Enabled>0</Enabled><ResetTime>600</ResetTime><PresetIndex>0</PresetIndex></HomePosition></Response>
```

**SN、DeviceID 完全一致，只有 `CmdType` 少了 `Query` 后缀。** 平台因此关联不到 operation：
`findPTZMessageOperation` 查出 `no_candidate` → `logUnmatchedPTZResponse` 丢弃 → 操作保持 `sent`
→ 重试 3 次后 `APPLICATION_TIMEOUT` → 前端 `homeNoticeText` 命中含 `TIMEOUT` 分支，
显示「查询设备超时，请重试」。

设备侧三帧应答（CSeq 476/477/478）逐字相同，SN 均为 10323，即**每次都答了，平台每次都丢掉**。

### 2.2 顺带发现：设备明确报错被读成成功

同一台海康 `37010301021320000002`（Hikvision DS-2DC2C40MY-DE，09-19）：

```xml
<Response><CmdType>HomePositionQuery</CmdType><SN>857</SN><DeviceID>37010301021320000002</DeviceID>
<Result>ERROR</Result><Reason>Cann't get ptz position</Reason></Response>
```

落库结果（`gb_ptz_operation` id 1386 / 1398）：

```
status=accepted  device_result=OK  response_has_data=0
```

设备说的是「取不到云台位置」，平台记的是「OK」。因为应答解析结构体里没有 `Result`：

```go
type homePositionResponseXML struct {      // manscdp/ptz_precise.go:384
    XMLName      xml.Name               `xml:"Response"`
    CmdType      string                 `xml:"CmdType"`
    SN           int                    `xml:"SN"`
    DeviceID     string                 `xml:"DeviceID"`
    HomePosition *homePositionConfigXML `xml:"HomePosition"`
    // ← 没有 Result，也没有 Reason
}
```

`<HomePosition>` 缺失时 `hasData=false`，于是走 `PTZOperationAccepted`。对照同族的
`DeviceControl` 应答（`ptz/handler.go:366` 会检查 `DeviceControlResultError`）——
**看守位查询这条支路漏了同一个检查**。

## 三、与国标原文的对照（回答「是不是不合规」）

| 环节 | 平台行为 | 标准依据 | 判定 |
|---|---|---|---|
| 控制下发 | `<Control><CmdType>DeviceControl</CmdType><SN/><DeviceID/><HomePosition><Enabled/><ResetTime/><PresetIndex/></HomePosition></Control>` | 2022 A.2.3.1.10「看守位控制命令」 | ✅ 一致 |
| 查询下发 | `<Query><CmdType>HomePositionQuery</CmdType><SN/><DeviceID/></Query>` | 2022 A.2.4.10「看守位信息查询请求」 | ✅ 一致 |
| 应答解析 | 期望 `<Response><CmdType>HomePositionQuery</CmdType>…<HomePosition><Enabled/>…` | 2022 A.2.6.12「看守位信息查询应答」 | ✅ 一致 |
| 是否要求应答 | 控制 `ResponseRequired=true` | 2022 **9.3.1 e)**「…看守位控制…目标设备**应**发送应答命令」 | ✅ 一致 |

即：**「平台要求设备回答」是标准明文要求的，不是平台加戏**；2016 版 9.3.1 同样把「看守位控制」
列入「有应答」名单（与云台/远程启动/强制关键帧/拉框这类「无应答」区分）。

**设备侧的实际情况**：
- 「设备回了 200 OK」只证明 SIP 层收到了 MESSAGE —— 9.3.1 e) 要的是**应用层 MANSCDP 应答**，两者不是一回事。
- 大华这台是 2016 设备（`SIP UAS V.2016.xxxx`），2022 新增的
  `HomePositionQuery` / `CruiseTrackListQuery` / `CruiseTrackQuery` **三种查询全部超时**，
  而 2016 就有的 `PresetQuery` / `DeviceStatus` / `PTZPosition` / `ConfigDownload` 全部 `accepted`。
  规律清晰：**它没实现 2022 新查询，收到后用非标 `CmdType` 作答**。
- 海康那台则是「识别但拒绝」：查询与控制的应答都带 `Result=ERROR`。

## 四、修复方案（按最小改动、不动既有口径）

### A. `CmdType` 归一（建议做，收益最大）

`HomePosition` 在标准里**不是命令类型**，它只是 `DeviceControl` 的一个子元素名；把它当
`CmdType` 回上来是设备自创的。归一方向上不存在歧义，可安全收敛：

1. `manscdp` 增加一个窄口径归一函数（只在 `Response` 根 + `CmdType=HomePosition` 时生效）：

```go
// normalizeResponseCmdType 把设备自创的看守位应答 CmdType 归一到 2022 标准值。
// 只认 Response 根：Control/Query 根的 HomePosition 绝不在此改动。
func NormalizeResponseCmdType(root, cmdType string) string {
    if root == "Response" && cmdType == "HomePosition" {
        return CmdHomePositionQuery
    }
    return cmdType
}
```

2. `ptz/handler.go` 的 `OnPTZMessage` 入口（`ParseHead` 之后、`findPTZMessageOperation` 之前）
   用归一后的值参与关联与分派；**operation 落库仍用标准值**，保持一致。
3. `manscdp/ptz_precise.go:437` 的硬校验同步接受归一值。

> ⚠️ **本节的落点已被第六节修正**：按上面第 2 点只改 `OnPTZMessage` **不生效** ——
> 报文在更上游的 `handler/message.go` 报文分派 switch 就被丢掉了。实际实现见第六节。

### B. 补 `Result` 检查（建议做，属于漏判）

在 `homePositionResponseXML` 补 `Result` / `Reason` 字段；
`applyHomePositionQueryResponse` 在 `Result=ERROR` 时走
`applyRejectedPTZResponse(..., ptzErrorDeviceRejected, "设备返回 Result=ERROR")`，
与 `DeviceControl` 支路口径对齐（拒绝时保留设备给的 `Reason` 作为 `error_message`）。

### C. 观测口径（可选，但建议）

- 未关联应答目前是 INFO 级 `ptz.response.unmatched`，本机日志配置下**看不到**；
  建议对「同 SN + 同 DeviceID 但 CmdType 仅差设备自创别名」的单列一条 WARN 或单独 event，
  否则这类问题全靠翻 trace 表才能发现。
- 前端把 `APPLICATION_TIMEOUT` 一律说成「查询设备超时」会掩盖责任方。
  建议区分「设备未回应答」（已收到 200 OK）与「信令未送达」，前者文案应引导「设备可能不支持该查询」。

## 五、待确认

1. 现场所谓「海康」具体是哪台／什么型号。库内唯一海康 `37010301021320000002`
   （DS-2DC2C40MY-DE）**没有** timeout 记录（只有 `rejected` 与 `accepted`），
   且已于 09-21 19:06 后离线；今天 10:11 复现「回了却超时」的是大华那台。
2. 兼容范围：是否要顺带兼容 2022 另两种非标应答
   （`CruiseTrackListQuery` / `CruiseTrackQuery` 同类问题，大华同样超时）——
   建议先只修 `HomePosition`，另两种等现场确认设备行为后再定。

---

## 六、第二轮复测：修法放错层，白改一轮（2026-09-22 11:00）

按第四节 A 方案改完、单测全绿之后，现场复测**依旧失败**：前端提示从
「查询设备超时，请重试」变成「暂时无法确认设备状态，请重试」，设备侧照常毫秒级应答。

### 6.1 再次取证：设备答了，报文也没丢，丢在平台自己的分派层

| 时间 | 方向 | CSeq | 报文 | 结果 |
|---|---|---|---|---|
| 10:53:34.283 | 出 | 5 | `<Query><CmdType>HomePositionQuery</CmdType><SN>10332</SN><DeviceID>34020000001310000001</DeviceID></Query>` | — |
| 10:53:34.368 | 入 | 538 | `<Response><CmdType>HomePosition</CmdType><SN>10332</SN><DeviceID>34020000001310000001</DeviceID><Result>OK</Result><HomePosition><Enabled>0</Enabled><ResetTime>600</ResetTime><PresetIndex>0</PresetIndex></HomePosition></Response>` | 84ms 就回了 |
| 10:53:34 ~ 10:53:49 | — | — | 平台重试 3 次 | `gb_ptz_operation` op 2028 = `APPLICATION_TIMEOUT` |

SN、DeviceID 与请求一字不差。设备没问题，第一轮的报文判断也没错 —— **错的是修法落点**。

### 6.2 真正根因：报文分派 switch 在服务层之前

`server/app/gb28181/handler/message.go` 的 `MessageHandler.Handle` 里有一处
**报文分派 `switch head.CmdType`**（`case` 列表含 `manscdp.CmdHomePositionQuery`）。
设备回的 `HomePosition` 匹配不上任何 case ⇒ **整条报文在这里就被丢掉**，
根本执行不到 `ptzProcessor.OnPTZMessage`。于是第一轮放在 `OnPTZMessage` 里的归一
**永远不会被执行**。

### 6.3 为什么第一轮没发现：中间指标骗人

第一轮同时在 `trace/business.go` 的 `classifyMANSCDP` 里加了归一 —— 那是**独立路径**，
它生效了，报文追溯表的 `business_code` 从 `unknown` 变成了 `ptz`，看上去"分类正常了"。
但 `gb_ptz_operation` 依旧 `APPLICATION_TIMEOUT`。

> ⭐ **教训：判「生效」必须看端到端业务结果（operation 终态），不能只看中间指标。**

### 6.4 修正后的落点

| 位置 | 作用 | 说明 |
|---|---|---|
| `handler/message.go` `Handle`（`ParseHead` 之后、`switch head.CmdType` 之前） | **归一生效点** | `head.CmdType = manscdp.NormalizeResponseCmdType(head.CmdType)` + 一条 INFO（`ptz.response.cmd_type_alias`）。归一后 metrics、业务码归类、下游 switch、关联 SQL、各家解析器**一起对齐** |
| `ptz/handler.go` `OnPTZMessage` | 幂等兜底 | `OnPTZMessage` 是公开 API，测试/新入口可能绕过分派层；⛔ 兜底**不打日志**（同一 event 名两层两个级别会被 `event_level_divergence` 拦下） |
| `manscdp/ptz_precise.go` | 解析层收别名 | 用 `IsResponseCmdType`，且返回值归成规范值 |
| `trace/business.go` | 追溯表归类 | 保留；但注意它**通了不代表业务链路通** |

### 6.5 测试：起点必须选对层

第一轮的"端到端"只到 `OnPTZMessage`，那已经是分派 switch 的**下游** ——
分派层丢报文时它照样全绿。新增 `handler/message_cmdtype_compat_test.go`，
起点是 `MessageHandler.Handle`：

```go
handler := NewMessageHandler(gbconfig.Config{})
handler.SetPTZProcessor(recorder)          // 假 processor，记录收到的 body
handler.Handle(req, tx)                    // req：prepareNotifyRequest + body 为抓包原文
require.Len(t, recorder.bodies, 1)         // 收到 = 没被 switch 丢掉
require.Contains(t, string(recorder.bodies[0]), "<CmdType>HomePosition</CmdType>")  // 原文不被改写
```

**验收这条测试合格的办法**：临时把归一那行改成恒 false，它必须变红
（实测 `bodies` 由 1 变 0，报 `"[]" should have 1 item(s)`）。不红说明起点选错了层。

另含反向用例：`HomePositions` / `HomePositionQueryResponse` / `home_position` /
`HomePositionReq` 这些"看起来像"的取值**必须仍被丢掉** —— 白名单不是启发式。

### 6.6 交付状态

- `go build ./...` OK；`handler` / `ptz` / `manscdp` / `trace` 四个包测试全绿。
- 日志门禁：本次新增行 **0 findings**。剩余红灯为 **HEAD 中存量违规**
  （`handler/message.go` 的 `gb28181.message.snapshot_finished_failed`、
  `ptz/device_config_read.go`、`controllers/device_snapshot.go`，
  这三个文件工作区均干净）+ `controllers/play.go` 中他人未提交的 220 行新增。
- ⚠️ **需要重启后端才能生效**（现场用 VSCode dlv 启动的调试进程仍是旧二进制）。

> ⛔ **后续（12:30）**：本节交付的改动已**全部回滚**，上述构建/测试绿灯状态不再代表当前工作区。
> 回滚后的复验：`go build ./...` OK，`manscdp` / `trace` / `ptz` / `handler` 四包测试全绿
> （与本节相同的四个包，用于证明回到改动前状态是干净的）；
> `internal/loggingcatalog` 仍红，但那几条是 **HEAD 存量违规 + 他人未提交改动**，与本节无关。

---

## 七、第三轮：「配置并启用」不生效 —— 这次是设备侧（2026-09-22 11:25）

查询修好后现场复测：查询正常了，但**配置并启用依旧失败**。本轮把「到底是平台没发对，
还是设备不执行」这一层彻底钉死。

### 7.1 现象与首次定性

`gb_ptz_operation` 里 `home_position` 只有两条，均 `timeout / APPLICATION_TIMEOUT`：

| id | cmd_type | action | sn | status | 时间 |
|---|---|---|---|---|---|
| 2030 | DeviceControl | home_position | 10334 | timeout | 11:15:54 → 11:16:09 |
| 2031 | DeviceControl | home_position | 10335 | timeout | 11:16:17 → 11:16:33 |

解密下发报文，**平台发的是标准形式**（2016 A.2.3.1.10 与 2022 A.2.3.1.10 同构）：

```xml
<Control><CmdType>DeviceControl</CmdType><SN>10334</SN><DeviceID>34020000001310000001</DeviceID>
<HomePosition><Enabled>1</Enabled><ResetTime>600</ResetTime><PresetIndex>1</PresetIndex></HomePosition></Control>
```

设备侧只回了 **SIP 200 OK（Content-Length: 0）**，**没有任何业务应答**。

### 7.2 标准怎么说

GB/T 28181—2022 §9.3.1 e) 原文：

> 源设备向目标设备发送**录像控制、报警布防/撤防、报警复位、看守位控制、软件升级、设备配置**
> 命令后，目标设备**应发送应答命令表示执行的结果**，命令流程见 9.3.2.2；

2016 版 §9.3.1 表述一致。**所以「看守位控制要回应答」是标准明文要求，平台要求应答没有过分。**

平台侧 `responseRequired=1`、`MaxAttempts=1`，行为正确。

### 7.3 关键：设备到底执行了没有

「设备没回应答」有两种可能：**执行了但不回**（不合规但可容忍）／**压根没执行**。
必须实测设备侧状态，不能靠推断。

用平台自己的查询路径（`GET .../ptz/home-position?refresh=true`）在每次下发后复查设备返回的
`<HomePosition>`，结果：

- 11:19:42 查询 → `<Enabled>0</Enabled><ResetTime>600</ResetTime><PresetIndex>0</PresetIndex>`
- 11:22:55 / 11:23:24 / 11:23:58 / 11:24:51 复查 → **始终 `Enabled=0`、`PresetIndex=0`**

**结论：设备既不应答、也不执行。** 平台报 `APPLICATION_TIMEOUT` 是**准确**的，
「没生效」是真实状态，不是平台误报。

### 7.4 排除「报文形式不对」：7 种形式逐条实测

为排除「平台报文形式与设备期望不符」，直接向设备（192.168.10.94:5060）手工发 MANSCDP
控制帧逐条试探（发完立即用平台查询复查设备状态）：

| 变体 | 报文形式 | 设备应答 | 是否执行 |
|---|---|---|---|
| v0 | 标准 `DeviceControl`+`HomePosition`，DeviceID=通道，`Enabled=1` | 仅 SIP 200 | ❌ |
| v4 | 同上，但 **DeviceID=设备编码**（`...1320000009`） | 仅 SIP 200 | ❌ |
| v5 | **`CmdType=HomePosition`**（按设备自己的写法），`Enabled=1` | 仅 SIP 200 | ❌ |
| v6 | 标准形式 + **`<Info><ControlPriority>5</ControlPriority></Info>`**，`Enabled=1` | 仅 SIP 200 | ❌ |
| v1 | 标准形式，`Enabled=0` | 仅 SIP 200 | — |
| v2 | 标准 + ControlPriority，`Enabled=0` | 仅 SIP 200 | — |
| v3 | `CmdType=HomePosition`，`Enabled=0` | 仅 SIP 200 | — |

含平台自己在下发的 2 次，**共 9 次、7 种形式，设备一律只回 SIP 200 OK，一律不执行**。
`PresetIndex` 参数也排除了：设备侧确有待用预置位（`PresetQuery` 返回预置位 1/2/3），
平台下发的 `1` 是有效编号。

### 7.5 三条旁证：为什么可以断定「设备不实现该命令」

1. **设备认得 `HomePosition` 这个概念**：它对 `HomePositionQuery` 能正常应答并**正确报告**
   `Enabled/ResetTime/PresetIndex` —— 即它实现了「读」，没实现「写」。
2. **设备会回这类业务应答**：同族、同样「控制类有应答」的 `DeviceConfig` 在它上面有
   `accepted` 记录（SN 10309 / 10311）。同一批里 10306–10308 是 timeout —— 即设备对它
   **不实现的子命令静默丢弃**，这与 `home_position` 的表现完全一致。不是「所有控制都不回」。
3. **发送路径无差异**：平台发的 `HomePositionQuery`（同源、同端口、同编码）设备每次都回，
   所以不是「设备不认平台来源」。

> ⭐ **判据沉淀**：`回业务应答` 与 `执行` 是两件事；`不回` 也要分「设备不认识 / 设备实现不全 /
> 平台认不出」。三者分别对应「兼容」「无解，只能如实标注」「修平台」，混在一起会一直改错地方。

### 7.6 平台侧现状评估

- **判定诚实**：`APPLICATION_TIMEOUT` + 设备真实状态 `Enabled=0` 一致，没有伪造成功。
- **但 UI 表达不足**：前端 `homeNoticeText` 把这类失败落成
  「暂时无法确认设备状态，请重试」（`PlayConsoleLinked.vue:1550`），**暗示「再试一次也许就行」**，
  而真实原因（设备不实现该命令）没有任何出口。用户只能反复无效重试。
- **平台其实已经为此设计好了**：`homeControlSupport.status === "unsupported"` 时，
  `PtzHomeCard` **不渲染**「配置并启用」按钮、状态行显示「设备不支持看守位」
  （`PlayConsoleLinked.vue:1445/1535` + 文案表 `unsupported`）。
  **缺的只是「如何判定 unsupported」这一环。**

### 7.7 建议改法（待决策）

**能力判定补一条「行为证据」分支**：`ResolveHomePositionCapabilities` 现在只有
`历史 accepted → supported` 一条证据分支（`ptz/home_position.go:285`），对称地补：

```
从未有过 accepted 的 home_position 控制
且 存在 ≥2 次 APPLICATION_TIMEOUT 失败
  ⇒ control = unsupported（reason 写清「设备对看守位控制命令无响应，已尝试 N 次」）
```

要点：
- **可恢复**：`accepted` 分支优先级更高，设备一旦成功一次即自动翻回 `supported`。
- **只影响表达，不影响发送**：不改 `responseRequired`、不伪造状态；
  仅仅让 UI 明说「设备不支持」，不再诱导无效重试。
- ⛔ **不要做的**：把「设备不回」降级成 `sent`（已下发）—— 那等于把「未知」写成功，
  与第二轮 `business_code` 那个教训同类。

**设备侧**：建议在设备 Web 界面确认看守位是否可配（能配说明是国标命令实现缺失）、
向大华确认该型号固件版本对 A.2.3.1.10 的支持，或升级固件。

### 7.8 本轮附带产出

- 手工发 SIP 探测 + 平台 API 复查的**闭环验证法**（不改平台代码即可判定「设备执行没执行」），
  脚本已固化到技能 `uvp-gb28181-ptz-linkage` 的 `scripts/`。
- 本次全部探测帧**未在平台留下噪音**：设备对探测帧连业务应答都没有，SIP 200 只回到探测端口。

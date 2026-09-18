# 「视频参数属性」平台面板设计 —— 播放控制台「视频参数」tab

> ⚠️ **2026-09-18 位置变更**：本面板原先落在右栏「高级」tab 的第 4 张卡，
> 客户反馈「混到高级里边不太好」后**已拆成独立标签**，并把三行对照下移到播放器下方详情条。
> 当前形态见 §十一-B；本文中「第 4 张卡」「右栏高级 tab 内」等表述是实施时的历史设计记录，保留供追溯。

> 日期：2026-09-18
> 对象：A-5 `VideoParamAttribute`（标准 A.2.1.13 / A.2.3.2.5 / A.2.4.7 / A.2.6.8 / A.2.6.9）
> 关联：`docs/gb28181-2022-master-backlog.md` §1.4 · `docs/gb28181-2022-device-config-ambiguity.md` · `docs/gb28181-2022-gap-inventory.md`
> 引用页码一律用**标准印刷页码**（PDF 页 = 标准页 + 7，2022 版）

---

## 〇、结论

1. **落点**：播放控制台右栏「高级」tab 第 4 张卡（`PlayConsoleLinked.vue:4806` 的 `snapshot-config` 之后），命名「**视频参数**」（跟探针卡里已有的「分辨率 / 帧率 / 编码格式」用词对齐，不叫「视频专业参数」）。
   ⚠️ 该落点在 2026-09-18 已变更为**独立标签**「视频参数」（`linked-tab-videoparam`），见 §十一-B。
2. **它不是单层表单**：标准是 `Item` 数组（每码流一条，`maxOccurs="unbounded"`、`minOccurs="0"`），面板必须按 `StreamNumber` 分段。
3. **下拉不能自拟**：`VideoFormat` / `Resolution` / `FrameRate` / `BitRateType` / `VideoBitRate` 五个字段的取值唯一出处是**附录 G 的 SDP `f` 字段码表**（标准页 130）。见 §二 —— 这是本卡最容易做错的地方。
4. **开工前提**：平台侧配置族协议层**目前是零**（`server/` 里搜不到任何 `DeviceConfig` / `ConfigDownload` 构造器）。必须先补 §六 的协议层，否则做出来就是左栏那句既有设计声明明令禁止的「伪控制滑杆」。
5. ⚠️ **`VideoParamAttribute` 是 2022 新增，2016 设备不认识它**（2016 只有 4 个配置类型）。但**协议骨架向后兼容**，所以这不是"能不能发"的问题，而是"发出去怎么判定结果"的问题 —— 结论是**不加 profile 门禁、靠强制回读对账暴露真相**。详见 **§十**，实施前必读。

---

## 一、协议骨架（标准原文）

### 写入 = `Control` / `DeviceConfig`（A.2.3.2.5，标准页 87）

```xml
<Control>
  <CmdType>DeviceConfig</CmdType>
  <SN>1</SN>
  <DeviceID>34020000001320000001</DeviceID>
  <VideoParamAttribute Num="2">
    <Item>
      <StreamNumber>0</StreamNumber>
      <VideoFormat>2</VideoFormat>
      <Resolution>5</Resolution>
      <FrameRate>25</FrameRate>
      <BitRateType>2</BitRateType>
      <VideoBitRate>2000</VideoBitRate>
    </Item>
    <Item>
      <!-- 子码流 1 -->
    </Item>
  </VideoParamAttribute>
</Control>
```

- `Num` 是 **`videoParamAttributeCfgType` 自己的属性**（标准页 64：`<attribute name="Num" type="integer"/>` 挂在外层 `complexType` 上），**不是元素**，也不是 `SumNum`。
- `Item` 内：`StreamNumber` / `VideoFormat` / `Resolution` / `FrameRate` / `BitRateType` **必选**；`VideoBitRate` **条件必选**（`minOccurs="0"`，注释「固定码率时必选」）。
- `Item` 本身 `minOccurs="0"` ⇒ **空配置合法**（`Num="0"` 且无 `Item`）。

### 写入应答（A.2.6.8，标准页 93）—— **没有回显**

```xml
<Response>
  <CmdType>DeviceConfig</CmdType>
  <SN>1</SN>
  <DeviceID>34020000001320000001</DeviceID>
  <Result>OK</Result>
</Response>
```

⛔ 这就是本卡**必须自己做「回读对账」**的根本原因：`Result=OK` 只说明「收到并接受」，既不代表值生效，也**不回显你到底发了什么**。

### 读取 = `Query` / `ConfigDownload`（A.2.4.7，标准页 83）

```xml
<Query>
  <CmdType>ConfigDownload</CmdType>
  <SN>2</SN>
  <DeviceID>34020000001320000001</DeviceID>
  <ConfigType>VideoParamAttribute</ConfigType>
</Query>
```

- `ConfigType` 支持多类型，以 `/` 分隔。
- ⭐ 原文明文：「**可返回与查询 SN 值相同的多个响应，每个响应对应一个配置类型**」⇒ 平台侧接收**必须按 SN 收集多条**，不能假定「一问一答」。

### 读取应答（A.2.6.9，标准页 94-95）

`Result` 必选，随后各配置元素**均 `minOccurs=0`**（含 `VideoParamAttribute`）。

---

## 二、★ 取值唯一出处：附录 G 的 SDP `f` 字段（标准页 130）

原文格式：

```
f=v/编码格式/分辨率/帧率/码率类型/码率大小 a/编码格式/码率大小/采样率
```

视频五个参数（各以 `/` 分割）：

| `f` 位置 | 标准元素 | 取值 | 含义 |
| --- | --- | --- | --- |
| 编码格式 | `VideoFormat` | `1` | MPEG-4 |
| | | `2` | H.264 |
| | | `3` | SVAC |
| | | `4` | 3GP |
| | | `5` | H.265 |
| 分辨率 | `Resolution` | `1` | QCIF |
| | | `2` | CIF |
| | | `3` | 4CIF |
| | | `4` | D1 |
| | | `5` | 720P |
| | | `6` | 1080P |
| | | 其余 | `WxH`（W 表示宽，H 表示高） |
| 帧率 | `FrameRate` | `0` ~ `99` | 十进制整数 |
| 码率类型 | `BitRateType` | `1` | 固定码率（CBR） |
| | | `2` | 可变码率（VBR） |
| 码率大小 | `VideoBitRate` | `0` ~ `100000` | **单位 kb/s**（1 表示 1 kb/s） |

三条落地提醒：

- ⛔ 这五个字段的 XSD 类型都是 `string`，所以**发的是数字字符串**（`<VideoFormat>2</VideoFormat>`），**不是 `H.264` / `720P`**。写成人读串 = 对端解析失败或当 0 处理。
- ⛔ `VideoBitRate` 单位是 **kb/s**，不是 Mbps。UI 若按 Mbps 展示，必须 ×1000 之后再发，否则差 1000 倍。
- ⚠️ 分辨率第 6 项在 OCR 里读到的是 `1080P/1`（`.workbuddy/ocr/full2022.txt:6610`），判断为**跨行 OCR 噪声**（同段 `720P;6` 落在上一行行尾）。落地前请肉眼核一次标准页 130。

---

## 三、字段表（面板每一格的定义）

| # | 显示名 | 标准元素 | 标准类型 | 必选性 | 取值出处 | 控件 | 缺省 | 备注 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 0 | 码流 | `StreamNumber` | integer | 必选 | 标准明文：0=主码流；1=子码流1；2=子码流2… | 分段标题（非输入框） | 由 `StreamNumberList` 生成（⚠️ **当前未落库**，见 §四①） | §四① |
| 1 | 编码格式 | `VideoFormat` | string | 必选 | **附录 G** `1`~`5` | 单选 | 回读值；无则 H.264(`2`) | |
| 2 | 分辨率 | `Resolution` | string | 必选 | **附录 G** `1`~`6`，或 `WxH` | 单选 + 「自定义 `WxH`」项 | 回读值；无则 720P(`5`) | §四② |
| 3 | 帧率 | `FrameRate` | string | 必选 | **附录 G** `0`~`99` | 数字输入 | 回读值；无则 25 | §四③ |
| 4 | 码率类型 | `BitRateType` | string | 必选 | **附录 G** `1`/`2` | 单选（CBR / VBR） | 回读值；无则 VBR(`2`) | §四④ |
| 5 | 码率 | `VideoBitRate` | string | **条件必选**（`BitRateType="1"` 时必选） | **附录 G** `0`~`100000` | 数字输入，**单位 kb/s** | 回读值 | VBR 时禁用并折叠 |

---

## 四、四条硬约束

### ① 码流分段，段数由「目录」决定（不是由 `VideoParamOpt` 决定）—— 但数据源目前**没落库**

`Item` 的 `maxOccurs="unbounded"`。「设备有几条码流」的唯一出处是**目录项类型 `itemType` 的可选子元素 `Info`（摄像机属性组）里的 `StreamNumberList`**（A.2.1.9，标准页 66-67；模拟器对应 `SimConfig.device.streamNumberList`，取值形如 `"0"` / `"0/1"`，注释标了「§9.3.1，2022 新增」）。

⛔ **不要拿 `VideoParamOpt` 当码流列表来源** —— 它没有这个字段（见 ③）。

⚠️ **但两个来源的现状不对称**（B-2 那单的刻意决定造成的）：

| 需要的值 | manscdp 解析层 | 落库 | HTTP 读得到？ |
| --- | --- | --- | --- |
| 设备支持的分辨率 `Resolution` | ✅ `manscdp/catalog.go:59` | ✅ 落 `gb_channel.resolution`（B-2 的十列之一） | ✅ |
| 支持的码流编号 `StreamNumberList` | ✅ `manscdp/catalog.go:66,131` | ⛔ **刻意不落库**（B-2 判定它属「点播取流能力」，不在「通道属性展示」范畴） | ❌ |

⇒ 两个选项，**得挑一个并在实施单里写明理由**：

- **(a) 把 `stream_number_list` 补落 `gb_channel` + 加进 `ChannelVO`**。理由是它现在有第二个消费者了（本面板的分段数），B-2 那句「不属于通道属性展示范畴」的判定**前提已变**；代价是一次迁移三方言 + 前端 DTO 补字段。
- **(b) 不落库，面板退化为「主码流 + 手动加一段」**。零改动，但用户要手动知道「这台设备有几条码流」—— 与「不能凭空编值」的整卡口径冲突。

推荐 **(a)**，并在迁移里按 B-2 的既有做法追加在列末尾（守卫式迁移无法重排已存在列）。

### ② 分辨率：码值 或 `WxH`，二选一

附录 G 允许「其余分辨率用 `WxH` 表示」⇒ 下拉必须有「自定义」项，命中附表六个码值就走码值。
⚠️ 别和 A-6 遮挡区坐标的**像素基准**混在一起：遮挡坐标是播放窗口像素（ambiguity 文档 B-2），两者互不相关。

### ③ 帧率没有范围可用 —— A-9 只能约束分辨率这一格

`videoParamOptCfgType`（A.2.1.20，标准页 69）**只有两个字段**：`DownloadSpeed`（下载倍速）+ `Resolution`（支持的分辨率，多值 `/` 分隔）。**没有帧率范围、没有码率范围、没有编码格式范围**。

⇒ A-9 的「用回读范围约束 A-5 表单」**只能约束分辨率**；帧率与码率只能靠「附录 G 的取值范围 + 回读当前值」。这个缺口要按 0-2 / 0-3 契约写进对接文档。

### ④ CBR / VBR 与码率的联动

`VideoBitRate` 注释是「（**固定码率时必选**）」⇒

- 选 `BitRateType="2"`(VBR)：该格禁用，**报文里不输出 `<VideoBitRate>`**。
- 选 `BitRateType="1"`(CBR)：变为必填，校验范围 `0`~`100000`。
- 解析回读值时：`VideoBitRate` **缺席 ≠ 0**，必须区分「因为 VBR 所以没给」与「给了个 0」。

---

## 五、交互态

### 状态机

| 状态 | 触发 | 界面 |
| --- | --- | --- |
| `idle` 未读 | 打开控制台 / 切通道 | 表单**禁用**，只亮「读取设备参数」；正文「尚未读取设备视频参数」 |
| `reading` | 点读取 | 按钮转圈；表单保持禁用 |
| `loaded` | 读回成功 | 表单启用；每格右侧带**来源徽标**：`设备`（回读命中）/ `缺省`（设备没给，用的默认值） |
| `dirty` | 任一格被改 | 「下发」按钮亮；旁侧出现「已改 N 项」与「还原」 |
| `applying` | 点下发 | 按钮转圈，表单冻结 |
| `acked_ok` | 收到 `Result=OK` | 状态行「设备已接受（SN=…）」——**不作为终态停留** |
| `verifying` | ack 之后自动 | 自动发起一次 `ConfigDownload`（**不复用** `Result`） |
| `verified` | 回读值与下发值逐格相同 | 绿；写库快照 |
| `mismatch` | 回读值 ≠ 下发值 | **黄**，逐格标出差在哪；文案「设备已接受命令，但值未生效」 |
| `failed` | `Result=ERROR` / SIP 超时 / 无应答 | 红，保留表单内容可重试 |

### 三条设计理由（写进代码注释，避免被"顺手简化"）

1. **必须有 `idle` 禁用态。** `DeviceConfig` 的应答没有回显，`Result=OK` 什么也不说明。先把回读值拿到手再让用户改；否则用户是在一片空白上猜数字。
2. **`acked_ok` 不许当终态。** `Result=OK` 的语义只是「收到并接受」。终态必须是 `verified` / `mismatch`，靠回读判定。⇒ 这正是左栏那句「本面板不提供未接入的伪控制滑杆」的可执行版本。
3. **`mismatch` 不是失败。** 典型来源就是模拟器/手机的摄像头能力边界（下发 1080P、实际 720P）。用黄色 + 逐格差异，**不要用红色报错** —— 设备没做错。

### 对照区（同卡内第二块）—— 把实测值引过来

复用探针 tab 已经在显示的实测值：`streamInfo.videoCodec` / `streamInfo.resolution` / `streamInfo.videoFps`（`PlayConsoleLinked.vue:4660-4664`）+ `liveMetrics.bitrate`。

三行呈现：**下发值 / 回读值 / 实测值**。

⇒ 主线单 A-5 的验收闭环原文就是「平台改分辨率 → **拉流实测分辨率变化（探针/ffprobe）**」。「改在哪、验在哪」必须同屏可达，否则改完还要切到别的模块找验证点，是割裂。

---

## 六、后端要加什么

照 `manscdp/storage_card.go` + `ptz/storage_card.go` + `controllers/device_storage_card.go` 三件套的同构写法。

### 1. `server/app/gb28181/manscdp/video_param.go`

- `BuildVideoParamAttributeConfig(deviceID string, sn int, items []VideoParamItem) ([]byte, error)` —— 拼 §一 的 `Control`/`DeviceConfig`
- `ParseVideoParamAttribute(xml string) ([]VideoParamItem, error)` —— 解析回读块
  - ⛔ **保持码值入库**，人读串只在前端做。别在解析层就把 `"5"` 转成 `"720P"`，否则对账时比的是两套表示。
- `BuildConfigDownloadQuery(deviceID string, sn int, configTypes []string) ([]byte, error)`
- `ParseConfigDownloadResponse(xml, wantType string) (*ConfigDownloadResult, error)`
  - `wantType` 不存在时返回 **nil + 明确 error**，不要静默回空。对应 ambiguity 文档 B-4（「不支持」与「配置为空」在协议上不可区分）—— 我们可以自己定口径，但**不能不定**。

### 2. `server/app/gb28181/ptz/video_param.go`

- `ReadVideoParams(ctx, target, actorID, deptID, idemKey) (Operation, error)` —— 发 `ConfigDownload`，等 SN 应答，落库
- `ApplyVideoParams(ctx, target, items, actorID, deptID, idemKey) (Operation, error)` —— 发 `DeviceConfig`，等 `Result`，**然后自动触发一次 Read 做对账**
- 复用 `scheduler.go` / `query.go` 既有的「等 SN 应答 + operation 终态」机制

### 3. 表 `gb_device_video_param`

`id`(UUID) + `device_id` + `target_code` + `stream_number`，唯一键 **`(device_id, target_code, stream_number)`**；列 `video_format` / `resolution` / `frame_rate` / `bit_rate_type` / `video_bit_rate`（**全存标准码值的字符串**）+ `observed_at` + `source_sn`。

迁移三方言 + down 齐全（契约见技能 `uvp-sqlite-migration-authoring` / `uvp-multi-dialect-baseline`）。

### 4. 控制器 + 路由

- `server/app/gb28181/controllers/device_video_param.go`
- `GET  /api/gb28181/device-mgmt/channel/:id/video-params`（`?refresh=true` 发起读取）
- `POST /api/gb28181/device-mgmt/channel/:id/video-params`（下发；带 `Idempotency-Key`）
- 路由加在 `routes/routes.go:861`（`storage-cards` 之后）

---

## 七、前端要改什么

1. **`web/src/api/gb28181.ts`**：`VideoParam` / `VideoParamResult` interface + `getChannelVideoParams` / `applyChannelVideoParams`（照 `getChannelStorageCards` 的 `BaseResult` + `silentRequestConfig` 形态）。
2. **`PlayConsoleLinked.vue`**：
   - 右栏 `linked-side-advanced`（`data-testid="linked-side-advanced"`，:4745）内、`snapshot-config`（:4806）之后插第 4 张卡
   - state：`videoParams` / `videoParamsLoaded` / `videoParamsPending` / `videoParamsError` / `videoParamDirty` / `videoParamVerify`
   - 轮询复用存储卡那套 `scheduleStorageCardPoll` 范式（`refreshOperationId` → `getPtzOperation` 到终态 → 重读）
   - **纯函数抽出来做单测**：码值 ↔ 人读串互转、`WxH` 合法性、帧率 0-99、码率 0-100000、CBR/VBR 时 `VideoBitRate` 的必填性
   - CSS / `data-testid` 命名沿用既有 `storage-card-*` / `snapshot-config` 一套
3. **权限**：读接口绑菜单（`type=2`）、写接口绑按钮（`type=3`）；按 path/component 定位菜单，**不按 `menu_id`**（跨环境会漂）。

---

## 八、验收

| # | 步骤 | 期望 |
| --- | --- | --- |
| 1 | 打开控制台 → **视频参数** tab | 显示「尚未读取设备视频参数」，表单禁用；下方详情条显示「还没有回读值…」（2026-09-18 起本面板为独立标签，见 §十一-B） |
| 2 | 点「读取设备参数」 | 设备回读值填入；来源徽标全为「设备」 |
| 3 | 改分辨率为 1080P，下发 | `Result=OK` → 自动回读 → 三行对照里「实测值」随之变化 |
| 4 | 帧率填 120 | 前端拦住（>99），**不发报文** |
| 5 | 码率类型选 VBR | 码率格禁用，报文里**不含** `<VideoBitRate>` |
| 6 | 切到别的通道再切回 | 不串数据（照存储卡的 `channelContextKey` 代际校验） |
| 7 | 设备离线 | 读取/下发按钮禁用，文案说明原因 |
| 8 | 连点两次「读取」 | 不叠加两个 operation、按钮不卡在转圈 |
| **9** | **设备 `protocolOverride` 改成 `2016`** → 读取 → 下发 | ⭐ **按钮仍然可用**（不设门禁）；文案出现版本提示「平台按 2016 版处理…」；下发后回读为空 → 结论落在「设备未返回此配置类型」（`type_absent`）；空表单占位**不许**说成「尚未读取」（那是操作问题） |
| **10** | **同设备改回 `2022`** → 同样操作 | 回读正常 → 面板显示实际值 ⇒ 证明判据是**回读结果**，不是 `profile`（被误登记的设备不会因为登记值而失去功能） |

### 验收怎么跑（当前可执行的范围）

第 1–10 条里，"面板行为"这一层已经可以在**组件级**跑完（不需要真机）：

```bash
cd web
npx vitest run src/views/gb28181/videoParamCodec.test.ts src/views/gb28181/components/PlayConsoleLinked.test.ts
```

第 9/10 条对应的正是 `生效版本 2016 只加版本提示，不动按钮也不改判据` 与 `渲染视频参数回读事实`
两个用例。⚠️ 第 9 条里「改 `protocolOverride`」那一步是**真机/模拟器侧的动作**，
组件级用 `registeredVersion: "2016"` 的应答模拟；端到端仍需接一台设备（或模拟器）走一遍。

✅ **前置条件已就绪（2026-09-18）**：模拟器侧原先**根本没有** `VideoParamAttribute` 的读写分支
（详见 §九-6），现已补齐并打包。⇒ 端到端**不必再等真 2016 设备**：
把模拟器设置页的「国标版本」切到 **2016 档**、重注册，即可复现第 9 条的整个形态
（面板按钮仍可用 → 下发 → 回读为空 → 文案落「设备未返回此配置类型」）；
切回 **2022 档**再走一遍，同一台设备回读正常。**判据是回读结果，不是档位** —— 这正是第 10 条要证的。

⚠️ 第 3 条的「实测值随之变化」依赖 `streamInfo`（ZLM 探针）——那一层不在本卡的组件测试范围内。

---

## 九、顺带核出的问题（**与本面板无关，单列**）

1. ✅ **已修（2026-09-18）**：模拟器 `ConfigDownload` 回的分辨率不合标准。
   原实现 `ConfigDownloadResponse.kt:88` 输出 `<Resolution>${v.resolution.label}</Resolution>`
   （即 `"1920×1080"`），附录 G 要求码值；而 SDP 侧用的却是正确码值 ⇒ 同一台设备两条出口不一致。
   **修法（抽单一真源，不是就地把 label 换成数字）**：
   - `VideoResolution` 新增 `gb28181Code: Int`（`shared/.../config/SimConfig.kt`），承载附录 G 码表；
   - `SipHeaderHelpers.buildSdpMediaSpec` 的 `when` 改读 `v.resolution.gb28181Code`（消除重复，行为不变）；
   - `ConfigDownloadResponse.buildVideoParamOpt` 改发**档位全集**码值 `4/5/6`。
   ⭐ 为什么报全集而不是当前值：`VideoParamOpt.Resolution` 的语义是「摄像机**支持**的分辨率，
   多值 `/` 分隔」，当前生效值属 A-5 的范畴。只报一个值会让 A-9「用回读范围约束表单」形同虚设。
   守卫测试：`ConfigDownloadResponseTest` 加两条 —— 一条禁人读串（`×`）进报文，
   一条断言回读码值与 `VideoResolution.entries` 逐字相同（防两条出口再次分家）。
2. `shared/.../config/SimConfig.kt` 的 `CatalogNode.resolution: String = "1280*720"` 用 `*` 分隔，
   附录 G 写的是 `WxH`（`x`）⇒ 目录 `Info` 里的分辨率串（`CatalogNotifyBuilder.kt:440`）
   最好一并归一。⚠️ 它与第 1 条**是同类缺陷的另一个出口**，但涉及 `CatalogNode.fields`
   这个用户可配面的语义变更，**单独决策，本轮未动**。
3. `shared/.../sip/Sdp.kt:130` 注释写「GB28181 § C.2」—— 2022 版里 SDP 定义是**附录 G**（2016 版才是附录 C / 附录 F）。注释陈旧，改到 A-5 时顺手更正。
4. `AndroidCameraStreamer.kt:70-77` 那段「OSD 仅作用于直播推流」的注释同样是陈旧的（实际已是三消费者模型），已在上一轮记录。
5. ⚠️ **`SD_480P -> 4` 的语义疑点（本轮新发现）**：附录 G 里 `4` = **D1**（704×576），
   而 `VideoResolution.SD_480P` 是 640×480。严格说 640×480 该用 `WxH` 形式。
   此处**保持既有 SDP 行为不变**（动它会牵拉流协商，超本轮范围），只在
   `VideoResolution.gb28181Code` 的注释里标注。**要不要归一到 `WxH` 需单独决策。**
6. ✅ **已补（2026-09-18）**：模拟器侧**原先完全没有** `VideoParamAttribute` 的读写分支，
   而这是本卡端到端验收的前置条件。核对时确认：`ConfigDownloadResponse.build()` 只处理
   `BasicParam` / `VideoParamOpt`，其余 `ConfigType` **静默跳过但仍回 `<Result>OK</Result>`**；
   `DeviceControlDispatcher.dispatch()` 里设备配置只有 `<BasicParam>` 一个分支。
   ⇒ 不补的话「下发 → 回读对账」永远跑不通，且失败形态是**假阴性**
   （平台会看到「设备未返回此配置类型」，把"模拟器没实现"误报成"设备不支持"）。
   **补法**（模拟器仓 `~/code/uvp/uvp-gb28181-sim`，新增 `gb28181/VideoParamAttribute.kt`）：
   - **读**：`ConfigDownloadResponse.build()` 加**必传** `gbVersion`（有效版本，附录 I 协商结果），
     仅 `V2022` 才输出该块 —— 有效版本是 2016 时整块不回、仍回 `OK`，
     **正好就是 §八 第 9 条要复现的形态**，端到端不再需要真 2016 设备。
   - **写**：`DeviceControlDispatcher` 加 `<VideoParamAttribute` 分支 →
     `SystemHandler.handleVideoParamAttribute` → 落 `DeviceControlModel.videoParams`
     （并进 `DeviceStateStore` 存档，跨 App 重启保留）。
   - **默认值**：平台从未下发过时回读**出厂默认**（从 `SimConfig.video` 派生，与设备实际出流同源），
     所以回读**永远有值** —— 「没人配过」绝不能被回成空（那会被判成 `type_absent`）。
   - ⛔ **版本门禁只关"设备主动声明"那一半**：平台**下发**侧没有任何版本判断，
     与 §十③ 的平台侧口径互为对照。
   - ⛔ **口径边界（刻意为之）**：模拟器只把这份配置**记账**，**不真去改手机摄像头的编码参数**；
     手机实际出流仍由 `VideoProfile` + CameraX 决定。所以「下发 1080P → 回读 1080P」证明的是
     **平台侧那条链通了**，不是画面真的换了分辨率。真 IPC 上两件事是同一件。

---

## 九-A、模拟器侧新增的落点（供端到端验收时对照）

| 关注点 | 文件 | 说明 |
| --- | --- | --- |
| 取值表 / 出厂默认 / 线格式 | `shared/.../gb28181/VideoParamAttribute.kt`（新） | `Num` 是**属性**；VBR 时 `VideoBitRate` **整个元素缺席** |
| 状态字段 + 写入口 | `shared/.../domain/DeviceControlModel.kt` | `videoParams: Map<Int, VideoParamState>` + `withVideoParamConfig`；**空 Item 不动状态** |
| 写入落地 | `shared/.../domain/devicecontrol/SystemHandler.kt` | `handleVideoParamAttribute`，带一条**插真值**的日志 |
| 分发 | `shared/.../domain/DeviceControlDispatcher.kt` | 按**块名**分流（`<BasicParam>` / `<VideoParamAttribute>` 同属 `DeviceConfig`） |
| 回读出口 | `shared/.../gb28181/ConfigDownloadResponse.kt` | 必传 `gbVersion`；`emitsVideoParamAttribute` 是版本规则的单一出处 |
| 调用与日志 | `shared/.../coord/manscdp/CatalogSubRouter.kt` | 日志里插「按有效版本 X 不回 / 回了 N 路码流」的真值 |
| 跨重启保留 | `shared/.../app/DeviceStateStore.kt` | `videoParams` 进存档（`hasContent` / `summary` 同步） |
| **写入应答出口** | `shared/.../coord/manscdp/DeviceControlSubRouter.kt` | `handleDeviceConfig` + `sendDeviceConfigResponse`；`DeviceID` 必须回**请求里的那个值**，未识别的配置块回 `ERROR`（见文末 §十一-A ②③） |

⚠️ 一处**刻意没做**的能力约束：设备在 `VideoParamOpt` 里声明支持的分辨率是档位集 `4/5/6`，
但写入侧**不做 clamp** —— 下发任何合法码值/`WxH` 都原样生效、原样回读。
好处是端到端验收时「回读 == 下发」就是**链路通了**的干净证据；
代价是模拟器**演不出 `mismatch` 形态**（设备已接受但值未生效）。要演它得另外给设备加能力约束。

---

## 十、对 2016 设备的行为（**实施前必读**）

### ① 事实：2016 设备不认识 `VideoParamAttribute`，但**报文发得出去**

2016 的 `ConfigType` 只有 4 个取值（`BasicParam` / `VideoParamOpt` / `SVACEncodeConfig` /
`SVACDecodeConfig`），**不含 `VideoParamAttribute`**。清单对照与骨架逐字段核对见
`docs/gb28181-2022-device-config-ambiguity.md` §六 / §六-A。

关键是**协议骨架向后兼容**：2022 是在同一层 `sequence` 里追加元素，2016 已有的三个原封不动。
⇒ 平台发的报文，2016 设备**一定能解析**；它只是**不认识那个配置类型**。
所以这不是"兼容不兼容"，而是"**发出去之后怎么判定结果**"。

### ② ⛔ 既有「查询不设门禁」的口径**不能照搬到这里**

`protocol.Capabilities` 里既有两族门禁（`HomePositionQuery` / `CruiseTrackQuery`）**都是查询**，
口径原文见 `ptz/storage_card.go:22`：

> profile 只是"登记的说法"，不是事实；一台被登记成 2016、实际按 2022 应答的设备，
> **发出这一帧是平台唯一能发现它的手段**。

**查询**发出去没有副作用 —— 最坏是超时 + 烧一个 SN + 一次重试预算，而且它**确实能探明真相**。
**写入**（本卡的 `DeviceConfig`）不是：它**有副作用**，且发给 2016 设备时**标准未定义行为**：

| 设备解析风格 | 设备回应 | 危害 |
| --- | --- | --- |
| 严格：不认识的元素直接报错 | `Result=ERROR` | ✅ 安全 |
| 宽松：忽略未知元素 | `Result=OK` 但**什么都没改** | ⛔ **假成功**（面板显示"已下发"，设备其实没动） |
| 畸形匹配：把新元素误当旧元素解析 | 可能**部分应用** | ⛔ 危险（改坏一半，且无法从应答看出） |

⇒ **写入的风险等级与查询不同，判定手段也必须不同。**

### ③ ✅ 解法：**不加 profile 门禁**，靠「强制回读对账」暴露真相

本卡的核心设计（§五 状态机）本来就是「下发 → **自动回读** → 逐格对账，`Result=OK` 不算终态」。
**这条链路恰好就是"设备到底认不认这个配置类型"的可靠判据** —— 比 `profile` 可靠得多：
`profile` 只是登记值，回读是设备的**实际回答**。

| 场景 | 做法 |
| --- | --- |
| **操作员手点「下发」** | **不挡**。理由同"查询不设门禁"：被误登记成 2016 的真 2022 设备，不试一次就永远用不了这功能。但 UI 要**提示**（见 ④）。 |
| **平台自动下发** | **挡**。加 `Capabilities.VideoParamAttribute`，照 `SupportsCruiseTrackQuery` 的范式，**只用于"平台自己决定要发"的场合**。 |
| **判定"设备不支持"** | **以回读结果为准，不以 profile 为准**（见 ④）。 |

⭐ 这条与本仓 `Capabilities` 的既有注释一致：
「device-reported capability hints **must never become a sending gate for standard controls**」。

### ④ 四态文案（与存储卡面板同源的分层思路）

| 设备实际回答 | 面板文案 | 是否等于"设备不支持" |
| --- | --- | --- |
| `Result=OK` + 带 `VideoParamAttribute` 元素 | 正常显示；值若与下发不一致 → 走 §五 对账逻辑 | 否 |
| `Result=OK` + **无** `VideoParamAttribute` 元素 | 「设备未返回此配置类型（登记版本 2016 / 厂商未实现）」 | ✅ **是**，最可靠的判据 |
| `Result=ERROR` | 「设备拒绝此配置类型」 | ✅ 是 |
| 超时无应答 | 「设备未响应（登记版本 2016，标准未定义该类型的应答行为）」 | ⚠️ 疑似 |

⛔ **`profile` 只用来选提示措辞，不用来拦按钮** ——
登记成 2022 的设备也可能不支持（厂商没实现），登记成 2016 的设备也可能支持（厂商提前实现）。

### ⑤ 副产品：2016 设备上这个面板**不是全空**

`VideoParamOpt`（A-9）**2016 就有** ⇒ 对 2016 设备，面板可**降级为只读视图**：
显示设备支持的分辨率范围，但不下发写入、也不显示帧率/码率范围
（2016 的 `VideoParamOpt` 同样只有 `DownloadSpeed` + `Resolution` 两个字段 —— 见 §四③）。
这比"把按钮整个藏掉"好：用户至少看到设备能力，也理解了为什么改不了。

---

## 十一、开工顺序

0. ✅ **§四① 已定：(a)** —— 补落 `stream_number_list` 列 + 加进 `ChannelVO`（`ChannelVO` 现在有了第二个消费者）。
1. ✅ **已完成（2026-09-18）** 修 §九-1（模拟器 `ConfigDownloadResponse` 输出码值）
   —— 抽 `VideoResolution.gb28181Code` 作单一真源，SDP 侧与配置回读侧统一。
   验证：`:shared:compileKotlinJvm` + `compileTestKotlinJvm` BUILD SUCCESSFUL；
   `ConfigDownloadResponseTest` 9/9、`SipHeaderHelpersTest` 29/29、
   `SipComplianceTest` 16/16、`SdpBuilderCapabilityTest` 3/3（后三组证明 SDP 行为未变）。
2. ✅ **已完成（2026-09-18）** 后端协议层（§六 1、2）+ Go 单测
   —— `manscdp/video_param.go`（构建/解析/`ValidateVideoParamItems`/`ConfigErrorTypeAbsent`）、
   `ptz/video_param.go`（读写命令 + 自动对账子 operation + 逐格 diff + 四态推导纯函数）。
3. ✅ **已完成（2026-09-18）** 迁移（§六 3 + `stream_number_list` 列）—— 三方言 up/down 共 6 个文件；
   开发库已实际执行并**重跑第二遍验证幂等**；三个全量快照已同步（见下方 ⚠️）。
4. ✅ **已完成（2026-09-18）** 控制器 + 路由（§六 4）—— `GET/POST /channel/:id/video-params`，
   读绑 `gb28181:ptz:view`、写绑 `gb28181:ptz:control`（同路径分方法，写**不**被读权限覆盖）。
5. ✅ **已完成（2026-09-18）** `protocol.Capabilities` 加 `VideoParamAttribute`（照 `SupportsCruiseTrackQuery` 范式）
   —— **只给"平台自动下发"用**，不挡操作员手点（§十③）。控制器侧**没有**任何版本门禁。
6. ✅ **已完成（2026-09-18）** 前端卡片 + 纯函数单测（§七）
   —— `web/src/views/gb28181/videoParamCodec.ts`（四态文案/码值映射/逐格校验/码流解析全是纯函数）
   + `PlayConsoleLinked.vue` 第 4 张卡（含对照区三行）+ 组件级用例。
7. ✅ **已完成（2026-09-18）** 验收补两条 2016 场景（§八 第 9/10 行）
   —— 为此把设备的**生效版本**（`gb_device.effective_version`）随读接口透出
   （`registeredVersion`），**只用来选提示措辞**：
   「设备未返回此配置类型（平台按 2016 版处理，标准未定义该类型的应答行为）」。
   ⛔ 措辞刻意说"平台按 2016 版处理"而不是"设备声明了 2016" —— 该列有 `default:2016`，
   设备从未声明时也会是 2016，说成"设备声明"会在排障时把人带偏。
8. ✅ **已完成（2026-09-18）** 模拟器侧补齐 `VideoParamAttribute` 读写（本节第 7 步的**前置条件**）
   —— 协议层/状态/分发/回读出口/存档六处，见 §九-6 与 §九-A。
   门禁：`:shared:jvmTest` 相关 5 个类 **14 + 18 + 60 + 11 + 9 全绿**；
   iOS 门禁 `:shared:compileKotlinIosSimulatorArm64` + `:androidApp:assembleDebug` BUILD SUCCESSFUL；
   变异自证：把分发匹配改成 `contains("<VideoParamAttribute>")`（少一个 `>`）→ 3 条新用例**变红**。
   ⚠️ 仍未做的是**装机**：当前 `adb devices` 里没有设备，且 A-5 的面板侧还要连平台后端一起验。
8. ✅ **已完成（2026-09-18）** 回写主线单 `docs/gb28181-2022-master-backlog.md`：
   §1.4 的 A-5 行改为 ✅ 并补「码流分段 / 条件必选 / 附录 G 码值 / 对账闭环 / 2016 不适用」五处；
   §4 的 A-5 条目同样补齐五条要点，**并纠正 §4 的 A-9 条目**（原写「用回读范围约束 A-5 表单」
   —— `VideoParamOpt` 只有 `DownloadSpeed` + `Resolution`，帧率/码率没有范围可用，
   A-5 的合法性判据一律取附录 G，A-9 降级为「展示设备支持档位」的可选只读信息）；
   更正 `docs/gb28181-2022-gap-inventory.md`：§1.1 表内 A-5 行改 ✅、**钉死「2016」列的判定依据**
   （8 个新增类型含 `VideoRecordPlan` / `VideoAlarmRecord` 全部是 2022 新增，**不是「待查」**），
   §2.1 补一行运行时链路证据，并在「平台侧整体结论」后加 09-18 补记（该结论对 A-5 已失效）。

### ⚠️ 实施中发现并纠正的两条惯例（写给下一个人）

1. **迁移的可观测效果必须同步进三个全量快照。**
   `app/gb28181/migration/runner.go` 的基线语义是：**版本表为空 + 基线探测表
   （`gb_sip_trace_session_diagnosis`）已存在 ⇒ 整批迁移被直接标记为"已应用"而一条都不执行**。
   ⇒ 快照里没有的物件，在**用快照新建出来的库**上永远不会出现，增量迁移补不回来。
   所以本次除 6 个迁移文件外，`uvp-gb28181.sql` / `postgresql_converted.sql` /
   `sqlserver_converted.sql` 都以 `feature:start/end` 块**追加在文件末尾**，
   另加了一个不变量测试 `TestVideoParamPresentInEverySnapshot` 钉住它。
   （上一轮从存储卡那批推出的"新表只走增量迁移"结论由此被**证伪**：
   存储卡、首页仪表盘等 8 个表属漂移遗漏。）
2. **手抄的应答子集类型会静默落后。** 前端 `applyVideoParamResult` 原先手写了
   `VideoParamResult` 的一个子集，本次后端新增 `registeredVersion` 时它没有跟着动，
  只有 `vue-tsc` 报错才暴露出来。已改为直接引用接口导出的 `VideoParamResult`。

---

## 十一-A、端到端实测发现并修掉的三个模拟器侧缺陷（2026-09-18）

这三个都是**平台侧代码没问题、模拟器侧对不上**造成的，而且三个的表象都是「平台看不出异常」。
按发现顺序：

### ① `DeviceConfig` 不在 `DeviceControlSubRouter.ACCEPTED` 里 —— 整类配置下发被静默吞掉

`DeviceConfig`（A.2.3.2 设备配置）与 `DeviceControl`（A.2.3.1 控制）是**两个不同的 `CmdType` 字符串**，
而 `ACCEPTED` 集合里原先只有后者 ⇒ `ManscdpDispatcher.route` 的
`firstOrNull { it.accepts(cmd) } ?: return false` 直接把它挡在门口。

- **现象**：设备侧只有一条
  `未识别 MANSCDP cmdType=DeviceConfig from=sip:…，已回 200 但不会处理`
  —— **200 是回了的**，所以平台侧完全看不出异常；设备侧也只是一条 Warning。
- **连带**：`SystemHandler.handleDeviceConfig`（处理 `<BasicParam>` 的那条路径）一直是**死代码**，
  此前从未被真实报文触发过。
- **修法**：`ACCEPTED` 补 `"DeviceConfig"`，`handle` 加对应分支。
  凡「平台下发了、设备侧毫无反应」的配置类命令，第一个要查的就是这个集合。

### ② 配置类应答被 `recordCmd ?: return` 吞掉 —— 平台 operation 超时且**不会回读**

`DeviceControlSubRouter.handleDeviceControl` 在拼应答之前有一句：

```kotlin
val recordCmd = ManscdpParser.recordCmd(xml) ?: return   // ← 只找 <RecordCmd>
```

配置类报文里**没有** `<RecordCmd>` 元素 ⇒ 直接返回，**一个字节都不回**。
PTZ 类只被要求回 SIP 200 OK，所以这个洞一直没暴露；而 `DeviceConfig` 不一样：

- 平台侧 `applyDeviceConfigResponse` 明确等着解析 `<CmdType>DeviceConfig</CmdType>` 的应答；
- 收不到 ⇒ 该 operation 置 `timeout`，**并且不会创建回读对账子 operation**
  （`createVideoParamReconcile` 只在 ack 成功路径里被调用）。

⇒ 现象是「设备侧日志明明记了值、平台上永远是旧值」，**两侧都不报错**。
平台还会因为等不到应答而**重发** —— 同一条下发的日志连着出现三次，是很强的指路信号。

- **修法**：`DeviceConfig` 不再复用 `handleDeviceControl`，改走独立的
  `handleDeviceConfig` + `sendDeviceConfigResponse`：
  `<Response><CmdType>DeviceConfig</CmdType><SN>…</SN><DeviceID>…</DeviceID><Result>OK</Result></Response>`。
  `Result` 取 `DeviceControlAck.needSipResponse` —— `dispatch` 落到 `else` 分支时为 `false`，
  即「这个配置块我不认识」；此时回 `ERROR` 让平台**显式失败**，比回 `OK` 再等一个永远
  `type_absent` 的回读好定位得多。

### ③ 应答的 `DeviceID` 回了本机设备编码，而不是请求里的那个 —— 应答被平台丢弃

平台对**读**（`ConfigDownload`）和**写**（`DeviceConfig`）的应答用同一个口径校验：

```go
manscdp.ConfigDownloadExpectation{SN: operation.SN, DeviceID: operationTargetCode(operation)}
```

`operationTargetCode` 在**通道作用域**下是**通道编码**，而平台下发查询时 `DeviceID` 填的也正是通道编码。
模拟器原先一律回本机设备编码 ⇒ 校验不匹配 ⇒ **整条应答被判为不属于本次操作而丢弃**，
而两边的日志、`malformed=0`、`sip_status=200` 全都是「正常」。

- **读侧现象**：`reconcile.state` 卡在 `pending`、`gb_device_video_param` 恒空，
  但 SIP trace 里明明有设备的应答报文（要解密才看得到 `DeviceID` 的差异）。
- **写侧现象**：与 ② 叠加，表现为 operation `timeout`。
- **修法**：两处都回**请求里的 `DeviceID`**
  —— `ManscdpParser.deviceId(xml) ?: ctx.config.device.deviceId`。
  `CatalogSubRouter` 的 Catalog 分支早有这个写法，ConfigDownload 分支只是漏了。
  ⇒ **协议报文里「回填请求值」的元素，一律不要用本机配置去拼。**

### ✅ 本轮端到端验收结论（真机 + 平台接口全通）

证据链（`channel id=3539` / 通道编码 `34020000001320000010`，设备 `37010301021180000007`）：

| 环节 | 修复前 | 修复后 |
| --- | --- | --- |
| 读 `refresh_video_params` | sn=770 `timeout` | sn=771 `accepted`；库落 `0/2/5/25/1/2000` |
| 写 `apply_video_params` | sn=772、773 **`timeout`** | **sn=774 `accepted`**（`device_result=OK`） |
| 自动回读（写派生） | 从未被创建 | **sn=775 `accepted`**，`response_has_data=1`，`derivedFromApply=true` |
| `gb_device_video_param` | 恒为旧值 `5/25/1/2000` | **`6/30/2/NULL`**（`source_sn=775`） |
| 接口 `reconcile` | `never_read` / `pending` | **`read_ok`** + `freshness=fresh` |

`resolution` 下发 `6`、`frameRate` 下发 `30`、`bitRateType` 下发 `2`，回读**逐格相等**；
`video_bit_rate` 回读为 **NULL** 而不是 `0` —— 这正是 §三 里
「VBR 时 `VideoBitRate` **整个元素缺席**」那条约定在端到端链路上的体现。

**回读值 == 下发值**，即 §九-A 所说「链路通了」的干净证据。

---

## 十一-B、面板位置变更：从「高级」第 4 张卡 → 独立「视频参数」标签（2026-09-18）

客户反馈「视频参数还是做成一个单独标签，现在混到高级里边不太好」。确认后的形态：

- 右栏标签栏新增第 4 个标签「**视频参数**」（`data-testid="linked-tab-videoparam"`），
  与云台控制 / 视频探针 / 高级并列。原「高级」栏回到 3 张卡（设备控制事实 / 存储卡 / 图像抓拍）。
- 右侧面板 `linked-side-videoparam` 只留**编辑表单 + 状态文案 + 动作**（原卡内容）。
- **「下发 / 回读 / 实测」三行对照搬到播放器下方的详情条** `linked-detail-videoparam`。
  理由：那里横向更宽更好读，且与"改在哪、验在哪"的大区语义一致；留在编辑表单旁边，
  会被误读成"我刚改的值"（「回读」行取的是**设备事实**，不是草稿）。
- ⭐ **可见性门禁顺带修正**：原先这张卡挂在 `canAdvancedPanel`（`canControlDevice || canSnapshot`）下，
  而它的加载链路（`loadPanelData` → `loadVideoParams`）用的门禁是 `gb28181:ptz:view`，
  与后端读写路由（`ptz:view` / `ptz:control`）一致。改用 `canViewPtz` 后语义才对得上，
  否则会出现"有读权限却看不见"或"看得见但读不出"的错配。
- ⛔ 副作用：权限为「有 `device:control` 但无 `ptz:view`」的账号从此**看不到**这个面板（此前能看）。
  这是修正而非回归 —— 那种账号本来也读不出数据。

**实现落点**（`web/src/views/gb28181/components/PlayConsoleLinked.vue`，七处一处不能漏）：

| # | 落点 | 内容 |
| --- | --- | --- |
| 1 | `type TabKey` | 加 `"videoparam"`（⛔ 必须**全小写**，见下） |
| 2 | `tabs` 数组 | `{ key: "videoparam", label: "视频参数", icon: Video, description: "A.2.1.13 读取 / 下发" }` |
| 3 | `visibleTabs` | `tab.key === "videoparam" && canViewPtz.value` |
| 4 | 侧栏 `:class` | 加 `'sidebar-videoparam'` 联动（配套 `.sidebar-videoparam .panels` 去外层大卡片） |
| 5 | 详情区 | 新增 `linked-detail-videoparam`（三行对照，`.vpc-grid` / `.vpc-row`） |
| 6 | 侧栏 | 新增 `linked-side-videoparam` 装原卡；原卡内删掉对照区 |
| 7 | CSS | `.linked-detail > .linked-videoparam-layout` 进 flex 列表；旧 `.video-param-compare*` 换成 `.vpc-*` |

⛔ **踩到的坑**：`data-testid` 是 `` `linked-tab-${t.key}` ``，第一版 key 写成驼峰 `"videoParam"`
⇒ 生成 `linked-tab-videoParam`，而测试查全小写 ⇒ 7 个用例同时报 `Unable to get
[data-testid='linked-tab-videoparam']`，报错里只有一大坨 DOM，看不出是大小写问题。
**本仓 tab key 一律全小写。**

**测试**：5 个视频参数用例的点 tab 行改为 `linked-tab-videoparam`（其余 23 个点「高级」的用例保留）；
「侧栏与详情条按 tab 分工」用例补新 tab 一段（含"原「高级」栏不再含视频参数"）；
新增「三行对照渲染在播放器下方详情条」用例；游客用例补新 tab 缺失 + `getChannelVideoParams` 未被调用。


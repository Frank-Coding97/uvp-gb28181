# GB/T 28181-2022「设备配置」家族 —— 标准条款清晰度审查

> 审查日期：2026-09-17
> 审查对象：GB/T 28181-2022 及 GB/T 28181-2016 的标准**原文**（非二手解读）
> 关联：`docs/gb28181-2022-gap-inventory.md`（本仓实现缺口）
> 引用页码一律用**标准印刷页码**（PDF 页 = 标准页 + 7，2022 版 / + 5，2016 版）

---

## 一、结论

**骨架写得很清楚，参数语义与互操作细节有明显留白和自相矛盾。**

- **可以直接照着实现的部分**：命令族、承载方式（`Control`/`DeviceConfig` 与 `Query`/`ConfigDownload`）、
  `ConfigType` 取值全集、多值分隔与多响应规则、应答方式与必答要求 —— 全部有明文，无歧义。
- **不能直接照着实现的部分**：约 **10 处**，其中 **4 处是注释与 XML Schema 直接矛盾**（必选性、默认值），
  **1 处悬空交叉引用**（`A.2.3.2.13` 不存在），**1 处同标准内元素名/类型名不一致**
  （`SnapShot` vs `SnapShotConfig`、`snapshotCfgType` vs `snapShotCfgType`），
  以及 **图像上传细节整体留白**（标准只写「宜采用 http」，其余不定义）。

一句话：**配置"有哪些"写清了，配置"每个字段什么意思、必不必选、不支持怎么办"没写清。**

---

## 二、查证方法（可复现）

两份 PDF **均无文本层**（CID 编码，`pdftotext` / `pypdf` 提不出中文），本机也没有
`pdftoppm` / `mutool` / `tesseract`。改用 macOS 自带的 **PDFKit 渲染 + Vision OCR**：

```bash
# 渲染 + 中文 OCR 的脚本（已归档）
swift .workbuddy/ocr/ocr.swift <pdf> <out.txt> <起页> <止页>

# 定位（先 OCR 目次，一页试读拿偏移，再整段 OCR）
swift .workbuddy/ocr/ocr.swift "国标文档/GBT+28181-2022.pdf" out.txt 63 106   # 附录A，标准页 56~99
swift .workbuddy/ocr/ocr.swift "国标文档/GBT28181-2016.pdf"  out16.txt 55 82  # 附录A，标准页 50~77
```

OCR 产物（原文可检索）已归档在 `.workbuddy/ocr/`（该目录已被 git 忽略）：
`appA.txt`(2022 附录A) · `appA2016.txt`(2016 附录A) · `sec93.txt`(§9.3) · `sec914.txt`(§9.13/9.14)。

---

## 三、写得明确的部分（可直接实现）

| 内容 | 条款 | 原文明文 |
| --- | --- | --- |
| 设置的承载 | A.2.3.2.1（标准页 79） | `<element name="CmdType" fixed="DeviceConfig"/>`，包在 `Control` 里 |
| 读取的承载 | A.2.4.7（标准页 83） | `<element name="CmdType" fixed="ConfigDownload"/>` + 必选 `ConfigType` |
| 配置类型全集 | A.2.4.7 注释（标准页 83） | 一次性列全 **12 项**，明文「可同时查询多个配置类型，各类型以"/"分隔，可返回与查询 SN 值相同的**多个响应**，每个响应对应一个配置类型」 |
| 设置应答 | A.2.6.8（标准页 93） | `CmdType fixed="DeviceConfig"` + `SN` + `DeviceID` + 必选 `Result` |
| 读取应答 | A.2.6.9（标准页 94-95） | `CmdType fixed="ConfigDownload"` + `Result` + 各配置元素（均 `minOccurs=0`） |
| 必答性 | §9.3.1 e)（标准页 23） | 「源设备向目标设备发送录像控制、报警布防/撤防、报警复位、看守位控制、软件升级、**设备配置**命令后，目标设备**应**发送应答命令」 |
| 功能↔章节映射 | 表2（标准页 25） | 11 个配置功能 → 请求章节 `A.2.3.2.2~A.2.3.2.12`，应答章节统一 `A.2.6.8` |
| 抓拍完成通知 | A.2.5.7（标准页 88） | `CmdType fixed="UploadSnapShotFinished"`；`SessionID`(32~128) + `SnapShotList/SnapShotFileID`(≤10) |
| 抓拍失败语义 | A.2.5.7 注释（标准页 88） | 「**无文件标识或文件标识个数少于要求抓拍的文件个数，表示全部或部分抓拍或上传操作异常失败**」——**这条写得比别的配置项都细** |
| 抓拍文件命名 | §9.14.1 + 表4（标准页 53-54） | 41 位：设备编码 20 + 图像编码 2（固定 02）+ 时间编码 17（YYYYMMDDhhmmssSSS）+ 序列码 2 |

> ⭐ 特别确认（推翻本仓此前的二手结论）：**图像抓拍配置的 `CmdType` 就是 `DeviceConfig`**，
> 依据是 §9.14.3 b)：「配置命令消息体采用 XML 封装，消息体元数据序列格式符合
> **A.2.3.2.1 和 A.2.3.2.12** 的格式规定」——A.2.3.2.1 的 CmdType 是 `fixed="DeviceConfig"`。
> 不是 `DeviceControl`。完成通知也不是 `Notify/SubCmd`，而是 `UploadSnapShotFinished`。

---

## 四、模糊与矛盾清单

### A 档 · 注释与 XML Schema 直接矛盾（会导致两侧实现必然不一致）

| # | 位置 | 矛盾内容 |
| --- | --- | --- |
| A-1 | `snapShotCfgType.Interval`，A.2.1.24（标准页 71） | 注释写「单张抓拍间隔时间，单位：秒（**必选**），取值范围：最短 1 秒」，Schema 却是 `minOccurs="0"` → **省略时默认间隔是多少，标准未定义**（本仓平台代码自己补了 1~3600 的校验，属自定口径） |
| A-2 | `OSDCfgType.TimeEnable` / `TextEnable`，A.2.1.12（标准页 63） | 注释写「（**可选**）」，却用 `default="1"` 而**没写 `minOccurs`** —— XSD 中 `minOccurs` 缺省 = 1（必选）。同文件其他可选元素都显式写了 `minOccurs="0"` → 到底是笔误还是真必选？ |
| A-3 | `SVACEncodeCfgType.ROIParam`，A.2.1.21（标准页 68） | 注释「感兴趣区域参数（**必选**）」，Schema `minOccurs="0"` |
| A-4 | `snapShotCfgType.SessionID`，A.2.1.24（标准页 71） | 注释限定字符集「由大小写英文字母、数字、短划线组成」，Schema 只约束 `minLength=32 / maxLength=128`，**没有 pattern** → 字符集约束不可机检 |

> 这一类是「同一份 Schema 谁都验不过」的根源：**标准用自然语言注释补充 Schema 表达力不足**，
> 例如 SVAC 里反复出现的「（配置可选，查询应答必选）」——**同一类型在设置与查询两个方向上必选性不同**，
> XML Schema 无法表达，只能靠人读注释。

### B 档 · 留白：标准没定义，实现必然各写各的

| # | 位置 | 留白内容 |
| --- | --- | --- |
| B-1 | §9.14.3 c)（标准页 55） | 只说「图像传输方式**宜**采用 http」——**「宜」是推荐性用词，不是强制**。`UploadURL` 只声明 `type="string"`：**没规定方法（POST/PUT）、没规定表单字段名、没规定请求头、没说用文件名还是 query 参数回传 SessionID**。这是整个配置家族最大的互操作黑洞（标准解读文章自己都承认「并没有对如何上传图片进行细化」） |
| B-2 | `PictureMask.Point`，A.2.1.17（标准页 66） | 只写「区域左上角、右下角坐标（lx,ly,rx,ry，单位像素）」——**没写原点、没说相对播放窗口还是设备原始分辨率**。而 OSD 的 `TimeX/TimeY` 明明白白写了「以播放窗口左上角像素原点，水平向右为正」。**同标准内同类坐标两套详略**；分辨率还能被 `VideoParamAttribute` 改，基准就更关键 |
| B-3 | `Result`，A.2.1.5（标准页 57） | `resultType` **枚举只有 `OK` / `ERROR`** → ① 一条 `DeviceConfig` 可同时带多个配置元素（A.2.3.2.1 里顺序排列、各自 `minOccurs=0`），若 3 个里坏 1 个，`Result` 只能给一个值，**也没有字段指出是哪个失败**；② **设备不支持某个 `ConfigType` 时用什么表达，标准完全没规定** |
| B-4 | A.2.6.9（标准页 94） | 所有配置元素都是 `minOccurs=0` → 设备可以 `Result=OK` 却一个元素都不返回。**「不支持」与「配置为空」在协议上不可区分** |
| B-5 | `basicParamCfgType`，A.2.1.19（标准页 67） | 四个字段全 `minOccurs=0` → 空 `<BasicParam/>` 合法；且 `Expiration` / `HeartBeatInterval` **单位（秒？）在类型定义里没写**（正文 §9.1 有 60s 默认值，但类型注释没带） |
| B-6 | `alarmReportCfgType`，A.2.1.18（标准页 67） | 只定义 2 个开关（`MotionDetection` 移动侦测 / `FieldDetection` 区域入侵），且**都是必选** → ① 无法只改其中一个；② 其他报警类型（遮挡报警、磁盘/风扇故障等）没有对应开关，与报警类型列表不对齐；③ **扩展机制未定义**（能否加自定义元素？没说） |

### C 档 · 编号与命名错误（可复核的硬伤）

| # | 位置 | 问题 |
| --- | --- | --- |
| C-1 | A.2.3.2.1 注释（标准页 79） | 「设备配置请求命令序列见 **A.2.3.2.2～A.2.3.2.13**」——**A.2.3.2.13 不存在**。A.2.3.2 实际只到 **.12**（图像抓拍配置），表2 亦只有 11 行、请求章节止于 `A.2.3.2.12`。**悬空交叉引用（差一）** |
| C-2 | A.2.6.9（标准页 95） | 图像抓拍的元素名写作 **`SnapShot`**（A.2.3.2.12 / A.2.4.7 / 表A.2 都是 `SnapShotConfig`），类型名写作 **`snapshotCfgType`**（A.2.1.24 与表A.2 是 `snapShotCfgType`）。XSD 类型名**大小写敏感**，严格按 Schema 校验时这两个名字是**不同的东西** |
| C-3 | `snapShotCfgType.SnapNum` 注释（标准页 71） | 「连拍张数（必选），最多 10 张，**当手动抓拍时，取值为 1**」——标准里**没有"手动抓拍"这个命令**（A.2.3.2.12 只有 `SnapShotConfig` 一项）。**悬空概念** |
| C-4 | A.2.2.1（标准页 72） | 请求消息体 Schema 根上写 `<choice maxOccurs="unbounded">`，字面允许一份文档里出现多个 `Control`/`Query`/`Notify`，与「一条 MESSAGE 一个命令」的语义不符（判定为历史遗留写法，本条建议看原件再定，未深入使用） |

### D 档 · 内部冗余（不一定错，但会产生二义）

| # | 位置 | 问题 |
| --- | --- | --- |
| D-1 | `pictureMaskCfgType`，A.2.1.17（标准页 66） | **两个数量字段**：外层 `SumNum`（区域总数，必选）+ `RegionList/@Num`（当前区域个数，必选），列表 `Item` 又 `minOccurs=0 maxOccurs=4` → 三者不一致时以谁为准？未说明。同理 OSD 的 `SumNum`（显示文字行数总数，必选）vs `Item` `minOccurs=0 maxOccurs=8`：声明 3 行却给 0 个 Item 怎么办？未说明 |
| D-2 | `pictureMaskCfgType.RegionList`（标准页 66） | `RegionList` 是 `minOccurs="0"`，但其内部属性 `Num` 标为**必选** → 元素缺省时 `Num` 无从取值 |
| D-3 | `pictureMaskCfgType`（标准页 66） | 「视频画面遮挡」的**遮挡方式未定义**（马赛克？纯色块？），也没说是否影响录像与回放 |

---

## 五、对本仓的直接含义

1. **抓拍口径必须按原文改**（§9.14.3 b) + A.2.3.2.1 + A.2.5.7）：
   本仓平台发 `CmdType=DeviceControl`、模拟器回 `Notify`+`SubCmd=SnapShot`，**两侧自洽但都不合标准**。
   详见缺口清单 2.3。
2. **"设备不支持"没有标准表达（B-3 / B-4）** → 本仓模拟器「忽略未知 `ConfigType` 仍回 `OK`」
   **在法律上不算违规**，但它会在联调里**掩盖问题**。这是标准留白，不是我们实现错；
   但既然要长期维护，建议我们**自己定一个可判定的口径**（例如：不支持的 `ConfigType` 回 `Result=ERROR`，
   或省略元素并在设备日志里留痕），并写进对接文档说清"这是我们自定的"。
3. **做设备配置家族前，必须先定死三份契约**（标准留白处）：
   - **坐标基准**：遮挡区坐标与 OSD 文字坐标统一按**播放窗口像素**（跟 OSD 注释对齐），并明确随分辨率变化时的换算规则
   - **必选性**：以 Schema 为准还是以注释为准 —— 建议**取并集（都当必选）**，宁可多收字段
   - **图片上传**：方法、路径、字段名、SessionID 回传方式，参考主流实现的 `POST multipart/form-data` + `file` 字段 + query `SessionID`
4. **`SumNum` 与列表不一致时的取舍要写进我们的实现约定**（D-1），否则前后端/设备三方各理解一次。

---

## 六、附带修正：2016 只有 4 个配置类型

用同样方法 OCR 了 2016 版附录 A（标准页 50~77，脚本与产物同上），
2016 的 A.2.4 设备配置查询注释**逐字列出的可查配置类型只有 4 个**：

> 「可查询的配置类型包括基本参数配置：BasicParam，视频参数范围：VideoParamOpt，
> SVAC 编码配置：SVACEncodeConfig，SVAC 解码配置：SVACDecodeConfig」

对照 2022 的 12 项，**2022 新增了 8 个配置类型**：

| 配置类型 | 2016 | 2022 |
| --- | --- | --- |
| `BasicParam` 基本参数 | ✅ | ✅ |
| `VideoParamOpt` 视频参数范围 | ✅ | ✅ |
| `SVACEncodeConfig` / `SVACDecodeConfig` | ✅ | ✅ |
| **`VideoParamAttribute` 视频参数属性** | ✗ | **新增** |
| **`VideoRecordPlan` 录像计划** | ✗ | **新增** |
| **`VideoAlarmRecord` 报警录像** | ✗ | **新增** |
| **`PictureMask` 视频画面遮挡** | ✗ | **新增** |
| **`FrameMirror` 画面翻转** | ✗ | **新增** |
| **`AlarmReport` 报警上报开关** | ✗ | **新增** |
| **`OSDConfig` 前端 OSD** | ✗ | **新增** |
| **`SnapShotConfig` 图像抓拍配置** | ✗ | **新增** |

> ⚠️ 这条推翻了缺口清单 v2 §1.1 里「`VideoRecordPlan` / `VideoAlarmRecord` 2016 待查」的标注 ——
> **它们也是 2022 新增**，2016 版附录 A 全文不含这两个词。v2 清单已同步更正。

---

## 六-A、协议骨架是**向后兼容**的（2022 是同层追加，不是重构）

上表只回答「多出哪些类型」，还需回答「平台多发这些类型，会不会把 2016 设备弄坏」。
逐字段对照两版的 `DeviceConfig` 请求骨架（2016 A.2.4 b) / 2022 A.2.3.2.1~.13）：

| 位置 | 2016 | 2022 | 兼容性 |
| --- | --- | --- | --- |
| `CmdType` | `fixed="DeviceConfig"` | 同 | ✅ |
| `SN` | `integer minInclusive=1` | `tg:SNType`（具名类型，语义同为 ≥1 整数） | ✅ 可同报文 |
| `DeviceID` | `tg:deviceIDType` | 同 | ✅ |
| `ConfigType`（查询侧） | 元素名 `ConfigType` | 同（§六-C 排除 OCR 噪声后） | ✅ |
| 配置元素容器 | 同层 `sequence` 直接放元素 | 同层 `sequence` 直接放元素 | ✅ |

2022 的配置元素是在**同一层 `sequence` 里按 A.2.3.2.2 → A.2.3.2.13 顺序追加**，
且 2016 已有的三个（`BasicParam` / `SVACEncodeConfig` / `SVACDecodeConfig`）
**名字、层级、语义都没动**。

⇒ **平台按 2016 骨架发的报文，2016 设备一定解析得了。** 风险不在"报文发不出去"，
而在"新元素它不认识"——那是**行为判定**问题，不是**协议兼容**问题。
门禁策略见 `docs/gb28181-2022-video-param-attribute-panel.md` §十一。

## 六-B、平台**接收侧**要容忍的两处字段差异

1. ⛔ **`BasicParam` 字段数变了。**
   2016 的 `ConfigDownload` 应答里 `BasicParam` 有 **7 个**字段
   （`Name` / `Expiration` / `HeartBeatInterval` / `HeartBeatCount` /
   `PositionCapability` / `Longitude` / `Latitude`，见 2016 A.2.4 j)）；
   2022 的 `basicParamCfgType`（标准页 74-75，`appA.txt:537`）**只剩 4 个** ——
   定位能力已移出，改由独立的移动设备位置订阅承担。
   ⇒ 平台解析 2016 设备应答时**必须容忍多出来的那 3 个字段**，
   不能因为「2022 没定义」就丢弃、更不能报错。
2. ✅ **`VideoParamOpt` 两版结构完全一致**（`DownloadSpeed` + `Resolution`，均 `minOccurs="0"`）。
   唯一差别是注释引用的附录号：2016 写「参见附录 **F** 中 SDP f 字段规定」，2022 写「附录 **G**」。
   ⇒ 解析层零差异；但**给 2016 设备用的界面/文档引附录号时别指向错的那本**。

## 六-C、`Config Type`（带空格）是 OCR 噪声

2022 全文里 `<element name="Config Type" type="string"/>` 只在 `full2022.txt:3944` 出现一次，
而 `ConfigType`（无空格）在 2022 全文 **零命中**（`grep -c` = 0）—— 表面上像"元素名改了"。
判为噪声的依据：**XSD 的元素名是 `NCName`，不允许含空格**，
且同一份 schema 里其他元素名（`DeviceID` / `BasicParam`…）都无空格。
⇒ **两版元素名都是 `ConfigType`**，无需在代码里做双写兼容。

> 同类噪声另有 2016 的 `<element name="VideoParamOpt"" minOccurs="0">`（重复引号）、
> 2022 的 `<attribute name = "Num" ...>`（等号两侧空格）。**落地改代码前建议肉眼核对应 PDF 页一次**，
> 本仓 OCR 脚本与页码定位方法见 §二。

---

## 七、证据边界

- 本审查**全部回到标准原文**（2022 版 + 2016 版），不再依赖解读文章或厂商文档。
- 唯一残留不确定性：以上结论基于 **OCR 文本**。OCR 对普通正文与 XML 元素的识别质量很好
  （全文可读、条款号与枚举值清晰），但**标点与个别字符可能有误**（如 `"`/`"`、`<`/`〈`、
  元素名中的空格）。**A 档「注释与 Schema 矛盾」与 C-1「A.2.3.2.13 不存在」这类结论，
  落地改代码前建议肉眼复核对应 PDF 页一次**（页码已在各条标注）。
- 相关 OCR 产物：`.workbuddy/ocr/`（git 已忽略，不随仓库分发，规避标准文本的版权分发问题）。

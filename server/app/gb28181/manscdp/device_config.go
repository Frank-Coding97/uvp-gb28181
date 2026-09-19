package manscdp

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"

	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
)

// 设备配置族**通用通道**（GB/T 28181-2022 A.2.3.2 下发 / A.2.4.7 查询 / A.2.6.9 应答）。
//
// ## 与 video_param.go 的分工
//
// `VideoParamAttribute`（A.2.1.13）已于 2026-09-18 单独落地：独立 DTO（[VideoParamItem]）、
// 按**码流分行**的专用落库表、按 `StreamNumber` 逐行对账。**本文件刻意不并它** ——
// 它的对账粒度是"行"而不是"块"，并入通用容器只会把一条已验证的链路重做一遍。
//
// 本文件承载**其余各配置类型**，走一条**通用**通道：一条报文可带多组、整块落 JSON 快照、
// 按 `config_type` 对账。
//
// ## 为什么是「一个全可选容器」而不是「每类型一条通道」
//
//  1. **与标准同构**：A.2.4.7 明文允许「可同时查询多个配置类型」，A.2.6.9 的消息体本身
//     也允许这些块作为**兄弟元素**同时出现。容器形态就是标准的形态。
//  2. **差异只在 XML 结构**：类型之间的通道部分（排队 / 重试 / 超时 / 回读对账 / 落库）
//     完全一致。每类型各写一条通道 = 7 份 scheduler 接线，改一处要改 7 遍。
//
// ⛔ **每个 `*Block` 指针的 `nil` 都是「报文里没有这一块」**，不是「块为空」。
// 两者在标准的 `minOccurs` 语义下完全不同（见 docs/gb28181-2022-* 的既有判据），
// 且平台侧 `type_absent` 判据正是靠它 —— 合并成值类型就再也分不出来了。
// ⭐ json tag 的键名 = 标准元素名的小驼峰，**刻意与 XML 元素名一一对应**。
// 于是同一套键名同时是三处的词汇表：API 请求/响应、库里的 payload_json、对账的字段路径。
// ⛔ 不要为了好看改成 UI 表单键（前端 deviceConfigGroups.ts 里的 `mask1` / `beginTime`）——
// 那是**展示层**的表单字段 id，与协议字段不是一对一（4 个 mask 对应一个 RegionList）。
// 把映射放到后端就是造第二套词汇表，然后两边迟早对不上。
//
// ⛔ 只有**指针字段**用 omitempty（nil = 元素缺席）。列表**一律不用**：
// 空列表是信息（"设备明确回了个空计划"），omitempty 会把它和"这一项没有"合并。
type DeviceConfigBlocks struct {
	BasicParam       *BasicParamBlock       `json:"basicParam,omitempty"`
	VideoParamOpt    *VideoParamOptBlock    `json:"videoParamOpt,omitempty"`
	VideoRecordPlan  *VideoRecordPlanBlock  `json:"videoRecordPlan,omitempty"`
	VideoAlarmRecord *VideoAlarmRecordBlock `json:"videoAlarmRecord,omitempty"`
	PictureMask      *PictureMaskBlock      `json:"pictureMask,omitempty"`
	FrameMirror      *FrameMirrorBlock      `json:"frameMirror,omitempty"`
	AlarmReport      *AlarmReportBlock      `json:"alarmReport,omitempty"`
	OSDConfig        *OSDConfigBlock        `json:"osdConfig,omitempty"`
}

// ConfigTypeOrder 是配置类型在报文里的**固定出现顺序**：与 [DeviceConfigBlocks] 的字段
// 声明顺序、以及 A.2.3.2.2 ~ A.2.3.2.13 的条款顺序一致。
//
// ⛔ 这个顺序是对外契约，不是随手排的：构建侧按它排元素、PresentConfigTypes 按它返回、
// 对账差异列表也按它排。三处同源，否则同一条报文在不同消费者眼里"顺序不同"，
// 日志比对会被当成"报文不一样"。
//
// 注：`VideoParamAttribute`（A.2.1.13）走 video_param.go 的独立通道，不在此列。
var ConfigTypeOrder = []string{
	ConfigTypeBasicParam,
	ConfigTypeVideoParamOpt,
	ConfigTypeVideoRecordPlan,
	ConfigTypeVideoAlarmRecord,
	ConfigTypePictureMask,
	ConfigTypeFrameMirror,
	ConfigTypeAlarmReport,
	ConfigTypeOSDConfig,
}

// IsEmpty 报告一个块都没有。构建侧据此拒发（空报文没有语义），解析侧据此判"设备什么都没回"。
func (b DeviceConfigBlocks) IsEmpty() bool {
	return len(b.PresentConfigTypes()) == 0
}

// PresentConfigTypes 返回**报文里真的带了的**配置类型名，按 [ConfigTypeOrder] 排序。
func (b DeviceConfigBlocks) PresentConfigTypes() []string {
	types := make([]string, 0, len(ConfigTypeOrder))
	for _, configType := range ConfigTypeOrder {
		if b.Has(configType) {
			types = append(types, configType)
		}
	}
	return types
}

// Block 按标准 ConfigType 名取回该组配置的块；该块缺席时返回 (nil, false)。
//
// ⛔ 它是「ConfigType 名 ↔ 结构字段」的**唯一映射点**：Has / PresentConfigTypes /
// 落库取值全部经由它。多写一份 switch 就是多一个会漂的真源 ——
// 本仓已经为"同一件事写两份判定、改一处忘一处"栽过三次（见 gb28181-2022 记忆里的两道门禁）。
//
// ⛔ 第二个返回值不可省：块指针是**有类型的 nil**，塞进 `any` 之后 `value == nil` 为假。
// 想靠 `== nil` 判缺席的写法在这里必然失效。
func (b DeviceConfigBlocks) Block(configType string) (any, bool) {
	switch strings.TrimSpace(configType) {
	case ConfigTypeBasicParam:
		return b.BasicParam, b.BasicParam != nil
	case ConfigTypeVideoParamOpt:
		return b.VideoParamOpt, b.VideoParamOpt != nil
	case ConfigTypeVideoRecordPlan:
		return b.VideoRecordPlan, b.VideoRecordPlan != nil
	case ConfigTypeVideoAlarmRecord:
		return b.VideoAlarmRecord, b.VideoAlarmRecord != nil
	case ConfigTypePictureMask:
		return b.PictureMask, b.PictureMask != nil
	case ConfigTypeFrameMirror:
		return b.FrameMirror, b.FrameMirror != nil
	case ConfigTypeAlarmReport:
		return b.AlarmReport, b.AlarmReport != nil
	case ConfigTypeOSDConfig:
		return b.OSDConfig, b.OSDConfig != nil
	default:
		return nil, false
	}
}

// Has 报告报文里是否带了指定配置类型。
func (b DeviceConfigBlocks) Has(configType string) bool {
	_, present := b.Block(configType)
	return present
}

// ---- 线格式：下发（A.2.3.2 的 `<Control>`） ----

// deviceConfigBlocksWire 是下发报文的线格式。
//
// ⛔ 各块用**指针 + omitempty**：nil 指针会被 encoding/xml 整个省掉，
// 这正是"平台这次没配这一项"的正确线形态。值类型做不到这件事（会发一个空元素）。
//
// ⛔ 字段声明顺序 = XML 输出顺序，**照 A.2.3.2.2 ~ A.2.3.2.13 的条款顺序**：
// `BasicParam` → `VideoRecordPlan` → `VideoAlarmRecord` → `PictureMask` → `FrameMirror`
// → `AlarmReport` → `OSDConfig`（`VideoParamAttribute` 走 video_param.go 的独立通道）。
// 顺序写错**不会报错**，但"照标准逐行核对"这件事就失效了。
type deviceConfigBlocksWire struct {
	XMLName  xml.Name `xml:"Control"`
	CmdType  string   `xml:"CmdType"`
	SN       int      `xml:"SN"`
	DeviceID string   `xml:"DeviceID"`

	BasicParam       *basicParamWire       `xml:"BasicParam,omitempty"`
	VideoRecordPlan  *videoRecordPlanWire  `xml:"VideoRecordPlan,omitempty"`
	VideoAlarmRecord *videoAlarmRecordWire `xml:"VideoAlarmRecord,omitempty"`
	PictureMask      *pictureMaskWire      `xml:"PictureMask,omitempty"`
	FrameMirror      *frameMirrorWire      `xml:"FrameMirror,omitempty"`
	AlarmReport      *alarmReportWire      `xml:"AlarmReport,omitempty"`
	OSDConfig        *osdConfigWire        `xml:"OSDConfig,omitempty"`
}

// BuildDeviceConfigBlocksWithProfile 序列化 A.2.3.2 的配置下发命令。
//
// ⛔ **不做版本门禁**：`PictureMask` / `FrameMirror` 等是 2022 新增，但门禁只拦
// "平台自己决定要发"的场合；操作员手点的那一次必须放行 —— 被误登记成 2016 的真 2022
// 设备，不试一次就永远用不了这功能。真相由**强制回读对账**暴露（同 A-5 §十③）。
//
// ⛔ 与解析侧的"宽松收"相反，这里是**严格发**：[ValidateDeviceConfigBlocks] 在发出前
// 收口所有取值域。平台自己发出的值乱来，对端会静默丢弃或当 0 处理，而这种错在回读对账里
// 只表现为"设备没照做"，归因成本极高。
func BuildDeviceConfigBlocksWithProfile(profile protocol.Profile, deviceID string, sn int, blocks DeviceConfigBlocks) ([]byte, error) {
	if err := validatePTZQueryTarget(deviceID, sn); err != nil {
		return nil, err
	}
	if blocks.IsEmpty() {
		return nil, fmt.Errorf("DeviceConfig 至少需要一个配置块")
	}
	if err := ValidateDeviceConfigBlocks(blocks); err != nil {
		return nil, err
	}
	wire := deviceConfigBlocksWire{
		CmdType: CmdDeviceConfig, SN: sn, DeviceID: deviceID,
		BasicParam:       blocks.BasicParam.wire(),
		VideoRecordPlan:  blocks.VideoRecordPlan.wire(),
		VideoAlarmRecord: blocks.VideoAlarmRecord.wire(),
		PictureMask:      blocks.PictureMask.wire(),
		FrameMirror:      blocks.FrameMirror.wire(),
		AlarmReport:      blocks.AlarmReport.wire(),
		OSDConfig:        blocks.OSDConfig.wire(),
	}
	return MarshalProfiledXML(profile, wire)
}

// ValidateDeviceConfigBlocks 校验平台**将要下发**的整族配置。
//
// 与解析侧的分工：这里"严格发"、解析侧"宽松收"。规则出处一律是各 `A.2.1.x` 的
// 必选性与取值表，不自行加严也不放宽。
func ValidateDeviceConfigBlocks(blocks DeviceConfigBlocks) error {
	if blocks.BasicParam != nil {
		if err := blocks.BasicParam.validate(); err != nil {
			return err
		}
	}
	if blocks.PictureMask != nil {
		if err := blocks.PictureMask.validate(); err != nil {
			return err
		}
	}
	if blocks.FrameMirror != nil {
		if err := blocks.FrameMirror.validate(); err != nil {
			return err
		}
	}
	if blocks.AlarmReport != nil {
		if err := blocks.AlarmReport.validate(); err != nil {
			return err
		}
	}
	if blocks.VideoRecordPlan != nil {
		if err := blocks.VideoRecordPlan.validate(); err != nil {
			return err
		}
	}
	if blocks.VideoAlarmRecord != nil {
		if err := blocks.VideoAlarmRecord.validate(); err != nil {
			return err
		}
	}
	if blocks.OSDConfig != nil {
		if err := blocks.OSDConfig.validate(); err != nil {
			return err
		}
	}
	return nil
}

// ---- 线格式：回读应答（A.2.6.9 的 `<Response>`） ----

// deviceConfigBlocksResponseWire 是回读应答的线格式。
//
// ⛔ 各块的解析一律用**指针**：`nil` = 设备这次没带这一块 ⇒ 平台判 `type_absent`
// （"设备不支持该配置类型"最可靠的判据）。用值类型会把"没带"与"带了个空的"合并。
type deviceConfigBlocksResponseWire struct {
	XMLName  xml.Name `xml:"Response"`
	CmdType  string   `xml:"CmdType"`
	SN       int      `xml:"SN"`
	DeviceID string   `xml:"DeviceID"`
	Result   string   `xml:"Result"`

	BasicParam       *basicParamWire        `xml:"BasicParam"`
	VideoParamOpt    *videoParamOptWire     `xml:"VideoParamOpt"`
	VideoRecordPlan  []videoRecordPlanWire  `xml:"VideoRecordPlan"`
	VideoAlarmRecord []videoAlarmRecordWire `xml:"VideoAlarmRecord"`
	PictureMask      *pictureMaskWire       `xml:"PictureMask"`
	FrameMirror      *frameMirrorWire       `xml:"FrameMirror"`
	AlarmReport      *alarmReportWire       `xml:"AlarmReport"`
	OSDConfig        *osdConfigWire         `xml:"OSDConfig"`
}

// selectRecordPlanBlock 从应答里的多个 `<VideoRecordPlan>` 块中选一块。
//
// ⛔ 不能用单指针接：`VideoRecordPlan` 带 `StreamNumber`，多码流设备每个码流一块，
// 而 encoding/xml 对指针字段遇重复元素是**后者覆盖前者**（不报错、不提示），
// 结果是平台把子码流的计划当主码流展示。这里选**最小 StreamNumber（主码流）**，
// 并把剩余块数记进 AdditionalStreams，让上层看得见。
func selectRecordPlanBlock(entries []videoRecordPlanWire) *VideoRecordPlanBlock {
	chosen := -1
	for index := range entries {
		if chosen < 0 || entries[index].StreamNumber < entries[chosen].StreamNumber {
			chosen = index
		}
	}
	if chosen < 0 {
		return nil
	}
	block := entries[chosen].toBlock()
	block.AdditionalStreams = len(entries) - 1
	return block
}

// selectAlarmRecordBlock 同 selectRecordPlanBlock，作用于 `VideoAlarmRecord`。
func selectAlarmRecordBlock(entries []videoAlarmRecordWire) *VideoAlarmRecordBlock {
	chosen := -1
	for index := range entries {
		if chosen < 0 || entries[index].StreamNumber < entries[chosen].StreamNumber {
			chosen = index
		}
	}
	if chosen < 0 {
		return nil
	}
	block := entries[chosen].toBlock()
	block.AdditionalStreams = len(entries) - 1
	return block
}

// DeviceConfigReadResult 是一次 `ConfigDownload` 应答（A.2.6.9）的解析结果。
type DeviceConfigReadResult struct {
	CmdType  string
	SN       int
	DeviceID string
	// Result 是应答里的 Result 元素。A.2.6.9 标了必选，但现场设备习惯性漏发，
	// 所以**缺失不报错**（同 ParseConfigDownloadResponse 对 Result 的处理）。
	Result string
	// Blocks 里每个字段为 nil 表示"这一帧没带该配置类型"。
	Blocks DeviceConfigBlocks
}

// ParseDeviceConfigReadResponse 解析 A.2.6.9 的读取应答（通用容器版）。
//
// ⛔ 与 [ParseConfigDownloadResponse] 的区别：那个只解 `VideoParamAttribute` 并支持
// `wantType` 缺席判定；本函数解**整族**，缺席判定交给调用方按
// [DeviceConfigBlocks.Has] 逐类型判 —— 一次请求可以点名多个类型，每个类型各自可能缺席，
// 用单一的 `wantType` 表达不了这种情形。
//
// ⛔ 取值**不做范围内校验**（宽松收）：设备回了越界的 `FrameMirror=7`，也要原样带上来，
// 让对账把它显示成"值不一致"。若在这里报错，整份应答会被丢掉，用户看到的是"协议错误"，
// 而真相是"设备回了台不合规的值"。
func ParseDeviceConfigReadResponse(body []byte) (*DeviceConfigReadResult, error) {
	var wire deviceConfigBlocksResponseWire
	if err := DecodeProfiledXML(protocol.ProfileFor(protocol.Version2016), body, &wire); err != nil {
		return nil, &ConfigDownloadError{Code: ConfigErrorMalformed, Msg: err.Error()}
	}
	if wire.CmdType != CmdConfigDownload {
		return nil, &ConfigDownloadError{Code: ConfigErrorCmdType, Msg: fmt.Sprintf("got %q", wire.CmdType)}
	}
	if wire.SN <= 0 || strings.TrimSpace(wire.DeviceID) == "" {
		return nil, &ConfigDownloadError{Code: ConfigErrorMalformed, Msg: "SN 或 DeviceID 缺失"}
	}

	result := &DeviceConfigReadResult{
		CmdType:  wire.CmdType,
		SN:       wire.SN,
		DeviceID: strings.TrimSpace(wire.DeviceID),
		Result:   strings.TrimSpace(wire.Result),
	}
	result.Blocks = DeviceConfigBlocks{
		BasicParam:       parseBasicParamBlock(wire.BasicParam),
		VideoParamOpt:    wire.VideoParamOpt.toBlock(),
		VideoRecordPlan:  selectRecordPlanBlock(wire.VideoRecordPlan),
		VideoAlarmRecord: selectAlarmRecordBlock(wire.VideoAlarmRecord),
		PictureMask:      wire.PictureMask.toBlock(),
		FrameMirror:      wire.FrameMirror.toBlock(),
		AlarmReport:      wire.AlarmReport.toBlock(),
		OSDConfig:        wire.OSDConfig.toBlock(),
	}
	return result, nil
}

// ParseDeviceConfigReadResponseFor 在解析基础上核对 SN / DeviceID。
//
// ⛔ DeviceID 核对对应答的**顶层** `<DeviceID>`：平台按通道编码查（`channel_code`），
// 设备必须**原样回填**请求里的那个值。回错编码的后果是整条应答被判"不属于本次操作"，
// 平台永远停在 `never_read`，而两侧日志都不报错（2026-09-18 实测过）。
func ParseDeviceConfigReadResponseFor(body []byte, expectation ConfigDownloadExpectation) (*DeviceConfigReadResult, error) {
	result, err := ParseDeviceConfigReadResponse(body)
	if err != nil {
		return nil, err
	}
	if expectation.SN > 0 && result.SN != expectation.SN {
		return nil, &ConfigDownloadError{
			Code: ConfigErrorCorrelation, Msg: fmt.Sprintf("SN got %d want %d", result.SN, expectation.SN),
		}
	}
	if want := strings.TrimSpace(expectation.DeviceID); want != "" && result.DeviceID != want {
		return nil, &ConfigDownloadError{
			Code: ConfigErrorCorrelation, Msg: fmt.Sprintf("DeviceID got %q want %q", result.DeviceID, want),
		}
	}
	return result, nil
}

// ============================ BasicParam（A.2.1.19） ============================

// BasicParamBlock 是 `basicParamCfgType`（A.2.1.19）。
//
// ⛔ 四个配置字段全部 `minOccurs="0"`，所以一律用指针：`nil` = **平台没下发这一项**，
// 与"平台下发了一个 0"是两件事。这条口径在回读对账上是决定性的 —— 合并之后
// 平台会把"没配注册有效期"显示成"有效期 0 秒"。
//
// ⭐ **2016 的*回读*比 2022 多三个字段（同一 ConfigType，读/写门禁不同）**：
// 2016 A.2.6 j) 的应答里还有 `PositionCapability` / `Longitude` / `Latitude`，
// 2022 A.2.1.19 把它们删掉了。那三个是**设备能力/状态的出口**、不是平台配置的入口，
// 所以只在本结构的下半区出现，且[ValidateDeviceConfigBlocks] 不管它们。
type BasicParamBlock struct {
	// ---- 平台可下发的四项（两版共通） ----
	Name              *string `json:"name,omitempty"`
	Expiration        *int    `json:"expiration,omitempty"`
	HeartBeatInterval *int    `json:"heartBeatInterval,omitempty"`
	HeartBeatCount    *int    `json:"heartBeatCount,omitempty"`

	// ---- 仅 2016 回读侧出现的三项（只读，平台不构建） ----
	PositionCapability *int    `json:"positionCapability,omitempty"`
	Longitude          *string `json:"longitude,omitempty"`
	Latitude           *string `json:"latitude,omitempty"`
}

type basicParamWire struct {
	XMLName           xml.Name `xml:"BasicParam"`
	Name              *string  `xml:"Name,omitempty"`
	Expiration        *int     `xml:"Expiration,omitempty"`
	HeartBeatInterval *int     `xml:"HeartBeatInterval,omitempty"`
	HeartBeatCount    *int     `xml:"HeartBeatCount,omitempty"`
	// 2016 回读专属；平台构建时恒为 nil（omitempty 会省掉）。
	PositionCapability *int    `xml:"PositionCapability,omitempty"`
	Longitude          *string `xml:"Longitude,omitempty"`
	Latitude           *string `xml:"Latitude,omitempty"`
}

func (b *BasicParamBlock) wire() *basicParamWire {
	if b == nil {
		return nil
	}
	return &basicParamWire{
		Name: b.Name, Expiration: b.Expiration,
		HeartBeatInterval: b.HeartBeatInterval, HeartBeatCount: b.HeartBeatCount,
		PositionCapability: b.PositionCapability, Longitude: b.Longitude, Latitude: b.Latitude,
	}
}

func parseBasicParamBlock(wire *basicParamWire) *BasicParamBlock {
	if wire == nil {
		return nil
	}
	return &BasicParamBlock{
		Name: trimOptional(wire.Name), Expiration: wire.Expiration,
		HeartBeatInterval: wire.HeartBeatInterval, HeartBeatCount: wire.HeartBeatCount,
		PositionCapability: wire.PositionCapability,
		Longitude:          trimOptional(wire.Longitude), Latitude: trimOptional(wire.Latitude),
	}
}

// Configurable 报告四项配置里**至少有一项**被下发过。
//
// ⛔ 四项全空的下发**没有可落库的内容**：静默收下会让日志与落库快照都看不出这次下发
// 到底做了什么。构建侧据此拒发（见 [validate]）。
func (b *BasicParamBlock) Configurable() bool {
	return b != nil && (b.Name != nil || b.Expiration != nil ||
		b.HeartBeatInterval != nil || b.HeartBeatCount != nil)
}

const (
	maxBasicParamSeconds        = 86400
	maxBasicParamHeartBeatCount = 1000
)

func (b *BasicParamBlock) validate() error {
	if b == nil {
		return nil
	}
	if !b.Configurable() {
		return fmt.Errorf("BasicParam 四项配置全缺（空下发没有可落库的内容）")
	}
	if b.Name != nil && strings.TrimSpace(*b.Name) == "" {
		return fmt.Errorf("BasicParam.Name 不得为空串（要么不发，要么给个名字）")
	}
	for _, field := range []struct {
		name  string
		value *int
		min   int
		max   int
	}{
		{"Expiration", b.Expiration, 1, maxBasicParamSeconds},
		{"HeartBeatInterval", b.HeartBeatInterval, 1, maxBasicParamSeconds},
		{"HeartBeatCount", b.HeartBeatCount, 1, maxBasicParamHeartBeatCount},
	} {
		if field.value == nil {
			continue
		}
		if *field.value < field.min || *field.value > field.max {
			return fmt.Errorf("BasicParam.%s 越界: %d（要求 %d~%d）", field.name, *field.value, field.min, field.max)
		}
	}
	return nil
}

// ============================ VideoParamOpt（A.2.1.20） ============================

// VideoParamOptBlock 是 `videoParamOptCfgType`（A.2.1.20）—— 视频参数**范围**配置。
//
// ⭐ **只读**：2016 的 A.2.3.2 下发清单里没有它，2022 的 A.2.4.7 查询清单里有它。
// 它报的是"摄像机**支持**哪些档位"，不是"当前生效值"（后者是 VideoParamAttribute 的范畴）。
//
// ⛔ `Resolution` 的多个值以 `/` 分隔，**取值必须出自附录 G 的码表**（1~6 或 `WxH`）。
type VideoParamOptBlock struct {
	// DownloadSpeed 下载速度档位，多值 `/` 分隔（如 `1/2/4`）。
	DownloadSpeed string `json:"downloadSpeed"`
	// Resolution 支持的分辨率档位，多值 `/` 分隔（如 `1/2/3/4/5/6`）。
	Resolution string `json:"resolution"`
}

type videoParamOptWire struct {
	XMLName       xml.Name `xml:"VideoParamOpt"`
	DownloadSpeed string   `xml:"DownloadSpeed"`
	Resolution    string   `xml:"Resolution"`
}

func (w *videoParamOptWire) toBlock() *VideoParamOptBlock {
	if w == nil {
		return nil
	}
	return &VideoParamOptBlock{
		DownloadSpeed: strings.TrimSpace(w.DownloadSpeed),
		Resolution:    strings.TrimSpace(w.Resolution),
	}
}

// SplitCodes 把 `/` 分隔的多值拆成码值列表（去空、去重、保序）。
//
// ⭐ 拆解口径放在协议层而不是让前端自己 split：报文里可能带空格（`1 / 2`），
// 两处各写一遍就会分家。
func splitSlashCodes(value string) []string {
	parts := strings.Split(value, "/")
	codes := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		code := strings.TrimSpace(part)
		if code == "" {
			continue
		}
		if _, duplicated := seen[code]; duplicated {
			continue
		}
		seen[code] = struct{}{}
		codes = append(codes, code)
	}
	return codes
}

// ============================ FrameMirror（A.2.1.23） ============================

// FrameMirror 的取值域（A.2.1.23 的 enumeration，逐个抄自原文）。
const (
	FrameMirrorOff       = 0 // 不启用镜像，基准画面
	FrameMirrorLeftRight = 1 // 水平镜像（左右翻转）
	FrameMirrorUpDown    = 2 // 上下镜像（上下翻转）
	FrameMirrorCenter    = 3 // 中心镜像（上下左右都翻转）
)

// FrameMirrorBlock 是 `frameMirrorCfgType`（A.2.1.23）。
//
// ⛔ 标准里它是 **simpleType**：元素体就是一个整数，**没有任何子元素**。
// 别照其它类型的习惯给它造 `<Mode>` / `<Value>` —— 标准里没有，对端解不出来且不报错。
//
// ⛔ 值域是 XSD 里封闭的 `enumeration`（0~3），**越界一律拒绝**。
// 与 VideoParamAttribute 的"宽松收"相反，理由：那个类型的取值范围是开放的，
// 设备收下什么就回什么，回读对账因此还能自洽；而这个类型一旦收下 7，
// 回读就会一直吐 `<FrameMirror>7</FrameMirror>` —— **这台设备从此永久只能回非法应答**，
// 而现象指向"查询"而不是当初那条越界报文。
type FrameMirrorBlock struct {
	Value int `json:"value"`
}

type frameMirrorWire struct {
	XMLName xml.Name `xml:"FrameMirror"`
	// ⛔ simpleType：元素体直接是整数，走 chardata（不是子元素）。
	Value string `xml:",chardata"`
}

func (b *FrameMirrorBlock) wire() *frameMirrorWire {
	if b == nil {
		return nil
	}
	return &frameMirrorWire{Value: strconv.Itoa(b.Value)}
}

func (w *frameMirrorWire) toBlock() *FrameMirrorBlock {
	if w == nil {
		return nil
	}
	// ⛔ 宽松收：解析不出整数时**保留原串**（放进 Value 的哨兵值 -1 会与合法值混淆），
	// 这里选择把非法形态交给上层对账展示 —— 但本结构只有 int 字段，所以用 -1 表达
	// "设备回了个解析不了的东西"，并在对账时按"值不一致"呈现（而不是解析失败）。
	value, err := strconv.Atoi(strings.TrimSpace(w.Value))
	if err != nil {
		return &FrameMirrorBlock{Value: frameMirrorUnparsable}
	}
	return &FrameMirrorBlock{Value: value}
}

// frameMirrorUnparsable 表示"设备回的值解析不成整数"。
//
// ⛔ 不能用 0：0 是合法取值（不启用镜像），混用会把"设备回了垃圾"显示成"镜像已关闭"。
// 也不能直接让解析失败：整份应答会被丢掉，用户看到"协议错误"而真相是"设备回了台不合规的值"。
const frameMirrorUnparsable = -1

// Unparsable 报告设备回的值不是合法整数（对账面板据此单独提示，见 [FrameMirrorBlock]）。
func (b *FrameMirrorBlock) Unparsable() bool {
	return b != nil && b.Value == frameMirrorUnparsable
}

func (b *FrameMirrorBlock) validate() error {
	if b == nil {
		return nil
	}
	if b.Value < FrameMirrorOff || b.Value > FrameMirrorCenter {
		return fmt.Errorf("FrameMirror 越界: %d（合法 %d~%d）",
			b.Value, FrameMirrorOff, FrameMirrorCenter)
	}
	return nil
}

// ============================ AlarmReport（A.2.1.18） ============================

// AlarmReport 的两个开关取值（A.2.1.18 注释：0-关闭 / 1-打开）。
const (
	AlarmReportOff = 0
	AlarmReportOn  = 1
)

// AlarmReportBlock 是 `alarmReportCfgType`（A.2.1.18）。
//
// 只有两个字段，**都是必选 integer**：
//   - `MotionDetection` 移动侦测事件上报开关
//   - `FieldDetection`  区域入侵事件上报开关
//
// ⛔ 别把这两个字段名换成业务语义的"视频报警 / 设备报警" —— 那不是标准字段名也不是标准语义，
// 对端会解不出来且**不报错**（报文看着正常，只是"有应答无数据"）。
type AlarmReportBlock struct {
	MotionDetection int `json:"motionDetection"`
	FieldDetection  int `json:"fieldDetection"`
}

type alarmReportWire struct {
	XMLName         xml.Name `xml:"AlarmReport"`
	MotionDetection *int     `xml:"MotionDetection"`
	FieldDetection  *int     `xml:"FieldDetection"`
}

func (b *AlarmReportBlock) wire() *alarmReportWire {
	if b == nil {
		return nil
	}
	motion, field := b.MotionDetection, b.FieldDetection
	// ⛔ 两个元素**都必须输出**（标准里是必选），不因为值是 0 就省掉 ——
	// 省掉之后对端 `xml:"MotionDetection"` 解出零值，与"元素真的不在"分不开。
	return &alarmReportWire{MotionDetection: &motion, FieldDetection: &field}
}

func (w *alarmReportWire) toBlock() *AlarmReportBlock {
	if w == nil {
		return nil
	}
	block := &AlarmReportBlock{}
	if w.MotionDetection != nil {
		block.MotionDetection = *w.MotionDetection
	} else {
		block.MotionDetection = alarmReportUnparsable
	}
	if w.FieldDetection != nil {
		block.FieldDetection = *w.FieldDetection
	} else {
		block.FieldDetection = alarmReportUnparsable
	}
	return block
}

// alarmReportUnparsable 表示"该必选元素缺席或解析不出整数"（同 frameMirrorUnparsable 的口径）。
const alarmReportUnparsable = -1

func (b *AlarmReportBlock) validate() error {
	if b == nil {
		return nil
	}
	for _, field := range []struct {
		name  string
		value int
	}{
		{"MotionDetection", b.MotionDetection},
		{"FieldDetection", b.FieldDetection},
	} {
		if field.value != AlarmReportOff && field.value != AlarmReportOn {
			return fmt.Errorf("AlarmReport.%s 非法: %d（只能 %d/%d）",
				field.name, field.value, AlarmReportOff, AlarmReportOn)
		}
	}
	return nil
}

// ============================ VideoAlarmRecord（A.2.1.16） ============================

// VideoAlarmRecordBlock 是 `videoAlarmRecordCfgType`（A.2.1.16）。
//
//	RecordEnable   是否启用报警录像配置，0-否/1-是（必选）
//	RecordTime     录像延时时间，报警时间点**后**，单位秒（可选）
//	PreRecordTime  预录时间，报警时间点**前**，单位秒（可选）
//	StreamNumber   码流编号，0-主码流/1-子码流1…（必选）
//
// ⛔ 两个可选字段用指针：`nil` = **报文里没有这个元素**，与"平台写了 0 秒"是两件事。
// 0 秒在业务上是"报警即停录"，与"没配延时"是完全不同的行为。
type VideoAlarmRecordBlock struct {
	RecordEnable  int  `json:"recordEnable"`
	RecordTime    *int `json:"recordTime,omitempty"`
	PreRecordTime *int `json:"preRecordTime,omitempty"`
	StreamNumber  int  `json:"streamNumber"`
	// AdditionalStreams 同 VideoRecordPlanBlock.AdditionalStreams：带 StreamNumber 的块
	// 在一条应答里可能按码流重复出现，解析侧选最小 StreamNumber 并计数，不做静默覆盖。
	// ⛔ 它不是协议字段，对账比较时必须剔除（见 ptz.comparableConfigTree）。
	AdditionalStreams int `json:"additionalStreams,omitempty"`
}

const maxAlarmRecordSeconds = 86400

type videoAlarmRecordWire struct {
	XMLName       xml.Name `xml:"VideoAlarmRecord"`
	RecordEnable  int      `xml:"RecordEnable"`
	RecordTime    *int     `xml:"RecordTime,omitempty"`
	PreRecordTime *int     `xml:"PreRecordTime,omitempty"`
	StreamNumber  int      `xml:"StreamNumber"`
}

func (b *VideoAlarmRecordBlock) wire() *videoAlarmRecordWire {
	if b == nil {
		return nil
	}
	// ⛔ 可选字段缺席时整个元素不出现 —— 不能输出空元素（对端会解成空串，
	// 与"没有这个元素"含义不同）。omitempty 对 nil 指针正好是这个行为。
	return &videoAlarmRecordWire{
		RecordEnable: b.RecordEnable, RecordTime: b.RecordTime,
		PreRecordTime: b.PreRecordTime, StreamNumber: b.StreamNumber,
	}
}

func (w *videoAlarmRecordWire) toBlock() *VideoAlarmRecordBlock {
	if w == nil {
		return nil
	}
	return &VideoAlarmRecordBlock{
		RecordEnable: w.RecordEnable, RecordTime: w.RecordTime,
		PreRecordTime: w.PreRecordTime, StreamNumber: w.StreamNumber,
	}
}

func (b *VideoAlarmRecordBlock) validate() error {
	if b == nil {
		return nil
	}
	if b.RecordEnable != AlarmReportOff && b.RecordEnable != AlarmReportOn {
		return fmt.Errorf("VideoAlarmRecord.RecordEnable 非法: %d（只能 0/1）", b.RecordEnable)
	}
	if b.StreamNumber < 0 {
		return fmt.Errorf("VideoAlarmRecord.StreamNumber 不能为负: %d（0=主码流）", b.StreamNumber)
	}
	for _, field := range []struct {
		name  string
		value *int
	}{
		{"RecordTime", b.RecordTime}, {"PreRecordTime", b.PreRecordTime},
	} {
		if field.value == nil {
			continue
		}
		if *field.value < 0 || *field.value > maxAlarmRecordSeconds {
			return fmt.Errorf("VideoAlarmRecord.%s 越界: %d（要求 0~%d 秒）",
				field.name, *field.value, maxAlarmRecordSeconds)
		}
	}
	return nil
}

// ============================ PictureMask（A.2.1.17） ============================

// PictureMask 的取值域。
const (
	PictureMaskOff = 0
	PictureMaskOn  = 1
	// MaxPictureMaskRegions 是标准的 `Item maxOccurs="4"`；`Seq` 的取值范围也是 1~4。
	MaxPictureMaskRegions = 4
	minPictureMaskSeq     = 1
	maxPictureMaskSeq     = 4
)

// PictureMaskRegion 是一个遮挡区域（`pictureMaskCfgType` 的 `RegionList/Item`）。
//
// ⛔ **`Point` 是「左上角 + 右下角」两个角点，不是 `x,y,w,h`**：
// 标准原文「区域左上角、右下角坐标（lx,ly,rx,ry，单位像素），格式如"20,30,50,60"」。
// 按 w/h 理解会画出**偏移一整个宽高**的假遮挡，而且两侧都不报错。
//
// ⛔ 四个坐标用 int 承载（不是原始串）：回读时统一按 `lx,ly,rx,ry` 规范化输出，
// 免得平台写 `20, 30, 50, 60`（带空格）就原样回带空格 —— 对端再解析一次就崩。
type PictureMaskRegion struct {
	Seq    int `json:"seq"`
	Left   int `json:"left"`
	Top    int `json:"top"`
	Right  int `json:"right"`
	Bottom int `json:"bottom"`
}

// PointLiteral 是线格式：`左x,左y,右x,右y`，逗号分隔、无空格。
func (r PictureMaskRegion) PointLiteral() string {
	return fmt.Sprintf("%d,%d,%d,%d", r.Left, r.Top, r.Right, r.Bottom)
}

// PictureMaskBlock 是 `pictureMaskCfgType`（A.2.1.17）。
//
// ⛔ `SumNum` **不独立存**：它必须恒等于 `len(Regions)`。存成两个字段就一定会出现
// 「计数说 3 个、实际 2 个」的自相矛盾状态，而回读时对端只能信一个。
type PictureMaskBlock struct {
	On int `json:"on"`
	// ⛔ 不用 omitempty：`null` / `[]` 与"整块缺席"是两件事，后者由 pictureMask 这个键
	// 本身存不存在表达。压成一样的 JSON 会让"设备回了个空遮挡列表"退化成"没配"。
	Regions []PictureMaskRegion `json:"regions"`
}

type pictureMaskWire struct {
	XMLName    xml.Name                   `xml:"PictureMask"`
	On         int                        `xml:"On"`
	SumNum     int                        `xml:"SumNum"`
	RegionList *pictureMaskRegionListWire `xml:"RegionList,omitempty"`
}

type pictureMaskRegionListWire struct {
	// ⛔ Num 是**属性**（不是子元素）。写成 `<Num>` 对端 `xml:"Num,attr"` 解不出来且不报错 ——
	// 与 VideoParamAttribute 的 `Num` 同一个坑。
	Num   int                         `xml:"Num,attr"`
	Items []pictureMaskRegionItemWire `xml:"Item"`
}

type pictureMaskRegionItemWire struct {
	Seq   int    `xml:"Seq"`
	Point string `xml:"Point"`
}

// PictureMaskClearPoint 是"删除该区域"的坐标写法：零面积（左上角与右下角重合）。
//
// ⭐ 2026-09-19 真机实测（海康 IPC，192.168.10.203）——**这是把设备里的残留区域真正
// 抹掉唯一有效的手段**，三种写法对比：
//
//	| 平台发出 | 设备回读 | 结论 |
//	| --- | --- | --- |
//	| `On=0`、`RegionList` 缺席（现行形态、标准里 `minOccurs=0`） | `Num="1"` 原样保留 | ❌ 只关开关 |
//	| `On=0` + 显式 `<RegionList Num="0"/>` | `Num="1"` 原样保留 | ❌ 无效 |
//	| `On=0` + 某 Seq 的 `Point` 写成零面积 | 该条消失（3 条只写 Seq=2 ⇒ 回读剩 2 条，`Num` 3→2） | ✅ 删除 |
//
// 即：设备把「零面积」解释为「删除这一条」，而不是「改成一个看不见的区域」。
// ⛔ 别把这里改回"不发 RegionList"，那正是"删除遮挡框删不掉"的成因。
const PictureMaskClearPoint = "0,0,0,0"

// pictureMaskClearRegions 是"停用遮挡"时随报文下发的清空占位：`Seq 1..4` 全部零面积
// （`MaxPictureMaskRegions = 4` 就是标准的 `Item maxOccurs="4"`）。
//
// ⭐ 为什么**固定铺满 1..4**、而不是只带"设备回读里有的那些 Seq"：
// 下发这一刻平台并不保证知道设备里还剩什么（可能从未读过、回读可能陈旧），
// 而真机实测表明**设备对不存在的 Seq 也只当删除、不会凭空创建**（发 `Seq 1..4` 全零、
// 设备里只有 Seq 1/3 ⇒ 回读 `Num="0"`，没有凭空多出区域）——铺满是幂等且覆盖更全的写法。
func pictureMaskClearRegions() []PictureMaskRegion {
	return pictureMaskFullRegions(nil)
}

// pictureMaskFullRegions 把平台给出的区域补成**全量声明**：`Seq 1..4` 一个不落，
// 没给的槽位补零面积（= 让设备把那一槽删掉）。
//
// ⭐⭐ 2026-09-20 真机实证（海康 DS-2DC2C040MY-DE，192.168.10.203）——设备把 `RegionList`
// 当**按 `Seq` 的增量补丁**，不是全量替换。所以"删掉其中一个区域"这件事，**省略那个槽位
// 是表达不出来的**：
//
//	| 设备里 | 平台发出 | 设备回读 |
//	| --- | --- | --- |
//	| Seq1..4 全有 | 只带 Seq1/2/3（前端跳过全零槽位，`Num="3"`） | `Num="4"`，**Seq4 原样还在** ❌ |
//	| Seq1..4 全有 | Seq1/2/3 真实 + Seq4 显式零面积 | `Num="3"`，Seq4 **消失** ✅ |
//
// 现场报文（`gb_ptz_operation` id=1520/1522，06:23，操作用户删掉的正是 Seq4）：
// 下发意图 `regions=[1,2,3]` ⇒ 设备回读 `Num="4"` ⇒ ① 那块遮挡**删不掉**；
// ② 紧接着的自动回读报 `DEVICE_CONFIG_RECONCILE_MISMATCH`（"设备已接受命令，但值未生效"）。
// 一个根因、两个症状 —— 都是"平台没能把删除意图说出口"。
//
// ⛔ 别退回"只发用户给的槽位"：那等于平台**永远无法表达删除**。
// ⛔ 这条会把"平台没给的槽位"一并声明成删除，所以它**必须**和"编辑/下发前先读到设备事实"
// 这道闸门配套（前端 `pictureFactsMissing` / `pictureEditable`）—— 否则一次没读过设备的
// 「启用」就会把设备里平台看不见的区域全抹掉。
func pictureMaskFullRegions(regions []PictureMaskRegion) []PictureMaskRegion {
	bySeq := make(map[int]PictureMaskRegion, len(regions))
	for _, region := range regions {
		bySeq[region.Seq] = region
	}
	full := make([]PictureMaskRegion, 0, MaxPictureMaskRegions)
	for seq := minPictureMaskSeq; seq <= maxPictureMaskSeq; seq++ {
		if region, ok := bySeq[seq]; ok {
			full = append(full, region)
			continue
		}
		// 零值坐标就是零面积（`PointLiteral` 输出 [PictureMaskClearPoint]）= 删除该槽。
		full = append(full, PictureMaskRegion{Seq: seq})
	}
	return full
}

func (b *PictureMaskBlock) wire() *pictureMaskWire {
	if b == nil {
		return nil
	}
	// 这一层的职责是"**报文怎么说**"，与用户意图的区别有两处：
	//
	//  1. **停用（`On=0`）必须把区域列表换成清空占位**，不能只发 `On=0`：
	//     设备（至少海康这一族）在 `On=0` 时只关开关、把 `RegionList` 原样留着，于是
	//     「删掉遮挡框」在页面上表现为"清不掉" —— 平台之后**没有任何办法**再把它抹掉。
	//     关掉总闸时区域本来就不生效，保留它没有语义价值，所以这里一律清干净。
	//     真机证据见 [PictureMaskClearPoint]；对账侧不会因此误报，见
	//     `ptz.device_config.go` 的 `diffDeviceConfigBlocks`（任一边 `on==0` 时 `regions`
	//     不参与比对）。
	//
	//  2. **启用（`On=1`）要把区域列表补成全量声明**，理由见 [pictureMaskFullRegions]：
	//     设备按 `Seq` 做增量补丁，省略某个槽位 ≠ 删除那个槽位，于是"删掉一个遮挡区"
	//     在页面上表现为删不掉，并且还会顺带报一次假的对账不一致。
	//
	// ⚠️ operation 的 `payload_json` 仍存**用户原始意图**（例如 `On=1` + 3 个区域），
	// 本函数只决定"报文怎么说"（那 3 个 + 第 4 个显式零面积），两者刻意不一致；
	// 排查报文一律以抓包/trace 为准。
	regions := pictureMaskFullRegions(b.Regions)
	if b.On == PictureMaskOff {
		regions = pictureMaskClearRegions()
	}
	wire := &pictureMaskWire{On: b.On, SumNum: len(regions)}
	// ⛔ `SumNum` / `Num` 是**报文里 `Item` 的条数**（含零面积的删除标记），不是"有面积的
	// 区域个数"。真机回读把这条语义坐实了：发 4 条（3 真 + 1 零）⇒ 设备回 `SumNum=3`、
	// `Num="3"` —— 设备自己会把零面积条目**当作删除执行、并且不回显**，所以对账侧拿到的
	// 就是平台声明的那个集合（见 `ptz.device_config.go` 的 `diffDeviceConfigBlocks`）。
	if len(regions) > 0 {
		list := &pictureMaskRegionListWire{Num: len(regions)}
		list.Items = make([]pictureMaskRegionItemWire, 0, len(regions))
		for _, region := range regions {
			list.Items = append(list.Items, pictureMaskRegionItemWire{
				Seq: region.Seq, Point: region.PointLiteral(),
			})
		}
		wire.RegionList = list
	}
	return wire
}

func (w *pictureMaskWire) toBlock() *PictureMaskBlock {
	if w == nil {
		return nil
	}
	block := &PictureMaskBlock{On: w.On}
	if w.RegionList == nil {
		return block
	}
	block.Regions = make([]PictureMaskRegion, 0, len(w.RegionList.Items))
	for _, item := range w.RegionList.Items {
		region, ok := parsePictureMaskPoint(item.Seq, item.Point)
		if !ok {
			// ⛔ 宽松收：单条坐标解析不出来时**保留该条的 Seq 并置哨兵坐标**，
			// 而不是丢掉整份配置 —— 丢掉会让平台判"设备不支持"（假阴性），
			// 而真相是"设备回了条不合规的坐标"。
			block.Regions = append(block.Regions, PictureMaskRegion{
				Seq: item.Seq, Left: pictureMaskUnparsable, Right: pictureMaskUnparsable,
				Top: pictureMaskUnparsable, Bottom: pictureMaskUnparsable,
			})
			continue
		}
		block.Regions = append(block.Regions, region)
	}
	return block
}

// pictureMaskUnparsable 是坐标解析失败时的哨兵值（同 frameMirrorUnparsable 口径）。
const pictureMaskUnparsable = -1

// parsePictureMaskPoint 解析 `lx,ly,rx,ry`。返回 false = 非法。
//
// 要求：恰好 4 段整数、都是非负、且 `lx ≤ rx` 且 `ly ≤ ry`。
// 倒置矩形按非法处理 —— 标准明写"左上角、右下角"，倒置的矩形没有定义的含义。
func parsePictureMaskPoint(seq int, literal string) (PictureMaskRegion, bool) {
	parts := strings.Split(literal, ",")
	if len(parts) != 4 {
		return PictureMaskRegion{}, false
	}
	nums := make([]int, 0, 4)
	for _, part := range parts {
		value, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || value < 0 {
			return PictureMaskRegion{}, false
		}
		nums = append(nums, value)
	}
	if nums[0] > nums[2] || nums[1] > nums[3] {
		return PictureMaskRegion{}, false
	}
	return PictureMaskRegion{Seq: seq, Left: nums[0], Top: nums[1], Right: nums[2], Bottom: nums[3]}, true
}

func (b *PictureMaskBlock) validate() error {
	if b == nil {
		return nil
	}
	if b.On != PictureMaskOff && b.On != PictureMaskOn {
		return fmt.Errorf("PictureMask.On 非法: %d（只能 0/1）", b.On)
	}
	if len(b.Regions) > MaxPictureMaskRegions {
		return fmt.Errorf("PictureMask 区域数 %d 超过标准上限 %d", len(b.Regions), MaxPictureMaskRegions)
	}
	seen := make(map[int]struct{}, len(b.Regions))
	for index, region := range b.Regions {
		prefix := fmt.Sprintf("PictureMask 第 %d 个区域", index+1)
		if region.Seq < minPictureMaskSeq || region.Seq > maxPictureMaskSeq {
			return fmt.Errorf("%s: Seq 越界 %d（合法 %d~%d）",
				prefix, region.Seq, minPictureMaskSeq, maxPictureMaskSeq)
		}
		if _, duplicated := seen[region.Seq]; duplicated {
			return fmt.Errorf("%s: Seq %d 重复", prefix, region.Seq)
		}
		seen[region.Seq] = struct{}{}
		for _, coordinate := range []struct {
			name  string
			value int
		}{
			{"Left", region.Left}, {"Top", region.Top}, {"Right", region.Right}, {"Bottom", region.Bottom},
		} {
			if coordinate.value < 0 {
				return fmt.Errorf("%s: %s 不能为负 (%d)", prefix, coordinate.name, coordinate.value)
			}
		}
		if region.Left > region.Right || region.Top > region.Bottom {
			// ⛔ 报错而不是"自动交换"：自动纠正会让下发值与回读值不一致，
			// 而对账只能看到"设备没照做"，归因成本极高。
			return fmt.Errorf("%s: 坐标倒置（要求 左上x ≤ 右下x 且 左上y ≤ 右下y）", prefix)
		}
	}
	return nil
}

// ============================ VideoRecordPlan（A.2.1.15） ============================

// VideoRecordPlan 的标准上限。
const (
	MaxRecordSchedules      = 7 // RecordSchedule maxOccurs="7"
	MaxRecordSegmentsPerDay = 8 // TimeSegment maxOccurs="8"
	minRecordWeekDay        = 1
	maxRecordWeekDay        = 7
)

// RecordTimeSegment 是录像计划里的一个时间段（`TimeSegment`）。
//
// 六个字段都是**必选 integer**：起止的时/分/秒，无日期概念（"每天的这个区间"）。
//
// ⚠️ 标准**没有**规定 `Stop < Start` 时算"跨零点"还是算非法，本层**不解释、如实记账**。
// 要不要把它当跨零点视频段是**上层的业务解释**，协议层替它决定会让对端收到的值与
// 平台看到的值不一致。
type RecordTimeSegment struct {
	StartHour int `json:"startHour"`
	StartMin  int `json:"startMin"`
	StartSec  int `json:"startSec"`
	StopHour  int `json:"stopHour"`
	StopMin   int `json:"stopMin"`
	StopSec   int `json:"stopSec"`
}

// RecordSchedule 是周几的录像计划（`RecordSchedule`）。
//
// `WeekDayNum` 取值 1~7 = 周一到周日。「如当天无录像计划可缺少」⇒ **没有计划的那天
// 就整条不发**，不发一条 `TimeSegment=0` 的空记录，所以 [Segments] 允许为空。
type RecordSchedule struct {
	WeekDayNum int `json:"weekDayNum"`
	// ⛔ 同样不用 omitempty：某天没有时间段 ≠ 这天没有计划条目（后者是整条不发）。
	Segments []RecordTimeSegment `json:"segments"`
}

// VideoRecordPlanBlock 是 `videoRecordPlanCfgType`（A.2.1.15）—— 本族字段数最多的一组。
//
// ⛔ 两个计数（`RecordScheduleSumNum` / `TimeSegmentSumNum`）**不独立存**：
// 分别恒等于 `len(Schedules)` 与 `len(schedule.Segments)`，构建时现算。
// 存成独立字段必然出现"计数说 2 天、实际 1 天"的自相矛盾状态。
type VideoRecordPlanBlock struct {
	RecordEnable int              `json:"recordEnable"`
	Schedules    []RecordSchedule `json:"schedules"`
	StreamNumber int              `json:"streamNumber"`
	// AdditionalStreams 是本次应答里**其他码流**的 VideoRecordPlan 块数。
	//
	// ⛔ `VideoRecordPlan` 带 `StreamNumber`，多码流设备的一条应答里每个码流一块。
	// Go 的 encoding/xml 对指针字段遇到重复元素是**后者覆盖前者**——不报错也不提示，
	// 平台会把子码流的计划当成主码流展示。所以解析侧显式选最小 StreamNumber（主码流），
	// 并把"还有几块别的码流"计数带出来：**让上层能看见，而不是被悄悄替换**。
	// > 0 时前端应提示"仅展示主码流计划，设备另返回 N 个码流的计划未展开"。
	AdditionalStreams int
}

type videoRecordPlanWire struct {
	XMLName              xml.Name             `xml:"VideoRecordPlan"`
	RecordEnable         int                  `xml:"RecordEnable"`
	RecordScheduleSumNum int                  `xml:"RecordScheduleSumNum"`
	Schedules            []recordScheduleWire `xml:"RecordSchedule"`
	// ⛔ StreamNumber 在**最后**（不在 RecordSchedule 里）。顺序写错不会报错，
	// 但"照标准逐行核对"这件事就失效了。
	StreamNumber int `xml:"StreamNumber"`
}

type recordScheduleWire struct {
	WeekDayNum        int                     `xml:"WeekDayNum"`
	TimeSegmentSumNum int                     `xml:"TimeSegmentSumNum"`
	Segments          []recordTimeSegmentWire `xml:"TimeSegment"`
}

type recordTimeSegmentWire struct {
	StartHour int `xml:"StartHour"`
	StartMin  int `xml:"StartMin"`
	StartSec  int `xml:"StartSec"`
	StopHour  int `xml:"StopHour"`
	StopMin   int `xml:"StopMin"`
	StopSec   int `xml:"StopSec"`
}

func (b *VideoRecordPlanBlock) wire() *videoRecordPlanWire {
	if b == nil {
		return nil
	}
	wire := &videoRecordPlanWire{
		RecordEnable: b.RecordEnable, StreamNumber: b.StreamNumber,
		RecordScheduleSumNum: len(b.Schedules),
	}
	for _, schedule := range b.Schedules {
		scheduleWire := recordScheduleWire{
			WeekDayNum: schedule.WeekDayNum, TimeSegmentSumNum: len(schedule.Segments),
		}
		for _, segment := range schedule.Segments {
			scheduleWire.Segments = append(scheduleWire.Segments, recordTimeSegmentWire{
				StartHour: segment.StartHour, StartMin: segment.StartMin, StartSec: segment.StartSec,
				StopHour: segment.StopHour, StopMin: segment.StopMin, StopSec: segment.StopSec,
			})
		}
		wire.Schedules = append(wire.Schedules, scheduleWire)
	}
	return wire
}

func (w *videoRecordPlanWire) toBlock() *VideoRecordPlanBlock {
	if w == nil {
		return nil
	}
	block := &VideoRecordPlanBlock{RecordEnable: w.RecordEnable, StreamNumber: w.StreamNumber}
	for _, scheduleWire := range w.Schedules {
		schedule := RecordSchedule{WeekDayNum: scheduleWire.WeekDayNum}
		for _, segmentWire := range scheduleWire.Segments {
			schedule.Segments = append(schedule.Segments, RecordTimeSegment{
				StartHour: segmentWire.StartHour, StartMin: segmentWire.StartMin, StartSec: segmentWire.StartSec,
				StopHour: segmentWire.StopHour, StopMin: segmentWire.StopMin, StopSec: segmentWire.StopSec,
			})
		}
		block.Schedules = append(block.Schedules, schedule)
	}
	return block
}

func (b *VideoRecordPlanBlock) validate() error {
	if b == nil {
		return nil
	}
	if b.RecordEnable != AlarmReportOff && b.RecordEnable != AlarmReportOn {
		return fmt.Errorf("VideoRecordPlan.RecordEnable 非法: %d（只能 0/1）", b.RecordEnable)
	}
	if b.StreamNumber < 0 {
		return fmt.Errorf("VideoRecordPlan.StreamNumber 不能为负: %d（0=主码流）", b.StreamNumber)
	}
	if len(b.Schedules) > MaxRecordSchedules {
		return fmt.Errorf("VideoRecordPlan 计划天数 %d 超过标准上限 %d", len(b.Schedules), MaxRecordSchedules)
	}
	seenDays := make(map[int]struct{}, len(b.Schedules))
	for _, schedule := range b.Schedules {
		if schedule.WeekDayNum < minRecordWeekDay || schedule.WeekDayNum > maxRecordWeekDay {
			return fmt.Errorf("VideoRecordPlan 的 WeekDayNum 越界: %d（合法 %d~%d，1=周一）",
				schedule.WeekDayNum, minRecordWeekDay, maxRecordWeekDay)
		}
		if _, duplicated := seenDays[schedule.WeekDayNum]; duplicated {
			return fmt.Errorf("VideoRecordPlan 的周 %d 计划重复出现", schedule.WeekDayNum)
		}
		seenDays[schedule.WeekDayNum] = struct{}{}
		if len(schedule.Segments) > MaxRecordSegmentsPerDay {
			return fmt.Errorf("VideoRecordPlan 周 %d 的时段数 %d 超过标准上限 %d",
				schedule.WeekDayNum, len(schedule.Segments), MaxRecordSegmentsPerDay)
		}
		for index, segment := range schedule.Segments {
			prefix := fmt.Sprintf("VideoRecordPlan 周 %d 第 %d 段", schedule.WeekDayNum, index+1)
			for _, field := range []struct {
				name  string
				value int
				max   int
			}{
				{"StartHour", segment.StartHour, 23}, {"StartMin", segment.StartMin, 59},
				{"StartSec", segment.StartSec, 59}, {"StopHour", segment.StopHour, 23},
				{"StopMin", segment.StopMin, 59}, {"StopSec", segment.StopSec, 59},
			} {
				if field.value < 0 || field.value > field.max {
					return fmt.Errorf("%s 的 %s 越界: %d（要求 0~%d）",
						prefix, field.name, field.value, field.max)
				}
			}
		}
	}
	return nil
}

// ---- 内部工具 ----

// trimOptional 修剪可选字符串指针，空串归一成 nil。
//
// ⛔ 空串与 nil 在协议层是**两件事**：nil = 元素缺席，空串 = 元素在场但没有内容。
// 设备回了 `<Longitude></Longitude>` 时应收成 nil（"没给"），而不是 `""`。
func trimOptional(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

package manscdp

import (
	"encoding/xml"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
)

// 设备配置族的两个 CmdType。
//
// ⭐ 与 SDCardStatus 一样，这里的**请求与应答 CmdType 同名**：
//   - 读 = `<Query><CmdType>ConfigDownload</CmdType>…` → 应答 `<Response><CmdType>ConfigDownload</CmdType>…`
//   - 写 = `<Control><CmdType>DeviceConfig</CmdType>…` → 应答 `<Response><CmdType>DeviceConfig</CmdType>…`
//
// 所以两个常量同时用于组建请求和识别应答，不存在 `…Query` / `…Response` 后缀写法。
const (
	CmdDeviceConfig   = "DeviceConfig"
	CmdConfigDownload = "ConfigDownload"
)

// 配置类型（`ConfigType` 元素取值）。
//
// ⛔ 前四个是 2016 就有的；后八个是 **2022 新增**。2016 设备收到 2022 独有的
// 配置类型时**标准未定义其行为** —— 这条事实决定了门禁策略（见
// docs/gb28181-2022-device-config-ambiguity.md §六 与
// docs/gb28181-2022-video-param-attribute-panel.md §十）。
//
// 全部列出来是为了让「平台认识哪些类型」这件事有一个可引用的单一出处，
// 而不是散落在各控制器里做字符串比较。
const (
	ConfigTypeBasicParam       = "BasicParam"
	ConfigTypeVideoParamOpt    = "VideoParamOpt"
	ConfigTypeSVACEncodeConfig = "SVACEncodeConfig"
	ConfigTypeSVACDecodeConfig = "SVACDecodeConfig"

	ConfigTypeVideoParamAttribute = "VideoParamAttribute"
	ConfigTypeVideoRecordPlan     = "VideoRecordPlan"
	ConfigTypeVideoAlarmRecord    = "VideoAlarmRecord"
	ConfigTypePictureMask         = "PictureMask"
	ConfigTypeFrameMirror         = "FrameMirror"
	ConfigTypeAlarmReport         = "AlarmReport"
	ConfigTypeOSDConfig           = "OSDConfig"
	ConfigTypeSnapShotConfig      = "SnapShotConfig"
)

// 附录 G（标准页 130）`f=v/编码格式/分辨率/帧率/码率类型/码率大小` 的码值。
//
// ⛔ 发的是**数字字符串**，不是 `H.264` / `720P` / `CBR` 这些人读串。
// 人读串只允许出现在前端展示层。
const (
	VideoFormatMPEG4 = "1"
	VideoFormatH264  = "2"
	VideoFormatSVAC  = "3"
	VideoFormat3GP   = "4"
	VideoFormatH265  = "5"

	ResolutionQCIF  = "1"
	ResolutionCIF   = "2"
	Resolution4CIF  = "3"
	ResolutionD1    = "4"
	Resolution720P  = "5"
	Resolution1080P = "6"

	BitRateTypeCBR = "1"
	BitRateTypeVBR = "2"
)

// 附录 G 里帧率与码率的取值边界。
const (
	minFrameRate    = 0
	maxFrameRate    = 99
	minVideoBitRate = 0
	maxVideoBitRate = 100000
)

// resolutionWxHRe 是附录 G 的「其余分辨率用 WxH 表示」形式。
//
// ⛔ 必须是**小写 x**（标准原文 `WxH`，其中 W/H 为十进制数）。不接受 `*`：
// 目录 `Info/Resolution` 里设备用的是 `1920*1080`（那是设备自报的字符串属性），
// 而本处是平台按标准拼给设备的码值，两回事，不能互相宽容。
// ⛔ 不允许前导零与 0 值：`0640x480` / `0x0` 都不是合法的分辨率表示。
var resolutionWxHRe = regexp.MustCompile(`^[1-9][0-9]*x[1-9][0-9]*$`)

// VideoParamItem 是 `videoParamAttributeCfgType/Item` 的一条码流配置（A.2.1.13）。
//
// 五个取值字段**一律用 string 承载**（标准的 XSD 类型就是 string），
// 好处是「解析回读」与「构造下发」共用同一个类型，不存在码值 ↔ 人读串两套表示：
// 对账时比的就是设备原样给的字符串。
//
// ⛔ StreamNumber 用 int：它决定了落库行的唯一键与面板的分段，结构上必须能比较；
// 其余五个是**取值**问题，宽松收（设备回什么收什么，是否合规由对账/校验判定）。
type VideoParamItem struct {
	// StreamNumber 标准明文：0=主码流；1=子码流1；2=子码流2…
	StreamNumber int    `json:"streamNumber"`
	VideoFormat  string `json:"videoFormat"`
	Resolution   string `json:"resolution"`
	// FrameRate 附录 G 取值 0~99。
	FrameRate string `json:"frameRate"`
	// BitRateType 附录 G 取值 1=固定码率(CBR) / 2=可变码率(VBR)。
	BitRateType string `json:"bitRateType"`
	// VideoBitRate 是**条件必选**：仅在 BitRateType=1(CBR) 时出现，单位 kb/s。
	// nil 表示"这一帧没有这个元素"，与"给了个 0"是两件事 —— 标准的注释写的是
	// 「固定码率时必选」，所以 VBR 时它本就该缺席。
	//
	// ⚠️ 这个类型会整体序列化进 operation 的 payload_json（供 scheduler 在重试时
	// 重建报文），所以下面的 json tag 属于**内部持久化契约**，改名等于让在途
	// operation 的重试失败。XML 侧另有 videoParamItemXML，两者互不影响。
	VideoBitRate *string `json:"videoBitRate"`
}

// VideoParamAttributeBlock 是应答里 `<VideoParamAttribute>` 这个块。
type VideoParamAttributeBlock struct {
	// Num 是元素**属性**（不是 SumNum 那种子元素），标准里表示配置条数。
	Num int
	// NumPresent 区分「Num="0"」与「设备压根没写 Num 属性」。
	NumPresent bool
	Items      []VideoParamItem
}

// ConfigDownloadResult 是一次 `ConfigDownload` 应答的解析结果。
type ConfigDownloadResult struct {
	CmdType  string
	SN       int
	DeviceID string
	// Result 是应答里的 Result 元素。A.2.6.9 标了必选，但现场设备习惯性漏发，
	// 所以**缺失不报错**（同 ParseSDCardStatusResponse 对 Result 的处理），
	// 空串即"设备没给"。调用方判"设备拒绝"要认显式的 ERROR。
	Result string
	// VideoParamAttribute 为 nil 表示这一帧**没有**带该配置类型。
	// ⛔ 这是"设备不支持该配置类型"最可靠的判据（规格 §十④）：
	// 比 profile 登记值可靠得多，因为它是设备的实际回答。
	// 当解析时指定了 wantType 而块缺席，Parse 会直接返回 ConfigErrorTypeAbsent。
	VideoParamAttribute *VideoParamAttributeBlock
}

// ConfigDownloadErrorCode 是解析失败的分类。调用方据此区分
// 「报文坏了」「设备拒了」「设备不支持」三种完全不同的处置。
type ConfigDownloadErrorCode string

const (
	ConfigErrorMalformed   ConfigDownloadErrorCode = "malformed"
	ConfigErrorCmdType     ConfigDownloadErrorCode = "cmd_type"
	ConfigErrorCorrelation ConfigDownloadErrorCode = "correlation"
	// ConfigErrorTypeAbsent 应答本身合法，但**没带请求的配置类型**。
	// ⇒ 等价于「设备不支持该配置类型」，是 §十④ 里最可靠的那条判据。
	ConfigErrorTypeAbsent ConfigDownloadErrorCode = "type_absent"
	// ConfigErrorUnsupportedType 调用方请求了本平台尚未实现的配置类型。
	ConfigErrorUnsupportedType ConfigDownloadErrorCode = "unsupported_type"
)

type ConfigDownloadError struct {
	Code ConfigDownloadErrorCode
	Msg  string
}

func (e *ConfigDownloadError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("DeviceConfig/ConfigDownload parse %s: %s", e.Code, e.Msg)
}

// ConfigDownloadExpectation 把应答与本地 operation 对齐。
type ConfigDownloadExpectation struct {
	SN       int
	DeviceID string
}

// ---- 线格式 ----

type videoParamAttributeXML struct {
	// Num 是属性。⛔ 不要改成元素：标准把它挂在 complexType 上，
	// 写成 <Num> 会被对端当成未知子元素忽略。
	Num   int                 `xml:"Num,attr"`
	Items []videoParamItemXML `xml:"Item"`
}

type videoParamItemXML struct {
	StreamNumber int     `xml:"StreamNumber"`
	VideoFormat  string  `xml:"VideoFormat"`
	Resolution   string  `xml:"Resolution"`
	FrameRate    string  `xml:"FrameRate"`
	BitRateType  string  `xml:"BitRateType"`
	VideoBitRate *string `xml:"VideoBitRate,omitempty"`
}

type deviceConfigXML struct {
	XMLName  xml.Name `xml:"Control"`
	CmdType  string   `xml:"CmdType"`
	SN       int      `xml:"SN"`
	DeviceID string   `xml:"DeviceID"`
	// ⛔ 不用 omitempty：**空配置是合法的**（`Num="0"` 且无 Item，见 A.2.1.13
	// 的 Item minOccurs="0"）。省掉这个元素会让设备分不清"清空"与"没这段"。
	VideoParamAttribute videoParamAttributeXML `xml:"VideoParamAttribute"`
}

type configDownloadQueryXML struct {
	XMLName    xml.Name `xml:"Query"`
	CmdType    string   `xml:"CmdType"`
	SN         int      `xml:"SN"`
	DeviceID   string   `xml:"DeviceID"`
	ConfigType string   `xml:"ConfigType"`
}

type configDownloadResponseWire struct {
	XMLName             xml.Name                 `xml:"Response"`
	CmdType             string                   `xml:"CmdType"`
	SN                  int                      `xml:"SN"`
	DeviceID            string                   `xml:"DeviceID"`
	Result              string                   `xml:"Result"`
	VideoParamAttribute *videoParamAttributeWire `xml:"VideoParamAttribute"`
}

type videoParamAttributeWire struct {
	Num   *int                 `xml:"Num,attr"`
	Items []videoParamItemWire `xml:"Item"`
}

type videoParamItemWire struct {
	StreamNumber string `xml:"StreamNumber"`
	VideoFormat  string `xml:"VideoFormat"`
	Resolution   string `xml:"Resolution"`
	FrameRate    string `xml:"FrameRate"`
	BitRateType  string `xml:"BitRateType"`
	VideoBitRate string `xml:"VideoBitRate"`
}

// ---- 组建 ----

// BuildConfigDownloadQuery 用 2016 profile 序列化。保留这个"不带 profile"的
// 重载只为与本包其它 Build* 保持一致；平台实际总是走 WithProfile 版本。
func BuildConfigDownloadQuery(deviceID string, sn int, configTypes []string) ([]byte, error) {
	return BuildConfigDownloadQueryWithProfile(protocol.ProfileFor(protocol.Version2016), deviceID, sn, configTypes)
}

// BuildConfigDownloadQueryWithProfile 序列化 A.2.4.7 的读取查询。
//
// profile 只决定字符集与 XML 声明，**不做版本门禁**：读是查询，发出去没有副作用，
// 而它恰好是平台发现"设备到底认不认这个配置类型"的手段（同 BuildSDCardStatusQueryWithProfile）。
// 多类型用 `/` 连接 —— 标准明文允许，且 A.2.4.7 提醒「可返回与查询 SN 值相同的
// 多个响应，每个响应对应一个配置类型」，接收侧必须按 SN 收集多条而不是一问一答。
func BuildConfigDownloadQueryWithProfile(profile protocol.Profile, deviceID string, sn int, configTypes []string) ([]byte, error) {
	if err := validatePTZQueryTarget(deviceID, sn); err != nil {
		return nil, err
	}
	joined, err := joinConfigTypes(configTypes)
	if err != nil {
		return nil, err
	}
	return MarshalProfiledXML(profile, configDownloadQueryXML{
		CmdType: CmdConfigDownload, SN: sn, DeviceID: deviceID, ConfigType: joined,
	})
}

// joinConfigTypes 把配置类型列表拼成 `/` 分隔的单个元素值。
func joinConfigTypes(configTypes []string) (string, error) {
	if len(configTypes) == 0 {
		// 标准里 ConfigType 是必选的查询条件，且没有定义"不写即查全部"的语义。
		// 空列表在这里报错，好过发一个语义靠猜的报文。
		return "", fmt.Errorf("ConfigType 不能为空")
	}
	seen := make(map[string]struct{}, len(configTypes))
	parts := make([]string, 0, len(configTypes))
	for _, configType := range configTypes {
		trimmed := strings.TrimSpace(configType)
		if trimmed == "" {
			return "", fmt.Errorf("ConfigType 含空项")
		}
		if strings.Contains(trimmed, "/") {
			// `/` 是分隔符，元素值里出现它会把一个类型拆成两个。
			return "", fmt.Errorf("ConfigType 不能包含分隔符 /: %q", trimmed)
		}
		if _, duplicated := seen[trimmed]; duplicated {
			continue
		}
		seen[trimmed] = struct{}{}
		parts = append(parts, trimmed)
	}
	return strings.Join(parts, "/"), nil
}

// BuildVideoParamAttributeConfig 用 2016 profile 序列化。
func BuildVideoParamAttributeConfig(deviceID string, sn int, items []VideoParamItem) ([]byte, error) {
	return BuildVideoParamAttributeConfigWithProfile(protocol.ProfileFor(protocol.Version2016), deviceID, sn, items)
}

// BuildVideoParamAttributeConfigWithProfile 序列化 A.2.3.2.5 的写入命令。
//
// ⛔ 这里**不做版本门禁**，理由见 docs/gb28181-2022-video-param-attribute-panel.md §十③：
// `VideoParamAttribute` 是 2022 新增，但门禁只拦"平台自己决定要发"的场合；
// 操作员手点的那一次**必须放行** —— 被误登记成 2016 的真 2022 设备，
// 不试一次就永远用不了这功能。真相由**强制回读对账**暴露，不由 profile 断言。
//
// ⛔ 与解析侧的"宽松收"相反，这里是**严格发**：平台自己发出的取值必须落在
// 附录 G 的取值域内，否则对端会静默当 0 处理，而这种错在回读对账时表现为
// "设备没照做"，归因成本极高。见 ValidateVideoParamItems。
func BuildVideoParamAttributeConfigWithProfile(profile protocol.Profile, deviceID string, sn int, items []VideoParamItem) ([]byte, error) {
	if err := validatePTZQueryTarget(deviceID, sn); err != nil {
		return nil, err
	}
	if err := ValidateVideoParamItems(items); err != nil {
		return nil, err
	}

	block := videoParamAttributeXML{Num: len(items)}
	if len(items) > 0 {
		block.Items = make([]videoParamItemXML, 0, len(items))
		for _, item := range items {
			block.Items = append(block.Items, videoParamItemXML{
				StreamNumber: item.StreamNumber,
				VideoFormat:  item.VideoFormat,
				Resolution:   item.Resolution,
				FrameRate:    item.FrameRate,
				BitRateType:  item.BitRateType,
				// VBR 时必须缺席（Validate 已保证此处为 nil）。
				VideoBitRate: item.VideoBitRate,
			})
		}
	}
	return MarshalProfiledXML(profile, deviceConfigXML{
		CmdType: CmdDeviceConfig, SN: sn, DeviceID: deviceID, VideoParamAttribute: block,
	})
}

// ValidateVideoParamItems 校验平台**将要下发**的码流配置。
//
// 与解析侧的分工：这里是"严格发"，解析侧是"宽松收"。规则出处一律是
// A.2.1.13 的必选性 + 附录 G 的取值表，不自行加严也不放宽。
func ValidateVideoParamItems(items []VideoParamItem) error {
	seen := make(map[int]struct{}, len(items))
	for index, item := range items {
		prefix := fmt.Sprintf("第 %d 条码流配置", index+1)
		if item.StreamNumber < 0 {
			return fmt.Errorf("%s: StreamNumber 不能为负 (%d)", prefix, item.StreamNumber)
		}
		if _, duplicated := seen[item.StreamNumber]; duplicated {
			// 同一份配置里出现两条同码流，设备侧行为未定义（可能后者覆盖前者，
			// 也可能报错）。宁可在发出前拦下。
			return fmt.Errorf("%s: StreamNumber %d 重复", prefix, item.StreamNumber)
		}
		seen[item.StreamNumber] = struct{}{}

		if !isVideoFormatCode(item.VideoFormat) {
			return fmt.Errorf("%s: VideoFormat 取值非法 %q（附录 G 要求 1~5）", prefix, item.VideoFormat)
		}
		if !IsValidResolution(item.Resolution) {
			return fmt.Errorf("%s: Resolution 取值非法 %q（附录 G 要求 1~6 或 WxH）", prefix, item.Resolution)
		}
		if err := validateBoundedInt("FrameRate", item.FrameRate, minFrameRate, maxFrameRate); err != nil {
			return fmt.Errorf("%s: %w", prefix, err)
		}
		if !isBitRateTypeCode(item.BitRateType) {
			return fmt.Errorf("%s: BitRateType 取值非法 %q（附录 G 要求 1=CBR / 2=VBR）", prefix, item.BitRateType)
		}

		switch item.BitRateType {
		case BitRateTypeCBR:
			// 标准注释原文：「视频码率配置值（固定码率时必选）」。
			if item.VideoBitRate == nil {
				return fmt.Errorf("%s: 固定码率(CBR)时 VideoBitRate 必选", prefix)
			}
			if err := validateBoundedInt("VideoBitRate", *item.VideoBitRate, minVideoBitRate, maxVideoBitRate); err != nil {
				return fmt.Errorf("%s: %w (单位 kb/s)", prefix, err)
			}
		case BitRateTypeVBR:
			// ⛔ 报错而不是静默丢弃：静默丢弃会让调用方以为"发了"，
			// 而回读对账只能看到"值不一致"，归因成本极高。
			if item.VideoBitRate != nil {
				return fmt.Errorf("%s: 可变码率(VBR)时不应携带 VideoBitRate（标准里它条件必选）", prefix)
			}
		}
	}
	return nil
}

// isVideoFormatCode / isBitRateTypeCode / IsValidResolution 是附录 G 取值域的
// 单一出处，前端与服务端的提示文案都引用它们，避免同一张码表写两遍后分家。
func isVideoFormatCode(value string) bool {
	switch strings.TrimSpace(value) {
	case VideoFormatMPEG4, VideoFormatH264, VideoFormatSVAC, VideoFormat3GP, VideoFormatH265:
		return true
	default:
		return false
	}
}

func isBitRateTypeCode(value string) bool {
	switch strings.TrimSpace(value) {
	case BitRateTypeCBR, BitRateTypeVBR:
		return true
	default:
		return false
	}
}

// IsValidResolution 报告取值是否落在附录 G 的分辨率表示法内：
// 六个附表码值 `1`~`6`，或「其余分辨率」的 `WxH`。
func IsValidResolution(value string) bool {
	switch strings.TrimSpace(value) {
	case ResolutionQCIF, ResolutionCIF, Resolution4CIF, ResolutionD1, Resolution720P, Resolution1080P:
		return true
	}
	return resolutionWxHRe.MatchString(strings.TrimSpace(value))
}

// validateBoundedInt 校验一个"十进制整数字符串"是否在闭区间内。
// 非数字不是"0"，而是取值非法 —— 报错而不是兜底。
func validateBoundedInt(field, value string, min, max int) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("%s 缺失", field)
	}
	number, err := strconv.Atoi(trimmed)
	if err != nil {
		return fmt.Errorf("%s 非十进制整数: %q", field, value)
	}
	if number < min || number > max {
		return fmt.Errorf("%s 越界: %d（要求 %d~%d）", field, number, min, max)
	}
	return nil
}

// ---- 解析 ----

// ParseConfigDownloadResponse 解析 A.2.6.9 的读取应答。
//
// wantType 是本次请求的配置类型（如 ConfigTypeVideoParamAttribute）：
//   - 给定且应答里**没有**该元素 ⇒ 返回 ConfigErrorTypeAbsent 类型的错误。
//     这是刻意的：规格 §十④ 把"回读为空"确立为「设备不支持该配置类型」最可靠的
//     判据，绝不能因为"没解析到东西"就返回一个空结果让调用方以为一切正常。
//   - 传空串 ⇒ 不判缺席，只解析骨架（用于"只想知道设备回没回 OK"的场合）。
//
// ⛔ 取值**不做范围内校验**（宽松收）：设备回了 `H.264` 这种不合附录 G 的写法，
// 也要原样带上来，让对账把它显示成"值不一致"。若在这里报错，整份应答会被丢掉，
// 用户看到的是"协议错误"，而真相是"设备回了个人读串"。
// 唯一的例外是 StreamNumber：它是结构字段，缺了就无法分段与落库，按 malformed 拒。
func ParseConfigDownloadResponse(body []byte, wantType string) (*ConfigDownloadResult, error) {
	var wire configDownloadResponseWire
	if err := DecodeProfiledXML(protocol.ProfileFor(protocol.Version2016), body, &wire); err != nil {
		return nil, &ConfigDownloadError{Code: ConfigErrorMalformed, Msg: err.Error()}
	}
	if wire.CmdType != CmdConfigDownload {
		return nil, &ConfigDownloadError{Code: ConfigErrorCmdType, Msg: fmt.Sprintf("got %q", wire.CmdType)}
	}
	if wire.SN <= 0 || strings.TrimSpace(wire.DeviceID) == "" {
		return nil, &ConfigDownloadError{Code: ConfigErrorMalformed, Msg: "SN 或 DeviceID 缺失"}
	}

	result := &ConfigDownloadResult{
		CmdType:  wire.CmdType,
		SN:       wire.SN,
		DeviceID: strings.TrimSpace(wire.DeviceID),
		Result:   strings.TrimSpace(wire.Result),
	}
	if block := wire.VideoParamAttribute; block != nil {
		parsed, err := parseVideoParamAttributeBlock(block)
		if err != nil {
			return nil, err
		}
		result.VideoParamAttribute = parsed
	}

	trimmedWant := strings.TrimSpace(wantType)
	if trimmedWant == "" {
		return result, nil
	}
	// ⛔ 「设备回了 Result=ERROR」与「设备没带这个元素」是两件事：
	// 前者是设备明确拒绝（调用方看 Result），后者才是"不支持该类型"。
	// 所以 Result=ERROR 时不在这里报 type_absent，交给上层按结果分流。
	if !strings.EqualFold(result.Result, "ERROR") {
		if err := ensureConfigTypePresent(trimmedWant, result); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// ParseConfigDownloadResponseFor 在解析基础上核对 SN / DeviceID。
func ParseConfigDownloadResponseFor(body []byte, expectation ConfigDownloadExpectation, wantType string) (*ConfigDownloadResult, error) {
	result, err := ParseConfigDownloadResponse(body, wantType)
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

// DeviceConfigAck 是 A.2.6.8 的写入应答。
//
// ⛔ 它**没有回显**：只有 CmdType / SN / DeviceID / Result 四个元素。
// 这就是本卡必须自己做「下发 → 自动回读 → 逐格对账」的根本原因 ——
// `Result=OK` 只说明"收到并接受"，既不代表值生效，也回显不了你到底发了什么。
type DeviceConfigAck struct {
	CmdType  string
	SN       int
	DeviceID string
	// Result 只有 "OK" / "ERROR" 两个取值。⚠️ 大小写按标准是大写；
	// 设备若回了小写也要能认（见 ParseDeviceConfigResponse 的 EqualFold）。
	Result string
}

// Accepted 报告设备是否明确接受了这次配置。
func (a *DeviceConfigAck) Accepted() bool {
	return a != nil && strings.EqualFold(strings.TrimSpace(a.Result), "OK")
}

type deviceConfigResponseWire struct {
	XMLName  xml.Name `xml:"Response"`
	CmdType  string   `xml:"CmdType"`
	SN       int      `xml:"SN"`
	DeviceID string   `xml:"DeviceID"`
	Result   string   `xml:"Result"`
}

// ParseDeviceConfigResponse 解析 A.2.6.8。
//
// ⛔ Result 缺失按 malformed 拒，而不是当成"非 OK"放手边等超时：
// 写应答的全部意义就是传 Result，缺了它这份应答没有任何信息量。
// 立即判失败（上层落 rejected + 协议错误）比烧完三次重试预算更省事，也更可解释。
func ParseDeviceConfigResponse(body []byte) (*DeviceConfigAck, error) {
	var wire deviceConfigResponseWire
	if err := DecodeProfiledXML(protocol.ProfileFor(protocol.Version2016), body, &wire); err != nil {
		return nil, &ConfigDownloadError{Code: ConfigErrorMalformed, Msg: err.Error()}
	}
	if wire.CmdType != CmdDeviceConfig {
		return nil, &ConfigDownloadError{Code: ConfigErrorCmdType, Msg: fmt.Sprintf("got %q", wire.CmdType)}
	}
	if wire.SN <= 0 || strings.TrimSpace(wire.DeviceID) == "" {
		return nil, &ConfigDownloadError{Code: ConfigErrorMalformed, Msg: "SN 或 DeviceID 缺失"}
	}
	result := strings.TrimSpace(wire.Result)
	if result == "" {
		return nil, &ConfigDownloadError{Code: ConfigErrorMalformed, Msg: "Result 缺失（写入应答没有回显，Result 是唯一的信息）"}
	}
	return &DeviceConfigAck{
		CmdType: wire.CmdType, SN: wire.SN, DeviceID: strings.TrimSpace(wire.DeviceID), Result: result,
	}, nil
}

// ParseDeviceConfigResponseFor 在解析基础上核对 SN / DeviceID。
func ParseDeviceConfigResponseFor(body []byte, expectation ConfigDownloadExpectation) (*DeviceConfigAck, error) {
	ack, err := ParseDeviceConfigResponse(body)
	if err != nil {
		return nil, err
	}
	if expectation.SN > 0 && ack.SN != expectation.SN {
		return nil, &ConfigDownloadError{
			Code: ConfigErrorCorrelation, Msg: fmt.Sprintf("SN got %d want %d", ack.SN, expectation.SN),
		}
	}
	if want := strings.TrimSpace(expectation.DeviceID); want != "" && ack.DeviceID != want {
		return nil, &ConfigDownloadError{
			Code: ConfigErrorCorrelation, Msg: fmt.Sprintf("DeviceID got %q want %q", ack.DeviceID, want),
		}
	}
	return ack, nil
}

// ensureConfigTypePresent 核对请求的配置类型是否出现在这一帧里。
func ensureConfigTypePresent(wantType string, result *ConfigDownloadResult) error {
	switch wantType {
	case ConfigTypeVideoParamAttribute:
		if result.VideoParamAttribute != nil {
			return nil
		}
		return &ConfigDownloadError{
			Code: ConfigErrorTypeAbsent,
			Msg:  "应答未携带 VideoParamAttribute（设备不支持该配置类型）",
		}
	default:
		// 本平台尚未实现其它配置类型的解析。⛔ 报错而不是静默放过：
		// 静默会让调用方把"我们没实现"误读成"设备没给"。
		return &ConfigDownloadError{
			Code: ConfigErrorUnsupportedType,
			Msg:  fmt.Sprintf("平台尚未实现配置类型 %q 的解析", wantType),
		}
	}
}

func parseVideoParamAttributeBlock(block *videoParamAttributeWire) (*VideoParamAttributeBlock, error) {
	parsed := &VideoParamAttributeBlock{Items: []VideoParamItem{}}
	if block.Num != nil {
		parsed.NumPresent = true
		parsed.Num = *block.Num
	}
	// ⛔ Items 不设上限：标准里 `Item` 是 maxOccurs="unbounded"（与存储卡的
	// maxOccurs=8 不同），设了上限就会误拒合规设备。
	for index, raw := range block.Items {
		streamNumber, err := parseRequiredInt("StreamNumber", raw.StreamNumber)
		if err != nil {
			return nil, &ConfigDownloadError{
				Code: ConfigErrorMalformed, Msg: fmt.Sprintf("第 %d 个 Item: %s", index+1, err.Error()),
			}
		}
		if streamNumber < 0 {
			return nil, &ConfigDownloadError{
				Code: ConfigErrorMalformed, Msg: fmt.Sprintf("第 %d 个 Item 的 StreamNumber 为负: %d", index+1, streamNumber),
			}
		}
		item := VideoParamItem{
			StreamNumber: streamNumber,
			VideoFormat:  strings.TrimSpace(raw.VideoFormat),
			Resolution:   strings.TrimSpace(raw.Resolution),
			FrameRate:    strings.TrimSpace(raw.FrameRate),
			BitRateType:  strings.TrimSpace(raw.BitRateType),
		}
		if bitRate := strings.TrimSpace(raw.VideoBitRate); bitRate != "" {
			item.VideoBitRate = &bitRate
		}
		parsed.Items = append(parsed.Items, item)
	}
	// ⭐ Num 与 len(Items) 不一致时**刻意不报错**：Num 是设备自己声明的一个数，
	// 而 Items 是它实际给的内容。以 Items 为准（分段与落库都靠它），
	// 同时把 Num 原样带上来，让上层有机会发现"设备自相矛盾"。
	return parsed, nil
}

func parseRequiredInt(field, value string) (int, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, fmt.Errorf("%s 缺失", field)
	}
	number, err := strconv.Atoi(trimmed)
	if err != nil {
		return 0, fmt.Errorf("%s 非整数: %q", field, value)
	}
	return number, nil
}

// VideoParamItems 是 nil 安全的取用入口，省去调用方逐处判空。
func (r *ConfigDownloadResult) VideoParamItems() []VideoParamItem {
	if r == nil || r.VideoParamAttribute == nil {
		return nil
	}
	return r.VideoParamAttribute.Items
}

// HasVideoParamAttribute 报告这一帧是否带了视频参数配置元素。
//
// ⛔ 不要用 `len(VideoParamItems()) > 0` 代替它：带元素但一条 Item 都没有，
// 是"设备明确回了空配置"，与"设备不支持"含义完全不同。
func (r *ConfigDownloadResult) HasVideoParamAttribute() bool {
	return r != nil && r.VideoParamAttribute != nil
}

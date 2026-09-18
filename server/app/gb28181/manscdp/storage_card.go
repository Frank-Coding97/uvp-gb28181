package manscdp

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"

	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
)

// CmdSDCardStatus is GB/T 28181-2022 A.2.4.14「存储卡状态查询」。
//
// ⭐ 请求与应答 **CmdType 同名**（都是 fixed="SDCardStatus"），这一点与
// HomePositionQuery / CruiseTrackListQuery 那族"查询问、应答答"的命名习惯
// 不同 —— 标准在这里把两边的 CmdType 都写成了 SDCardStatus，没有
// `SDCardStatusQuery` / `SDCardStatusResponse` 这种后缀写法。
//
// ⛔ 2022 新增：2016 版附录 A 中既没有 SDCardStatus 也没有「存储卡」字样
// （已对 2016 OCR 全文核对，零命中）。所以它和 HomePositionQuery 同属
// "2022 才有"的查询家族。
//
// ⛔ 模拟器早期实现自造过 `StorageCardStatusQuery` + `<StorageList>`/
// `<CardNum>`/`<TotalCapacity>` 这一套（既非标准、字段名也对不上），
// 平台侧**只认标准写法**，详见 LoadStorageCardResponse 的字段注释。
const CmdSDCardStatus = "SDCardStatus"

// SDCardState 是 A.2.6.16 里 Status 元素的取值域。标准原文写的是小写，
// 这里原样保留小写常量；解析时大小写不敏感（见 parseSDCardState）。
type SDCardState string

const (
	SDCardStateOK          SDCardState = "ok"
	SDCardStateFormatting  SDCardState = "formatting"
	SDCardStateUnformatted SDCardState = "unformatted"
	SDCardStateIdle        SDCardState = "idle"
	SDCardStateError       SDCardState = "error"
	SDCardStateUnknown     SDCardState = "unknown"
)

// maxSDCardItems 对应 A.2.6.16 里 `<element name="Item" minOccurs="0" maxOccurs="8">`。
// 超出即视为协议违规：设备最多只能报 8 张卡，多出来的条目不是"多几行展示"
// 的问题，而是对不上标准的报文，宁可按解析失败拒掉（调用方会记 rejected），
// 也不要把它当成一个"部分成功"的查询结果落库。
const maxSDCardItems = 8

// SDCardStatusExpectation 用于把应答与本地 operation 对齐。SN 与 DeviceID
// 任一不符都说明这一帧答的不是我们刚才问的那件事。
type SDCardStatusExpectation struct {
	SN       int
	DeviceID string
}

type SDCardItem struct {
	// ID 是「SD卡编号」，标准里 type="integer"。注意它从 1 开始编号
	// （A.2.3.1.13 FormatSDCard 的 DiskNum 也是同一套编号，0 表示"所有卡"）。
	ID int
	// HddName 元素名就叫 HddName（标准原文如此，虽然内容装的是 SD 卡名）。
	HddName string
	Status  SDCardState
	// FormatProgress 是可选字段：只在 Status=formatting 时有意义，0-100。
	// 标准标了 minOccurs="0"，所以必须用指针区分"设备没给"与"给了 0"。
	FormatProgress *int
	// Capacity / FreeSpace 单位都是 MB。
	Capacity  int
	FreeSpace int
}

type SDCardStatus struct {
	CmdType  string
	SN       int
	DeviceID string
	// SumNum 是标准里的必选字段「查询结果总数」。注意它与 Items 的真实条数
	// 可能不一致（设备报 0 张卡时 SumNum=0 且不带任何 Item）。
	SumNum int
	Items  []SDCardItem
}

type SDCardErrorCode string

const (
	SDCardErrorMalformed    SDCardErrorCode = "malformed"
	SDCardErrorCmdType      SDCardErrorCode = "cmd_type"
	SDCardErrorCorrelation  SDCardErrorCode = "correlation"
	SDCardErrorTooManyItems SDCardErrorCode = "too_many_items"
)

type SDCardParseError struct {
	Code SDCardErrorCode
	Msg  string
}

func (e *SDCardParseError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("SDCardStatus parse %s: %s", e.Code, e.Msg)
}

type sdcardStatusQuery struct {
	XMLName  xml.Name `xml:"Query"`
	CmdType  string   `xml:"CmdType"`
	SN       int      `xml:"SN"`
	DeviceID string   `xml:"DeviceID"`
}

type sdcardStatusWire struct {
	XMLName  xml.Name        `xml:"Response"`
	CmdType  string          `xml:"CmdType"`
	SN       int             `xml:"SN"`
	DeviceID string          `xml:"DeviceID"`
	SumNum   *int            `xml:"SumNum"`
	Info     *sdcardInfoWire `xml:"SDCardStatusInfo"`
}

type sdcardInfoWire struct {
	Items []sdcardItemWire `xml:"Item"`
}

type sdcardItemWire struct {
	ID             string `xml:"ID"`
	HddName        string `xml:"HddName"`
	Status         string `xml:"Status"`
	FormatProgress string `xml:"FormatProgress"`
	Capacity       string `xml:"Capacity"`
	FreeSpace      string `xml:"FreeSpace"`
}

// BuildSDCardStatusQuery 用 2016 profile 序列化。保留这个"不带 profile"的
// 重载只为与本包其它 Build* 保持一致；平台实际总是走 WithProfile 版本。
func BuildSDCardStatusQuery(deviceID string, sn int) ([]byte, error) {
	return BuildSDCardStatusQueryWithProfile(protocol.ProfileFor(protocol.Version2016), deviceID, sn)
}

// BuildSDCardStatusQueryWithProfile 序列化 A.2.4.14，**不判断该不该发**。
//
// profile 在这里只决定字符集与 XML 声明。存储卡状态查询是 2022 新增命令，
// 但门禁遵循本仓既有口径：**只挡平台主动下发的自动对账，不挡操作员点出来的
// 查询**（同 BuildHomePositionQueryWithProfile）。所以这里不做版本判断。
func BuildSDCardStatusQueryWithProfile(profile protocol.Profile, deviceID string, sn int) ([]byte, error) {
	if err := validatePTZQueryTarget(deviceID, sn); err != nil {
		return nil, err
	}
	return MarshalProfiledXML(profile, sdcardStatusQuery{CmdType: CmdSDCardStatus, SN: sn, DeviceID: deviceID})
}

// ParseSDCardStatusResponse 解析 A.2.6.16。
//
// 刻意**不校验 Result**：标准 A.2.6.16 的 schema 里没有 Result 元素，
// 但现场设备习惯性地带一个，校严了反而会把合规应答判成失败。
func ParseSDCardStatusResponse(body []byte) (SDCardStatus, error) {
	var wire sdcardStatusWire
	if err := DecodeProfiledXML(protocol.ProfileFor(protocol.Version2016), body, &wire); err != nil {
		return SDCardStatus{}, &SDCardParseError{Code: SDCardErrorMalformed, Msg: err.Error()}
	}
	if wire.CmdType != CmdSDCardStatus {
		return SDCardStatus{}, &SDCardParseError{Code: SDCardErrorCmdType, Msg: fmt.Sprintf("got %q", wire.CmdType)}
	}
	if wire.SN <= 0 || strings.TrimSpace(wire.DeviceID) == "" {
		return SDCardStatus{}, &SDCardParseError{Code: SDCardErrorMalformed, Msg: "SN 或 DeviceID 缺失"}
	}
	if wire.SumNum == nil {
		return SDCardStatus{}, &SDCardParseError{Code: SDCardErrorMalformed, Msg: "SumNum 缺失"}
	}
	if *wire.SumNum < 0 {
		return SDCardStatus{}, &SDCardParseError{Code: SDCardErrorMalformed, Msg: fmt.Sprintf("SumNum 为负: %d", *wire.SumNum)}
	}

	status := SDCardStatus{
		CmdType:  wire.CmdType,
		SN:       wire.SN,
		DeviceID: strings.TrimSpace(wire.DeviceID),
		SumNum:   *wire.SumNum,
	}
	if wire.Info == nil {
		return status, nil
	}
	if len(wire.Info.Items) > maxSDCardItems {
		return SDCardStatus{}, &SDCardParseError{
			Code: SDCardErrorTooManyItems,
			Msg:  fmt.Sprintf("Item 条数 %d 超过标准上限 %d", len(wire.Info.Items), maxSDCardItems),
		}
	}
	items := make([]SDCardItem, 0, len(wire.Info.Items))
	for index, raw := range wire.Info.Items {
		item, err := parseSDCardItem(raw, index)
		if err != nil {
			return SDCardStatus{}, err
		}
		items = append(items, item)
	}
	status.Items = items
	return status, nil
}

// ParseSDCardStatusResponseFor 在解析基础上核对 SN / DeviceID。
func ParseSDCardStatusResponseFor(body []byte, expectation SDCardStatusExpectation) (SDCardStatus, error) {
	status, err := ParseSDCardStatusResponse(body)
	if err != nil {
		return SDCardStatus{}, err
	}
	if expectation.SN > 0 && status.SN != expectation.SN {
		return SDCardStatus{}, &SDCardParseError{
			Code: SDCardErrorCorrelation, Msg: fmt.Sprintf("SN got %d want %d", status.SN, expectation.SN),
		}
	}
	if want := strings.TrimSpace(expectation.DeviceID); want != "" && status.DeviceID != want {
		return SDCardStatus{}, &SDCardParseError{
			Code: SDCardErrorCorrelation, Msg: fmt.Sprintf("DeviceID got %q want %q", status.DeviceID, want),
		}
	}
	return status, nil
}

func parseSDCardItem(raw sdcardItemWire, index int) (SDCardItem, error) {
	id, err := parseSDCardInt("ID", raw.ID)
	if err != nil {
		return SDCardItem{}, err
	}
	if id < 0 {
		return SDCardItem{}, &SDCardParseError{Code: SDCardErrorMalformed, Msg: fmt.Sprintf("第 %d 个 Item 的 ID 为负: %d", index+1, id)}
	}
	capacity, err := parseSDCardInt("Capacity", raw.Capacity)
	if err != nil {
		return SDCardItem{}, err
	}
	if capacity < 0 {
		return SDCardItem{}, &SDCardParseError{Code: SDCardErrorMalformed, Msg: fmt.Sprintf("卡 %d 的 Capacity 为负: %d", id, capacity)}
	}
	freeSpace, err := parseSDCardInt("FreeSpace", raw.FreeSpace)
	if err != nil {
		return SDCardItem{}, err
	}
	if freeSpace < 0 {
		return SDCardItem{}, &SDCardParseError{Code: SDCardErrorMalformed, Msg: fmt.Sprintf("卡 %d 的 FreeSpace 为负: %d", id, freeSpace)}
	}

	item := SDCardItem{
		ID:        id,
		HddName:   strings.TrimSpace(raw.HddName),
		Status:    parseSDCardState(raw.Status),
		Capacity:  capacity,
		FreeSpace: freeSpace,
	}
	if progress := strings.TrimSpace(raw.FormatProgress); progress != "" {
		value, err := parseSDCardInt("FormatProgress", progress)
		if err != nil {
			return SDCardItem{}, err
		}
		if value < 0 || value > 100 {
			return SDCardItem{}, &SDCardParseError{
				Code: SDCardErrorMalformed, Msg: fmt.Sprintf("卡 %d 的 FormatProgress 越界: %d", id, value),
			}
		}
		item.FormatProgress = &value
	}
	return item, nil
}

func parseSDCardInt(field, value string) (int, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		// 标准里 ID/Capacity/FreeSpace 都是必选整数。缺了不是"0"，
		// 而是这一帧报文对不上 schema —— 报错而不是静默填 0。
		return 0, &SDCardParseError{Code: SDCardErrorMalformed, Msg: field + " 缺失"}
	}
	number, err := strconv.Atoi(trimmed)
	if err != nil {
		return 0, &SDCardParseError{Code: SDCardErrorMalformed, Msg: fmt.Sprintf("%s 非整数: %q", field, value)}
	}
	return number, nil
}

// parseSDCardState 大小写不敏感地映射 Status。未识别的取值落到 unknown，
// 而不是当成 error —— 设备报了个我们不认识的状态，不等于卡坏了。
func parseSDCardState(value string) SDCardState {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(SDCardStateOK):
		return SDCardStateOK
	case string(SDCardStateFormatting):
		return SDCardStateFormatting
	case string(SDCardStateUnformatted):
		return SDCardStateUnformatted
	case string(SDCardStateIdle):
		return SDCardStateIdle
	case string(SDCardStateError):
		return SDCardStateError
	default:
		return SDCardStateUnknown
	}
}

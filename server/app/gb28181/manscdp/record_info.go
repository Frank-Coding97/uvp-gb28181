package manscdp

import (
	"encoding/xml"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
)

const (
	CmdRecordInfo = "RecordInfo"

	recordInfoWireTimeLayout = "2006-01-02T15:04:05"
)

// RecordInfoQueryType is deliberately narrower than the Type values accepted
// in responses. GB/T 28181-2022 queries only use all, manual, or alarm here.
type RecordInfoQueryType string

const (
	RecordInfoQueryTypeAll    RecordInfoQueryType = "all"
	RecordInfoQueryTypeManual RecordInfoQueryType = "manual"
	RecordInfoQueryTypeAlarm  RecordInfoQueryType = "alarm"
)

var ErrRecordInfoInvalidArgument = errors.New("manscdp: invalid RecordInfo argument")

type RecordInfoErrorCode string

const (
	RecordInfoErrorInvalidArgument RecordInfoErrorCode = "invalid_argument"
	RecordInfoErrorMalformed       RecordInfoErrorCode = "malformed"
	RecordInfoErrorCommand         RecordInfoErrorCode = "command"
)

// RecordInfoError identifies request validation and response header failures.
// Individual invalid records are reported in RecordInfoItemResult instead.
type RecordInfoError struct {
	Code  RecordInfoErrorCode
	Field string
	Err   error
}

func (e *RecordInfoError) Error() string {
	if e == nil {
		return ""
	}
	message := fmt.Sprintf("RecordInfo %s", e.Code)
	if e.Field != "" {
		message += " " + e.Field
	}
	if e.Err != nil {
		message += ": " + e.Err.Error()
	}
	return message
}

func (e *RecordInfoError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (e *RecordInfoError) Is(target error) bool {
	return target == ErrRecordInfoInvalidArgument && e != nil && e.Code == RecordInfoErrorInvalidArgument
}

type RecordInfoQuery struct {
	SN         int
	DeviceID   string
	StartTime  time.Time
	EndTime    time.Time
	Type       RecordInfoQueryType
	Secrecy    int
	RecorderID string
}

type recordInfoQueryWire struct {
	XMLName    xml.Name `xml:"Query"`
	CmdType    string   `xml:"CmdType"`
	SN         int      `xml:"SN"`
	DeviceID   string   `xml:"DeviceID"`
	StartTime  string   `xml:"StartTime"`
	EndTime    string   `xml:"EndTime"`
	Secrecy    int      `xml:"Secrecy"`
	Type       string   `xml:"Type"`
	RecorderID string   `xml:"RecorderID,omitempty"`
}

// BuildRecordInfoQuery builds the GB/T 28181-2022 RecordInfo query body.
func BuildRecordInfoQuery(query RecordInfoQuery) ([]byte, error) {
	return BuildRecordInfoQueryWithProfile(protocol.ProfileFor(protocol.Version2022), query)
}

// BuildRecordInfoQueryWithProfile applies the shared profile codec while
// enforcing the 2022 GB18030 wire contract used by device recording queries.
func BuildRecordInfoQueryWithProfile(profile protocol.Profile, query RecordInfoQuery) ([]byte, error) {
	if profile.Version != protocol.Version(protocol.Version2022) ||
		(profile.Charset != "" && profile.Charset != protocol.Charset(protocol.CharsetGB18030)) {
		return nil, invalidRecordInfoArgument("profile", "必须使用 2022/GB18030 profile")
	}
	query.DeviceID = strings.TrimSpace(query.DeviceID)
	query.RecorderID = strings.TrimSpace(query.RecorderID)
	query.Type = RecordInfoQueryType(strings.ToLower(strings.TrimSpace(string(query.Type))))
	if query.Type == "" {
		query.Type = RecordInfoQueryTypeAll
	}
	if query.SN <= 0 {
		return nil, invalidRecordInfoArgument("SN", "必须为正数")
	}
	if query.DeviceID == "" {
		return nil, invalidRecordInfoArgument("DeviceID", "不能为空")
	}
	if query.StartTime.IsZero() {
		return nil, invalidRecordInfoArgument("StartTime", "不能为空")
	}
	if query.EndTime.IsZero() || !query.EndTime.After(query.StartTime) {
		return nil, invalidRecordInfoArgument("EndTime", "必须晚于 StartTime")
	}
	if query.Secrecy != 0 && query.Secrecy != 1 {
		return nil, invalidRecordInfoArgument("Secrecy", "必须为 0 或 1")
	}
	if !validRecordInfoQueryType(query.Type) {
		return nil, invalidRecordInfoArgument("Type", fmt.Sprintf("不支持 %q", query.Type))
	}

	return MarshalProfiledXML(profile, recordInfoQueryWire{
		CmdType:    CmdRecordInfo,
		SN:         query.SN,
		DeviceID:   query.DeviceID,
		StartTime:  query.StartTime.Format(recordInfoWireTimeLayout),
		EndTime:    query.EndTime.Format(recordInfoWireTimeLayout),
		Secrecy:    query.Secrecy,
		Type:       string(query.Type),
		RecorderID: query.RecorderID,
	})
}

func invalidRecordInfoArgument(field, message string) error {
	return &RecordInfoError{
		Code:  RecordInfoErrorInvalidArgument,
		Field: field,
		Err:   errors.New(message),
	}
}

func validRecordInfoQueryType(recordType RecordInfoQueryType) bool {
	switch recordType {
	case RecordInfoQueryTypeAll, RecordInfoQueryTypeManual, RecordInfoQueryTypeAlarm:
		return true
	default:
		return false
	}
}

// RecordInfoItemType preserves the device-provided value. Standard values are
// time, alarm, and manual, while vendor extensions remain available as raw data.
type RecordInfoItemType string

type RecordInfoItem struct {
	DeviceID       string
	Name           string
	FilePath       string
	Address        string
	StartTime      string
	EndTime        string
	Secrecy        int
	Type           RecordInfoItemType
	RecorderID     string
	FileSize       *int64
	RecordLocation string
	StreamNumber   *int
}

type RecordInfoItemErrorCode string

const (
	RecordInfoItemMissingField   RecordInfoItemErrorCode = "missing_field"
	RecordInfoItemInvalidTime    RecordInfoItemErrorCode = "invalid_time"
	RecordInfoItemInvalidRange   RecordInfoItemErrorCode = "invalid_range"
	RecordInfoItemDeviceMismatch RecordInfoItemErrorCode = "device_mismatch"
)

type RecordInfoItemError struct {
	Code  RecordInfoItemErrorCode
	Field string
	Value string
}

func (e *RecordInfoItemError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("RecordInfo Item %s: %s=%q", e.Code, e.Field, e.Value)
}

type RecordInfoItemResult struct {
	Index int
	Item  RecordInfoItem
	Valid bool
	Error *RecordInfoItemError
}

type RecordInfoWarningCode string

const (
	RecordInfoWarningContradictoryEmpty RecordInfoWarningCode = "contradictory_empty"
	RecordInfoWarningNumMismatch        RecordInfoWarningCode = "num_mismatch"
)

type RecordInfoWarning struct {
	Code    RecordInfoWarningCode
	Message string
}

type RecordInfoResponse struct {
	SN                int
	DeviceID          string
	Name              string
	SumNum            int
	RecordListPresent bool
	RecordListNum     int
	Items             []RecordInfoItem
	ItemResults       []RecordInfoItemResult
	Warnings          []RecordInfoWarning
	ExtraInfo         []string
	Empty             bool
}

func (r *RecordInfoResponse) HasWarning(code RecordInfoWarningCode) bool {
	if r == nil {
		return false
	}
	for _, warning := range r.Warnings {
		if warning.Code == code {
			return true
		}
	}
	return false
}

type recordInfoResponseWire struct {
	XMLName   xml.Name            `xml:"Response"`
	CmdType   string              `xml:"CmdType"`
	SN        int                 `xml:"SN"`
	DeviceID  string              `xml:"DeviceID"`
	Name      string              `xml:"Name"`
	SumNum    *int                `xml:"SumNum"`
	List      *recordInfoListWire `xml:"RecordList"`
	ExtraInfo []string            `xml:"ExtraInfo"`
}

type recordInfoListWire struct {
	Num   int                  `xml:"Num,attr"`
	Items []recordInfoItemWire `xml:"Item"`
}

type recordInfoItemWire struct {
	DeviceID       string `xml:"DeviceID"`
	Name           string `xml:"Name"`
	FilePath       string `xml:"FilePath"`
	Address        string `xml:"Address"`
	StartTime      string `xml:"StartTime"`
	EndTime        string `xml:"EndTime"`
	Secrecy        int    `xml:"Secrecy"`
	Type           string `xml:"Type"`
	RecorderID     string `xml:"RecorderID"`
	FileSize       string `xml:"FileSize"`
	RecordLocation string `xml:"RecordLocation"`
	StreamNumber   string `xml:"StreamNumber"`
}

// ParseRecordInfoResponse decodes one possibly segmented RecordInfo MESSAGE.
// Aggregation is intentionally left to recordquery; this layer validates each
// item independently and retains protocol warnings alongside valid records.
func ParseRecordInfoResponse(body []byte) (*RecordInfoResponse, error) {
	var wire recordInfoResponseWire
	if err := DecodeProfiledXML(protocol.ProfileFor(protocol.Version2022), body, &wire); err != nil {
		return nil, &RecordInfoError{Code: RecordInfoErrorMalformed, Err: err}
	}
	wire.CmdType = strings.TrimSpace(wire.CmdType)
	wire.DeviceID = strings.TrimSpace(wire.DeviceID)
	wire.Name = strings.TrimSpace(wire.Name)
	if wire.CmdType == "" {
		return nil, &RecordInfoError{Code: RecordInfoErrorMalformed, Field: "CmdType", Err: errors.New("不能为空")}
	}
	if wire.CmdType != CmdRecordInfo {
		return nil, &RecordInfoError{Code: RecordInfoErrorCommand, Field: "CmdType", Err: fmt.Errorf("got %q", wire.CmdType)}
	}
	if wire.SN <= 0 {
		return nil, &RecordInfoError{Code: RecordInfoErrorMalformed, Field: "SN", Err: errors.New("必须为正数")}
	}
	if wire.DeviceID == "" {
		return nil, &RecordInfoError{Code: RecordInfoErrorMalformed, Field: "DeviceID", Err: errors.New("不能为空")}
	}
	if wire.SumNum == nil {
		return nil, &RecordInfoError{Code: RecordInfoErrorMalformed, Field: "SumNum", Err: errors.New("不能为空")}
	}
	if *wire.SumNum < 0 {
		return nil, &RecordInfoError{Code: RecordInfoErrorMalformed, Field: "SumNum", Err: errors.New("不能为负数")}
	}

	response := &RecordInfoResponse{
		SN:                wire.SN,
		DeviceID:          wire.DeviceID,
		Name:              wire.Name,
		SumNum:            *wire.SumNum,
		RecordListPresent: wire.List != nil,
		ExtraInfo:         normalizeRecordInfoExtraInfo(wire.ExtraInfo),
		Empty:             *wire.SumNum == 0 && wire.List == nil,
	}
	if wire.List == nil {
		return response, nil
	}
	response.RecordListNum = wire.List.Num
	if wire.List.Num != len(wire.List.Items) {
		response.Warnings = append(response.Warnings, RecordInfoWarning{
			Code:    RecordInfoWarningNumMismatch,
			Message: fmt.Sprintf("RecordList Num=%d, actual=%d", wire.List.Num, len(wire.List.Items)),
		})
	}
	if *wire.SumNum == 0 && (wire.List.Num > 0 || len(wire.List.Items) > 0) {
		response.Warnings = append(response.Warnings, RecordInfoWarning{
			Code:    RecordInfoWarningContradictoryEmpty,
			Message: "SumNum=0 but RecordList contains items",
		})
	}

	response.ItemResults = make([]RecordInfoItemResult, 0, len(wire.List.Items))
	response.Items = make([]RecordInfoItem, 0, len(wire.List.Items))
	for index, itemWire := range wire.List.Items {
		item := normalizeRecordInfoItem(itemWire)
		itemErr := validateRecordInfoItem(item, wire.DeviceID)
		result := RecordInfoItemResult{Index: index, Item: item, Valid: itemErr == nil, Error: itemErr}
		response.ItemResults = append(response.ItemResults, result)
		if itemErr == nil {
			response.Items = append(response.Items, item)
		}
	}
	return response, nil
}

func normalizeRecordInfoItem(wire recordInfoItemWire) RecordInfoItem {
	return RecordInfoItem{
		DeviceID:       strings.TrimSpace(wire.DeviceID),
		Name:           strings.TrimSpace(wire.Name),
		FilePath:       strings.TrimSpace(wire.FilePath),
		Address:        strings.TrimSpace(wire.Address),
		StartTime:      strings.TrimSpace(wire.StartTime),
		EndTime:        strings.TrimSpace(wire.EndTime),
		Secrecy:        wire.Secrecy,
		Type:           RecordInfoItemType(strings.TrimSpace(wire.Type)),
		RecorderID:     strings.TrimSpace(wire.RecorderID),
		FileSize:       parseOptionalNonNegativeInt64(wire.FileSize),
		RecordLocation: strings.TrimSpace(wire.RecordLocation),
		StreamNumber:   parseOptionalRecordInfoInt(wire.StreamNumber),
	}
}

func parseOptionalNonNegativeInt64(value string) *int64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 0 {
		return nil
	}
	return &parsed
}

func parseOptionalRecordInfoInt(value string) *int {
	parsed := parseOptionalNonNegativeInt64(value)
	if parsed == nil || int64(int(*parsed)) != *parsed {
		return nil
	}
	result := int(*parsed)
	return &result
}

func normalizeRecordInfoExtraInfo(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func validateRecordInfoItem(item RecordInfoItem, expectedDeviceID string) *RecordInfoItemError {
	if item.DeviceID == "" {
		return &RecordInfoItemError{Code: RecordInfoItemMissingField, Field: "DeviceID"}
	}
	if item.DeviceID != expectedDeviceID {
		return &RecordInfoItemError{Code: RecordInfoItemDeviceMismatch, Field: "DeviceID", Value: item.DeviceID}
	}
	if item.StartTime == "" {
		return &RecordInfoItemError{Code: RecordInfoItemMissingField, Field: "StartTime"}
	}
	if item.EndTime == "" {
		return &RecordInfoItemError{Code: RecordInfoItemMissingField, Field: "EndTime"}
	}
	start, err := time.Parse(recordInfoWireTimeLayout, item.StartTime)
	if err != nil {
		return &RecordInfoItemError{Code: RecordInfoItemInvalidTime, Field: "StartTime", Value: item.StartTime}
	}
	end, err := time.Parse(recordInfoWireTimeLayout, item.EndTime)
	if err != nil {
		return &RecordInfoItemError{Code: RecordInfoItemInvalidTime, Field: "EndTime", Value: item.EndTime}
	}
	if !end.After(start) {
		return &RecordInfoItemError{Code: RecordInfoItemInvalidRange, Field: "EndTime", Value: item.EndTime}
	}
	return nil
}

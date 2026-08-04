package manscdp

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/text/encoding/simplifiedchinese"
	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
)

const recordInfoTimeLayout = "2006-01-02T15:04:05"

func mustRecordInfoTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.ParseInLocation(recordInfoTimeLayout, value, time.FixedZone("CST", 8*60*60))
	require.NoError(t, err)
	return parsed
}

func TestBuildRecordInfoQueryUses2022GB18030(t *testing.T) {
	profile := protocol.ProfileFor(protocol.Version2022)
	body, err := BuildRecordInfoQueryWithProfile(profile, RecordInfoQuery{
		SN:         8,
		DeviceID:   " 34020000001320000001 ",
		StartTime:  mustRecordInfoTime(t, "2026-08-02T08:00:00"),
		EndTime:    mustRecordInfoTime(t, "2026-08-02T09:00:00"),
		Type:       RecordInfoQueryTypeManual,
		Secrecy:    1,
		RecorderID: "录像机𠀀",
	})
	require.NoError(t, err)
	require.True(t, bytes.HasPrefix(body, []byte(`<?xml version="1.0" encoding="GB18030"?>`)))
	require.NotContains(t, string(body), "录像机𠀀", "wire body must not retain UTF-8 text")

	encodedRecorder, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte("录像机𠀀"))
	require.NoError(t, err)
	require.True(t, bytes.Contains(body, encodedRecorder))

	var decoded struct {
		XMLName    xml.Name `xml:"Query"`
		CmdType    string   `xml:"CmdType"`
		SN         int      `xml:"SN"`
		DeviceID   string   `xml:"DeviceID"`
		StartTime  string   `xml:"StartTime"`
		EndTime    string   `xml:"EndTime"`
		Secrecy    int      `xml:"Secrecy"`
		Type       string   `xml:"Type"`
		RecorderID string   `xml:"RecorderID"`
	}
	require.NoError(t, DecodeProfiledXML(profile, body, &decoded))
	require.Equal(t, CmdRecordInfo, decoded.CmdType)
	require.Equal(t, 8, decoded.SN)
	require.Equal(t, "34020000001320000001", decoded.DeviceID)
	require.Equal(t, "2026-08-02T08:00:00", decoded.StartTime)
	require.Equal(t, "2026-08-02T09:00:00", decoded.EndTime)
	require.Equal(t, 1, decoded.Secrecy)
	require.Equal(t, "manual", decoded.Type)
	require.Equal(t, "录像机𠀀", decoded.RecorderID)

	normalized, err := DecodeProfiledXMLBytes(profile, body)
	require.NoError(t, err)
	for _, forbidden := range []string{"FilePath", "Address", "IndistinctQuery", "StreamNumber", "AlarmMethod", "AlarmType"} {
		require.NotContains(t, string(normalized), "<"+forbidden+">")
	}
}

func TestBuildRecordInfoQueryTypeAndArgumentValidation(t *testing.T) {
	base := RecordInfoQuery{
		SN:        1,
		DeviceID:  "34020000001320000001",
		StartTime: mustRecordInfoTime(t, "2026-08-02T08:00:00"),
		EndTime:   mustRecordInfoTime(t, "2026-08-02T09:00:00"),
		Secrecy:   0,
	}

	for _, recordType := range []RecordInfoQueryType{
		RecordInfoQueryTypeAll,
		RecordInfoQueryTypeManual,
		RecordInfoQueryTypeAlarm,
	} {
		t.Run(string(recordType), func(t *testing.T) {
			query := base
			query.Type = recordType
			body, err := BuildRecordInfoQuery(query)
			require.NoError(t, err)
			require.NotNil(t, body)
		})
	}

	for _, recordType := range []RecordInfoQueryType{"time", "vendor"} {
		t.Run("reject_"+string(recordType), func(t *testing.T) {
			query := base
			query.Type = recordType
			body, err := BuildRecordInfoQuery(query)
			require.Nil(t, body)
			require.ErrorIs(t, err, ErrRecordInfoInvalidArgument)
		})
	}

	tests := []struct {
		name   string
		mutate func(*RecordInfoQuery)
	}{
		{name: "zero SN", mutate: func(q *RecordInfoQuery) { q.SN = 0 }},
		{name: "blank DeviceID", mutate: func(q *RecordInfoQuery) { q.DeviceID = "  " }},
		{name: "zero start", mutate: func(q *RecordInfoQuery) { q.StartTime = time.Time{} }},
		{name: "zero end", mutate: func(q *RecordInfoQuery) { q.EndTime = time.Time{} }},
		{name: "equal range", mutate: func(q *RecordInfoQuery) { q.EndTime = q.StartTime }},
		{name: "reversed range", mutate: func(q *RecordInfoQuery) { q.EndTime = q.StartTime.Add(-time.Second) }},
		{name: "invalid secrecy", mutate: func(q *RecordInfoQuery) { q.Secrecy = 2 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query := base
			query.Type = RecordInfoQueryTypeAll
			test.mutate(&query)
			body, err := BuildRecordInfoQuery(query)
			require.Nil(t, body)
			require.ErrorIs(t, err, ErrRecordInfoInvalidArgument)
			var argumentErr *RecordInfoError
			require.ErrorAs(t, err, &argumentErr)
			require.Equal(t, RecordInfoErrorInvalidArgument, argumentErr.Code)
		})
	}
}

func TestBuildRecordInfoQueryOmitsEmptyRecorderID(t *testing.T) {
	query := RecordInfoQuery{
		SN:        2,
		DeviceID:  "34020000001320000001",
		StartTime: mustRecordInfoTime(t, "2026-08-02T08:00:00"),
		EndTime:   mustRecordInfoTime(t, "2026-08-02T09:00:00"),
		Type:      RecordInfoQueryTypeAll,
	}
	body, err := BuildRecordInfoQuery(query)
	require.NoError(t, err)
	normalized, err := DecodeProfiledXMLBytes(protocol.ProfileFor(protocol.Version2022), body)
	require.NoError(t, err)
	require.NotContains(t, string(normalized), "<RecorderID>")

	query.RecorderID = " recorder-1 "
	body, err = BuildRecordInfoQuery(query)
	require.NoError(t, err)
	normalized, err = DecodeProfiledXMLBytes(protocol.ProfileFor(protocol.Version2022), body)
	require.NoError(t, err)
	require.Contains(t, string(normalized), "<RecorderID>recorder-1</RecorderID>")
}

func TestParseRecordInfoResponseEncodingMatrix(t *testing.T) {
	for _, charset := range []string{"GB2312", "GB18030", "UTF-8"} {
		t.Run(charset, func(t *testing.T) {
			name := "园区东门"
			address := "上海市浦东新区"
			if charset == "GB18030" {
				name += "𠀀"
			}
			xmlText := recordInfoResponseXMLText(1, 1, recordInfoItemXML(
				"34020000001320000001", name, address,
				"2026-08-02T08:00:00", "2026-08-02T08:10:00", "time", "1",
			))
			body := encodeRecordInfoXML(t, charset, xmlText)

			response, err := ParseRecordInfoResponse(body)
			require.NoError(t, err)
			require.Len(t, response.Items, 1)
			require.Equal(t, name, response.Items[0].Name)
			require.Equal(t, address, response.Items[0].Address)
		})
	}
}

func TestParseRecordInfoResponseEmptyAndListWarnings(t *testing.T) {
	t.Run("standard empty", func(t *testing.T) {
		body := encodeRecordInfoXML(t, "UTF-8", recordInfoResponseXMLText(0, -1, ""))
		response, err := ParseRecordInfoResponse(body)
		require.NoError(t, err)
		require.True(t, response.Empty)
		require.Empty(t, response.Items)
		require.Empty(t, response.Warnings)
		require.False(t, response.RecordListPresent)
	})

	t.Run("sum zero with item is not empty", func(t *testing.T) {
		item := recordInfoItemXML("34020000001320000001", "东门", "园区", "2026-08-02T08:00:00", "2026-08-02T08:10:00", "time", "1")
		body := encodeRecordInfoXML(t, "UTF-8", recordInfoResponseXMLText(0, 1, item))
		response, err := ParseRecordInfoResponse(body)
		require.NoError(t, err)
		require.False(t, response.Empty)
		require.Len(t, response.Items, 1)
		require.True(t, response.HasWarning(RecordInfoWarningContradictoryEmpty))
	})

	t.Run("declared Num mismatch retains actual items", func(t *testing.T) {
		item := recordInfoItemXML("34020000001320000001", "东门", "园区", "2026-08-02T08:00:00", "2026-08-02T08:10:00", "time", "1")
		body := encodeRecordInfoXML(t, "UTF-8", recordInfoResponseXMLText(2, 2, item))
		response, err := ParseRecordInfoResponse(body)
		require.NoError(t, err)
		require.Equal(t, 2, response.RecordListNum)
		require.Len(t, response.Items, 1)
		require.True(t, response.HasWarning(RecordInfoWarningNumMismatch))
	})
}

func TestParseRecordInfoResponseIsolatesInvalidItems(t *testing.T) {
	items := strings.Join([]string{
		recordInfoItemXML("34020000001320000001", "valid", "园区", "2026-08-02T08:00:00", "2026-08-02T08:10:00", "time", "1"),
		recordInfoItemXML("34020000001320000001", "bad start", "园区", "not-a-time", "2026-08-02T08:20:00", "alarm", "1"),
		recordInfoItemXML("34020000001320000001", "reversed", "园区", "2026-08-02T08:30:00", "2026-08-02T08:20:00", "manual", "1"),
		recordInfoItemXML("34020000001320099999", "wrong channel", "园区", "2026-08-02T08:40:00", "2026-08-02T08:50:00", "time", "1"),
		recordInfoItemXML("", "missing channel", "园区", "2026-08-02T09:00:00", "2026-08-02T09:10:00", "time", "1"),
		recordInfoItemXML("34020000001320000001", "missing start", "园区", "", "2026-08-02T09:20:00", "time", "1"),
		recordInfoItemXML("34020000001320000001", "missing end", "园区", "2026-08-02T09:30:00", "", "time", "1"),
	}, "")
	body := encodeRecordInfoXML(t, "UTF-8", recordInfoResponseXMLText(7, 7, items))

	response, err := ParseRecordInfoResponse(body)
	require.NoError(t, err)
	require.Len(t, response.Items, 1)
	require.Equal(t, "valid", response.Items[0].Name)
	require.Len(t, response.ItemResults, 7)
	require.True(t, response.ItemResults[0].Valid)
	for _, result := range response.ItemResults[1:] {
		require.False(t, result.Valid)
		require.NotNil(t, result.Error)
	}
	require.Equal(t, RecordInfoItemInvalidTime, response.ItemResults[1].Error.Code)
	require.Equal(t, RecordInfoItemInvalidRange, response.ItemResults[2].Error.Code)
	require.Equal(t, RecordInfoItemDeviceMismatch, response.ItemResults[3].Error.Code)
	require.Equal(t, RecordInfoItemMissingField, response.ItemResults[4].Error.Code)
	require.Equal(t, RecordInfoItemMissingField, response.ItemResults[5].Error.Code)
	require.Equal(t, RecordInfoItemMissingField, response.ItemResults[6].Error.Code)
}

func TestParseRecordInfoResponsePreservesTypesAndRecordLocation(t *testing.T) {
	var items strings.Builder
	for index, recordType := range []string{"time", "alarm", "manual", "vendor-private"} {
		start := fmt.Sprintf("2026-08-02T%02d:00:00", 8+index)
		end := fmt.Sprintf("2026-08-02T%02d:10:00", 8+index)
		items.WriteString(recordInfoItemXML("34020000001320000001", recordType, "园区", start, end, recordType, "NVR-A"))
	}
	body := encodeRecordInfoXML(t, "UTF-8", recordInfoResponseXMLText(4, 4, items.String()))

	response, err := ParseRecordInfoResponse(body)
	require.NoError(t, err)
	require.Len(t, response.Items, 4)
	for index, wantType := range []string{"time", "alarm", "manual", "vendor-private"} {
		require.Equal(t, RecordInfoItemType(wantType), response.Items[index].Type)
		require.Equal(t, "NVR-A", response.Items[index].RecordLocation)
		require.Equal(t, "34020000001320000001", response.Items[index].DeviceID)
	}
}

func TestParseRecordInfoResponseRejectsMalformedHeaders(t *testing.T) {
	tests := []struct {
		name string
		body string
		code RecordInfoErrorCode
	}{
		{name: "malformed XML", body: `<Response>`, code: RecordInfoErrorMalformed},
		{name: "wrong root", body: `<Notify><CmdType>RecordInfo</CmdType><SN>1</SN><DeviceID>C</DeviceID><SumNum>0</SumNum></Notify>`, code: RecordInfoErrorMalformed},
		{name: "wrong command", body: `<Response><CmdType>Catalog</CmdType><SN>1</SN><DeviceID>C</DeviceID><SumNum>0</SumNum></Response>`, code: RecordInfoErrorCommand},
		{name: "missing CmdType", body: `<Response><SN>1</SN><DeviceID>C</DeviceID><SumNum>0</SumNum></Response>`, code: RecordInfoErrorMalformed},
		{name: "missing SN", body: `<Response><CmdType>RecordInfo</CmdType><DeviceID>C</DeviceID><SumNum>0</SumNum></Response>`, code: RecordInfoErrorMalformed},
		{name: "missing DeviceID", body: `<Response><CmdType>RecordInfo</CmdType><SN>1</SN><SumNum>0</SumNum></Response>`, code: RecordInfoErrorMalformed},
		{name: "missing SumNum", body: `<Response><CmdType>RecordInfo</CmdType><SN>1</SN><DeviceID>C</DeviceID></Response>`, code: RecordInfoErrorMalformed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := encodeRecordInfoXML(t, "UTF-8", test.body)
			response, err := ParseRecordInfoResponse(body)
			require.Nil(t, response)
			var recordErr *RecordInfoError
			require.ErrorAs(t, err, &recordErr)
			require.Equal(t, test.code, recordErr.Code)
		})
	}
}

func encodeRecordInfoXML(t *testing.T, charset, xmlText string) []byte {
	t.Helper()
	payload := []byte(xmlText)
	var err error
	switch charset {
	case "GB2312":
		payload, err = simplifiedchinese.GBK.NewEncoder().Bytes(payload)
	case "GB18030":
		payload, err = simplifiedchinese.GB18030.NewEncoder().Bytes(payload)
	case "UTF-8":
	default:
		t.Fatalf("unsupported test charset %q", charset)
	}
	require.NoError(t, err)
	return append([]byte(fmt.Sprintf(`<?xml version="1.0" encoding="%s"?>`, charset)), payload...)
}

func recordInfoResponseXMLText(sumNum, listNum int, items string) string {
	recordList := ""
	if listNum >= 0 {
		recordList = fmt.Sprintf(`<RecordList Num="%d">%s</RecordList>`, listNum, items)
	}
	return fmt.Sprintf(`<Response><CmdType>RecordInfo</CmdType><SN>8</SN><DeviceID>34020000001320000001</DeviceID><SumNum>%d</SumNum>%s</Response>`, sumNum, recordList)
}

func recordInfoItemXML(deviceID, name, address, startTime, endTime, recordType, location string) string {
	return fmt.Sprintf(`<Item><DeviceID>%s</DeviceID><Name>%s</Name><FilePath>/record/%s.ps</FilePath><Address>%s</Address><StartTime>%s</StartTime><EndTime>%s</EndTime><Secrecy>0</Secrecy><Type>%s</Type><RecorderID>recorder-1</RecorderID><RecordLocation>%s</RecordLocation></Item>`,
		deviceID, name, name, address, startTime, endTime, recordType, location)
}

func TestRecordInfoInvalidArgumentSentinel(t *testing.T) {
	err := &RecordInfoError{Code: RecordInfoErrorInvalidArgument, Field: "SN", Err: errors.New("zero")}
	require.True(t, errors.Is(err, ErrRecordInfoInvalidArgument))
}

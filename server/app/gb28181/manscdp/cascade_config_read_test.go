package manscdp

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
)

func cascadeConfigBody(block string) []byte {
	return []byte(`<?xml version="1.0" encoding="UTF-8"?><Response><CmdType>ConfigDownload</CmdType><SN>5</SN><DeviceID>34020000001320000002</DeviceID><Result>OK</Result>` + block + `<PrivateDeviceID>private-source</PrivateDeviceID></Response>`)
}

func TestCascadeConfigReadPreservesAllStandardTypesAndStripsExtensions(t *testing.T) {
	for configType, block := range map[string]string{
		ConfigTypeBasicParam:          `<BasicParam><Name>摄像机</Name><Expiration>3600</Expiration><Password>secret</Password></BasicParam>`,
		ConfigTypeVideoParamOpt:       `<VideoParamOpt><DownloadSpeed>1/2/4</DownloadSpeed><Resolution>1/2</Resolution></VideoParamOpt>`,
		ConfigTypeSVACEncodeConfig:    `<SVACEncodeConfig><SVCParam><SVCSpaceDomainMode>1</SVCSpaceDomainMode><SVCTimeDomainMode>1</SVCTimeDomainMode><SVCSpaceSupportMode>3</SVCSpaceSupportMode><SVCTimeSupportMode>3</SVCTimeSupportMode></SVCParam><Password>secret</Password></SVACEncodeConfig>`,
		ConfigTypeSVACDecodeConfig:    `<SVACDecodeConfig><SVCParam><SVCSTMMode>1</SVCSTMMode><SVCSpaceSupportMode>3</SVCSpaceSupportMode><SVCTimeSupportMode>3</SVCTimeSupportMode></SVCParam></SVACDecodeConfig>`,
		ConfigTypeVideoParamAttribute: `<VideoParamAttribute Num="0"/>`,
		ConfigTypeVideoRecordPlan:     `<VideoRecordPlan><RecordEnable>0</RecordEnable><RecordScheduleSumNum>0</RecordScheduleSumNum><StreamNumber>1</StreamNumber></VideoRecordPlan><VideoRecordPlan><RecordEnable>1</RecordEnable><RecordScheduleSumNum>0</RecordScheduleSumNum><StreamNumber>0</StreamNumber></VideoRecordPlan>`,
		ConfigTypeVideoAlarmRecord:    `<VideoAlarmRecord><RecordEnable>1</RecordEnable><StreamNumber>0</StreamNumber></VideoAlarmRecord><VideoAlarmRecord><RecordEnable>0</RecordEnable><StreamNumber>1</StreamNumber></VideoAlarmRecord>`,
		ConfigTypePictureMask:         `<PictureMask><On>0</On><SumNum>1</SumNum><RegionList Num="1"><Item><Seq>1</Seq><Point>1,2,3,4</Point></Item></RegionList></PictureMask>`,
		ConfigTypeFrameMirror:         `<FrameMirror>2</FrameMirror>`,
		ConfigTypeAlarmReport:         `<AlarmReport><MotionDetection>1</MotionDetection><FieldDetection>0</FieldDetection></AlarmReport>`,
		ConfigTypeOSDConfig:           `<OSDConfig><Length>704</Length><Width>576</Width><TimeX>0</TimeX><TimeY>0</TimeY><TimeEnable>0</TimeEnable><TimeType>0</TimeType><TextEnable>0</TextEnable><SumNum>0</SumNum></OSDConfig>`,
		ConfigTypeSnapShotConfig:      `<SnapShot><SnapNum>1</SnapNum><Interval>0</Interval><UploadURL>https://example.test/upload</UploadURL><SessionID>01234567890123456789012345678901</SessionID></SnapShot>`,
	} {
		t.Run(configType, func(t *testing.T) {
			parsed, err := ParseCascadeConfigReadResponse(cascadeConfigBody(block), ConfigDownloadExpectation{SN: 5, DeviceID: "34020000001320000002"}, configType)
			require.NoError(t, err)
			for _, version := range []protocol.Version{protocol.Version2016, protocol.Version2022} {
				body, err := parsed.Build(protocol.ProfileFor(version), "34020000001320000012", 97)
				require.NoError(t, err)
				utf8Body, err := DecodeProfiledXMLBytes(protocol.ProfileFor(version), body)
				require.NoError(t, err)
				out := string(utf8Body)
				require.Contains(t, out, "<SN>97</SN>")
				require.Contains(t, out, "34020000001320000012")
				require.NotContains(t, out, "private-source")
				require.NotContains(t, out, "secret")
				require.NotContains(t, out, "34020000001320000002")
				if configType == ConfigTypeVideoRecordPlan {
					require.Equal(t, 2, strings.Count(out, "<VideoRecordPlan>"))
				}
				if configType == ConfigTypeVideoAlarmRecord {
					require.Equal(t, 2, strings.Count(out, "<VideoAlarmRecord>"))
				}
				if configType == ConfigTypePictureMask {
					require.Contains(t, out, "<Point>1,2,3,4</Point>")
				}
				if configType == ConfigTypeSnapShotConfig {
					require.Contains(t, out, "<SnapShot>")
					require.NotContains(t, out, "<SnapShotConfig>")
				}
			}
		})
	}
}

func TestCascadeConfigReadRejectsIncompleteOrUncorrelatedResponse(t *testing.T) {
	for _, test := range []struct{ kind, block string }{
		{ConfigTypeVideoParamAttribute, `<VideoParamAttribute Num="2"/>`},
		{ConfigTypeVideoRecordPlan, `<VideoRecordPlan><RecordEnable>1</RecordEnable><RecordScheduleSumNum>1</RecordScheduleSumNum><StreamNumber>0</StreamNumber></VideoRecordPlan>`},
		{ConfigTypePictureMask, `<PictureMask><On>1</On><SumNum>1</SumNum></PictureMask>`},
		{ConfigTypeSVACEncodeConfig, `<SVACEncodeConfig><ROIParam><ROIFlag>1</ROIFlag><ROINumber>1</ROINumber></ROIParam></SVACEncodeConfig>`},
	} {
		_, err := ParseCascadeConfigReadResponse(cascadeConfigBody(test.block), ConfigDownloadExpectation{SN: 5}, test.kind)
		require.Error(t, err, test.kind)
	}
	for _, expected := range []ConfigDownloadExpectation{{SN: 6}, {DeviceID: "another"}} {
		_, err := ParseCascadeConfigReadResponse(cascadeConfigBody(`<FrameMirror>1</FrameMirror>`), expected, ConfigTypeFrameMirror)
		require.Error(t, err)
	}
	_, err := ParseCascadeConfigReadResponse([]byte(strings.ReplaceAll(string(cascadeConfigBody("")), "Response", "Query")), ConfigDownloadExpectation{}, ConfigTypeFrameMirror)
	require.Error(t, err)
}

func TestCascadeConfigReadDoesNotInventAbsentOrUnrequestedValues(t *testing.T) {
	parsed, err := ParseCascadeConfigReadResponse(cascadeConfigBody(`<FrameMirror>1</FrameMirror>`), ConfigDownloadExpectation{}, ConfigTypeBasicParam)
	require.NoError(t, err)
	body, err := parsed.Build(protocol.ProfileFor(protocol.Version2022), "34020000001320000012", 97)
	require.NoError(t, err)
	require.NotContains(t, string(body), "<BasicParam>")
	require.NotContains(t, string(body), "<FrameMirror>")
	require.Contains(t, string(body), "<Result>OK</Result>")
	for _, value := range []string{"", "BasicParam/", "unknown", "BasicParam//FrameMirror"} {
		_, err := NormalizeCascadeConfigTypes(strings.Split(value, "/"))
		require.Error(t, err, fmt.Sprintf("invalid ConfigType %q", value))
	}
	got, err := NormalizeCascadeConfigTypes([]string{"BasicParam", "FrameMirror", "BasicParam"})
	require.NoError(t, err)
	require.Equal(t, []string{"BasicParam", "FrameMirror"}, got)
}

func TestCascadeConfigReadDoesNotCreateMissingRequiredValues(t *testing.T) {
	for _, test := range []struct{ kind, block string }{
		{ConfigTypeVideoRecordPlan, `<VideoRecordPlan><RecordScheduleSumNum>0</RecordScheduleSumNum><StreamNumber>0</StreamNumber></VideoRecordPlan>`},
		{ConfigTypeOSDConfig, `<OSDConfig><SumNum>0</SumNum></OSDConfig>`},
		{ConfigTypePictureMask, `<PictureMask><On>0</On><SumNum>0</SumNum><RegionList/></PictureMask>`},
		{ConfigTypeBasicParam, `<BasicParam><Name>first</Name></BasicParam><BasicParam><Name>second</Name></BasicParam>`},
		{ConfigTypeSVACEncodeConfig, `<SVACEncodeConfig><SVCParam><SVCSpaceDomainMode>0</SVCSpaceDomainMode></SVCParam></SVACEncodeConfig>`},
	} {
		_, err := ParseCascadeConfigReadResponse(cascadeConfigBody(test.block), ConfigDownloadExpectation{}, test.kind)
		require.Error(t, err, test.kind)
	}
}

func TestCascadeConfigReadOptionalFieldsAndCredentials(t *testing.T) {
	parsed, err := ParseCascadeConfigReadResponse(cascadeConfigBody(`<VideoParamOpt/>`), ConfigDownloadExpectation{}, ConfigTypeVideoParamOpt)
	require.NoError(t, err)
	body, err := parsed.Build(protocol.ProfileFor(protocol.Version2022), "34020000001320000012", 97)
	require.NoError(t, err)
	require.NotContains(t, string(body), "<Resolution>")
	require.NotContains(t, string(body), "<DownloadSpeed>")
	parsed, err = ParseCascadeConfigReadResponse(cascadeConfigBody(`<SnapShot><SnapNum>1</SnapNum><UploadURL>http://user:password@example.test/upload</UploadURL><SessionID>01234567890123456789012345678901</SessionID></SnapShot>`), ConfigDownloadExpectation{}, ConfigTypeSnapShotConfig)
	require.NoError(t, err)
	_, err = parsed.Build(protocol.ProfileFor(protocol.Version2022), "34020000001320000012", 97)
	require.Error(t, err)
}

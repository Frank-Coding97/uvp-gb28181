package manscdp

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
)

var cascadeWritableTestBlocks = map[string]string{
	"BasicParam":          `<BasicParam><Name>国标设备</Name><HeartBeatInterval>60</HeartBeatInterval><Password>secret</Password></BasicParam>`,
	"SVACEncodeConfig":    `<SVACEncodeConfig><ROIParam><ROIFlag>0</ROIFlag></ROIParam><SVCParam><SVCSpaceDomainMode>1</SVCSpaceDomainMode><SVCTimeDomainMode>2</SVCTimeDomainMode></SVCParam><AudioParam><AudioRecognitionFlag>1</AudioRecognitionFlag></AudioParam></SVACEncodeConfig>`,
	"SVACDecodeConfig":    `<SVACDecodeConfig><SVCParam><SVCSTMMode>1</SVCSTMMode></SVCParam><SurveillanceParam><OSDShowFlag>0</OSDShowFlag></SurveillanceParam></SVACDecodeConfig>`,
	"VideoParamAttribute": `<VideoParamAttribute Num="0"/>`,
	"VideoRecordPlan":     `<VideoRecordPlan><RecordEnable>1</RecordEnable><RecordScheduleSumNum>0</RecordScheduleSumNum><StreamNumber>0</StreamNumber></VideoRecordPlan><VideoRecordPlan><RecordEnable>0</RecordEnable><RecordScheduleSumNum>0</RecordScheduleSumNum><StreamNumber>1</StreamNumber></VideoRecordPlan>`,
	"VideoAlarmRecord":    `<VideoAlarmRecord><RecordEnable>1</RecordEnable><StreamNumber>0</StreamNumber></VideoAlarmRecord><VideoAlarmRecord><RecordEnable>0</RecordEnable><StreamNumber>1</StreamNumber></VideoAlarmRecord>`,
	"PictureMask":         `<PictureMask><On>0</On><SumNum>0</SumNum></PictureMask>`,
	"FrameMirror":         `<FrameMirror>2</FrameMirror>`,
	"AlarmReport":         `<AlarmReport><MotionDetection>1</MotionDetection><FieldDetection>0</FieldDetection></AlarmReport>`,
	"OSDConfig":           `<OSDConfig><Length>704</Length><Width>576</Width><TimeX>0</TimeX><TimeY>0</TimeY><SumNum>0</SumNum></OSDConfig>`,
	"SnapShotConfig":      `<SnapShotConfig><SnapNum>1</SnapNum><UploadURL>https://example.test/upload</UploadURL><SessionID>01234567890123456789012345678901</SessionID></SnapShotConfig>`,
}

func TestCascadeConfigWriteAllTypes(t *testing.T) {
	for kind, block := range cascadeWritableTestBlocks {
		t.Run(kind, func(t *testing.T) {
			command, err := ParseCascadeConfigWrite([]byte(`<?xml version="1.0" encoding="UTF-8"?><Control><CmdType>DeviceConfig</CmdType><SN>97</SN><DeviceID>34020000001320000012</DeviceID>` + block + `<ExtraInfo>private</ExtraInfo></Control>`))
			require.NoError(t, err)
			require.Equal(t, 97, command.SN())
			require.Equal(t, "34020000001320000012", command.DeviceID())
			require.Equal(t, []string{kind}, command.Types())
			for _, version := range []protocol.Version{protocol.Version2016, protocol.Version2022} {
				body, err := command.Build(protocol.ProfileFor(version), "34020000001320000002", 11)
				require.NoError(t, err)
				normalized, err := DecodeProfiledXMLBytes(protocol.ProfileFor(version), body)
				require.NoError(t, err)
				require.Contains(t, string(normalized), "<SN>11</SN>")
				require.Contains(t, string(normalized), "<DeviceID>34020000001320000002</DeviceID>")
				require.Contains(t, string(normalized), "<"+kind)
				require.NotContains(t, string(normalized), "secret")
				require.NotContains(t, string(normalized), "private")
				require.NotContains(t, string(normalized), "<Result>")
				if kind == "BasicParam" {
					require.Contains(t, string(normalized), "国标设备")
				}
				if kind == "VideoRecordPlan" || kind == "VideoAlarmRecord" {
					require.Equal(t, 2, strings.Count(string(normalized), "<"+kind+">"))
				}
				if kind == "SVACEncodeConfig" {
					require.NotContains(t, string(normalized), "SVCSpaceSupportMode")
					require.NotContains(t, string(normalized), "ROINumber")
				}
			}
		})
	}
}

func TestCascadeConfigWriteExposesSnapshotSession(t *testing.T) {
	body := `<Control><CmdType>DeviceConfig</CmdType><SN>97</SN><DeviceID>34020000001320000012</DeviceID><SnapShotConfig><SnapNum>2</SnapNum><Interval>3</Interval><UploadURL>https://up.example/upload</UploadURL><SessionID>01234567890123456789012345678901</SessionID></SnapShotConfig></Control>`
	command, err := ParseCascadeConfigWrite([]byte(body))
	require.NoError(t, err)
	snapshot, ok := command.SnapshotConfig()
	require.True(t, ok)
	require.Equal(t, 2, *snapshot.SnapNum)
	require.Equal(t, 3, *snapshot.Interval)
	require.Equal(t, "https://up.example/upload", *snapshot.UploadURL)
	require.Equal(t, "01234567890123456789012345678901", *snapshot.SessionID)
}

func TestCascadeConfigWriteRejectsMalformedAndReadOnly(t *testing.T) {
	for _, block := range []string{
		``, `<VideoParamOpt><DownloadSpeed>1/2</DownloadSpeed></VideoParamOpt>`, `<Unknown>1</Unknown>`,
		`<FrameMirror>1</FrameMirror><FrameMirror>2</FrameMirror>`, `<FrameMirror>9</FrameMirror>`,
		`<AlarmReport><FieldDetection>0</FieldDetection></AlarmReport>`,
		`<VideoRecordPlan><RecordEnable>1</RecordEnable><StreamNumber>0</StreamNumber><RecordScheduleSumNum>1</RecordScheduleSumNum></VideoRecordPlan>`,
		`<PictureMask><On>0</On><SumNum>1</SumNum></PictureMask>`,
		`<SVACEncodeConfig><SVCParam><SVCSpaceDomainMode>1</SVCSpaceDomainMode></SVCParam></SVACEncodeConfig>`,
		`<SVACEncodeConfig><ROIParam><ROIFlag>3</ROIFlag></ROIParam></SVACEncodeConfig>`,
		`<SVACDecodeConfig><SVCParam><SVCSTMMode>4</SVCSTMMode></SVCParam></SVACDecodeConfig>`,
		`<SVACEncodeConfig><SVCParam><SVCSpaceSupportMode>1</SVCSpaceSupportMode></SVCParam></SVACEncodeConfig>`,
		`<SnapShotConfig><SnapNum>1</SnapNum></SnapShotConfig>`,
	} {
		t.Run(block, func(t *testing.T) {
			_, err := ParseCascadeConfigWrite([]byte(fmt.Sprintf(`<Control><CmdType>DeviceConfig</CmdType><SN>97</SN><DeviceID>34020000001320000012</DeviceID>%s</Control>`, block)))
			require.Error(t, err)
		})
	}
	for _, body := range []string{
		`<Response><CmdType>DeviceConfig</CmdType><SN>1</SN><DeviceID>C</DeviceID><FrameMirror>0</FrameMirror></Response>`,
		`<Control><CmdType>DeviceConfig</CmdType><CmdType>DeviceControl</CmdType><SN>1</SN><DeviceID>C</DeviceID><FrameMirror>0</FrameMirror></Control>`,
		`<Control><CmdType>DeviceConfig</CmdType><SN>1</SN><DeviceID>C</DeviceID><FrameMirror>0</FrameMirror></Control><Control/>`,
		`<Control><CmdType>DeviceConfig</CmdType><SN>1</SN><DeviceID>C</DeviceID><FrameMirror>0</FrameMirror></Control>unexpected`,
	} {
		_, err := ParseCascadeConfigWrite([]byte(body))
		require.Error(t, err)
	}
}

func TestCascadeConfigWriteROIPreservesRasterOrdinals(t *testing.T) {
	// A.2.1.21 defines 32x32 raster ordinals without specifying a zero/one origin.
	for _, start := range []int{0, 1} {
		body := fmt.Sprintf(`<Control><CmdType>DeviceConfig</CmdType><SN>1</SN><DeviceID>C</DeviceID><SVACEncodeConfig><ROIParam><ROIFlag>1</ROIFlag><ROINumber>1</ROINumber><Item><ROISeq>1</ROISeq><TopLeft>%d</TopLeft><BottomRight>%d</BottomRight><ROIQP>0</ROIQP></Item></ROIParam></SVACEncodeConfig></Control>`, start, start+1023)
		command, err := ParseCascadeConfigWrite([]byte(body))
		require.NoError(t, err)
		rebuilt, err := command.Build(protocol.ProfileFor(protocol.Version2022), "C", 2)
		require.NoError(t, err)
		require.Contains(t, string(rebuilt), fmt.Sprintf("<BottomRight>%d</BottomRight>", start+1023))
	}
}

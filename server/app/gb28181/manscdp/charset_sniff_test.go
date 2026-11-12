package manscdp

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
	"golang.org/x/text/encoding/simplifiedchinese"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
)

// 线上报障设备 flyvideo-gb-android/1.0 的真实 Catalog 应答：
// 声明 GB18030，但 <Name> 的字节实际是 UTF-8。原样照声明解会得到
// 「椋炶鍥芥爣鎽勫儚澶」（库里现存的乱码）。
const flyvideoCatalogResponse = `<?xml version="1.0" encoding="GB18030"?>
<Response><CmdType>Catalog</CmdType><SN>154</SN>
<DeviceID>45020290931310910004</DeviceID><SumNum>1</SumNum>
<DeviceList Num="1"><Item>
<DeviceID>45020290931310910004</DeviceID><Name>飞视国标摄像头</Name>
<Manufacturer>flyvideo</Manufacturer><Model>flyvideo-gb-h264</Model>
<Owner>flyvideo</Owner><CivilCode>450202</CivilCode><Address/>
<Parental>0</Parental><ParentID>45020290931310910004</ParentID>
<Secrecy>0</Secrecy><RegisterWay>1</RegisterWay><Status>ON</Status>
</Item></DeviceList></Response>`

const flyvideoDeviceInfoResponse = `<?xml version="1.0" encoding="GB2312"?>
<Response><CmdType>DeviceInfo</CmdType><SN>153</SN>
<DeviceID>45020290931310910004</DeviceID><DeviceName>飞视国标摄像头</DeviceName>
<Manufacturer>flyvideo</Manufacturer><Model>flyvideo-gb-h264</Model>
<Firmware>1.0.0</Firmware><Channel>1</Channel></Response>`

// 关键防误判用例：GB2312 的「一」字节 D2BB 恰好是合法 UTF-8
// （会读成西里尔字母 U+04BB），但没有任何 U+0800 以上字符，
// 必须判为「不是 UTF-8」，否则真正的 GB 报文会被读成一串西里尔/希腊字母。
func TestLooksLikeUTF8TextRejectsGBKChinese(t *testing.T) {
	gbk, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte("一一"))
	require.NoError(t, err)
	require.True(t, utf8.Valid(gbk), "前提：这两个字的 GBK 字节恰好是合法 UTF-8")
	require.False(t, looksLikeUTF8Text(gbk), "没有汉字级字符，不能判成 UTF-8")
}

func TestLooksLikeUTF8TextAcceptsUTF8Chinese(t *testing.T) {
	require.True(t, looksLikeUTF8Text([]byte("飞视国标摄像头")))
	require.False(t, looksLikeUTF8Text([]byte("IP CAMERA")), "纯 ASCII 无需改判")
}

// 声明 GB18030、内容 UTF-8：规范化后必须变成「声明白 UTF-8 + UTF-8 字节」，且中文完好。
func TestNormalizeIncomingXMLRetagsUTF8ContentDeclaredAsGB(t *testing.T) {
	out := NormalizeIncomingXML([]byte(flyvideoCatalogResponse))

	require.NotEqual(t, flyvideoCatalogResponse, string(out), "声明与字节冲突时必须纠偏")
	require.True(t, utf8.Valid(out))
	require.True(t, strings.HasPrefix(string(out), `<?xml version="1.0" encoding="UTF-8"?>`),
		"声明要改写为 UTF-8，实际=%q", string(out[:40]))
	require.Contains(t, string(out), "飞视国标摄像头")
	require.NotContains(t, string(out), "椋炶")
}

// 真 GB2312 报文（声明与字节一致）必须一个字节都不改，保持既有行为。
func TestNormalizeIncomingXMLKeepsRealGB2312Untouched(t *testing.T) {
	payload, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(`<Response><CmdType>Catalog</CmdType><DeviceID>1</DeviceID><DeviceList Num="0"></DeviceList></Response>`))
	require.NoError(t, err)
	body := append([]byte(`<?xml version="1.0" encoding="GB2312"?>`), payload...)

	out := NormalizeIncomingXML(body)
	require.True(t, bytes.Equal(body, out), "声明与字节一致时绝不能改动")
}

// 反向错配：声明 UTF-8、实际 GB2312，也要按内容解出中文。
func TestNormalizeIncomingXMLDecodesGBContentDeclaredAsUTF8(t *testing.T) {
	payload, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(`<Response><DeviceName>视频动检</DeviceName></Response>`))
	require.NoError(t, err)
	body := append([]byte(`<?xml version="1.0" encoding="UTF-8"?>`), payload...)

	out := NormalizeIncomingXML(body)
	require.True(t, utf8.Valid(out))
	require.Contains(t, string(out), "视频动检")
}

// 端到端：客户真实报文经 Catalog 解析后，设备名必须是正确中文（线上乱码的回归锁）。
func TestParseCatalogResponseDecodesUTF8NameDeclaredAsGB18030(t *testing.T) {
	resp, err := ParseCatalogResponse([]byte(flyvideoCatalogResponse))
	require.NoError(t, err)
	require.Len(t, resp.DeviceList.Items, 1)
	require.Equal(t, "飞视国标摄像头", resp.DeviceList.Items[0].Name)
}

func TestParseDeviceInfoResponseDecodesUTF8NameDeclaredAsGB2312(t *testing.T) {
	resp, err := ParseDeviceInfoResponse([]byte(flyvideoDeviceInfoResponse))
	require.NoError(t, err)
	require.Equal(t, "飞视国标摄像头", resp.DeviceName)
	require.Equal(t, "flyvideo", resp.Manufacturer)
}

// 级联/查询路径（DecodeProfiledXML）同一条判据。
func TestDecodeProfiledXMLPrefersUTF8ContentOverGBDeclaration(t *testing.T) {
	profile := protocol.ProfileFor(protocol.Version2016) // profile 默认 GB2312

	var decoded struct {
		DeviceName string `xml:"DeviceName"`
	}
	require.NoError(t, DecodeProfiledXML(profile, []byte(flyvideoDeviceInfoResponse), &decoded))
	require.Equal(t, "飞视国标摄像头", decoded.DeviceName)
}

// 真 GB2312 内容 + 声明 GB2312 仍然正确（防止把既有正确行为改坏）。
func TestDecodeProfiledXMLKeepsRealGB2312Content(t *testing.T) {
	profile := protocol.ProfileFor(protocol.Version2016)
	payload, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(`<Response><DeviceName>视频动检</DeviceName></Response>`))
	require.NoError(t, err)
	body := append([]byte(`<?xml version="1.0" encoding="GB2312"?>`), payload...)

	var decoded struct {
		DeviceName string `xml:"DeviceName"`
	}
	require.NoError(t, DecodeProfiledXML(profile, body, &decoded))
	require.Equal(t, "视频动检", decoded.DeviceName)
}

// 报警原文落库路径同样纠偏，避免把乱码写进 raw_summary。
func TestDecodeToUTF8RetagsUTF8ContentDeclaredAsGB(t *testing.T) {
	out := DecodeToUTF8([]byte(flyvideoCatalogResponse))
	require.True(t, utf8.Valid(out))
	require.Contains(t, string(out), "飞视国标摄像头")
	require.NotContains(t, string(out), "椋炶")
}

// ---- SniffIncomingCharset：设备详情「上报字符集」的展示口径 ----

func TestSniffIncomingCharsetDeclaredMatchesContent(t *testing.T) {
	payload, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(`<Response><DeviceName>视频动检</DeviceName></Response>`))
	require.NoError(t, err)
	body := append([]byte(`<?xml version="1.0" encoding="GB2312"?>`), payload...)

	report := SniffIncomingCharset(body)
	require.Equal(t, "GB2312", report.Declared)
	require.Equal(t, "GB2312", report.Effective, "声明与字节一致时按声明解")
	require.False(t, report.Converted)
}

func TestSniffIncomingCharsetReportedLies(t *testing.T) {
	// 线上报障设备的真实形态：声明 GB18030、内容 UTF-8。
	report := SniffIncomingCharset([]byte(flyvideoCatalogResponse))
	require.Equal(t, "GB18030", report.Declared)
	require.Equal(t, "UTF-8", report.Effective, "内容优先改判")
	require.True(t, report.Converted)
}

func TestSniffIncomingCharsetDeclaredUTF8ButGBContent(t *testing.T) {
	payload, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(`<Response><DeviceName>视频动检</DeviceName></Response>`))
	require.NoError(t, err)
	body := append([]byte(`<?xml version="1.0" encoding="UTF-8"?>`), payload...)

	report := SniffIncomingCharset(body)
	require.Equal(t, "UTF-8", report.Declared)
	require.Equal(t, "GB18030", report.Effective)
	require.True(t, report.Converted)
}

func TestSniffIncomingCharsetUndeclaredUTF8(t *testing.T) {
	report := SniffIncomingCharset([]byte(`<Response><DeviceName>飞视国标摄像头</DeviceName></Response>`))
	require.Empty(t, report.Declared, "无声明")
	// 无声明且内容是合法 UTF-8：resolveWireCharset 不改判，此时实际按 profile 默认解。
	require.Empty(t, report.Effective)
	require.False(t, report.Converted)
}

func TestSniffIncomingCharsetUndeclaredGBContent(t *testing.T) {
	payload, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(`<Response><DeviceName>视频动检</DeviceName></Response>`))
	require.NoError(t, err)

	report := SniffIncomingCharset(payload)
	require.Empty(t, report.Declared)
	require.Equal(t, "GB18030", report.Effective, "无声明但字节是 GB 家族")
	require.True(t, report.Converted, "converted = 以内容为准（无论声明是否在场）")
}

func TestSniffIncomingCharsetEmptyAndPureASCII(t *testing.T) {
	require.Equal(t, CharsetReport{}, SniffIncomingCharset(nil))
	report := SniffIncomingCharset([]byte(`<?xml version="1.0" encoding="UTF-8"?><Notify><CmdType>Keepalive</CmdType></Notify>`))
	require.Equal(t, "UTF-8", report.Declared)
	require.Equal(t, "UTF-8", report.Effective)
	require.False(t, report.Converted)
}

func TestSniffIncomingCharsetUnknownDeclarationKeptVerbatim(t *testing.T) {
	body := []byte(`<?xml version="1.0" encoding="ISO-8859-1"?><Response><DeviceName>x</DeviceName></Response>`)
	report := SniffIncomingCharset(body)
	require.Equal(t, "iso-8859-1", report.Declared, "不认识的声明原样保留小写")
}

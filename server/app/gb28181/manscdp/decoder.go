package manscdp

import (
	"bytes"
	"encoding/xml"

	"golang.org/x/net/html/charset"
)

// newDecoder 创建支持 GB2312/GB18030 等非 UTF-8 编码的 XML decoder
// 国标设备 XML 声明常为 <?xml version="1.0" encoding="GB2312"?>
//
// ⭐ 解码前先做「内容优先」纠偏（见 charset_sniff.go）：声明写 GB 家族、
// 字节实际是 UTF-8 的设备（模板抄声明、应用层发 UTF-8）会被改判为 UTF-8，
// 否则设备名等中文字段整串乱码。声明与字节一致时 NormalizeIncomingXML
// 原样返回，行为不变。
func newDecoder(body []byte) *xml.Decoder {
	d := xml.NewDecoder(bytes.NewReader(NormalizeIncomingXML(body)))
	d.CharsetReader = charset.NewReaderLabel
	return d
}

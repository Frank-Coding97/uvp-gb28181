package manscdp

import (
	"bytes"
	"encoding/xml"

	"golang.org/x/net/html/charset"
)

// newDecoder 创建支持 GB2312/GB18030 等非 UTF-8 编码的 XML decoder
// 国标设备 XML 声明常为 <?xml version="1.0" encoding="GB2312"?>
func newDecoder(body []byte) *xml.Decoder {
	d := xml.NewDecoder(bytes.NewReader(body))
	d.CharsetReader = charset.NewReaderLabel
	return d
}

package manscdp

import (
	"regexp"

	"golang.org/x/net/html/charset"
)

// xmlEncodingRe 从 <?xml ... encoding="..."?> 声明里提取 encoding label。
// 只在前 128 字节内匹配,避免扫描整份 body。
var xmlEncodingRe = regexp.MustCompile(`(?i)<\?xml[^>]*\bencoding\s*=\s*["']([^"']+)["']`)

// DecodeToUTF8 根据 XML 声明中的 encoding 属性(GB2312 / GB18030 / UTF-8 等)
// 把 body 转换成 UTF-8 字节序列后返回;声明缺失、无法识别或本身就是 UTF-8 时原样返回。
// 用于把原始 SIP MESSAGE / NOTIFY body 落库前统一到 UTF-8,避免 utf8mb4 列拒收 GB2312 字节串。
// 转换失败时降级返回原 body。
func DecodeToUTF8(body []byte) []byte {
	if len(body) == 0 {
		return body
	}
	head := body
	if len(head) > 128 {
		head = head[:128]
	}
	m := xmlEncodingRe.FindSubmatch(head)
	if len(m) < 2 {
		return body
	}
	enc, _ := charset.Lookup(string(m[1]))
	if enc == nil {
		return body
	}
	out, err := enc.NewDecoder().Bytes(body)
	if err != nil || len(out) == 0 {
		return body
	}
	return out
}

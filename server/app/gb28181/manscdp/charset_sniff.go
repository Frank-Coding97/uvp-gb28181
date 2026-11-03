package manscdp

import (
	"unicode/utf8"

	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
)

// 设备的字符集声明并不可信。GB/T 28181 报文模板里的 encoding 常被厂商原样
// 抄成 GB2312 / GB18030，而应用层又按 UTF-8 填字段。线上报障设备
// flyvideo-gb-android/1.0 就是「声明 GB18030、内容 UTF-8」，设备名整串乱码
// （飞视国标摄像头 → 椋炶鍥芥爣鎽勫儚澶）。
//
// 所以解码策略改为「内容优先、声明兜底」：
//
//  1. 声明是 GB 家族，但字节本身是带汉字（≥ U+0800）的合法 UTF-8 → 按 UTF-8 解；
//  2. 声明是 UTF-8（或无声明 / profile 默认 UTF-8），但字节根本不是 UTF-8，
//     且整串找不到任何合法 UTF-8 汉字 → 按 GB18030 解（GBK/GB2312 的超集）；
//  3. 其余情况（声明与字节一致、纯 ASCII）行为完全不变。
//
// 判据为什么是 U+0800，而不是「合法 UTF-8 就行」：GB2312/GBK 的中文是双字节，
// 整串被误当 UTF-8 读时，每一对字节最多只能凑成 U+0080..U+07FF 的拉丁/希腊/
// 西里尔字符（「一」= D2BB → U+04BB 西里尔字母），要读出 U+0800 及以上必须出现
// 真正的三字节序列。GB 双字节流跨对对齐成三字节 UTF-8 的概率极低，所以不会把
// 真正的 GB2312 报文误判成 UTF-8（那样会把中文读成一串西里尔字母，比乱码更难发现）。
const utf8CJKFloor = 0x0800

// looksLikeUTF8Text 判断字节串是否「确定是 UTF-8 文本」：整串合法且至少含一个
// U+0800 以上的字符（对中文场景即至少一个汉字）。
func looksLikeUTF8Text(body []byte) bool {
	if len(body) == 0 || !utf8.Valid(body) {
		return false
	}
	return hasUTF8CJK(body)
}

// hasUTF8CJK 判断字节串里是否存在合法的 UTF-8 汉字级字符。不要求整串合法：
// 用来识别「被截断/夹了坏字节的 UTF-8」，避免把它当 GB 编码整串重解。
func hasUTF8CJK(body []byte) bool {
	for i := 0; i < len(body); {
		r, size := utf8.DecodeRune(body[i:])
		if r >= utf8CJKFloor && r != utf8.RuneError {
			return true
		}
		i += size
	}
	return false
}

func hasHighBytes(body []byte) bool {
	for _, b := range body {
		if b >= 0x80 {
			return true
		}
	}
	return false
}

// resolveWireCharset 按「内容优先」给出报文实际需要的字符集。
// declared 是报文声明（无声明时传 profile 默认值）。第二个返回值 converted
// 表示「声明与字节冲突，已改判」——调用方据此把 XML 声明改写为 UTF-8。
func resolveWireCharset(declared protocol.Charset, body []byte) (protocol.Charset, bool) {
	if len(body) == 0 {
		return declared, false
	}
	switch declared {
	case protocol.Charset(protocol.CharsetGB2312), protocol.Charset(protocol.CharsetGB18030):
		// 声明 GB、实际 UTF-8：把 UTF-8 字节当 GB 解就是乱码，改判 UTF-8。
		if looksLikeUTF8Text(body) {
			return protocol.Charset(protocol.CharsetUTF8), true
		}
	case protocol.Charset(protocol.CharsetUTF8), "":
		// 声明 UTF-8（或无声明），但字节不是 UTF-8 且没有任何 UTF-8 汉字：
		// 只可能是 GB 家族编码，按 GB18030 解。
		if !utf8.Valid(body) && !hasUTF8CJK(body) && hasHighBytes(body) {
			return protocol.Charset(protocol.CharsetGB18030), true
		}
	}
	return declared, false
}

// NormalizeIncomingXML 在交给 XML 解析器之前，把「声明与字节冲突」的报文
// 统一成 UTF-8（含把声明改写为 UTF-8）。声明与字节一致时原样返回入参，
// 不做任何拷贝。
func NormalizeIncomingXML(body []byte) []byte {
	if len(body) == 0 {
		return body
	}
	label, declared := xmlEncodingLabel(body)
	declaredCharset := protocol.Charset("")
	if declared {
		canonical := canonicalCharset(label)
		if canonical == "" {
			// 不认识的声明交给下层维持既有错误语义（newDecoder 的
			// CharsetReader / DecodeProfiledXML 的类型化错误）。
			return body
		}
		declaredCharset = canonical
	}

	charset, converted := resolveWireCharset(declaredCharset, body)
	if !converted {
		return body
	}

	normalized := body
	if charset != protocol.Charset(protocol.CharsetUTF8) {
		_, encoder, err := encodingForLabel(string(charset))
		if err != nil || encoder == nil {
			return body
		}
		decoded, err := encoder.NewDecoder().Bytes(body)
		if err != nil || len(decoded) == 0 {
			return body
		}
		normalized = decoded
	}
	if declared {
		normalized = normalizeXMLDeclarationEncoding(normalized)
	}
	return normalized
}

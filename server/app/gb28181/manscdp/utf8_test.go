package manscdp

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

// GB2312 字节:视频动检
var gb2312VideoMotion = []byte{0xCA, 0xD3, 0xC6, 0xB5, 0xB6, 0xAF, 0xBC, 0xEC}

func TestDecodeToUTF8_GB2312XML(t *testing.T) {
	body := append([]byte(`<?xml version="1.0" encoding="GB2312"?><Root><Desc>`), gb2312VideoMotion...)
	body = append(body, []byte(`</Desc></Root>`)...)

	out := DecodeToUTF8(body)

	require.True(t, utf8.Valid(out), "输出应为合法 UTF-8")
	require.True(t, strings.Contains(string(out), "视频动检"), "GB2312 字节应被转成对应中文,实际=%q", string(out))
}

func TestDecodeToUTF8_UTF8NoDeclaration(t *testing.T) {
	body := []byte(`<Root><Desc>hello 世界</Desc></Root>`)

	out := DecodeToUTF8(body)

	require.True(t, utf8.Valid(out))
	require.Equal(t, string(body), string(out), "无 encoding 声明的 UTF-8 body 应原样返回")
}

func TestDecodeToUTF8_EmptyBody(t *testing.T) {
	require.NotPanics(t, func() { _ = DecodeToUTF8(nil) })
	require.NotPanics(t, func() { _ = DecodeToUTF8([]byte{}) })
	require.Len(t, DecodeToUTF8(nil), 0)
	require.Len(t, DecodeToUTF8([]byte{}), 0)
}

func TestDecodeToUTF8_UnsupportedCharsetFallsBack(t *testing.T) {
	body := []byte(`<?xml version="1.0" encoding="not-a-real-charset"?><Root/>`)

	require.NotPanics(t, func() { _ = DecodeToUTF8(body) })
	out := DecodeToUTF8(body)
	require.Equal(t, string(body), string(out), "无法识别 encoding 时应降级返回原字节")
}

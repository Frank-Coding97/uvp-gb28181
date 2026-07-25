package manscdp

import (
	"bytes"
	"encoding/xml"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
	"golang.org/x/text/encoding/simplifiedchinese"
	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
)

type codecFixture struct {
	XMLName    xml.Name `xml:"Root"`
	DeviceName string   `xml:"DeviceName"`
}

func TestMarshalProfiledXMLGB2312UsesCompatibleBytes(t *testing.T) {
	profile := protocol.ProfileFor(protocol.Version2016)
	value := codecFixture{DeviceName: "视频动检兼容"}

	body, err := MarshalProfiledXML(profile, value)
	require.NoError(t, err)
	require.True(t, bytes.HasPrefix(body, []byte(`<?xml version="1.0" encoding="GB2312"?>`)))
	require.False(t, bytes.Contains(body, []byte("视频动检兼容")), "GB2312 body must contain encoded bytes, not UTF-8 text")

	want, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte("视频动检兼容"))
	require.NoError(t, err)
	require.True(t, bytes.Contains(body, want))

	var decoded struct {
		DeviceName string `xml:"DeviceName"`
	}
	require.NoError(t, DecodeProfiledXML(profile, body, &decoded))
	require.Equal(t, "视频动检兼容", decoded.DeviceName)
}

func TestMarshalProfiledXMLGB18030RoundTripsExtensionCharacter(t *testing.T) {
	profile := protocol.ProfileFor(protocol.Version2022)
	value := codecFixture{DeviceName: "中文𠀀"}

	body, err := MarshalProfiledXML(profile, value)
	require.NoError(t, err)
	require.True(t, bytes.HasPrefix(body, []byte(`<?xml version="1.0" encoding="GB18030"?>`)))

	want, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte(value.DeviceName))
	require.NoError(t, err)
	require.True(t, bytes.Contains(body, want))

	var decoded struct {
		DeviceName string `xml:"DeviceName"`
	}
	require.NoError(t, DecodeProfiledXML(profile, body, &decoded))
	require.Equal(t, value.DeviceName, decoded.DeviceName)
}

func TestMarshalProfiledXMLDeclarationMatchesActualBytes(t *testing.T) {
	for _, tc := range []struct {
		name    string
		profile protocol.Profile
		text    string
	}{
		{name: "2016", profile: protocol.ProfileFor(protocol.Version2016), text: "视频动检"},
		{name: "2022", profile: protocol.ProfileFor(protocol.Version2022), text: "视频𠀀"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, err := MarshalProfiledXML(tc.profile, struct {
				XMLName xml.Name `xml:"Root"`
				Text    string   `xml:"Text"`
			}{Text: tc.text})
			require.NoError(t, err)

			// DecodeProfiledXMLBytes validates the declaration and converts the
			// exact wire bytes back to UTF-8 before XML parsing.
			normalized, err := DecodeProfiledXMLBytes(tc.profile, body)
			require.NoError(t, err)
			require.True(t, utf8.Valid(normalized))
			require.Contains(t, string(normalized), tc.text)
		})
	}
}

func TestDecodeProfiledXMLUsesProfileWhenDeclarationIsMissing(t *testing.T) {
	profile := protocol.ProfileFor(protocol.Version2016)
	payload, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(`<Root><DeviceName>视频动检</DeviceName></Root>`))
	require.NoError(t, err)

	var decoded struct {
		DeviceName string `xml:"DeviceName"`
	}
	require.NoError(t, DecodeProfiledXML(profile, payload, &decoded))
	require.Equal(t, "视频动检", decoded.DeviceName)
}

func TestDecodeProfiledXMLDeclarationOverridesProfile(t *testing.T) {
	profile := protocol.ProfileFor(protocol.Version2016)
	payload, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte(`<Root><DeviceName>中文𠀀</DeviceName></Root>`))
	require.NoError(t, err)
	body := append([]byte(`<?xml version="1.0" encoding="GB18030"?>`), payload...)

	var decoded struct {
		DeviceName string `xml:"DeviceName"`
	}
	require.NoError(t, DecodeProfiledXML(profile, body, &decoded))
	require.Equal(t, "中文𠀀", decoded.DeviceName)
}

func TestMarshalProfiledXMLUnencodableGB2312ReturnsTypedError(t *testing.T) {
	profile := protocol.ProfileFor(protocol.Version2016)
	_, err := MarshalProfiledXML(profile, codecFixture{DeviceName: "𠀀"})

	var codecErr *CodecError
	require.ErrorAs(t, err, &codecErr)
	require.Equal(t, CodecErrorEncode, codecErr.Kind)
	require.Equal(t, protocol.Charset(protocol.CharsetGB2312), codecErr.Charset)
	require.ErrorIs(t, err, ErrXMLCoding)
}

func TestMarshalProfiledXMLUnsupportedCharsetReturnsTypedError(t *testing.T) {
	profile := protocol.Profile{Version: protocol.Version(protocol.Version2016), Charset: protocol.Charset("ISO-8859-1")}
	_, err := MarshalProfiledXML(profile, struct {
		DeviceName string `xml:"DeviceName"`
	}{DeviceName: "test"})

	var codecErr *CodecError
	require.ErrorAs(t, err, &codecErr)
	require.Equal(t, CodecErrorUnsupportedCharset, codecErr.Kind)
	require.Equal(t, protocol.Charset("ISO-8859-1"), codecErr.Charset)
	require.ErrorIs(t, err, ErrUnsupportedCharset)
}

func TestMarshalProfiledXMLExplicitUTF8IsAvailableForExtensions(t *testing.T) {
	profile := protocol.Profile{Version: protocol.Version(protocol.Version2016), Charset: protocol.Charset(protocol.CharsetUTF8)}
	body, err := MarshalProfiledXML(profile, codecFixture{DeviceName: "中文𠀀"})
	require.NoError(t, err)
	require.True(t, utf8.Valid(body))
	require.True(t, strings.HasPrefix(string(body), `<?xml version="1.0" encoding="UTF-8"?>`))
	require.Contains(t, string(body), "中文𠀀")
}

func TestDecodeProfiledXMLUnsupportedDeclarationReturnsTypedError(t *testing.T) {
	body := []byte(`<?xml version="1.0" encoding="ISO-8859-1"?><Root/>`)
	var decoded struct{}
	err := DecodeProfiledXML(protocol.ProfileFor(protocol.Version2016), body, &decoded)
	var codecErr *CodecError
	require.ErrorAs(t, err, &codecErr)
	require.Equal(t, CodecErrorUnsupportedCharset, codecErr.Kind)
	require.ErrorIs(t, err, ErrUnsupportedCharset)
	require.True(t, errors.Is(err, ErrUnsupportedCharset))
}

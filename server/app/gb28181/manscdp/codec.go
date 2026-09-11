package manscdp

import (
	"encoding/xml"
	"errors"
	"fmt"
	"regexp"
	"strings"

	xencoding "golang.org/x/text/encoding"
	"golang.org/x/text/encoding/simplifiedchinese"
	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
)

// CodecErrorKind identifies the stage at which a profiled XML operation
// failed. Keeping the stage in a typed error lets callers distinguish an
// unsupported profile from text that cannot be represented by that profile.
type CodecErrorKind string

const (
	CodecErrorUnsupportedCharset CodecErrorKind = "unsupported_charset"
	CodecErrorEncode             CodecErrorKind = "encode"
	CodecErrorDecode             CodecErrorKind = "decode"
)

var (
	// ErrUnsupportedCharset is returned (via CodecError) when a profile or XML
	// declaration names an encoding that this protocol layer does not support.
	ErrUnsupportedCharset = errors.New("manscdp: unsupported XML charset")
	// ErrXMLCoding identifies a failure while converting XML bytes between UTF-8
	// and a GB/T 28181 wire charset.
	ErrXMLCoding = errors.New("manscdp: XML charset conversion failed")
)

// CodecError is the structured error returned by profiled XML encoding and
// decoding. Charset is the canonical name when it was recognized, otherwise
// it contains the original label supplied by the caller/device.
type CodecError struct {
	Kind    CodecErrorKind
	Charset protocol.Charset
	Err     error
}

func (e *CodecError) Error() string {
	if e == nil {
		return ""
	}
	message := fmt.Sprintf("manscdp: XML %s", e.Kind)
	if e.Charset != "" {
		message += fmt.Sprintf(" charset %q", e.Charset)
	}
	if e.Err != nil {
		message += ": " + e.Err.Error()
	}
	return message
}

func (e *CodecError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (e *CodecError) Is(target error) bool {
	if e == nil {
		return false
	}
	switch target {
	case ErrUnsupportedCharset:
		return e.Kind == CodecErrorUnsupportedCharset
	case ErrXMLCoding:
		return e.Kind == CodecErrorEncode || e.Kind == CodecErrorDecode
	default:
		return false
	}
}

// EncodingError is kept as a descriptive compatibility alias for callers
// that only care that a typed charset conversion error was returned.
type EncodingError = CodecError

// XMLCodecError is an explicit alias for integrations that use XML-oriented
// terminology in their error handling.
type XMLCodecError = CodecError

// profiledXMLDeclarationRe only inspects the XML declaration. XML sent by
// devices is small here, so scanning the first kilobyte is enough and avoids
// treating text content that happens to contain "encoding=" as metadata.
var profiledXMLDeclarationRe = regexp.MustCompile(`(?is)<\?xml\b[^>]*\?>`)

// MarshalProfiledXML marshals value as UTF-8 XML first, then converts the
// payload to the profile's actual wire charset. The declaration is generated
// from that same charset; changing only the declaration is intentionally not
// supported.
//
// A zero-char profile follows the version defaults: 2016/unknown uses the
// GB2312-compatible GBK encoder and 2022 uses GB18030. UTF-8 is accepted only
// when explicitly selected for a vendor extension profile.
func MarshalProfiledXML(profile protocol.Profile, value any) ([]byte, error) {
	charset, encoder, err := profileCharset(profile)
	if err != nil {
		return nil, err
	}

	payload, err := xml.Marshal(value)
	if err != nil {
		return nil, err
	}
	if encoder != nil {
		payload, err = encoder.NewEncoder().Bytes(payload)
		if err != nil {
			return nil, &CodecError{Kind: CodecErrorEncode, Charset: charset, Err: err}
		}
	}

	declaration := []byte(fmt.Sprintf("<?xml version=\"1.0\" encoding=\"%s\"?>\r\n", charset))
	return append(declaration, payload...), nil
}

// MarshalProfiled is a short alias for MarshalProfiledXML.
func MarshalProfiled(profile protocol.Profile, value any) ([]byte, error) {
	return MarshalProfiledXML(profile, value)
}

// DecodeProfiledXML decodes a MANSCDP XML body into value. A declared charset
// wins over the profile; when the declaration is absent, the profile charset
// is used. Bytes are normalized to UTF-8 before encoding/xml sees them so a
// declaration naming GB2312/GB18030 cannot be accidentally applied twice.
func DecodeProfiledXML(profile protocol.Profile, body []byte, value any) error {
	if len(body) == 0 {
		return xml.Unmarshal(body, value)
	}

	label, declared := xmlEncodingLabel(body)
	if !declared {
		var err error
		var profileLabel protocol.Charset
		profileLabel, _, err = profileCharset(profile)
		label = string(profileLabel)
		if err != nil {
			return err
		}
	}

	charset, encoder, err := encodingForLabel(label)
	if err != nil {
		return err
	}
	normalized := body
	if encoder != nil {
		normalized, err = encoder.NewDecoder().Bytes(body)
		if err != nil {
			return &CodecError{Kind: CodecErrorDecode, Charset: charset, Err: err}
		}
	}
	if declared && charset != protocol.Charset(protocol.CharsetUTF8) {
		normalized = normalizeXMLDeclarationEncoding(normalized)
	}
	if err := xml.Unmarshal(normalized, value); err != nil {
		return err
	}
	return nil
}

// DecodeProfiledXMLBytes converts an XML body to UTF-8 using the same
// declaration/profile rules as DecodeProfiledXML. It is useful to callers
// that need to persist normalized raw XML before parsing it.
func DecodeProfiledXMLBytes(profile protocol.Profile, body []byte) ([]byte, error) {
	if len(body) == 0 {
		return body, nil
	}
	label, declared := xmlEncodingLabel(body)
	if !declared {
		var err error
		var profileLabel protocol.Charset
		profileLabel, _, err = profileCharset(profile)
		label = string(profileLabel)
		if err != nil {
			return nil, err
		}
	}
	charset, encoder, err := encodingForLabel(label)
	if err != nil {
		return nil, err
	}
	normalized := body
	if encoder != nil {
		normalized, err = encoder.NewDecoder().Bytes(body)
		if err != nil {
			return nil, &CodecError{Kind: CodecErrorDecode, Charset: charset, Err: err}
		}
	}
	if declared && charset != protocol.Charset(protocol.CharsetUTF8) {
		normalized = normalizeXMLDeclarationEncoding(normalized)
	}
	return normalized, nil
}

// DecodeProfiled is a short alias for DecodeProfiledXML.
func DecodeProfiled(profile protocol.Profile, body []byte, value any) error {
	return DecodeProfiledXML(profile, body, value)
}

func profileCharset(profile protocol.Profile) (protocol.Charset, xencoding.Encoding, error) {
	label := strings.TrimSpace(string(profile.Charset))
	if label == "" {
		if string(profile.Version) == string(protocol.Version2022) {
			label = string(protocol.CharsetGB18030)
		} else {
			label = string(protocol.CharsetGB2312)
		}
	}
	return encodingForLabel(label)
}

func encodingForLabel(label string) (protocol.Charset, xencoding.Encoding, error) {
	canonical := canonicalCharset(label)
	if canonical == "" {
		return protocol.Charset(strings.TrimSpace(label)), nil, &CodecError{
			Kind:    CodecErrorUnsupportedCharset,
			Charset: protocol.Charset(strings.TrimSpace(label)),
			Err:     ErrUnsupportedCharset,
		}
	}
	switch canonical {
	case protocol.Charset(protocol.CharsetGB2312):
		// GB/T 28181-2016 calls this GB2312. GBK is the compatible superset
		// used by deployed devices and by the standard Go text encoder.
		return canonical, simplifiedchinese.GBK, nil
	case protocol.Charset(protocol.CharsetGB18030):
		return canonical, simplifiedchinese.GB18030, nil
	case protocol.Charset(protocol.CharsetUTF8):
		return canonical, nil, nil
	default:
		return canonical, nil, &CodecError{Kind: CodecErrorUnsupportedCharset, Charset: canonical, Err: ErrUnsupportedCharset}
	}
}

func canonicalCharset(label string) protocol.Charset {
	switch strings.ToUpper(strings.TrimSpace(label)) {
	case "GB2312", "GB_2312-80", "GB_2312-1980", "GBK", "CP936", "MS936", "WINDOWS-936":
		return protocol.Charset(protocol.CharsetGB2312)
	case "GB18030", "GB-18030":
		return protocol.Charset(protocol.CharsetGB18030)
	case "UTF-8", "UTF8", "UNICODE-1-1-UTF-8":
		return protocol.Charset(protocol.CharsetUTF8)
	default:
		return ""
	}
}

func xmlEncodingLabel(body []byte) (string, bool) {
	head := body
	if len(head) > 1024 {
		head = head[:1024]
	}
	match := xmlEncodingRe.FindSubmatch(head)
	if len(match) < 2 {
		return "", false
	}
	return strings.TrimSpace(string(match[1])), true
}

func normalizeXMLDeclarationEncoding(body []byte) []byte {
	head := body
	if len(head) > 1024 {
		head = head[:1024]
	}
	declaration := profiledXMLDeclarationRe.FindIndex(head)
	if declaration == nil {
		return body
	}
	decl := body[declaration[0]:declaration[1]]
	encodingMatch := xmlEncodingRe.FindSubmatchIndex(decl)
	if len(encodingMatch) < 4 {
		return body
	}
	start := declaration[0] + encodingMatch[2]
	end := declaration[0] + encodingMatch[3]
	normalized := make([]byte, 0, len(body)-end+start+len("UTF-8"))
	normalized = append(normalized, body[:start]...)
	normalized = append(normalized, "UTF-8"...)
	normalized = append(normalized, body[end:]...)
	return normalized
}

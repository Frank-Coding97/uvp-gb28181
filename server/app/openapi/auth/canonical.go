package auth

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	maxBodyBytes  = 64 * 1024
	maxQueryBytes = 8 * 1024
)

var errInvalidInput = errors.New("invalid signature input")

// SignatureInput contains the request fields that participate in the v1
// canonical signature. Body is hashed exactly as received; it is never
// decoded and re-serialized for signing.
type SignatureInput struct {
	Method      string
	Path        string
	RawQuery    string
	ContentType string
	Body        []byte
	AccessKey   string
	Timestamp   string
	Nonce       string
	Audience    string
}

// HeaderValues represents all values observed for the v1 authentication and
// transport headers. A slice is intentional: duplicate and comma-combined
// header values must be rejected before any business handler is reached.
type HeaderValues struct {
	SignVersion     []string
	AccessKey       []string
	Timestamp       []string
	Nonce           []string
	Signature       []string
	ContentType     []string
	ContentEncoding []string
	MethodOverride  []string
}

// Headers contains the validated single-value authentication headers.
type Headers struct {
	SignVersion string
	AccessKey   string
	Timestamp   string
	Nonce       string
	Signature   string
	ContentType string
}

// CanonicalString returns the ten-line v1 canonical string with UTF-8 LF
// separators and no final LF.
func CanonicalString(input SignatureInput) (string, error) {
	method, err := normalizeMethod(input.Method)
	if err != nil {
		return "", errInvalidInput
	}
	if err := validateAccessKey(input.AccessKey); err != nil {
		return "", errInvalidInput
	}
	if err := validateTimestamp(input.Timestamp); err != nil {
		return "", errInvalidInput
	}
	if err := validateNonce(input.Nonce); err != nil {
		return "", errInvalidInput
	}
	if err := validateLineField(input.Audience); err != nil {
		return "", errInvalidInput
	}
	if err := validatePath(input.Path); err != nil {
		return "", errInvalidInput
	}
	canonicalQuery, err := canonicalQuery(input.RawQuery)
	if err != nil {
		return "", errInvalidInput
	}
	if len(input.Body) > maxBodyBytes {
		return "", errInvalidInput
	}

	contentType := input.ContentType
	switch method {
	case "GET":
		if contentType != "" || len(input.Body) != 0 {
			return "", errInvalidInput
		}
	case "POST":
		if contentType != "application/json" {
			return "", errInvalidInput
		}
	default:
		if contentType != "" && contentType != "application/json" {
			return "", errInvalidInput
		}
	}
	if contentType == "application/json" && len(input.Body) > 0 {
		if err := validateJSONBody(input.Body); err != nil {
			return "", errInvalidInput
		}
	}

	bodyHash := sha256.Sum256(input.Body)
	return strings.Join([]string{
		"UVP-HMAC-SHA256/1",
		input.AccessKey,
		input.Timestamp,
		input.Nonce,
		method,
		input.Path,
		canonicalQuery,
		contentType,
		hex.EncodeToString(bodyHash[:]),
		input.Audience,
	}, "\n"), nil
}

// ParseHeaders validates the authentication and transport header cardinality
// and syntax without touching request business data.
func ParseHeaders(method string, values HeaderValues) (Headers, error) {
	normalizedMethod, err := normalizeMethod(method)
	if err != nil {
		return Headers{}, errInvalidInput
	}
	signVersion, err := oneHeader(values.SignVersion)
	if err != nil || signVersion != "1" {
		return Headers{}, errInvalidInput
	}
	accessKey, err := oneHeader(values.AccessKey)
	if err != nil || validateAccessKey(accessKey) != nil {
		return Headers{}, errInvalidInput
	}
	timestamp, err := oneHeader(values.Timestamp)
	if err != nil || validateTimestamp(timestamp) != nil {
		return Headers{}, errInvalidInput
	}
	nonce, err := oneHeader(values.Nonce)
	if err != nil || validateNonce(nonce) != nil {
		return Headers{}, errInvalidInput
	}
	signature, err := oneHeader(values.Signature)
	if err != nil || !isLowerHex(signature, 32) {
		return Headers{}, errInvalidInput
	}
	if len(values.ContentEncoding) != 0 || len(values.MethodOverride) != 0 {
		return Headers{}, errInvalidInput
	}
	if normalizedMethod == "GET" {
		if len(values.ContentType) != 0 {
			return Headers{}, errInvalidInput
		}
	} else {
		if len(values.ContentType) != 1 {
			return Headers{}, errInvalidInput
		}
		contentType, err := oneHeader(values.ContentType)
		if err != nil || contentType != "application/json" {
			return Headers{}, errInvalidInput
		}
	}
	contentType := ""
	if len(values.ContentType) == 1 {
		contentType = values.ContentType[0]
	}
	return Headers{
		SignVersion: signVersion,
		AccessKey:   accessKey,
		Timestamp:   timestamp,
		Nonce:       nonce,
		Signature:   signature,
		ContentType: contentType,
	}, nil
}

func oneHeader(values []string) (string, error) {
	if len(values) != 1 {
		return "", errInvalidInput
	}
	value := values[0]
	if value == "" || strings.TrimSpace(value) != value || strings.ContainsRune(value, ',') {
		return "", errInvalidInput
	}
	return value, nil
}

func validateAccessKey(value string) error {
	if len(value) != len("uvp_")+32 || !strings.HasPrefix(value, "uvp_") || !isLowerHex(value[len("uvp_"):], 16) {
		return errInvalidInput
	}
	return nil
}

func validateTimestamp(value string) error {
	if value == "" || (len(value) > 1 && value[0] == '0') {
		return errInvalidInput
	}
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return errInvalidInput
		}
	}
	if _, err := strconv.ParseInt(value, 10, 64); err != nil {
		return errInvalidInput
	}
	return nil
}

func validateNonce(value string) error {
	if !isLowerHex(value, 16) {
		return errInvalidInput
	}
	return nil
}

func validateLineField(value string) error {
	if value == "" || !utf8.ValidString(value) {
		return errInvalidInput
	}
	for i := 0; i < len(value); i++ {
		if value[i] == '\r' || value[i] == '\n' || value[i] == 0 || value[i] == 0x7f {
			return errInvalidInput
		}
	}
	return nil
}

func isLowerHex(value string, byteCount int) bool {
	if len(value) != byteCount*2 {
		return false
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if !(c >= '0' && c <= '9') && !(c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func normalizeMethod(value string) (string, error) {
	if value == "" {
		return "", errInvalidInput
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c > 0x7f || c <= 0x20 || c == 0x7f {
			return "", errInvalidInput
		}
	}
	return strings.ToUpper(value), nil
}

func validatePath(path string) error {
	if path == "" || path[0] != '/' {
		return errInvalidInput
	}
	for i := 0; i < len(path); i++ {
		c := path[i]
		if c > 0x7f || c <= 0x20 || c == 0x7f || c == '%' || c == '\\' {
			return errInvalidInput
		}
		if i > 0 && c == '/' && path[i-1] == '/' {
			return errInvalidInput
		}
	}
	if path != "/" && strings.HasSuffix(path, "/") {
		return errInvalidInput
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == "." || segment == ".." {
			return errInvalidInput
		}
	}
	return nil
}

type queryPair struct {
	key   string
	value string
}

func canonicalQuery(raw string) (string, error) {
	if len([]byte(raw)) > maxQueryBytes {
		return "", errInvalidInput
	}
	if raw == "" {
		return "", nil
	}
	pairs := make([]queryPair, 0, strings.Count(raw, "&")+1)
	seen := make(map[string]struct{})
	for _, part := range strings.Split(raw, "&") {
		if part == "" || strings.ContainsRune(part, ';') {
			return "", errInvalidInput
		}
		separator := strings.IndexByte(part, '=')
		if separator < 0 {
			return "", errInvalidInput
		}
		rawKey, rawValue := part[:separator], part[separator+1:]
		if strings.ContainsRune(rawKey, '+') || strings.ContainsRune(rawValue, '+') {
			return "", errInvalidInput
		}
		key, err := decodeQueryComponent(rawKey)
		if err != nil || key == "" {
			return "", errInvalidInput
		}
		value, err := decodeQueryComponent(rawValue)
		if err != nil {
			return "", errInvalidInput
		}
		encodedKey := encodeRFC3986([]byte(key))
		if _, exists := seen[encodedKey]; exists {
			return "", errInvalidInput
		}
		seen[encodedKey] = struct{}{}
		pairs = append(pairs, queryPair{key: encodedKey, value: encodeRFC3986([]byte(value))})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].key == pairs[j].key {
			return pairs[i].value < pairs[j].value
		}
		return pairs[i].key < pairs[j].key
	})
	parts := make([]string, len(pairs))
	for i, pair := range pairs {
		parts[i] = pair.key + "=" + pair.value
	}
	return strings.Join(parts, "&"), nil
}

func decodeQueryComponent(raw string) (string, error) {
	decoded := make([]byte, 0, len(raw))
	for i := 0; i < len(raw); i++ {
		if raw[i] != '%' {
			decoded = append(decoded, raw[i])
			continue
		}
		if i+2 >= len(raw) {
			return "", errInvalidInput
		}
		hi, okHigh := fromHex(raw[i+1])
		lo, okLow := fromHex(raw[i+2])
		if !okHigh || !okLow {
			return "", errInvalidInput
		}
		decoded = append(decoded, hi<<4|lo)
		i += 2
	}
	if !utf8.Valid(decoded) {
		return "", errInvalidInput
	}
	return string(decoded), nil
}

func fromHex(value byte) (byte, bool) {
	switch {
	case value >= '0' && value <= '9':
		return value - '0', true
	case value >= 'a' && value <= 'f':
		return value - 'a' + 10, true
	case value >= 'A' && value <= 'F':
		return value - 'A' + 10, true
	default:
		return 0, false
	}
}

func encodeRFC3986(value []byte) string {
	const hexDigits = "0123456789ABCDEF"
	var builder strings.Builder
	for _, c := range value {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || strings.ContainsRune("-._~", rune(c)) {
			builder.WriteByte(c)
			continue
		}
		builder.WriteByte('%')
		builder.WriteByte(hexDigits[c>>4])
		builder.WriteByte(hexDigits[c&0x0f])
	}
	return builder.String()
}

func validateJSONBody(body []byte) error {
	if !utf8.Valid(body) {
		return errInvalidInput
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := validateJSONValue(decoder); err != nil {
		return errInvalidInput
	}
	if token, err := decoder.Token(); err != io.EOF || token != nil {
		return errInvalidInput
	}
	return nil
}

func validateJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return errInvalidInput
	}
	delim, isDelim := token.(json.Delim)
	if !isDelim {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			key, ok := keyToken.(string)
			if err != nil || !ok {
				return errInvalidInput
			}
			if _, exists := seen[key]; exists {
				return errInvalidInput
			}
			seen[key] = struct{}{}
			if err := validateJSONValue(decoder); err != nil {
				return errInvalidInput
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim('}') {
			return errInvalidInput
		}
	case '[':
		for decoder.More() {
			if err := validateJSONValue(decoder); err != nil {
				return errInvalidInput
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim(']') {
			return errInvalidInput
		}
	default:
		return errInvalidInput
	}
	return nil
}

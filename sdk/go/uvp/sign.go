// Package uvp 是 UVP OpenAPI v1 的 Go 参考实现：只用标准库，可直接抄进项目。
//
// canonical 是 11 行、LF 分隔、末尾不带换行。第 10 行是 Idempotency-Key，
// 漏掉它签名照样算得出来，但平台一律 401，且现象与"AK 填错"一模一样。
// 改动这里的任何逻辑后，务必跑 cmd/demo 的 --self-test。
package uvp

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
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
	SignVersion   = "1"
	MaxBodyBytes  = 64 * 1024
	MaxQueryBytes = 8 * 1024

	HeaderSignVersion = "X-UVP-Sign-Version"
	HeaderAccessKey   = "X-UVP-Access-Key"
	HeaderTimestamp   = "X-UVP-Timestamp"
	HeaderNonce       = "X-UVP-Nonce"
	HeaderSignature   = "X-UVP-Signature"
	HeaderIdempotency = "Idempotency-Key"
)

// ErrInvalidInput 表示签名输入在本地就不合法，根本没发出去。
var ErrInvalidInput = errors.New("invalid signature input")

// Input 是参与签名的请求字段。Body 必须是原始字节，
// 不要解析 JSON 后重新序列化，否则字节不同、签名不同。
type Input struct {
	Method         string
	Path           string
	RawQuery       string
	ContentType    string
	Body           []byte
	AccessKey      string
	Timestamp      string
	Nonce          string
	IdempotencyKey string
	Audience       string
}

// Canonical 生成 11 行 canonical 字符串。
func Canonical(in Input) (string, error) {
	method, err := normalizeMethod(in.Method)
	if err != nil {
		return "", err
	}
	if err := validateAccessKey(in.AccessKey); err != nil {
		return "", err
	}
	if err := validateTimestamp(in.Timestamp); err != nil {
		return "", err
	}
	if err := validateNonce(in.Nonce); err != nil {
		return "", err
	}
	if err := validateLineField(in.Audience); err != nil {
		return "", err
	}
	if err := validateIdempotencyKey(in.IdempotencyKey); err != nil {
		return "", err
	}
	if err := validatePath(in.Path); err != nil {
		return "", err
	}
	if len(in.Body) > MaxBodyBytes {
		return "", ErrInvalidInput
	}
	query, err := CanonicalQuery(in.RawQuery)
	if err != nil {
		return "", err
	}

	contentType := in.ContentType
	switch method {
	case "GET":
		if contentType != "" || len(in.Body) != 0 {
			return "", ErrInvalidInput
		}
	case "POST":
		if len(in.Body) == 0 && contentType != "" && contentType != "application/json" {
			return "", ErrInvalidInput
		}
		if len(in.Body) > 0 && contentType != "application/json" {
			return "", ErrInvalidInput
		}
	default:
		if contentType != "" && contentType != "application/json" {
			return "", ErrInvalidInput
		}
	}
	if contentType == "application/json" && len(in.Body) > 0 {
		if err := validateJSONBody(in.Body); err != nil {
			return "", err
		}
	}

	sum := sha256.Sum256(in.Body)
	return strings.Join([]string{
		"UVP-HMAC-SHA256/1",
		in.AccessKey,
		in.Timestamp,
		in.Nonce,
		method,
		in.Path,
		query,
		contentType,
		hex.EncodeToString(sum[:]),
		in.IdempotencyKey,
		in.Audience,
	}, "\n"), nil
}

// Sign 返回小写十六进制 HMAC-SHA256。SK 是 base64url 无填充的 32 字节，
// HMAC 用的是解码后的字节，不是 SK 这段文本。
func Sign(secretKey, canonical string) (string, error) {
	key, err := DecodeSecretKey(secretKey)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, key)
	if _, err := mac.Write([]byte(canonical)); err != nil {
		return "", err
	}
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// DecodeSecretKey 把 SK 解成 32 字节。
func DecodeSecretKey(secretKey string) ([]byte, error) {
	if secretKey == "" {
		return nil, ErrInvalidInput
	}
	for i := 0; i < len(secretKey); i++ {
		c := secretKey[i]
		isLower := c >= 'a' && c <= 'z'
		isUpper := c >= 'A' && c <= 'Z'
		isDigit := c >= '0' && c <= '9'
		if !isLower && !isUpper && !isDigit && c != '-' && c != '_' {
			return nil, ErrInvalidInput
		}
	}
	key, err := base64.RawURLEncoding.DecodeString(secretKey)
	if err != nil || len(key) != 32 {
		return nil, ErrInvalidInput
	}
	return key, nil
}

// SignRequest 返回可直接使用的请求头。
func SignRequest(accessKey, secretKey, audience, method, path, rawQuery string,
	body []byte, idempotencyKey, timestamp, nonce string) (map[string]string, error) {
	contentType := ""
	if len(body) > 0 {
		contentType = "application/json"
	}
	canonical, err := Canonical(Input{
		Method: method, Path: path, RawQuery: rawQuery, ContentType: contentType,
		Body: body, AccessKey: accessKey, Timestamp: timestamp, Nonce: nonce,
		IdempotencyKey: idempotencyKey, Audience: audience,
	})
	if err != nil {
		return nil, err
	}
	signature, err := Sign(secretKey, canonical)
	if err != nil {
		return nil, err
	}
	headers := map[string]string{
		HeaderSignVersion: SignVersion,
		HeaderAccessKey:   accessKey,
		HeaderTimestamp:   timestamp,
		HeaderNonce:       nonce,
		HeaderSignature:   signature,
	}
	if contentType != "" {
		headers["Content-Type"] = contentType
	}
	if idempotencyKey != "" {
		headers[HeaderIdempotency] = idempotencyKey
	}
	return headers, nil
}

// CanonicalQuery 解码一次、按 RFC3986 重编码、再排序。
func CanonicalQuery(raw string) (string, error) {
	if len(raw) > MaxQueryBytes {
		return "", ErrInvalidInput
	}
	if raw == "" {
		return "", nil
	}
	type pair struct{ key, value string }
	pairs := make([]pair, 0, strings.Count(raw, "&")+1)
	seen := make(map[string]struct{})
	for _, part := range strings.Split(raw, "&") {
		if part == "" || strings.ContainsRune(part, ';') {
			return "", ErrInvalidInput
		}
		index := strings.IndexByte(part, '=')
		if index < 0 {
			return "", ErrInvalidInput
		}
		rawKey, rawValue := part[:index], part[index+1:]
		if strings.ContainsRune(rawKey, '+') || strings.ContainsRune(rawValue, '+') {
			return "", ErrInvalidInput
		}
		key, err := decodeComponent(rawKey)
		if err != nil || key == "" {
			return "", ErrInvalidInput
		}
		value, err := decodeComponent(rawValue)
		if err != nil {
			return "", ErrInvalidInput
		}
		encodedKey := encodeRFC3986([]byte(key))
		if _, exists := seen[encodedKey]; exists {
			return "", ErrInvalidInput
		}
		seen[encodedKey] = struct{}{}
		pairs = append(pairs, pair{key: encodedKey, value: encodeRFC3986([]byte(value))})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].key == pairs[j].key {
			return pairs[i].value < pairs[j].value
		}
		return pairs[i].key < pairs[j].key
	})
	parts := make([]string, len(pairs))
	for i, item := range pairs {
		parts[i] = item.key + "=" + item.value
	}
	return strings.Join(parts, "&"), nil
}

// EncodeValue 把单个值按 RFC3986 编码，用于拼 rawQuery。
func EncodeValue(value string) string {
	return encodeRFC3986([]byte(value))
}

func decodeComponent(raw string) (string, error) {
	decoded := make([]byte, 0, len(raw))
	for i := 0; i < len(raw); i++ {
		if raw[i] != '%' {
			decoded = append(decoded, raw[i])
			continue
		}
		if i+2 >= len(raw) {
			return "", ErrInvalidInput
		}
		high, okHigh := fromHex(raw[i+1])
		low, okLow := fromHex(raw[i+2])
		if !okHigh || !okLow {
			return "", ErrInvalidInput
		}
		decoded = append(decoded, high<<4|low)
		i += 2
	}
	if !utf8.Valid(decoded) {
		return "", ErrInvalidInput
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
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '-' || c == '.' || c == '_' || c == '~' {
			builder.WriteByte(c)
			continue
		}
		builder.WriteByte('%')
		builder.WriteByte(hexDigits[c>>4])
		builder.WriteByte(hexDigits[c&0x0f])
	}
	return builder.String()
}

func normalizeMethod(value string) (string, error) {
	if value == "" {
		return "", ErrInvalidInput
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c > 0x7f || c <= 0x20 || c == 0x7f {
			return "", ErrInvalidInput
		}
	}
	return strings.ToUpper(value), nil
}

func validateAccessKey(value string) error {
	if len(value) != 36 || !strings.HasPrefix(value, "uvp_") || !isLowerHex(value[4:], 16) {
		return ErrInvalidInput
	}
	return nil
}

func validateTimestamp(value string) error {
	if value == "" || (len(value) > 1 && value[0] == '0') {
		return ErrInvalidInput
	}
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return ErrInvalidInput
		}
	}
	if _, err := strconv.ParseInt(value, 10, 64); err != nil {
		return ErrInvalidInput
	}
	return nil
}

func validateNonce(value string) error {
	if !isLowerHex(value, 16) {
		return ErrInvalidInput
	}
	return nil
}

func validateLineField(value string) error {
	if value == "" || !utf8.ValidString(value) {
		return ErrInvalidInput
	}
	for i := 0; i < len(value); i++ {
		if value[i] == '\r' || value[i] == '\n' || value[i] == 0 || value[i] == 0x7f {
			return ErrInvalidInput
		}
	}
	return nil
}

func validateIdempotencyKey(value string) error {
	if value == "" {
		return nil
	}
	if len(value) > 128 || validateLineField(value) != nil {
		return ErrInvalidInput
	}
	for _, r := range value {
		if r < 0x21 || r > 0x7e {
			return ErrInvalidInput
		}
	}
	return nil
}

func validatePath(path string) error {
	if path == "" || path[0] != '/' {
		return ErrInvalidInput
	}
	for i := 0; i < len(path); i++ {
		c := path[i]
		if c > 0x7f || c <= 0x20 || c == 0x7f || c == '%' || c == '\\' {
			return ErrInvalidInput
		}
		if i > 0 && c == '/' && path[i-1] == '/' {
			return ErrInvalidInput
		}
	}
	if path != "/" && strings.HasSuffix(path, "/") {
		return ErrInvalidInput
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == "." || segment == ".." {
			return ErrInvalidInput
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
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func validateJSONBody(body []byte) error {
	if !utf8.Valid(body) {
		return ErrInvalidInput
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := validateJSONValue(decoder); err != nil {
		return err
	}
	if token, err := decoder.Token(); err != io.EOF || token != nil {
		return ErrInvalidInput
	}
	return nil
}

func validateJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return ErrInvalidInput
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
				return ErrInvalidInput
			}
			if _, exists := seen[key]; exists {
				return ErrInvalidInput
			}
			seen[key] = struct{}{}
			if err := validateJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim('}') {
			return ErrInvalidInput
		}
	case '[':
		for decoder.More() {
			if err := validateJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim(']') {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return nil
}

package openapisign

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type fixture struct {
	AccessKey  string   `json:"accessKey"`
	SecretKey  string   `json:"secretKey"`
	DecodedHex string   `json:"decodedKeyHex"`
	Timestamp  string   `json:"timestamp"`
	Nonce      string   `json:"nonce"`
	Audience   string   `json:"audience"`
	Vectors    []vector `json:"vectors"`
}

type vector struct {
	Name        string `json:"name"`
	Method      string `json:"method"`
	Path        string `json:"path"`
	RawQuery    string `json:"rawQuery"`
	ContentType string `json:"contentType"`
	Body        string `json:"body"`
	BodySHA256  string `json:"bodySHA256"`
	Bytes       int    `json:"canonicalBytes"`
	Canonical   string `json:"canonical"`
	Signature   string `json:"signature"`
}

func loadFixture(t *testing.T) fixture {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve fixture test path")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "app", "openapi", "auth", "testdata", "signature-v1.json"))
	if err != nil {
		t.Fatalf("read public signature fixture: %v", err)
	}
	var got fixture
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode public signature fixture: %v", err)
	}
	return got
}

func requestFor(f fixture, v vector) Request {
	return Request{
		Method: v.Method, Path: v.Path, RawQuery: v.RawQuery, ContentType: v.ContentType,
		Body: []byte(v.Body), AccessKey: f.AccessKey, Timestamp: f.Timestamp,
		Nonce: f.Nonce, Audience: f.Audience,
	}
}

func TestOpenAPISignExampleGoldenVectors(t *testing.T) {
	f := loadFixture(t)
	key, err := base64.RawURLEncoding.DecodeString(f.SecretKey)
	if err != nil || len(key) != 32 {
		t.Fatalf("fixture secret key must decode to 32 bytes: %v", err)
	}
	if got := hex.EncodeToString(key); got != f.DecodedHex {
		t.Fatalf("fixture key hex = %s, want %s", got, f.DecodedHex)
	}
	for _, v := range f.Vectors {
		t.Run(v.Name, func(t *testing.T) {
			input := requestFor(f, v)
			canonical, err := CanonicalString(input)
			if err != nil {
				t.Fatalf("canonicalize: %v", err)
			}
			if canonical != v.Canonical {
				t.Fatalf("canonical mismatch\ngot: %q\nwant: %q", canonical, v.Canonical)
			}
			if len([]byte(canonical)) != v.Bytes {
				t.Fatalf("canonical byte count = %d, want %d", len([]byte(canonical)), v.Bytes)
			}
			hash := sha256.Sum256(input.Body)
			if got := hex.EncodeToString(hash[:]); got != v.BodySHA256 {
				t.Fatalf("body sha256 = %s, want %s", got, v.BodySHA256)
			}
			mac := hmac.New(sha256.New, key)
			_, _ = mac.Write([]byte(canonical))
			if got := hex.EncodeToString(mac.Sum(nil)); got != v.Signature {
				t.Fatalf("independent HMAC = %s, want %s", got, v.Signature)
			}
			got, err := Sign(input, f.SecretKey)
			if err != nil || got != v.Signature {
				t.Fatalf("sign = %s, err=%v, want %s", got, err, v.Signature)
			}
		})
	}
}

func TestOpenAPISignExampleInputRejection(t *testing.T) {
	f := loadFixture(t)
	base := requestFor(f, f.Vectors[0])
	for _, tc := range []struct {
		name   string
		mutate func(*Request)
	}{
		{"raw-plus", func(r *Request) { r.RawQuery = "keyword=a+b" }},
		{"empty-key", func(r *Request) { r.RawQuery = "=value" }},
		{"missing-equals", func(r *Request) { r.RawQuery = "keyword" }},
		{"duplicate-query", func(r *Request) { r.RawQuery = "a=1&a=2" }},
		{"invalid-percent", func(r *Request) { r.RawQuery = "a=%ZZ" }},
		{"invalid-utf8", func(r *Request) { r.RawQuery = "a=%FF" }},
		{"encoded-path", func(r *Request) { r.Path = "/openapi/%76%31/devices" }},
		{"double-slash", func(r *Request) { r.Path = "/openapi/v1//devices" }},
		{"trailing-slash", func(r *Request) { r.Path = "/openapi/v1/devices/" }},
		{"dot-segment", func(r *Request) { r.Path = "/openapi/v1/../devices" }},
		{"backslash", func(r *Request) { r.Path = "/openapi/v1\\devices" }},
		{"empty-audience", func(r *Request) { r.Audience = "" }},
		{"leading-zero-timestamp", func(r *Request) { r.Timestamp = "01790000000" }},
		{"uppercase-nonce", func(r *Request) { r.Nonce = strings.ToUpper(r.Nonce) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := base
			tc.mutate(&input)
			if _, err := Sign(input, f.SecretKey); err == nil {
				t.Fatal("invalid input was accepted")
			}
		})
	}

	for _, secret := range []string{"", f.SecretKey + "=", strings.Replace(f.SecretKey, "A", "+", 1), "not-base64url", "AA"} {
		if _, err := Sign(base, secret); err == nil {
			t.Fatalf("invalid secret %q was accepted", secret)
		}
	}

	post := requestFor(f, f.Vectors[1])
	post.Body = []byte(`{"protocol":"https-flv","protocol":"https-flv"}`)
	if _, err := Sign(post, f.SecretKey); err == nil {
		t.Fatal("duplicate JSON member was accepted")
	}
}

func TestOpenAPISignExampleHeaderRulesAndCase(t *testing.T) {
	f := loadFixture(t)
	get := requestFor(f, f.Vectors[0])
	upper := get
	upper.Method = "get"
	canonical, err := CanonicalString(upper)
	if err != nil {
		t.Fatalf("lowercase method: %v", err)
	}
	if canonical != f.Vectors[0].Canonical {
		t.Fatalf("method normalization changed canonical bytes")
	}
	reordered := get
	reordered.RawQuery = "pageSize=20&page=1&keyword=%E6%91%84%E5%83%8F%E6%9C%BA%20A"
	reorderedCanonical, err := CanonicalString(reordered)
	if err != nil || reorderedCanonical != f.Vectors[0].Canonical {
		t.Fatalf("query order changed canonical bytes: %v", err)
	}
	emptyValue := get
	emptyValue.RawQuery = "empty=&a=1"
	emptyCanonical, err := CanonicalString(emptyValue)
	if err != nil || !strings.HasSuffix(emptyCanonical, "\na=1&empty=\n\n"+f.Vectors[0].BodySHA256+"\n"+f.Audience) {
		t.Fatalf("empty query value was not preserved: %v", err)
	}
	if _, err := ParseHeaders("GET", HeaderValues{
		SignVersion: []string{"1"}, AccessKey: []string{f.AccessKey},
		Timestamp: []string{f.Timestamp}, Nonce: []string{f.Nonce},
		Signature: []string{f.Vectors[0].Signature},
	}); err != nil {
		t.Fatalf("valid headers rejected: %v", err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*HeaderValues)
	}{
		{"duplicate-access-key", func(h *HeaderValues) { h.AccessKey = []string{f.AccessKey, f.AccessKey} }},
		{"combined-signature", func(h *HeaderValues) { h.Signature = []string{f.Vectors[0].Signature + ",other"} }},
		{"uppercase-signature", func(h *HeaderValues) { h.Signature = []string{strings.ToUpper(f.Vectors[0].Signature)} }},
		{"content-encoding", func(h *HeaderValues) { h.ContentEncoding = []string{"gzip"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := HeaderValues{
				SignVersion: []string{"1"}, AccessKey: []string{f.AccessKey},
				Timestamp: []string{f.Timestamp}, Nonce: []string{f.Nonce},
				Signature: []string{f.Vectors[0].Signature},
			}
			tc.mutate(&h)
			if _, err := ParseHeaders("GET", h); err == nil {
				t.Fatal("invalid header set was accepted")
			}
		})
	}

	if _, err := ParseHeaders("POST", HeaderValues{
		SignVersion: []string{"1"}, AccessKey: []string{f.AccessKey},
		Timestamp: []string{f.Timestamp}, Nonce: []string{f.Nonce},
		Signature: []string{f.Vectors[1].Signature}, ContentType: []string{"application/json"},
	}); err != nil {
		t.Fatalf("valid POST headers rejected: %v", err)
	}
	if _, err := ParseHeaders("POST", HeaderValues{
		SignVersion: []string{"1"}, AccessKey: []string{f.AccessKey},
		Timestamp: []string{f.Timestamp}, Nonce: []string{f.Nonce},
		Signature: []string{f.Vectors[1].Signature}, ContentType: []string{"application/json", "application/json"},
	}); err == nil {
		t.Fatal("duplicate content type was accepted")
	}
}

func TestOpenAPISignExampleRawBodyDifference(t *testing.T) {
	f := loadFixture(t)
	base := requestFor(f, f.Vectors[1])
	variants := []string{`{"protocol":"https-flv"}`, `{ "protocol": "https-flv" }`, `{"protocol":"https-flv","scope":"live"}`}
	signatures := make([]string, 0, len(variants))
	for _, body := range variants {
		input := base
		input.Body = []byte(body)
		signature, err := Sign(input, f.SecretKey)
		if err != nil {
			t.Fatalf("sign body %q: %v", body, err)
		}
		signatures = append(signatures, signature)
	}
	if signatures[0] == signatures[1] || signatures[1] == signatures[2] {
		t.Fatal("raw body changes must change signature")
	}
	if bytes.Equal([]byte(variants[0]), []byte(variants[1])) {
		t.Fatal("test body variants unexpectedly equal")
	}
}

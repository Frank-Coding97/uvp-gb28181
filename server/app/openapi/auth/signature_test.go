package auth

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type signatureFixture struct {
	AccessKey  string            `json:"accessKey"`
	SecretKey  string            `json:"secretKey"`
	DecodedKey string            `json:"decodedKeyHex"`
	Timestamp  string            `json:"timestamp"`
	Nonce      string            `json:"nonce"`
	Audience   string            `json:"audience"`
	Vectors    []signatureVector `json:"vectors"`
}

type signatureVector struct {
	Name           string `json:"name"`
	Method         string `json:"method"`
	Path           string `json:"path"`
	RawQuery       string `json:"rawQuery"`
	ContentType    string `json:"contentType"`
	Body           string `json:"body"`
	BodySHA256     string `json:"bodySHA256"`
	CanonicalBytes int    `json:"canonicalBytes"`
	Canonical      string `json:"canonical"`
	Signature      string `json:"signature"`
}

func loadSignatureFixture(t *testing.T) signatureFixture {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "signature-v1.json"))
	if err != nil {
		t.Fatalf("read signature fixture: %v", err)
	}
	var fixture signatureFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("decode signature fixture: %v", err)
	}
	return fixture
}

func fixtureInput(fixture signatureFixture, vector signatureVector) SignatureInput {
	return SignatureInput{
		Method:      vector.Method,
		Path:        vector.Path,
		RawQuery:    vector.RawQuery,
		ContentType: vector.ContentType,
		Body:        []byte(vector.Body),
		AccessKey:   fixture.AccessKey,
		Timestamp:   fixture.Timestamp,
		Nonce:       fixture.Nonce,
		Audience:    fixture.Audience,
	}
}

func TestOpenAPISignatureFixedVectors(t *testing.T) {
	fixture := loadSignatureFixture(t)
	if decoded, err := hex.DecodeString(fixture.DecodedKey); err != nil || len(decoded) != 32 {
		t.Fatalf("fixture decoded key must be 32 bytes: %v", err)
	}
	if len(fixture.Vectors) != 2 {
		t.Fatalf("fixture vectors = %d, want 2", len(fixture.Vectors))
	}

	for _, vector := range fixture.Vectors {
		t.Run(vector.Name, func(t *testing.T) {
			input := fixtureInput(fixture, vector)
			canonical, err := CanonicalString(input)
			if err != nil {
				t.Fatalf("canonicalize: %v", err)
			}
			if canonical != vector.Canonical {
				t.Fatalf("canonical mismatch\n got: %q\nwant: %q", canonical, vector.Canonical)
			}
			if !bytes.Equal([]byte(canonical), []byte(vector.Canonical)) {
				t.Fatal("canonical bytes changed")
			}
			if len([]byte(canonical)) != vector.CanonicalBytes {
				t.Fatalf("canonical byte length = %d, want %d", len([]byte(canonical)), vector.CanonicalBytes)
			}
			hash := sha256.Sum256(input.Body)
			if got := hex.EncodeToString(hash[:]); got != vector.BodySHA256 {
				t.Fatalf("body sha256 = %s, want %s", got, vector.BodySHA256)
			}
			key, err := base64.RawURLEncoding.DecodeString(fixture.SecretKey)
			if err != nil {
				t.Fatalf("decode fixture secret: %v", err)
			}
			mac := hmac.New(sha256.New, key)
			_, _ = mac.Write([]byte(vector.Canonical))
			if got := hex.EncodeToString(mac.Sum(nil)); got != vector.Signature {
				t.Fatalf("independent HMAC = %s, want %s", got, vector.Signature)
			}
			signature, err := Sign(input, fixture.SecretKey)
			if err != nil {
				t.Fatalf("sign: %v", err)
			}
			if signature != vector.Signature {
				t.Fatalf("signature = %s, want %s", signature, vector.Signature)
			}
			if err := Verify(input, fixture.SecretKey, vector.Signature); err != nil {
				t.Fatalf("verify fixed signature: %v", err)
			}
		})
	}
}

func TestOpenAPISignatureMutationsFail(t *testing.T) {
	fixture := loadSignatureFixture(t)
	base := fixtureInput(fixture, fixture.Vectors[0])
	signature, err := Sign(base, fixture.SecretKey)
	if err != nil {
		t.Fatalf("sign base input: %v", err)
	}

	mutations := []struct {
		name   string
		mutate func(*SignatureInput)
	}{
		{name: "method", mutate: func(input *SignatureInput) { input.Method = "HEAD" }},
		{name: "path", mutate: func(input *SignatureInput) { input.Path += "/extra" }},
		{name: "query", mutate: func(input *SignatureInput) { input.RawQuery = "page=2" }},
		{name: "access-key", mutate: func(input *SignatureInput) { input.AccessKey = "uvp_0102030405060708090a0b0c0d0e0f10" }},
		{name: "timestamp", mutate: func(input *SignatureInput) { input.Timestamp = "1790000001" }},
		{name: "nonce", mutate: func(input *SignatureInput) { input.Nonce = "100102030405060708090a0b0c0d0e0f" }},
		{name: "audience", mutate: func(input *SignatureInput) { input.Audience = "another-audience" }},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			input := base
			mutation.mutate(&input)
			if err := Verify(input, fixture.SecretKey, signature); err == nil {
				t.Fatal("verification unexpectedly succeeded after signed input mutation")
			}
		})
	}
	if err := Verify(base, fixture.SecretKey, strings.Repeat("0", 64)); err == nil {
		t.Fatal("verification unexpectedly succeeded for a different signature")
	}
}

func TestOpenAPISignatureRawBodyAndJSONDuplicates(t *testing.T) {
	fixture := loadSignatureFixture(t)
	base := fixtureInput(fixture, fixture.Vectors[1])
	variants := []string{
		`{"protocol":"https-flv","scope":"live"}`,
		`{ "protocol": "https-flv" }`,
		`{"scope":"live","protocol":"https-flv"}`,
	}
	signatures := make(map[string]string)
	for _, body := range variants {
		input := base
		input.Body = []byte(body)
		signature, err := Sign(input, fixture.SecretKey)
		if err != nil {
			t.Fatalf("sign body %q: %v", body, err)
		}
		signatures[body] = signature
	}
	if signatures[variants[0]] == signatures[variants[1]] || signatures[variants[1]] == signatures[variants[2]] {
		t.Fatal("raw body changes must change the signature")
	}
	duplicate := base
	duplicate.Body = []byte(`{"protocol":"https-flv","protocol":"https-flv"}`)
	if _, err := CanonicalString(duplicate); err == nil {
		t.Fatal("duplicate JSON member name was accepted")
	}
}

func validHeaderValues(fixture signatureFixture, vector signatureVector) HeaderValues {
	return HeaderValues{
		SignVersion: []string{"1"},
		AccessKey:   []string{fixture.AccessKey},
		Timestamp:   []string{fixture.Timestamp},
		Nonce:       []string{fixture.Nonce},
		Signature:   []string{vector.Signature},
	}
}

func TestOpenAPISignatureHeaderFormat(t *testing.T) {
	fixture := loadSignatureFixture(t)
	valid := validHeaderValues(fixture, fixture.Vectors[0])
	parsed, err := ParseHeaders("GET", valid)
	if err != nil {
		t.Fatalf("parse valid headers: %v", err)
	}
	if parsed.AccessKey != fixture.AccessKey || parsed.Timestamp != fixture.Timestamp || parsed.Nonce != fixture.Nonce || parsed.Signature != fixture.Vectors[0].Signature {
		t.Fatalf("parsed valid headers lost values: %+v", parsed)
	}

	cases := []struct {
		name   string
		modify func(*HeaderValues)
	}{
		{name: "missing-required", modify: func(headers *HeaderValues) { headers.Nonce = nil }},
		{name: "duplicate-header", modify: func(headers *HeaderValues) { headers.AccessKey = []string{fixture.AccessKey, fixture.AccessKey} }},
		{name: "combined-header", modify: func(headers *HeaderValues) { headers.Signature = []string{fixture.Vectors[0].Signature + ",other"} }},
		{name: "outer-whitespace", modify: func(headers *HeaderValues) { headers.Timestamp = []string{" " + fixture.Timestamp} }},
		{name: "wrong-version", modify: func(headers *HeaderValues) { headers.SignVersion = []string{"2"} }},
		{name: "invalid-access-key", modify: func(headers *HeaderValues) { headers.AccessKey = []string{"uvp_not-hex"} }},
		{name: "leading-zero-timestamp", modify: func(headers *HeaderValues) { headers.Timestamp = []string{"01790000000"} }},
		{name: "invalid-nonce", modify: func(headers *HeaderValues) { headers.Nonce = []string{"00010203"} }},
		{name: "uppercase-signature", modify: func(headers *HeaderValues) {
			headers.Signature = []string{strings.ToUpper(fixture.Vectors[0].Signature)}
		}},
		{name: "short-signature", modify: func(headers *HeaderValues) { headers.Signature = []string{strings.Repeat("a", 63)} }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			headers := valid
			testCase.modify(&headers)
			if _, err := ParseHeaders("GET", headers); err == nil {
				t.Fatal("invalid headers were accepted")
			}
		})
	}
	if _, err := Sign(fixtureInput(fixture, fixture.Vectors[0]), "not-a-base64url-secret"); err == nil {
		t.Fatal("invalid secret key was accepted")
	}
}

func TestOpenAPISignaturePathCanonicalization(t *testing.T) {
	fixture := loadSignatureFixture(t)
	base := fixtureInput(fixture, fixture.Vectors[0])
	for _, path := range []string{
		"/openapi/%76%31/devices",
		"/openapi/v1//devices",
		"/openapi/v1/devices/",
		"/openapi/v1\\devices",
		"/openapi/v1/./devices",
		"/openapi/v1/../devices",
	} {
		t.Run(path, func(t *testing.T) {
			input := base
			input.Path = path
			if _, err := CanonicalString(input); err == nil {
				t.Fatal("invalid path was accepted")
			}
		})
	}
}

func TestOpenAPISignatureQueryCanonicalization(t *testing.T) {
	fixture := loadSignatureFixture(t)
	base := fixtureInput(fixture, fixture.Vectors[0])
	base.RawQuery = "b=2&a=%E6%91%84%E5%83%8F%E6%9C%BA%20A"
	equivalent := base
	equivalent.RawQuery = "a=%e6%91%84%e5%83%8f%e6%9c%ba%20A&b=2"
	canonical, err := CanonicalString(base)
	if err != nil {
		t.Fatalf("canonicalize base query: %v", err)
	}
	equivalentCanonical, err := CanonicalString(equivalent)
	if err != nil {
		t.Fatalf("canonicalize equivalent query: %v", err)
	}
	if canonical != equivalentCanonical {
		t.Fatalf("equivalent parsed queries differ\nbase: %q\nequivalent: %q", canonical, equivalentCanonical)
	}

	for _, query := range []string{
		"a+b=1",
		"a=1&a=2",
		"=value",
		"missing-equals",
		"a=1;b=2",
		"a=%ZZ",
		"a=%FF",
	} {
		t.Run(query, func(t *testing.T) {
			input := base
			input.RawQuery = query
			if _, err := CanonicalString(input); err == nil {
				t.Fatal("invalid query was accepted")
			}
		})
	}

	input := base
	input.RawQuery = "empty=&a=1"
	if _, err := CanonicalString(input); err != nil {
		t.Fatalf("empty query value rejected: %v", err)
	}
}

func TestOpenAPISignatureTransportRules(t *testing.T) {
	fixture := loadSignatureFixture(t)
	get := fixtureInput(fixture, fixture.Vectors[0])
	get.ContentType = "application/json"
	if _, err := CanonicalString(get); err == nil {
		t.Fatal("GET content type was accepted")
	}
	get = fixtureInput(fixture, fixture.Vectors[0])
	get.Body = []byte("{}")
	if _, err := CanonicalString(get); err == nil {
		t.Fatal("GET body was accepted")
	}
	post := fixtureInput(fixture, fixture.Vectors[1])
	post.ContentType = "text/plain"
	if _, err := CanonicalString(post); err == nil {
		t.Fatal("unsupported POST content type was accepted")
	}

	validPostHeaders := validHeaderValues(fixture, fixture.Vectors[1])
	validPostHeaders.ContentType = []string{"application/json"}
	if _, err := ParseHeaders("POST", validPostHeaders); err != nil {
		t.Fatalf("valid POST headers rejected: %v", err)
	}
	for _, testCase := range []struct {
		name   string
		modify func(*HeaderValues)
	}{
		{name: "GET-content-type", modify: func(headers *HeaderValues) { headers.ContentType = []string{"application/json"} }},
		{name: "content-encoding", modify: func(headers *HeaderValues) { headers.ContentEncoding = []string{"gzip"} }},
		{name: "method-override", modify: func(headers *HeaderValues) { headers.MethodOverride = []string{"DELETE"} }},
		{name: "duplicate-content-type", modify: func(headers *HeaderValues) { headers.ContentType = []string{"application/json", "application/json"} }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			headers := validPostHeaders
			testCase.modify(&headers)
			method := "POST"
			if testCase.name == "GET-content-type" {
				method = "GET"
			}
			if _, err := ParseHeaders(method, headers); err == nil {
				t.Fatal("transport-invalid headers were accepted")
			}
		})
	}
}

func TestOpenAPISignatureLimits(t *testing.T) {
	fixture := loadSignatureFixture(t)
	post := fixtureInput(fixture, fixture.Vectors[1])
	post.Body = append([]byte{'"'}, bytes.Repeat([]byte{'a'}, 64*1024-2)...)
	post.Body = append(post.Body, '"')
	if len(post.Body) != 64*1024 {
		t.Fatalf("test body length = %d, want %d", len(post.Body), 64*1024)
	}
	if _, err := CanonicalString(post); err != nil {
		t.Fatalf("64 KiB body rejected: %v", err)
	}
	post.Body = append(post.Body, ' ')
	if _, err := CanonicalString(post); err == nil {
		t.Fatal("body over 64 KiB accepted")
	}

	get := fixtureInput(fixture, fixture.Vectors[0])
	get.RawQuery = "k=" + strings.Repeat("a", 8190)
	if len(get.RawQuery) != 8192 {
		t.Fatalf("test query length = %d, want 8192", len(get.RawQuery))
	}
	if _, err := CanonicalString(get); err != nil {
		t.Fatalf("8 KiB query rejected: %v", err)
	}
	get.RawQuery += "a"
	if _, err := CanonicalString(get); err == nil {
		t.Fatal("query over 8 KiB accepted")
	}
}

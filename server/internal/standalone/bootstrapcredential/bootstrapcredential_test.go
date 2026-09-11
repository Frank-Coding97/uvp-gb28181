package bootstrapcredential

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewTokenIsCanonical32ByteRawURL(t *testing.T) {
	seen := make(map[string]struct{}, 64)
	for i := 0; i < 64; i++ {
		token, err := NewToken()
		if err != nil {
			t.Fatal(err)
		}
		if len(token) != tokenTextSize {
			t.Fatalf("token length = %d, want %d", len(token), tokenTextSize)
		}
		raw, err := decodeCanonicalToken(token)
		if err != nil {
			t.Fatal(err)
		}
		if len(raw) != tokenByteSize {
			t.Fatalf("decoded token length = %d, want %d", len(raw), tokenByteSize)
		}
		if _, duplicate := seen[token]; duplicate {
			t.Fatal("NewToken returned a duplicate credential")
		}
		seen[token] = struct{}{}
	}
}

func TestWriteReadRoundTripUsesFixedFrame(t *testing.T) {
	token, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := Write(&output, token); err != nil {
		t.Fatal(err)
	}
	want := protocolPrefix + token + "\n"
	if output.String() != want {
		t.Fatalf("frame = %q, want %q", output.String(), want)
	}
	got, err := Read(&output)
	if err != nil {
		t.Fatal(err)
	}
	if got != token {
		t.Fatalf("read token = %q, want generated token", got)
	}
}

func TestReadRejectsMalformedStrictFrames(t *testing.T) {
	token, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	valid := []byte(protocolPrefix + token + "\n")
	cases := []struct {
		name string
		data []byte
	}{
		{name: "empty", data: nil},
		{name: "short", data: valid[:len(valid)-1]},
		{name: "wrong prefix", data: append([]byte("UVP-BOOTSTRAP-0 "), valid[len(protocolPrefix):]...)},
		{name: "wrong token length short", data: []byte(protocolPrefix + strings.Repeat("A", tokenTextSize-1) + "\n")},
		{name: "wrong token length long", data: []byte(protocolPrefix + strings.Repeat("A", tokenTextSize+1) + "\n")},
		{name: "invalid alphabet", data: []byte(protocolPrefix + strings.Repeat("!", tokenTextSize) + "\n")},
		{name: "padded base64", data: []byte(protocolPrefix + token[:tokenTextSize-1] + "=" + "\n")},
		{name: "carriage return", data: append(append([]byte(nil), valid[:len(valid)-1]...), '\r', '\n')},
		{name: "trailing byte", data: append(append([]byte(nil), valid...), 'x')},
		{name: "oversize", data: append(append([]byte(nil), valid...), bytes.Repeat([]byte{'x'}, 32)...)},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := Read(bytes.NewReader(testCase.data))
			if !errors.Is(err, ErrInvalidCredential) {
				t.Fatalf("Read(%q) error = %v, want ErrInvalidCredential", testCase.name, err)
			}
			if got != "" {
				t.Fatalf("Read(%q) returned a credential on error", testCase.name)
			}
		})
	}
}

func TestReadIsBoundedToOneFramePlusOneByte(t *testing.T) {
	token, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	reader := &infiniteReader{data: []byte(protocolPrefix + token + "x")}
	if _, err := Read(reader); !errors.Is(err, ErrInvalidCredential) {
		t.Fatalf("Read(infinite reader) error = %v, want ErrInvalidCredential", err)
	}
	if reader.read > frameSize+1 {
		t.Fatalf("Read consumed %d bytes, want at most %d", reader.read, frameSize+1)
	}
}

func TestReadDoesNotLeakInputOrReaderError(t *testing.T) {
	secret, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	malformed := protocolPrefix + secret + "!"
	if _, err := Read(strings.NewReader(malformed)); !errors.Is(err, ErrInvalidCredential) || strings.Contains(err.Error(), secret) {
		t.Fatalf("malformed input error = %v, contains credential or has wrong sentinel", err)
	}
	readerErr := errors.New("reader failed while carrying " + secret)
	if _, err := Read(&errorReader{err: readerErr}); !errors.Is(err, ErrInvalidCredential) || strings.Contains(err.Error(), secret) {
		t.Fatalf("reader error = %v, contains credential or has wrong sentinel", err)
	}
}

func TestWriteHandlesShortWritesAndDoesNotLeakWriterError(t *testing.T) {
	token, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	writer := &shortWriter{max: 2}
	if err := Write(writer, token); err != nil {
		t.Fatal(err)
	}
	if got, want := writer.data.String(), protocolPrefix+token+"\n"; got != want {
		t.Fatalf("short-writer frame = %q, want %q", got, want)
	}

	writerErr := errors.New("pipe failed for " + token)
	if err := Write(&errorWriter{err: writerErr}, token); err == nil || strings.Contains(err.Error(), token) {
		t.Fatalf("writer error = %v, want non-secret error", err)
	}
	if err := Write(&zeroWriter{}, token); err == nil || strings.Contains(err.Error(), token) {
		t.Fatalf("zero-writer error = %v, want non-secret error", err)
	}
	if err := Write(io.Discard, strings.Repeat("A", tokenTextSize-1)); !errors.Is(err, ErrInvalidCredential) {
		t.Fatalf("invalid token write error = %v, want ErrInvalidCredential", err)
	}
}

func TestVerifierConsumesOnlyAfterSuccessfulCallback(t *testing.T) {
	token, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewVerifier(token)
	if err != nil {
		t.Fatal(err)
	}
	rollback := errors.New("transaction rolled back")
	if err := verifier.Use(token, func() error { return rollback }); !errors.Is(err, rollback) {
		t.Fatalf("rollback error = %v, want callback error", err)
	}
	if !verifier.valid {
		t.Fatal("verifier was consumed after callback failure")
	}
	if err := verifier.WithCredential(token, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if verifier.valid || !isZero(verifier.hash[:]) {
		t.Fatal("verifier retained secret digest after successful callback")
	}
	if err := verifier.Use(token, func() error { t.Fatal("consumed credential invoked callback"); return nil }); !errors.Is(err, ErrInvalidCredential) {
		t.Fatalf("reused credential error = %v, want ErrInvalidCredential", err)
	}
}

func TestVerifierSerializesConcurrentUse(t *testing.T) {
	token, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewVerifier(token)
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	var callbacks atomic.Int32
	firstResult := make(chan error, 1)
	go func() {
		firstResult <- verifier.WithCredential(token, func() error {
			callbacks.Add(1)
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered

	secondStarted := make(chan struct{})
	secondResult := make(chan error, 1)
	go func() {
		close(secondStarted)
		secondResult <- verifier.Use(token, func() error {
			callbacks.Add(1)
			return nil
		})
	}()
	<-secondStarted
	select {
	case err := <-secondResult:
		t.Fatalf("second concurrent use returned while first callback held lock: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if err := <-firstResult; err != nil {
		t.Fatal(err)
	}
	if err := <-secondResult; !errors.Is(err, ErrInvalidCredential) {
		t.Fatalf("second concurrent use error = %v, want ErrInvalidCredential", err)
	}
	if got := callbacks.Load(); got != 1 {
		t.Fatalf("callback count = %d, want 1", got)
	}
}

func TestVerifierInvalidateIsPermanentAndIdempotent(t *testing.T) {
	token, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewVerifier(token)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifier.Consume(token); err != nil {
		t.Fatal(err)
	}
	if err := verifier.Consume(token); !errors.Is(err, ErrInvalidCredential) {
		t.Fatalf("consumed credential error = %v, want ErrInvalidCredential", err)
	}

	verifier, err = NewVerifier(token)
	if err != nil {
		t.Fatal(err)
	}
	verifier.Invalidate()
	verifier.Invalidate()
	if verifier.valid || !isZero(verifier.hash[:]) {
		t.Fatal("Invalidate did not clear verifier state")
	}
	if err := verifier.Use(token, func() error { t.Fatal("invalidated credential invoked callback"); return nil }); !errors.Is(err, ErrInvalidCredential) {
		t.Fatalf("invalidated credential error = %v, want ErrInvalidCredential", err)
	}
}

func TestVerifierRejectsInvalidConstructionAndNilCallback(t *testing.T) {
	secret := "invalid-credential-secret"
	if _, err := NewVerifier(secret); !errors.Is(err, ErrInvalidCredential) || strings.Contains(err.Error(), secret) {
		t.Fatalf("invalid verifier error = %v, contains input or has wrong sentinel", err)
	}
	token, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewVerifier(token)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifier.Use(token, nil); !errors.Is(err, ErrInvalidCredential) {
		t.Fatalf("nil callback error = %v, want ErrInvalidCredential", err)
	}
	if !verifier.valid {
		t.Fatal("nil callback consumed verifier")
	}
}

func decodeCanonicalToken(token string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(token)
}

type shortWriter struct {
	data bytes.Buffer
	max  int
}

func (writer *shortWriter) Write(p []byte) (int, error) {
	n := writer.max
	if n > len(p) {
		n = len(p)
	}
	_, _ = writer.data.Write(p[:n])
	return n, nil
}

type errorWriter struct{ err error }

func (writer *errorWriter) Write([]byte) (int, error) { return 0, writer.err }

type zeroWriter struct{}

func (*zeroWriter) Write([]byte) (int, error) { return 0, nil }

type infiniteReader struct {
	data []byte
	read int
}

func (reader *infiniteReader) Read(p []byte) (int, error) {
	if len(reader.data) == 0 {
		for i := range p {
			p[i] = 'x'
		}
		reader.read += len(p)
		return len(p), nil
	}
	n := len(p)
	if n > len(reader.data) {
		n = len(reader.data)
	}
	copy(p[:n], reader.data[:n])
	reader.data = reader.data[n:]
	reader.read += n
	return n, nil
}

type errorReader struct{ err error }

func (reader *errorReader) Read([]byte) (int, error) { return 0, reader.err }

func isZero(raw []byte) bool {
	var zero byte
	for _, value := range raw {
		if value != zero {
			return false
		}
	}
	return true
}

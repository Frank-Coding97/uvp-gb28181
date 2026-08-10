package securestore

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestLoadCipherFromEnv(t *testing.T) {
	const envName = "UVP_TEST_CASCADE_KEY"
	t.Setenv(envName, "")
	if _, err := LoadCipherFromEnv(envName, "v1"); !errors.Is(err, ErrKeyUnavailable) {
		t.Fatalf("missing key err=%v", err)
	}
	t.Setenv(envName, "short")
	if _, err := LoadCipherFromEnv(envName, "v1"); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("invalid key err=%v", err)
	}
	t.Setenv(envName, base64.StdEncoding.EncodeToString([]byte("01234567890123456789012345678901")))
	if _, err := LoadCipherFromEnv(envName, "v1"); err != nil {
		t.Fatal(err)
	}
}

func TestCipherRoundTripAndMetadataBinding(t *testing.T) {
	cipher, err := NewCipher([]byte("01234567890123456789012345678901"), "v1")
	if err != nil {
		t.Fatal(err)
	}
	for _, plaintext := range []string{"", "密碼-パスワード", strings.Repeat("x", 4096)} {
		envelope, encryptErr := cipher.Encrypt("upstream-password", []byte(plaintext))
		if encryptErr != nil {
			t.Fatal(encryptErr)
		}
		decoded, decryptErr := cipher.Decrypt("upstream-password", envelope)
		if decryptErr != nil || string(decoded) != plaintext {
			t.Fatalf("decoded=%q err=%v", decoded, decryptErr)
		}
		if plaintext != "" {
			serialized, _ := json.Marshal(envelope)
			if strings.Contains(string(serialized), plaintext) {
				t.Fatal("serialized envelope leaked plaintext")
			}
		}
		if _, decryptErr = cipher.Decrypt("another-purpose", envelope); !errors.Is(decryptErr, ErrAuthentication) {
			t.Fatalf("purpose mismatch err=%v", decryptErr)
		}
		tampered := envelope
		tampered.KeyVersion = "v2"
		if _, decryptErr = cipher.Decrypt("upstream-password", tampered); !errors.Is(decryptErr, ErrAuthentication) {
			t.Fatalf("version mismatch err=%v", decryptErr)
		}
	}
}

func TestCipherRejectsInvalidKeyAndPurpose(t *testing.T) {
	if _, err := NewCipher([]byte("short"), "v1"); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("err=%v", err)
	}
	cipher, err := NewCipher([]byte("01234567890123456789012345678901"), "v1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = cipher.Encrypt("", []byte("secret")); !errors.Is(err, ErrInvalidPurpose) {
		t.Fatalf("err=%v", err)
	}
}

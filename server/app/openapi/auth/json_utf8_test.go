package auth

import "testing"

func TestOpenAPISignatureRejectsInvalidUTF8JSON(t *testing.T) {
	f := loadSignatureFixture(t)
	input := fixtureInput(f, f.Vectors[1])
	input.Body = []byte("{\"protocol\":\"https-\xffflv\"}")
	if _, err := Sign(input, f.SecretKey); err == nil {
		t.Fatal("invalid UTF-8 JSON must not be accepted via decoder replacement")
	}
}

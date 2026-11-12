package ginhelper

import (
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAccessLogFormatterHidesOpenAPIQueryDynamicPathAndError(t *testing.T) {
	cases := []struct {
		name          string
		path          string
		errorMessage  string
		wantPathToken string
	}{
		{
			name:          "namespace",
			path:          "/openapi/v1/devices?ak=client-a&signature=sk-secret",
			errorMessage:  "signature=error-secret",
			wantPathToken: "/openapi",
		},
		{
			name:          "percent encoded namespace",
			path:          "/%6F%70%65%6E%61%70%69/%76%31/devices?signature=sk-secret",
			errorMessage:  "play_token=error-secret",
			wantPathToken: "/openapi",
		},
		{
			name:          "unknown dynamic path",
			path:          "/openapi/v1/unknown/device/SK-secret?nonce=nonce-secret",
			errorMessage:  "internal request SK-secret",
			wantPathToken: "/openapi",
		},
		{
			name:          "case and invalid URL",
			path:          "/OPENAPI/%zz?signature=sk-secret",
			errorMessage:  "invalid URL secret=error-secret",
			wantPathToken: "/openapi",
		},
		{
			name:          "escaped namespace with invalid suffix",
			path:          "/%6fpenapi%2fv1/%zz?signature=sk-secret",
			errorMessage:  "invalid URL secret=error-secret",
			wantPathToken: "/openapi",
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := accessLogFormatter(gin.LogFormatterParams{
				StatusCode:   401,
				Method:       "GET",
				Path:         tt.path,
				ErrorMessage: tt.errorMessage,
			})
			if !strings.Contains(got, tt.wantPathToken) {
				t.Fatalf("formatter path = %q, want fixed OpenAPI namespace", got)
			}
			for _, secret := range []string{
				"client-a",
				"sk-secret",
				"nonce-secret",
				"error-secret",
				"SK-secret",
			} {
				if strings.Contains(got, secret) {
					t.Fatalf("formatter leaked %q: %s", secret, got)
				}
			}
		})
	}
}

func TestRedactAccessLogPathFallbackKeepsBackendRedactionCaseInsensitive(t *testing.T) {
	for _, path := range []string{
		"/index/hook/on_stream_not_found?PLAY_TOKEN=play-secret%zz&keep=visible",
		"/index/hook/on_flow_report?CAP=cap-secret%zz",
	} {
		got := redactAccessLogPathFallback(path)
		if strings.Contains(got, "play-secret") || strings.Contains(got, "cap-secret") {
			t.Fatalf("backend fallback leaked credential for %q: %s", path, got)
		}
		if !strings.Contains(got, "REDACTED") {
			t.Fatalf("backend fallback did not redact credential for %q: %s", path, got)
		}
	}
}

func TestAccessLogFormatterPreservesNonOpenAPIPathAndError(t *testing.T) {
	got := accessLogFormatter(gin.LogFormatterParams{
		StatusCode:   500,
		Method:       "GET",
		Path:         "/api/devices?id=42",
		ErrorMessage: "backend-error",
	})
	if !strings.Contains(got, "/api/devices?id=42") || !strings.Contains(got, "backend-error") {
		t.Fatalf("non-OpenAPI access log changed unexpectedly: %s", got)
	}
}

func TestAccessLogFormatterHidesOpenAPICustomMethod(t *testing.T) {
	got := accessLogFormatter(gin.LogFormatterParams{
		StatusCode:   405,
		Method:       "SK-secret",
		Path:         "/openapi/v1/unknown",
		ErrorMessage: "method rejected",
	})
	if strings.Contains(got, "SK-secret") {
		t.Fatalf("OpenAPI access log leaked custom method: %s", got)
	}
	if !strings.Contains(got, "UNKNOWN") {
		t.Fatalf("OpenAPI access log did not use UNKNOWN for custom method: %s", got)
	}
}

func TestRedactAccessLogPathDoesNotTreatOpenAPI2AsNamespace(t *testing.T) {
	path := "/openapi2/v1?signature=secret"
	if got := redactAccessLogPath(path); got != path {
		t.Fatalf("non-OpenAPI namespace was redacted: got %q, want %q", got, path)
	}
}

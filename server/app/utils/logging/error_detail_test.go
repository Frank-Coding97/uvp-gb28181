package logging

import (
	"errors"
	"fmt"
	"go.uber.org/zap"
	"strings"
	"testing"
)

func TestErrorDetailPreservesWrappedCause(t *testing.T) {
	r, sink := runtimeForTest(t, nil)
	err := fmt.Errorf("启动后端: %w", errors.New("local process authority already owned"))
	r.Root.Error("failed", Error(err), zap.NamedError("secondary", err))
	for _, key := range []string{"error", "secondary"} {
		got := records(t, sink)[0][key].(map[string]interface{})["detail"]
		if got != err.Error() {
			t.Fatalf("lost error chain: %v", got)
		}
	}
}

func TestErrorDetailRedactsCredentials(t *testing.T) {
	for _, input := range []string{
		"dial https://admin:secret123@example.test/api: refused",
		"connect admin:secret123@tcp(localhost:3306)/db failed",
		"password=secret123 failed", `token="secret123" failed`,
		"Authorization: Bearer secret123 failed", "UVP_OPENAPI_MASTER_KEY=secret123 failed",
	} {
		got := redactErrorDetail(input)
		if strings.Contains(got, "secret123") || !strings.Contains(got, "[REDACTED]") {
			t.Fatalf("credential not redacted")
		}
	}
}

func TestErrorDetailConsoleAndStringFields(t *testing.T) {
	r, sink := runtimeForTest(t, values{"logs.textformat": "console"})
	r.Root.Error("启动失败", Error(errors.New("保存目录: permission denied")), zap.String("err", "password=secret123 connection refused"))
	got := sink.String()
	if !strings.Contains(got, "permission denied") || !strings.Contains(got, "connection refused") || strings.Contains(got, "secret123") {
		t.Fatalf("console diagnostic missing or unsanitized")
	}
}

type brokenError struct{}

func (brokenError) Error() string { panic("format panic") }
func TestErrorDetailFormattingFailure(t *testing.T) {
	if got := errorDetail(brokenError{}); got != "[error formatting failed]" {
		t.Fatal(got)
	}
}

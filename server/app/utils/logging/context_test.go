package logging

import (
	"context"
	"go.uber.org/zap"
	"testing"
)

func TestLoggingContextDerivedScope(t *testing.T) {
	r, b := runtimeForTest(t, nil)
	root := context.Background()
	scope := WithContext(root, WithIdentity(r.Root, zap.String("request_id", "request-one")))
	child, cancel := context.WithCancel(scope)
	cancel()
	FromContext(context.WithoutCancel(child), nil).Info("detached audit", zap.String("event", "audit.test"))
	FromContext(root, r.Root).Info("background operation", zap.String("event", "background.test"))
	rows := records(t, b)
	if rows[0]["request_id"] != "request-one" {
		t.Fatal("derived context lost scope")
	}
	if _, ok := rows[1]["request_id"]; ok {
		t.Fatal("root scope polluted")
	}
	FromContext(nil, nil).Info("safe no-op")
}

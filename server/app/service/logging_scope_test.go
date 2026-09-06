package service

import (
	"context"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"testing"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
)

func TestLoggingBaseHTTPServiceScope(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	root := zap.New(core)
	ctx, cancel := context.WithCancel(logging.WithContext(context.Background(), logging.WithIdentity(root, zap.String("request_id", "generator-request"))))
	cancel()
	generator := NewCodeGenService()
	child := generator.WithContext(ctx)
	if child == generator || child.scope != ctx || generator.scope != nil {
		t.Fatal("codegen scope did not clone immutably")
	}
	plugins := NewPluginsManagerService()
	pluginChild := plugins.WithContext(ctx)
	if pluginChild == plugins || pluginChild.scope == nil || pluginChild.scope.Err() != nil || plugins.scope != nil {
		t.Fatal("plugin work adopted request cancellation or mutated singleton")
	}
	logging.FromContext(pluginChild.scope, nil).Info("plugin scope test")
	if logs.All()[0].ContextMap()["request_id"] != "generator-request" {
		t.Fatal("detached plugin scope lost correlation")
	}
}

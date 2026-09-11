package main

import (
	"testing"

	"uvplatform.cn/uvp-gb28181/internal/standalone/launcher"
)

func TestTakeManagementURLOnlyReturnsTheFirstReadyURL(t *testing.T) {
	printed := false
	status := launcher.Status{ManagementURL: "http://127.0.0.1:8280/"}
	got, ok := takeManagementURL(&printed, status)
	if !ok || got != status.ManagementURL {
		t.Fatalf("first management URL = %q, %v", got, ok)
	}
	got, ok = takeManagementURL(&printed, status)
	if ok || got != "" {
		t.Fatalf("management URL repeated = %q, %v", got, ok)
	}
}

func TestDisplayBusinessReasonKeepsComponentAndBusinessStatesDistinct(t *testing.T) {
	if got := displayBusinessReason("status_unavailable"); got != "业务状态暂不可用" {
		t.Fatalf("unavailable reason = %q", got)
	}
	if got := displayBusinessReason("installation_pending"); got != "安装尚未完成" {
		t.Fatalf("pending reason = %q", got)
	}
	if got := displayBusinessReason(""); got != "业务状态待就绪" {
		t.Fatalf("empty reason = %q", got)
	}
}

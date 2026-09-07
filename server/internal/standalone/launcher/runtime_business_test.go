package launcher

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"uvplatform.cn/uvp-gb28181/internal/standalone/readiness"
)

func TestObserveBusinessPublishesAnImmediateProbeAndStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	probed := make(chan struct{}, 1)
	updates := observeBusiness(ctx, time.Hour, time.Second, func(context.Context) (readiness.Status, error) {
		probed <- struct{}{}
		return readiness.Status{SIPState: "starting", BusinessReason: "SIP 正在启动"}, nil
	})

	select {
	case <-probed:
	case <-time.After(time.Second):
		t.Fatal("business readiness was not probed immediately")
	}
	select {
	case status := <-updates:
		if status.SIPState != "starting" || status.BusinessReady || status.BusinessReason != "SIP 正在启动" {
			t.Fatalf("unexpected business status: %+v", status)
		}
	case <-time.After(time.Second):
		t.Fatal("business status was not published")
	}

	cancel()
	select {
	case _, ok := <-updates:
		if ok {
			for range updates {
			}
		}
	case <-time.After(time.Second):
		t.Fatal("business observer did not stop")
	}
}

func TestBusinessStatusFromReadinessHidesProbeErrors(t *testing.T) {
	state := readiness.Status{SIPState: "failed", BusinessReason: "should be replaced"}
	status := businessStatusFromReadiness(state, errors.New("GET http://127.0.0.1:8280/?secret=leak"))
	if status.BusinessReady || status.BusinessReason != "status_unavailable" {
		t.Fatalf("unexpected unavailable status: %+v", status)
	}
	if strings.Contains(status.BusinessReason, "127.0.0.1") || strings.Contains(status.BusinessReason, "secret") {
		t.Fatal("probe details leaked into business reason")
	}
}

func TestBusinessStatusFromReadinessUsesChineseFallbackReason(t *testing.T) {
	status := businessStatusFromReadiness(readiness.Status{SIPState: "unconfigured"}, nil)
	if status.BusinessReady || status.BusinessReason != "SIP 尚未配置" {
		t.Fatalf("unexpected fallback status: %+v", status)
	}
}

func TestReadyBrowserEntryIsOnlyGeneratedOnce(t *testing.T) {
	opened := false
	status := Status{State: Ready, ManagementURL: "http://127.0.0.1:8280/"}
	entry, ok := readyBrowserEntry(&opened, status, "pending_admin", "token")
	if !ok || entry != status.ManagementURL+"#/standalone-setup?bootstrap_token=token" {
		t.Fatalf("unexpected first browser entry: %q %v", entry, ok)
	}
	entry, ok = readyBrowserEntry(&opened, status, "pending_admin", "token")
	if ok || entry != "" {
		t.Fatalf("repeated Ready update reopened browser: %q %v", entry, ok)
	}
}

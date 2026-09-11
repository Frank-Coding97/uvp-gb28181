package main

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"uvplatform.cn/uvp-gb28181/internal/standalone"
)

func TestRecoveryConfirmCommandRejectsArgumentsAndDoesNotInvokeCore(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"--operation"},
		{"--operation", "op", "extra"},
		{"--operation", "op", "--password", "secret"},
		{"--operation", "op", "--password=secret"},
		{"--operation", "op", "--username", "admin"},
		{"--operation", "op", "--install-dir", "root", "extra"},
	} {
		var out, diagnostic bytes.Buffer
		called := false
		confirm := func(context.Context, string, string, func(standalone.RecoveryConfirmationInfo) (standalone.RecoveryConfirmationInput, error)) error {
			called = true
			return nil
		}
		prompt := func(standalone.RecoveryConfirmationInfo) (standalone.RecoveryConfirmationInput, error) {
			called = true
			return standalone.RecoveryConfirmationInput{}, nil
		}
		if code := runRecoveryConfirmCommandWith(args, "default-root", &out, &diagnostic, confirm, prompt); code == 0 || out.Len() != 0 || diagnostic.Len() == 0 || called {
			t.Fatalf("args=%v: expected strict argument failure without core invocation; code=%d out=%q diagnostic=%q called=%v", args, code, out.String(), diagnostic.String(), called)
		}
	}
}

func TestRecoveryConfirmationReviewDistinguishesUncleanSnapshot(t *testing.T) {
	var out bytes.Buffer
	err := writeRecoveryConfirmationReview(&out, standalone.RecoveryConfirmationInfo{Kind: "unclean_recovery", OperationID: "fixture-operation", BackupTime: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"异常现场快照时间", "当前版本保持不变", "已重建会话和播放凭据", "设备长期凭据保留自异常现场"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q in review", want)
		}
	}
	if strings.Contains(out.String(), "回退到备份") {
		t.Fatal("unclean recovery misrepresented as historical rollback")
	}
}

func TestRecoveryConfirmCommandDisplaysReviewAndPassesSecretOnlyToInjectedCore(t *testing.T) {
	operation := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	password := "secret-do-not-print"
	backupTime := time.Date(2026, 9, 8, 18, 30, 0, 0, time.FixedZone("CST", 8*60*60))
	wantInfo := standalone.RecoveryConfirmationInfo{
		OperationID: operation,
		BackupTime:  backupTime,
		Impacts:     []string{"SQLite 数据将使用恢复快照", "Redis 活跃会话将被撤销", "录像索引需要重新核对"},
	}
	var out, diagnostic bytes.Buffer
	var gotInfo standalone.RecoveryConfirmationInfo
	var gotInput standalone.RecoveryConfirmationInput
	confirm := func(ctx context.Context, root, gotOperation string, prompt func(standalone.RecoveryConfirmationInfo) (standalone.RecoveryConfirmationInput, error)) error {
		if ctx == nil || root != "isolated-root" || gotOperation != operation {
			t.Fatalf("unexpected core arguments: ctx=%v root=%q operation=%q", ctx, root, gotOperation)
		}
		gotInfo = wantInfo
		var err error
		gotInput, err = prompt(wantInfo)
		return err
	}
	prompt := func(info standalone.RecoveryConfirmationInfo) (standalone.RecoveryConfirmationInput, error) {
		if !reflect.DeepEqual(info, wantInfo) {
			t.Fatalf("prompt info = %+v, want %+v", info, wantInfo)
		}
		return standalone.RecoveryConfirmationInput{Username: "admin", Password: password, Acknowledgement: "CONFIRM " + operation}, nil
	}
	if code := runRecoveryConfirmCommandWith([]string{"--operation", operation}, "isolated-root", &out, &diagnostic, confirm, prompt); code != 0 {
		t.Fatalf("confirmation failed: code=%d out=%q diagnostic=%q", code, out.String(), diagnostic.String())
	}
	if !reflect.DeepEqual(gotInfo, wantInfo) || gotInput.Username != "admin" || gotInput.Password != password || gotInput.Acknowledgement != "CONFIRM "+operation {
		t.Fatalf("core received info/input = %+v / %+v", gotInfo, gotInput)
	}
	for _, visible := range []string{"备份时间：2026-09-08T18:30:00+08:00", "SQLite 数据将使用恢复快照", "Redis 活跃会话将被撤销", "录像索引需要重新核对", "长期设备凭据", "回退", "本机确认已完成"} {
		if !strings.Contains(out.String(), visible) {
			t.Fatalf("review output omitted %q: %s", visible, out.String())
		}
	}
	if strings.Contains(out.String(), password) || strings.Contains(diagnostic.String(), password) {
		t.Fatal("confirmation password leaked to command output")
	}
}

func TestRecoveryConfirmCommandDoesNotReportSuccessWhenCoreRejects(t *testing.T) {
	var out, diagnostic bytes.Buffer
	promptCalled := false
	confirm := func(context.Context, string, string, func(standalone.RecoveryConfirmationInfo) (standalone.RecoveryConfirmationInput, error)) error {
		return errors.New("recovery precondition failed")
	}
	prompt := func(standalone.RecoveryConfirmationInfo) (standalone.RecoveryConfirmationInput, error) {
		promptCalled = true
		return standalone.RecoveryConfirmationInput{Password: "secret"}, nil
	}
	if code := runRecoveryConfirmCommandWith([]string{"--operation", "op"}, "root", &out, &diagnostic, confirm, prompt); code == 0 || out.Len() != 0 || diagnostic.Len() == 0 || promptCalled {
		t.Fatalf("core rejection was reported as success or prompted: code=%d out=%q diagnostic=%q prompted=%v", code, out.String(), diagnostic.String(), promptCalled)
	}
	if strings.Contains(diagnostic.String(), "secret") {
		t.Fatal("core rejection output leaked a password")
	}
}

func TestRecoveryConfirmCommandPassesExactAcknowledgement(t *testing.T) {
	operation := "op-123"
	var got standalone.RecoveryConfirmationInput
	confirm := func(_ context.Context, _ string, _ string, prompt func(standalone.RecoveryConfirmationInfo) (standalone.RecoveryConfirmationInput, error)) error {
		input, err := prompt(standalone.RecoveryConfirmationInfo{OperationID: operation, BackupTime: time.Now(), Impacts: []string{"impact"}})
		got = input
		return err
	}
	prompt := func(standalone.RecoveryConfirmationInfo) (standalone.RecoveryConfirmationInput, error) {
		return standalone.RecoveryConfirmationInput{Acknowledgement: " CONFIRM " + operation + " "}, nil
	}
	var out, diagnostic bytes.Buffer
	if code := runRecoveryConfirmCommandWith([]string{"--operation", operation}, "root", &out, &diagnostic, confirm, prompt); code != 0 {
		t.Fatalf("command unexpectedly failed: code=%d diagnostic=%q", code, diagnostic.String())
	}
	if got.Acknowledgement != " CONFIRM "+operation+" " {
		t.Fatalf("acknowledgement was normalized: %q", got.Acknowledgement)
	}
}

func TestRecoveryConfirmReviewDoesNotClaimUnverifiedScopeHasNoImpact(t *testing.T) {
	var out bytes.Buffer
	if err := writeRecoveryConfirmationReview(&out, standalone.RecoveryConfirmationInfo{}); err != nil {
		t.Fatalf("writeRecoveryConfirmationReview failed: %v", err)
	}
	if strings.Contains(out.String(), "无额外影响") || !strings.Contains(out.String(), "未发现已支持的用户与角色差异；长期凭据仍需核对") {
		t.Fatalf("empty-impact review made an unsupported claim: %q", out.String())
	}
}

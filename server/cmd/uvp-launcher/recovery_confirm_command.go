package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"uvplatform.cn/uvp-gb28181/internal/standalone"
)

type recoveryConfirmationRunner func(context.Context, string, string, func(standalone.RecoveryConfirmationInfo) (standalone.RecoveryConfirmationInput, error)) error
type recoveryConfirmationPrompt func(standalone.RecoveryConfirmationInfo) (standalone.RecoveryConfirmationInput, error)

func runRecoveryConfirmCommand(args []string, defaultRoot string, stdout, stderr io.Writer) int {
	return runRecoveryConfirmCommandWith(args, defaultRoot, stdout, stderr, standalone.ConfirmRecovery, func(info standalone.RecoveryConfirmationInfo) (standalone.RecoveryConfirmationInput, error) {
		return readRecoveryConfirmationInput(info, stdout)
	})
}

func runRecoveryConfirmCommandWith(args []string, defaultRoot string, stdout, stderr io.Writer, confirm recoveryConfirmationRunner, prompt recoveryConfirmationPrompt) int {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	flags := flag.NewFlagSet("recovery-confirm", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("install-dir", defaultRoot, "安装目录")
	operation := flags.String("operation", "", "待确认的恢复操作编号")
	if err := flags.Parse(args); err != nil {
		return 1
	}
	if flags.NArg() != 0 || strings.TrimSpace(*operation) == "" || strings.TrimSpace(*root) == "" {
		fmt.Fprintln(stderr, "用法：UVP.exe recovery-confirm --operation <操作编号> [--install-dir <安装目录>]")
		return 1
	}
	if confirm == nil || prompt == nil {
		fmt.Fprintln(stderr, "本机确认未完成：确认处理器不可用")
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	wrappedPrompt := func(info standalone.RecoveryConfirmationInfo) (standalone.RecoveryConfirmationInput, error) {
		if err := writeRecoveryConfirmationReview(stdout, info); err != nil {
			return standalone.RecoveryConfirmationInput{}, err
		}
		return prompt(info)
	}
	if err := confirm(ctx, *root, *operation, wrappedPrompt); err != nil {
		fmt.Fprintln(stderr, "本机确认未完成：", err)
		return 1
	}
	fmt.Fprintf(stdout, "本机确认已完成：%s\n恢复档案已封存，请重新启动 UVP。\n", *operation)
	return 0
}

func writeRecoveryConfirmationReview(out io.Writer, info standalone.RecoveryConfirmationInfo) error {
	if out == nil {
		return errors.New("recovery confirmation output is unavailable")
	}
	if _, err := fmt.Fprintln(out, "UVP 恢复本机确认"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "操作编号：%s\n", info.OperationID); err != nil {
		return err
	}
	timeLabel := "备份时间"
	if info.Kind == "unclean_recovery" {
		timeLabel = "异常现场快照时间"
	}
	if _, err := fmt.Fprintf(out, "%s：%s\n", timeLabel, info.BackupTime.Format(time.RFC3339)); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out, "影响范围："); err != nil {
		return err
	}
	if len(info.Impacts) == 0 {
		if _, err := fmt.Fprintln(out, "- 未发现已支持的用户与角色差异；长期凭据仍需核对"); err != nil {
			return err
		}
	} else {
		for _, impact := range info.Impacts {
			if _, err := fmt.Fprintf(out, "- %s\n", impact); err != nil {
				return err
			}
		}
	}
	if info.Kind == "unclean_recovery" {
		_, err := fmt.Fprintln(out, "当前版本保持不变；已重建会话和播放凭据。设备长期凭据保留自异常现场，请核对设备注册状态。")
		return err
	}
	_, err := fmt.Fprintln(out, "长期设备凭据可能回退到备份时状态，恢复后请重新核对设备注册信息。")
	return err
}

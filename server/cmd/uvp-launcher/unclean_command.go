package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"uvplatform.cn/uvp-gb28181/internal/standalone"
	"uvplatform.cn/uvp-gb28181/internal/standalone/launcher"
)

type uncleanRecoveryRunner func(context.Context, string, string, string) (standalone.UncleanRecoveryResult, error)

func runUncleanRecoveryCommand(args []string, defaultRoot string, stdout, stderr io.Writer) int {
	return runUncleanRecoveryCommandWith(args, defaultRoot, stdout, stderr, launcher.RecoverUncleanStopped)
}

func runUncleanRecoveryCommandWith(args []string, defaultRoot string, stdout, stderr io.Writer, runner uncleanRecoveryRunner) int {
	flags := flag.NewFlagSet("recover", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("install-dir", defaultRoot, "安装目录")
	recordings := flags.String("recordings-dir", "", "实际录像目录（外置录像必须指定）")
	snapshot := flags.String("snapshot", "", "安装目录外新的恢复快照目录")
	if err := flags.Parse(args); err != nil {
		return 1
	}
	if flags.NArg() != 0 || strings.TrimSpace(*root) == "" {
		fmt.Fprintln(stderr, "用法：UVP.exe recover [--snapshot <安装目录外的新目录>] [--install-dir <安装目录>] [--recordings-dir <实际录像目录>]")
		return 1
	}
	if runner == nil {
		fmt.Fprintln(stderr, "恢复未完成：恢复处理器不可用")
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	result, err := runner(ctx, *root, *recordings, *snapshot)
	if err != nil {
		fmt.Fprintln(stderr, "恢复未完成：", err)
		return 1
	}
	if strings.TrimSpace(result.OperationID) == "" {
		fmt.Fprintln(stderr, "恢复未完成：恢复操作编号不可用")
		return 1
	}
	if result.AwaitingLocalConfirmation {
		fmt.Fprint(stdout, uncleanRecoveryConfirmationMessage(result.OperationID))
		return 0
	}
	fmt.Fprint(stdout, uncleanRecoveryPristineMessage())
	return 0
}

func uncleanRecoveryConfirmationMessage(operationID string) string {
	return fmt.Sprintf("恢复已完成，等待本机确认：%s\n请运行：UVP.exe recovery-confirm --operation %s\n", operationID, operationID)
}

func uncleanRecoveryPristineMessage() string {
	return "恢复已完成：数据库仍处于首装状态。\n请重新启动 UVP 继续首装。\n"
}

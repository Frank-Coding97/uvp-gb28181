package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"time"

	"uvplatform.cn/uvp-gb28181/internal/standalone"
	"uvplatform.cn/uvp-gb28181/internal/standalone/launcher"
)

func runRestoreCommand(args []string, defaultRoot string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("restore", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("install-dir", defaultRoot, "安装目录")
	recordings := flags.String("recordings-dir", "", "实际录像目录（外置录像必须指定）")
	if err := flags.Parse(args); err != nil {
		return 1
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "用法：UVP.exe restore [--install-dir <安装目录>] [--recordings-dir <实际录像目录>]")
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	result, err := launcher.RestoreStopped(ctx, *root, *recordings)
	if err != nil {
		fmt.Fprintln(stderr, "恢复未完成：", err)
		return 1
	}
	if result.Phase != standalone.MaintenanceAwaitingConfirmation {
		fmt.Fprintln(stderr, "恢复未完成：恢复未进入本机确认阶段")
		return 1
	}
	fmt.Fprint(stdout, restoreSuccessMessage(result.OperationID))
	return 0
}

func restoreSuccessMessage(operationID string) string {
	return fmt.Sprintf("恢复已完成，等待本机确认：%s\n请运行：UVP.exe recovery-confirm --operation %s\n", operationID, operationID)
}

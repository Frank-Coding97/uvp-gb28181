package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"time"

	"uvplatform.cn/uvp-gb28181/internal/standalone/launcher"
)

func runBackupCommand(args []string, defaultRoot string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("backup", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("install-dir", defaultRoot, "安装目录")
	recordings := flags.String("recordings-dir", "", "实际录像目录（外置录像必须指定）")
	output := flags.String("output", "", "新的备份目录绝对路径，不能已存在")
	if err := flags.Parse(args); err != nil {
		return 1
	}
	if flags.NArg() != 0 || *output == "" {
		fmt.Fprintln(stderr, "用法：UVP.exe backup --output <新备份目录> [--recordings-dir <实际录像目录>]")
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	manifest, err := launcher.Backup(ctx, *root, *recordings, *output)
	if err != nil {
		fmt.Fprintln(stderr, "备份未完成：", err)
		return 1
	}
	fmt.Fprintf(stdout, "备份已完成：%s\n版本：%s\n录像仅记录路径，未复制视频；此备份不覆盖录像磁盘损坏。\n", *output, manifest.Version)
	return 0
}

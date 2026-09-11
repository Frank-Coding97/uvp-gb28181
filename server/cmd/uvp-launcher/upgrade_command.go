package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"time"

	"uvplatform.cn/uvp-gb28181/internal/standalone/launcher"
)

func runUpgradeCommand(args []string, defaultRoot string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("upgrade", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("install-dir", defaultRoot, "安装目录")
	recordings := flags.String("recordings-dir", "", "实际录像目录（外置录像必须指定）")
	version := flags.String("version", "", "已放入releases的候选版本")
	backup := flags.String("backup", "", "新的备份目录绝对路径，不能已存在")
	if err := flags.Parse(args); err != nil {
		return 1
	}
	if flags.NArg() != 0 || *version == "" || *backup == "" {
		fmt.Fprintln(stderr, "用法：UVP.exe upgrade --version <候选版本> --backup <新备份目录> [--recordings-dir <实际录像目录>]")
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	if err := launcher.UpgradeStopped(ctx, *root, *recordings, *version, *backup); err != nil {
		fmt.Fprintln(stderr, "升级未完成：", err)
		return 1
	}
	fmt.Fprintf(stdout, "升级已完成：%s\n备份：%s\n请重新双击UVP.exe启动。\n", *version, *backup)
	return 0
}

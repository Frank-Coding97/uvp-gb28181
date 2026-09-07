package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"time"

	"uvplatform.cn/uvp-gb28181/internal/standalone/launcher"
)

func main() {
	if runtime.GOOS != "windows" {
		fmt.Fprintln(os.Stderr, "UVP 单机启动器仅支持 Windows 10 x64 及以上兼容系统")
		os.Exit(1)
	}
	executable, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	root := flag.String("install-dir", filepath.Dir(executable), "安装目录")
	recordings := flag.String("recordings-dir", "", "录像目录（默认安装目录下 recordings）")
	noBrowser := flag.Bool("no-browser", false, "仅启动组件，不打开浏览器（用于自动测试）")
	stop := flag.Bool("stop", false, "停止当前安装目录的实例并等待完成")
	flag.Parse()
	if *stop {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if err := launcher.Stop(ctx, *root, *recordings); err != nil {
			fmt.Fprintln(os.Stderr, "停止未确认完成：", err)
			os.Exit(1)
		}
		fmt.Println("UVP 已停止")
		return
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	err = launcher.LaunchWithBrowser(ctx, *root, *recordings, func(status launcher.Status) {
		if status.State == launcher.Ready {
			if status.PreviousUnclean {
				fmt.Println("检测到上次异常退出；本次启动检查已通过，录像完整性仍需核对")
			}
			fmt.Println("管理地址：", status.ManagementURL)
		}
		if status.State == launcher.Ready && !status.BusinessReady {
			fmt.Println("基础组件已就绪，SIP 待配置或待就绪")
			return
		}
		fmt.Printf("UVP: %s\n", status.State)
	}, func(entry string) {
		if !*noBrowser {
			if err := openManagementBrowser(entry); err != nil {
				fmt.Fprintln(os.Stderr, "浏览器打开失败，请从本机启动器重新打开初始化页面")
			}
		}
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "启动器退出：", err)
		os.Exit(1)
	}
}

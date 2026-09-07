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
	componentReadyPrinted := false
	managementURLPrinted := false
	previousUncleanPrinted := false
	err = launcher.LaunchWithBrowser(ctx, *root, *recordings, func(status launcher.Status) {
		if status.State == launcher.Ready {
			if !componentReadyPrinted {
				componentReadyPrinted = true
				fmt.Println("基础组件已就绪")
			}
			if status.PreviousUnclean && !previousUncleanPrinted {
				fmt.Println("检测到上次异常退出；本次启动检查已通过，录像完整性仍需核对")
				previousUncleanPrinted = true
			}
			if managementURL, ok := takeManagementURL(&managementURLPrinted, status); ok {
				fmt.Println("管理地址：", managementURL)
			}
			if status.BusinessReady {
				fmt.Println("业务服务已就绪")
			} else {
				fmt.Println("业务状态：", displayBusinessReason(status.BusinessReason))
			}
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

func displayBusinessReason(reason string) string {
	switch reason {
	case "status_unavailable":
		return "业务状态暂不可用"
	case "installation_pending":
		return "安装尚未完成"
	case "":
		return "业务状态待就绪"
	default:
		return reason
	}
}

func takeManagementURL(printed *bool, status launcher.Status) (string, bool) {
	if printed == nil || *printed || status.ManagementURL == "" {
		return "", false
	}
	*printed = true
	return status.ManagementURL, true
}

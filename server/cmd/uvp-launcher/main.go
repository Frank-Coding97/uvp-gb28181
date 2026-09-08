package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"uvplatform.cn/uvp-gb28181/internal/standalone/launcher"
)

func main() {
	if code, handled := runFirewallCommand(os.Args[1:]); handled {
		os.Exit(code)
	}
	if runtime.GOOS != "windows" {
		fmt.Fprintln(os.Stderr, "UVP 单机启动器仅支持 Windows 10 x64 及以上兼容系统")
		os.Exit(1)
	}
	executable, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(os.Args) > 1 && os.Args[1] == "backup" {
		os.Exit(runBackupCommand(os.Args[2:], filepath.Dir(executable), os.Stdout, os.Stderr))
	}
	root := flag.String("install-dir", filepath.Dir(executable), "安装目录")
	recordings := flag.String("recordings-dir", "", "录像目录（默认安装目录下 recordings）")
	noBrowser := flag.Bool("no-browser", false, "仅启动组件，不打开浏览器（用于自动测试）")
	stop := flag.Bool("stop", false, "停止当前安装目录的实例并等待完成")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "不支持的启动参数")
		os.Exit(1)
	}
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
	ctx, cancel := shutdownContext(context.Background())
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
	case "node_missing":
		return "未找到本机媒体节点"
	case "node_ambiguous":
		return "本机媒体节点配置重复，请检查节点管理"
	case "node_inactive":
		return "媒体节点离线或等待恢复"
	case "media_unreachable":
		return "暂时无法连接媒体服务"
	case "media_address_unavailable":
		return "已保存的媒体网卡地址不可用，请检查节点配置"
	case "identity_mismatch":
		return "媒体节点身份与本实例配置不一致"
	case "config_not_converged":
		return "正在确认媒体配置"
	case "hook_unconfirmed":
		return "等待媒体服务认证心跳"
	case "sip_not_running":
		return "SIP 服务尚未运行"
	case "ready":
		return "业务服务已就绪"
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

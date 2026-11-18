//go:build windows

// Command uvp-tray 是 Windows 绿色包的托盘启动器。
//
// 双击它 → 调用同目录的 uvp-gb28181-ctl.ps1 拉起整套服务（Redis / ZLM /
// nginx / 后端）→ 常驻系统托盘。右键托盘图标可打开管理页、查看状态、
// 重启 / 停止服务、退出。退出会先停服务再退出托盘（老板 2026-11-18 拍板）。
//
// 设计要点：
//   - 启动逻辑完全复用 ctl.ps1，不在本程序里重复实现（新增端口、HTTPS 分支
//     只改脚本一处）。本程序只负责「调脚本 + 托盘 UI」。
//   - 所有 powershell 调用都带 CREATE_NO_WINDOW，避免每次点菜单闪黑框。
//   - 单实例用命名 Mutex，防止重复双击起两个托盘、两套菜单抢着停服务。
//   - systray 在 Windows 上是纯 Win32 系统调用（见其 systray_windows.go），
//     因此本程序可以 CGO_ENABLED=0 交叉编译，不依赖目标机任何运行环境。
package main

import (
	_ "embed"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/getlantern/systray"
	"golang.org/x/sys/windows"
)

//go:embed app.ico
var appIcon []byte

// 单实例互斥体名字。加 Global\ 前缀，保证跨会话（远程桌面/不同用户）也唯一。
const mutexName = `Global\UVP-GB28181-Tray-6f2c1d9e`

var (
	rootDir string // 包根目录（本 exe 所在目录）
	ctlPS1  string // uvp-gb28181-ctl.ps1 绝对路径
	psExe   string // powershell.exe 绝对路径

	// 菜单动作串行化：start/stop/restart 都是重活，避免连点导致脚本互相打架。
	actionMu sync.Mutex
)

func main() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	rootDir = filepath.Dir(exe)
	ctlPS1 = filepath.Join(rootDir, "uvp-gb28181-ctl.ps1")
	psExe = findPowerShell()

	if _, err := os.Stat(ctlPS1); err != nil {
		msgBox("找不到控制脚本", "同目录下缺少 uvp-gb28181-ctl.ps1，请重新解压完整安装包。")
		return
	}
	if psExe == "" {
		msgBox("找不到 PowerShell", "本机缺少 Windows PowerShell，无法启动服务。")
		return
	}
	if !acquireSingleInstance() {
		msgBox("已在运行", "UVP-GB28181 托盘程序已经在运行了，请勿重复启动。")
		return
	}

	systray.Run(onReady, onExit)
}

// findPowerShell 按与 start.cmd 相同的优先级找 PowerShell：
// 先认死 System32 下的桌面版 5.1（脚本是按 5.1 的 UTF-8 BOM 行为写的），
// 找不到再退回 PATH 里的 powershell.exe，最后尝试 pwsh。
func findPowerShell() string {
	if sr := os.Getenv("SystemRoot"); sr != "" {
		c := filepath.Join(sr, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	for _, name := range []string{"powershell.exe", "pwsh.exe"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return ""
}

// acquireSingleInstance 用命名 Mutex 保证只有一个托盘实例。
// 句柄故意不 Close：进程存活期间一直持有，退出时由 OS 自动释放。
func acquireSingleInstance() bool {
	name, err := syscall.UTF16PtrFromString(mutexName)
	if err != nil {
		return true // 名字编码失败就放行，总比全部起不来强
	}
	h, err := windows.CreateMutex(nil, true, name)
	// 注意：互斥体已存在时 Windows 仍返回**有效句柄**，错误码为 ERROR_ALREADY_EXISTS。
	// 仅判断 err != nil 会放行第二个实例 —— 必须先判这个特定错误码。
	if err == windows.ERROR_ALREADY_EXISTS {
		windows.CloseHandle(h)
		return false
	}
	if err != nil {
		return true // 其它错误放行，总比全部起不来强
	}
	_ = h // 句柄持有到进程退出，保证互斥
	return true
}

func onReady() {
	systray.SetIcon(appIcon)
	systray.SetTitle("UVP-GB28181")
	systray.SetTooltip("UVP-GB28181 服务管理")

	mOpen := systray.AddMenuItem("打开管理页面", "在浏览器中打开管理页面")
	mStatus := systray.AddMenuItem("查看运行状态", "检查后端 / Redis / ZLM / 管理页是否在跑")
	systray.AddSeparator()
	mRestart := systray.AddMenuItem("重启服务", "停止后重新启动全部服务")
	mStop := systray.AddMenuItem("停止服务", "停止全部服务（托盘保留）")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("退出", "停止全部服务并退出托盘")

	// 启动时自动拉起服务（后台 goroutine，不阻塞托盘菜单注册）。
	go runAction("start", false, false)

	go func() {
		for {
			select {
			case <-mOpen.ClickedCh:
				go openManagementPage()
			case <-mStatus.ClickedCh:
				go runAction("status", true, false)
			case <-mRestart.ClickedCh:
				go runAction("restart", true, false)
			case <-mStop.ClickedCh:
				go runAction("stop", true, false)
			case <-mQuit.ClickedCh:
				// 退出 = 先停服务，再退托盘（老板拍板）。
				// wait=true：即使 start 还在跑也要等它结束、把锁让出来，
				// 否则 stop 会被 TryLock 跳过，托盘退了服务却没停。
				runAction("stop", false, true)
				systray.Quit()
				return
			}
		}
	}()
}

func onExit() {}

// runAction 串性地调用 ctl.ps1 <action>。showResult=true 时把输出用消息框
// 展示给用户（status/restart/stop 的菜单点击），false 则静默（启动/退出）。
// wait=true 时阻塞等锁（仅退出停服用），false 则用 TryLock 忽略连点。
func runAction(action string, showResult, wait bool) {
	if wait {
		actionMu.Lock()
	} else if !actionMu.TryLock() {
		return // 上一次动作还没跑完，忽略本次点击
	}
	defer actionMu.Unlock()

	stdout, stderr, code := callCtl(action)
	if showResult {
		text := strings.TrimSpace(stdout)
		if strings.TrimSpace(stderr) != "" {
			text += "\n" + strings.TrimSpace(stderr)
		}
		if text == "" {
			text = "（无输出）"
		}
		if code != 0 {
			text += "\n\n[退出码 " + strconv.Itoa(code) + "]"
		}
		msgBox("UVP-GB28181 · "+action, text)
	}
}

// callCtl 调用 ctl.ps1 的某个 action，分开收集 stdout/stderr 与退出码。
// CREATE_NO_WINDOW 让 PowerShell 完全在后台跑，不闪黑框。
func callCtl(action string) (stdout, stderr string, code int) {
	cmd := exec.Command(psExe, "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", ctlPS1, action)
	cmd.Dir = rootDir
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NO_WINDOW,
	}
	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	code = 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			code = -1
		}
	}
	return outBuf.String(), errBuf.String(), code
}

// openManagementPage 从 ctl.ps1 entry-url 拿到访问地址，交给系统默认浏览器。
// 地址逻辑留在脚本里（含 HTTPS 分支），这里只做「取字符串 + 打开」。
func openManagementPage() {
	stdout, _, code := callCtl("entry-url")
	if code != 0 {
		msgBox("无法获取访问地址", "请先启动服务，再打开管理页面。")
		return
	}
	url := extractEntryURL(stdout)
	if url == "" {
		msgBox("无法获取访问地址", "服务可能还没起来，请稍后再试或用 start.cmd 启动。")
		return
	}
	p, err := syscall.UTF16PtrFromString(url)
	if err != nil {
		return
	}
	// SW_SHOWNORMAL：交给默认浏览器；url.dll 方案对 http/https 都适用。
	_ = windows.ShellExecute(0, nil, p, nil, nil, 1)
}

// extractEntryURL 从 ctl.ps1 entry-url 的 stdout 中提取访问地址。
// stdout 可能混入 PowerShell 的 Warning 行（坏配置时 Get-Config 会写 warn），
// 因此只认带 UVP_ENTRY_URL= 前缀的那一行，取第一行。
func extractEntryURL(stdout string) string {
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "UVP_ENTRY_URL=") {
			return strings.TrimSpace(strings.TrimPrefix(line, "UVP_ENTRY_URL="))
		}
	}
	return ""
}

func msgBox(title, text string) {
	t, err1 := syscall.UTF16PtrFromString(title)
	b, err2 := syscall.UTF16PtrFromString(text)
	if err1 != nil || err2 != nil {
		return
	}
	windows.MessageBox(0, b, t, windows.MB_OK|windows.MB_ICONINFORMATION|windows.MB_SETFOREGROUND)
}

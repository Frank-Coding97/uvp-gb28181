//go:build windows

// Command uvp-tray 是 Windows 绿色包的托盘启动器。
//
// 双击它 → 在一个**独立控制台窗口**里跑同目录的 start.cmd（窗口里实时滚动
// ctl.ps1 的启动过程：阶段、勾叉、最后的访问地址）→ 托盘图标常驻。
// 启动完成后这个窗口会停在「按任意键」上，用户看完自己关掉；关窗口只结束
// 这个显示进程，托盘与服务都不受影响（老板 2026-11-25 拍板）。
//
// 菜单里的「停止服务」和「退出」同样开一个独立控制台窗口（stop.cmd），
// 逐个显示各组件有没有停干净 —— tooltip 只是补充，不能指望用户去悬浮查看
// （老板 2026-11-26：「托盘上是有悬浮提示但是用户不一定悬浮去看」）。
//
// 服务探到就绪后，自动用默认浏览器打开管理页面（老板 2026-11-26）。
//
// 设计要点：
//   - 启动逻辑完全复用 start.cmd / ctl.ps1，不在本程序里重复实现（新增端口、
//     HTTPS 分支只改脚本一处）。本程序只负责「开窗口 + 托盘 UI」。
//   - ⛔⛔ 双击后必须立刻看到反应：服务初始化要 30~60 秒，旧实现全程静默，
//     而托盘图标在 Win11 默认被收进「^」折叠区，用户看到的就是「双击没反应」，
//     于是反复双击（现场反馈）。控制台窗口是唯一能同时给出「有反应 + 全过程 +
//     出错原因」的东西，比自己画的进度条信息量大得多。
//   - 托盘只做三件轻活：起窗口、探活改 tooltip、菜单里的状态/重启/停止/退出。
//   - 单实例用命名 Mutex，防止重复双击起两套托盘抢着停服务；重复双击时
//     改成问「要不要打开管理页面」，而不是弹一句「已在运行」把人晾住。
//   - systray 在 Windows 上是纯 Win32 系统调用，本程序可 CGO_ENABLED=0 交叉编译。
package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/getlantern/systray"
	"golang.org/x/sys/windows"
)

//go:embed app.ico
var appIcon []byte

// 单实例互斥体名字。加 Global\ 前缀，保证跨会话（远程桌面/不同用户）也唯一。
const mutexName = `Global\UVP-GB28181-Tray-6f2c1d9e`

const idYes = 6 // MessageBox 的 IDYES

var (
	rootDir string // 包根目录（本 exe 所在目录）
	ctlPS1  string // uvp-gb28181-ctl.ps1 绝对路径
	psExe   string // powershell.exe 绝对路径
	logPath string // logs\uvp-tray.log：所有动作的落地记录

	// 菜单动作串行化：start/stop/restart 都是重活，避免连点导致脚本互相打架。
	actionMu sync.Mutex

	// 服务端口是否已探通。用来区分「用户看完按任意键正常关窗」和
	// 「启动还没跑完就把窗口叉掉了」——后者要留一条日志、tooltip 也别再报运行中。
	svcReady atomic.Bool
)

// startWindowTitle 是启动窗口的标题，必须和 start.cmd 里那行 title 完全一致。
// 托盘要靠它找到那个窗口（窗口是 cmd 的 start 开的，我们拿不到它的 PID）。
const startWindowTitle = "UVP-GB28181 - Startup"

// stopWindowTitle 是停止窗口的标题，必须和 stop.cmd 里那行 title 完全一致，
// 用途同 startWindowTitle（判断窗口是否还在、是不是被用户提前关掉了）。
const stopWindowTitle = "UVP-GB28181 - Stopping"

// stopMarkerEnv 是交给 stop.cmd 的环境变量名：脚本跑完（pause 之前）把
// 退出码写进这个文件。窗口是 cmd 的 start 开的，我们既拿不到它的进程号也
// 拿不到退出码，这个文件是唯一的回报通道；没有它，托盘就只能靠猜端口。
const stopMarkerEnv = "UVP_STOP_MARKER"

var (
	trayUser32                   = windows.NewLazySystemDLL("user32.dll")
	procFindWindowW              = trayUser32.NewProc("FindWindowW")
	procGetWindowThreadProcessID = trayUser32.NewProc("GetWindowThreadProcessId")
)

// 各动作的超时。停止要逐组件等退出，实测 ~20 秒；启动含建库/证书，给得宽一点。
// ⛔ 超时的意义不是「催快」，而是**保证托盘一定能退出**：以前 callCtl 没有超时，
//
//	一旦脚本卡住（等端口、等文件句柄），「退出」就永远走不到 systray.Quit()。
func actionTimeout(action string) time.Duration {
	switch action {
	case "stop":
		return 180 * time.Second
	case "start", "restart":
		return 240 * time.Second
	default:
		return 60 * time.Second
	}
}

func main() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	rootDir = filepath.Dir(exe)
	ctlPS1 = filepath.Join(rootDir, "uvp-gb28181-ctl.ps1")
	psExe = findPowerShell()
	logPath = filepath.Join(rootDir, "logs", "uvp-tray.log")
	rotateLog()
	logf("=== 托盘启动 pid=%d exe=%s ===", os.Getpid(), exe)

	if _, err := os.Stat(ctlPS1); err != nil {
		msgBox("找不到控制脚本", "同目录下缺少 uvp-gb28181-ctl.ps1，请重新解压完整安装包。")
		return
	}
	if psExe == "" {
		msgBox("找不到 PowerShell", "本机缺少 Windows PowerShell，无法启动服务。")
		return
	}
	if !acquireSingleInstance() {
		// 已经有一套在跑。双击的意图基本都是「我要用它」，而不是「再开一份」；
		// 但双击得太快（启动还没完）时页面还打不开，所以先分清这两种情况：
		//   - 服务已就绪 → 问要不要打开管理页面（托盘图标在 Win11 可能被折叠，
		//     弹一句「已在运行」就结束的话，用户连入口都找不到）；
		//   - 还在启动   → 只提示去看那个启动窗口，别让用户点开一个打不开的页面。
		logf("已有实例在运行")
		if url, ok := entryURL(); ok && portOpen(hostPort(url)) {
			if confirmBox("UVP-GB28181 已在运行",
				"UVP-GB28181 已经在运行了。\n\n是否打开管理页面？\n\n（托盘图标在屏幕右下角，可能收在「^」折叠区里）") {
				openURL(url)
			}
			return
		}
		msgBox("UVP-GB28181 正在启动",
			"托盘程序已经在运行，服务正在启动中。\n\n"+
				"启动过程就在那个控制台窗口里；启动完成后窗口会停住，按任意键关掉即可。\n"+
				"关掉窗口不会停止服务。")
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
	// tooltip 是托盘唯一能显示的「状态」，先报「启动中」，探到端口通了再改。
	systray.SetTooltip("UVP-GB28181 · 正在启动服务…")

	mOpen := systray.AddMenuItem("打开管理页面", "在浏览器中打开管理页面")
	mStatus := systray.AddMenuItem("查看运行状态", "检查后端 / Redis / ZLM / 管理页是否在跑")
	systray.AddSeparator()
	mRestart := systray.AddMenuItem("重启服务", "停止后重新启动全部服务")
	mStop := systray.AddMenuItem("停止服务", "停止全部服务（托盘保留）")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("退出", "停止全部服务并退出托盘")

	// 启动服务：开一个独立控制台窗口跑 start.cmd（不阻塞托盘菜单注册）。
	go launchStartWindow()

	go func() {
		for {
			select {
			case <-mOpen.ClickedCh:
				go openManagementPage()
			case <-mStatus.ClickedCh:
				go runAction("status", true, false)
			case <-mRestart.ClickedCh:
				// 重启前先收掉「启动过程」窗口：它显示的是上一次启动的旧内容，
				// 留着会让人以为现在这轮也跑完了。
				closeStartConsole()
				go runAction("restart", true, false)
			case <-mStop.ClickedCh:
				closeStartConsole()
				go handleStop(false)
			case <-mQuit.ClickedCh:
				// 退出 = 先停服务，再退托盘（老板拍板）。
				// ⛔⛔ 停止要十几秒（nginx → 后端 → ZLM → Redis 逐个等退出），这段时间
				//   必须给用户看得见的反馈：以前这里一点动静都没有，用户点完以为没反应，
				//   又反复去点，最后反馈成「从托盘退不掉」（222 现场）。
				//   tooltip 也算反馈，但用户不会专门去悬浮图标看（老板 2026-11-26），
				//   所以停止同样开一个控制台窗口，把每个组件的勾叉摊在台面上。
				mQuit.SetTitle("正在停止服务…")
				mQuit.Disable()
				mStop.Disable()
				mRestart.Disable()
				mStatus.Disable()
				systray.SetTooltip("正在停止 UVP-GB28181 服务，请稍候…")
				logf("用户点击退出：开始停止服务")
				closeStartConsole()
				// wait=true：即使前面还有动作在跑（比如刚点了停止）也要等锁，
				// 否则 stop 会被 TryLock 跳过，托盘退了服务却没停。
				handleStop(true)
				logf("退出流程结束，关闭托盘")
				systray.Quit()
				return
			}
		}
	}()
}

func onExit() { logf("=== 托盘退出 pid=%d ===", os.Getpid()) }

// launchStartWindow 在独立控制台窗口里跑 start.cmd。
//
// 窗口里的内容就是脚本原本那份输出（阶段 1/3 → 3/3、每个组件的勾/叉、最后框起来
// 的访问地址），信息量比托盘自造的进度提示大得多；窗口归用户所有，启动完成后
// 自己按任意键关掉即可，关窗口只结束这个显示进程，托盘与后台服务照常运行。
//
// ⛔ 不要改成把输出接回管道再弹窗：那样又回到「静默 60 秒」的老问题。
func launchStartWindow() {
	// 服务可能已经起着了（上一次 start.cmd、或另一个会话起的），这时再跑 start
	// 会在「端口预检」处 Fail 报「端口被占用」——看着像启动失败，其实一切正常。
	// 所以先探一次：已经在跑就跳过启动窗口，直接把管理页面递到用户面前。
	// （这次 entry-url 的结果顺便给后面的探活复用，省一次 PowerShell 启动。）
	rawURL, ok := entryURL()
	if ok && portOpen(hostPort(rawURL)) {
		svcReady.Store(true)
		systray.SetTooltip("UVP-GB28181 服务运行中 · 右键可管理")
		logf("服务已在运行（%s），跳过启动窗口并打开管理页面", rawURL)
		openURL(rawURL)
		return
	}

	startCmd := filepath.Join(rootDir, "start.cmd")
	if _, err := os.Stat(startCmd); err != nil {
		logf("缺少 start.cmd，启动窗口无法打开")
		msgBox("无法启动服务",
			"安装目录下缺少 start.cmd，说明包没解压完整。\n\n请重新解压整个安装包后再双击 UVP-GB28181.exe。")
		return
	}

	// ⛔⛔ 必须让 cmd 的 start 去开这个窗口，不能自己 exec cmd /c start.cmd：
	//   os/exec 在 Stdout/Stderr 为 nil 时会把子进程输出接到 **NUL**
	//   （见 GOROOT/src/os/exec/exec.go 的 writerDescriptor 注释），
	//   子进程虽然拿到了新控制台，日志却全进黑洞 —— 窗口一片空白，
	//   等于把「双击没反应」换成了「双击弹个空窗口」。
	//   start 由 cmd 自己创建控制台并接好标准句柄，行为和资源管理器里双击
	//   start.cmd 完全一致（连颜色、编码都对）。
	cmd := exec.Command("cmd.exe", "/c", "start", "", startCmd)
	cmd.Dir = rootDir
	// 外层 cmd 自己不要弹窗（它只负责把 start 转出去）。
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
	if err := cmd.Run(); err != nil {
		logf("打开启动窗口失败: %v", err)
		msgBox("无法启动服务",
			"没能打开启动窗口："+err.Error()+
				"\n\n请直接双击安装目录下的 start.cmd 手动启动。")
		return
	}
	logf("已请求打开启动窗口（标题 %q）", startWindowTitle)

	go watchStartup(rawURL)
}

// windowAlive 判断标题为 title 的控制台窗口是否还开着。
// 找不到窗口返回 false（用户关掉了，或者压根没起来）。
func windowAlive(title string) bool {
	t, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return false
	}
	hwnd, _, _ := procFindWindowW.Call(0, uintptr(unsafe.Pointer(t)))
	return hwnd != 0
}

// startWindowAlive 判断启动窗口是否还开着。
func startWindowAlive() bool { return windowAlive(startWindowTitle) }

// closeStartConsole 关掉那个「启动过程」窗口（含脚本子进程）。
//
// 为什么按标题找而不记 PID：窗口是 cmd 的 start 命令开的，我们只拿得到
// 「请求已发出」，拿不到它的进程 ID。标题由 start.cmd 里的 title 定死，
// 唯一性够用；找不到就什么也不做（窗口没了本来就不用关）。
func closeStartConsole() {
	title, err := syscall.UTF16PtrFromString(startWindowTitle)
	if err != nil {
		return
	}
	hwnd, _, _ := procFindWindowW.Call(0, uintptr(unsafe.Pointer(title)))
	if hwnd == 0 {
		return
	}
	var pid uint32
	procGetWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid == 0 {
		return
	}
	killer := exec.Command("taskkill.exe", "/PID", strconv.FormatUint(uint64(pid), 10), "/T", "/F")
	killer.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
	out, _ := killer.CombinedOutput()
	logf("关闭启动窗口 pid=%d: %s", pid, strings.TrimSpace(string(out)))
}

// handleStop 是「停止服务」与「退出」共用的停止路径：开一个可见的控制台窗口
// 跑 stop.cmd，等它把结果回报过来，再决定 tooltip / 是否退出托盘。
//
// ⛔⛔ 不要再退回「托盘静默跑 ctl.ps1 stop + 消息框」那套：停止要十几秒，
// 期间用户看不到任何东西（tooltip 得把鼠标悬浮到右下角才能看见，用户不会去看），
// 表现就是「点了没反应、退不掉」（222 现场）。窗口里滚的是每个组件的勾叉，
// 失败时还会红字说清是哪个进程没停掉。
func handleStop(quitTray bool) {
	res := runStopInWindow(quitTray)
	if quitTray {
		// 退不退出由调用方决定（它还要收尾日志），这里只把结果记下来。
		logf("停止流程结束（成功=%v），准备关闭托盘", res.ok())
		return
	}
	if res.skipped {
		// 已经有一个停止/重启在跑，窗口本来就在那儿，连点直接忽略。
		return
	}
	if res.ok() {
		systray.SetTooltip("UVP-GB28181 服务已停止 · 右键可管理")
		logf("服务已停止，托盘保留")
	} else {
		systray.SetTooltip("UVP-GB28181 · 停止未成功，请看停止窗口的输出")
	}
}

// runStopInWindow 开停止窗口并等它回报退出码。
//
// wait=true 时阻塞拿锁（退出场景：必须真的停完）；false 时 TryLock，连点直接忽略。
func runStopInWindow(wait bool) actionResult {
	res := actionResult{action: "stop"}
	if wait {
		actionMu.Lock()
	} else if !actionMu.TryLock() {
		logf("stop 被忽略：上一个动作还在执行")
		res.skipped = true
		return res
	}
	defer actionMu.Unlock()

	// 回报文件放在 logs 下：那是托盘和脚本都确知可写的地方，且随包走、不污染 %TEMP%。
	marker := filepath.Join(rootDir, "logs", "uvp-stop-"+strconv.Itoa(os.Getpid())+".result")
	start := time.Now()
	if err := spawnStopWindow(marker); err != nil {
		logf("打开停止窗口失败: %v", err)
		msgBox("无法停止服务",
			"没能打开停止窗口："+err.Error()+
				"\n\n请直接双击安装目录下的 stop.cmd 手动停止。")
		res.code = -1
		res.elapsed = time.Since(start)
		return res
	}
	logf("已请求打开停止窗口（标题 %q）", stopWindowTitle)

	deadline := start.Add(actionTimeout("stop"))
	gone := 0
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(marker); err == nil {
			_ = os.Remove(marker)
			res.code, _ = strconv.Atoi(strings.TrimSpace(string(b)))
			res.elapsed = time.Since(start)
			logf("stop 结束（窗口回报）耗时=%s 退出码=%d", res.elapsed.Round(time.Millisecond), res.code)
			return res
		}
		// 窗口没了又没回报：用户把停止窗口叉掉了（脚本连带被杀，停到一半）。
		// 前 3 秒不算，那时窗口可能还没建出来；发现消失后多给 1.2 秒，
		// 避开「刚写完回报文件、窗口正好被关」的竞态。
		if windowAlive(stopWindowTitle) {
			gone = 0
		} else if time.Since(start) > 3*time.Second {
			gone++
			if gone >= 4 {
				// ⛔ 别就这么走人：叉窗把 ctl.ps1 一起杀了，服务很可能停到一半，
				//   托盘再退出就成了「图标没了、进程还在」。用户已经表示不想看窗口，
				//   这里静默补跑一次（不弹第二个窗），把停止做完为止。
				logf("停止窗口已被关闭，改静默补跑一次 stop")
				out, errOut, code, el, to := callCtl("stop")
				res.stdout, res.stderr = out, errOut
				res.code, res.timedOut, res.elapsed = code, to, el
				if s := tailText(strings.TrimSpace(out+"\n"+errOut), 300); s != "" {
					logf("兜底 stop 输出: %s", s)
				}
				logf("兜底 stop 结束 耗时=%s 退出码=%d 超时=%v", el.Round(time.Millisecond), code, to)
				return res
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	res.code = -1
	res.timedOut = true
	res.elapsed = time.Since(start)
	logf("等待停止窗口回报超时（%s）", actionTimeout("stop"))
	return res
}

// spawnStopWindow 用 cmd 的 start 开一个独立控制台窗口跑 stop.cmd。
//
// ⛔ 和启动窗口同一个道理：不能自己 exec cmd /c stop.cmd —— os/exec 在
// Stdout/Stderr 为 nil 时把子进程输出接到 NUL，窗口会是一片空白。
func spawnStopWindow(marker string) error {
	stopCmd := filepath.Join(rootDir, "stop.cmd")
	if _, err := os.Stat(stopCmd); err != nil {
		return errors.New("安装目录下缺少 stop.cmd")
	}
	if err := os.MkdirAll(filepath.Dir(marker), 0o755); err != nil {
		return err
	}
	_ = os.Remove(marker) // 清掉上一轮的旧回报，否则会误判成立刻就停完了

	cmd := exec.Command("cmd.exe", "/c", "start", "", stopCmd)
	cmd.Dir = rootDir
	cmd.Env = append(os.Environ(), stopMarkerEnv+"="+marker)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
	return cmd.Run()
}

// watchStartup 等服务真的起来，把 tooltip 从「正在启动」改成「运行中」，
// 并**自动用默认浏览器打开管理页面**（老板 2026-11-26：双击启动的人十有八九
// 就是要用页面，别让人再去右下角托盘里翻菜单）。
// addr 来自调用方已经取到的 entry-url（HTTPS/HTTP 分支由 ctl.ps1 决定），
// 之后只做 TCP 探活 —— 反复起 PowerShell 每次要 1 秒以上，轮询几十次太贵。
func watchStartup(rawURL string) {
	started := time.Now()
	if rawURL == "" {
		logf("拿不到访问地址，跳过启动探活（tooltip 保持「正在启动」）")
		return
	}
	addr := hostPort(rawURL)
	if addr == "" {
		logf("访问地址解析不出 host:port：%s", rawURL)
		return
	}

	deadline := time.Now().Add(3 * time.Minute)
	sawWindow := false
	noted := false
	for time.Now().Before(deadline) {
		if portOpen(addr) {
			svcReady.Store(true)
			systray.SetTooltip("UVP-GB28181 服务运行中 · 右键可管理")
			elapsed := time.Since(started).Round(time.Second)
			// ⛔ 只在这里自动开一次：本函数探到就绪就 return，不会反复弹页面；
			//   「重启服务」走的是另一条路（runAction），不会把浏览器再拉一次。
			if openURL(rawURL) {
				logf("服务已就绪 %s（耗时 %s），已自动打开管理页面", addr, elapsed)
			} else {
				logf("服务已就绪 %s（耗时 %s），但没能唤起浏览器，请手动访问 %s", addr, elapsed, rawURL)
			}
			return
		}
		if startWindowAlive() {
			sawWindow = true
		} else if !noted && time.Since(started) > 12*time.Second {
			// 只记一次、只当线索，不当结论：窗口可能被按掉了，也可能只是起得慢。
			// 继续探活到超时为止——万一服务其实起来了，tooltip 不该冤枉它。
			noted = true
			if sawWindow {
				logf("启动窗口已关闭，但端口 %s 还没通，继续观察", addr)
			} else {
				logf("12 秒内没见到启动窗口，端口 %s 也没通，继续观察", addr)
			}
		}
		time.Sleep(1200 * time.Millisecond)
	}
	systray.SetTooltip("UVP-GB28181 · 未检测到服务就绪，请看启动窗口的输出")
	logf("等待服务就绪超时（3 分钟）：%s", addr)
}

// portOpen 探一次 TCP 是否可连（不发起任何应用层请求）。
func portOpen(addr string) bool {
	if addr == "" {
		return false
	}
	conn, err := net.DialTimeout("tcp", addr, 800*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// hostPort 从 http(s)://host:port 里取出 host:port 供 TCP 探活用。
func hostPort(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	if u.Port() != "" {
		return u.Host
	}
	if u.Scheme == "https" {
		return net.JoinHostPort(u.Hostname(), "443")
	}
	return net.JoinHostPort(u.Hostname(), "80")
}

// actionResult 是一次 ctl.ps1 调用的结果，让调用方自己决定怎么反馈给用户。
type actionResult struct {
	action   string
	stdout   string
	stderr   string
	code     int
	elapsed  time.Duration
	timedOut bool
	skipped  bool // 上一个动作还没跑完，本次点击被忽略
}

// ok 表示动作真的跑成功了。skipped（被忽略）绝不能算成功。
func (r actionResult) ok() bool { return !r.skipped && !r.timedOut && r.code == 0 }

// doAction 串行执行 ctl.ps1 <action> 并落日志。
// wait=true 时阻塞等锁（仅退出停服用），false 则用 TryLock 忽略连点。
func doAction(action string, wait bool) actionResult {
	res := actionResult{action: action}
	if wait {
		actionMu.Lock()
	} else if !actionMu.TryLock() {
		logf("%s 被忽略：上一个动作还在执行", action)
		res.skipped = true
		return res
	}
	defer actionMu.Unlock()

	logf("%s 开始", action)
	res.stdout, res.stderr, res.code, res.elapsed, res.timedOut = callCtl(action)
	logf("%s 结束 耗时=%s 退出码=%d 超时=%v", action, res.elapsed.Round(time.Millisecond), res.code, res.timedOut)
	if out := tailText(strings.TrimSpace(res.stdout+"\n"+res.stderr), 400); out != "" {
		logf("%s 输出: %s", action, out)
	}
	return res
}

// runAction 在 doAction 之上加「消息框反馈」，供托盘菜单点击使用。
func runAction(action string, showResult, wait bool) actionResult {
	res := doAction(action, wait)
	if showResult {
		msgBox("UVP-GB28181 · "+action, res.summary())
	}
	return res
}

// summary 把结果整理成给消息框看的一段文字。
func (r actionResult) summary() string {
	if r.skipped {
		return "上一个操作还在执行中，请稍后再试。"
	}
	text := strings.TrimSpace(r.stdout)
	if s := strings.TrimSpace(r.stderr); s != "" {
		if text != "" {
			text += "\n"
		}
		text += s
	}
	if text == "" {
		text = "（无输出）"
	}
	if r.timedOut {
		text += "\n\n[执行超时，已中断；详见 logs\\uvp-tray.log]"
	}
	if r.code != 0 {
		text += "\n\n[退出码 " + strconv.Itoa(r.code) + "]"
	}
	return text
}

// callCtl 调用 ctl.ps1 的某个 action，分开收集 stdout/stderr 与退出码。
// CREATE_NO_WINDOW 让 PowerShell 完全在后台跑，不闪黑框（这是菜单里那些
// 快速动作的调用方式；首次启动走的是 launchStartWindow 的可见窗口）。
// ⛔ 必须带超时（CommandContext）：没有超时的话，脚本一旦卡住，「退出」就永远
//
//	走不到 systray.Quit()，表现就是托盘图标怎么点都不消失。
func callCtl(action string) (stdout, stderr string, code int, elapsed time.Duration, timedOut bool) {
	ctx, cancel := context.WithTimeout(context.Background(), actionTimeout(action))
	defer cancel()

	cmd := exec.CommandContext(ctx, psExe, "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", ctlPS1, action)
	cmd.Dir = rootDir
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NO_WINDOW,
	}
	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	start := time.Now()
	err := cmd.Run()
	elapsed = time.Since(start)
	timedOut = errors.Is(ctx.Err(), context.DeadlineExceeded)
	code = 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			code = -1
		}
	}
	if timedOut {
		// 被 CommandContext 杀掉时退出码没有意义，统一记 -1，日志里另有「超时=true」。
		code = -1
	}
	return outBuf.String(), errBuf.String(), code, elapsed, timedOut
}

// logf 往 logs\uvp-tray.log 追加一行。
// 托盘动作以前只活在内存和消息框里，客户说「点了没反应」时无从查证；落盘之后至少能
// 看到动作有没有发起、跑了多久、退出码多少。写失败一律静默——日志不该变成新的故障点。
func logf(format string, args ...any) {
	if logPath == "" {
		return
	}
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	line := fmt.Sprintf(format, args...)
	line = strings.ReplaceAll(line, "\r\n", " | ")
	line = strings.ReplaceAll(line, "\n", " | ")
	_, _ = fmt.Fprintf(f, "%s %s\n", time.Now().Format("2006-01-02 15:04:05"), line)
}

// rotateLog 只在启动时做一次：超过 2MiB 就把旧文件改名成 .1（覆盖更早的 .1），
// 避免长年运行把日志养成几百兆。
func rotateLog() {
	info, err := os.Stat(logPath)
	if err != nil || info.Size() < 2<<20 {
		return
	}
	_ = os.Remove(logPath + ".1")
	_ = os.Rename(logPath, logPath+".1")
}

// tailText 取最后 max 个字符（按 rune 截，避免把中文切成半个字）。
func tailText(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return "…" + string(r[len(r)-max:])
}

// openManagementPage 从 ctl.ps1 entry-url 拿到访问地址，交给系统默认浏览器。
// 地址逻辑留在脚本里（含 HTTPS 分支），这里只做「取字符串 + 打开」。
func openManagementPage() {
	url, ok := entryURL()
	if !ok {
		msgBox("无法获取访问地址", "没能从配置里取到访问地址；详见 logs\\uvp-tray.log。")
		return
	}
	if !openURL(url) {
		msgBox("无法打开浏览器", "请手动在浏览器中访问：\n"+url)
	}
}

// entryURL 取管理页访问地址（端口/协议/Host 都由 ctl.ps1 决定）。
func entryURL() (string, bool) {
	stdout, _, code, _, _ := callCtl("entry-url")
	if code != 0 {
		return "", false
	}
	url := extractEntryURL(stdout)
	return url, url != ""
}

// openURL 交给系统默认浏览器（url.dll 方案对 http/https 都适用）。
func openURL(url string) bool {
	p, err := syscall.UTF16PtrFromString(url)
	if err != nil {
		return false
	}
	return windows.ShellExecute(0, nil, p, nil, nil, 1) == nil
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

// confirmBox 是「是/否」消息框，返回用户是否点了「是」。
func confirmBox(title, text string) bool {
	t, err1 := syscall.UTF16PtrFromString(title)
	b, err2 := syscall.UTF16PtrFromString(text)
	if err1 != nil || err2 != nil {
		return false
	}
	r, _ := windows.MessageBox(0, b, t,
		windows.MB_YESNO|windows.MB_ICONQUESTION|windows.MB_SETFOREGROUND)
	return r == idYes
}

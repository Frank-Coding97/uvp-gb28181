#Requires -Version 5.1
<#
  UVP-GB28181 统一视频接入平台 · Windows 绿色安装包 —— 控制脚本

  用法（包目录下执行）：
      .\uvp-gb28181-ctl.ps1 start      启动全部组件
      .\uvp-gb28181-ctl.ps1 stop       停止全部组件（零残留）
      .\uvp-gb28181-ctl.ps1 restart    重启
      .\uvp-gb28181-ctl.ps1 status     查看运行状态
      .\uvp-gb28181-ctl.ps1 reset-password   忘记了登录口令时，把管理员口令还原成出厂口令
  也可以直接双击同目录的 start.cmd / stop.cmd / status.cmd / reset-password.cmd。

  ⛔⛔ 本文件必须保存为 **UTF-8 with BOM**。
     Windows PowerShell 5.1 读 .ps1 时若没有 BOM，会按系统 ANSI 代码页（简中=GBK）
     解码 —— 中文注释与字符串会变成乱码，轻则输出花屏、重则**语法错误**（引号被吃掉）。
     出包脚本 build-standalone.ps1 里有硬校验，缺 BOM 直接拒绝打包。

  ⛔ 输出编码：启动时把 [Console]::OutputEncoding 设成 UTF-8，否则子进程（Go 写的
     uvp-setup.exe、后端）输出的中文会被 PowerShell 按 GBK 解码成乱码。

  与 Linux 版（deploy/standalone/uvp-gb28181-ctl.sh）的对应关系：
     - 端口规划、启动/停止顺序、目录布局、幂等语义**完全一致**；
     - 差异只在实现手段：bash→PowerShell、openssl→uvp-setup certgen、
       内嵌 Python→uvp-setup（编译好的 exe，客户机不需要 Python）。
#>
[CmdletBinding()]
param(
    [Parameter(Position = 0)]
    [string]$Action = 'start'
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
try { [Console]::OutputEncoding = New-Object System.Text.UTF8Encoding $false } catch { }
$OutputEncoding = [Console]::OutputEncoding

$Root = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location -LiteralPath $Root

# ---------------------------------------------------------------- 目录布局 ----
$BinDir      = Join-Path $Root 'bin'
$VendorDir   = Join-Path $Root 'vendor'
$DataDir     = Join-Path $Root 'data'
$LogsDir     = Join-Path $Root 'logs'
$RunDir      = Join-Path $Root 'run'
$ConfDir     = Join-Path $Root 'config'
$ConfFile    = Join-Path $ConfDir 'config.yml'
$EnvFile     = Join-Path $ConfDir 'config.env'

$ServerExe   = Join-Path $BinDir 'uvp-server.exe'
$SetupExe    = Join-Path $BinDir 'uvp-setup.exe'
$RedisDir    = Join-Path $VendorDir 'redis'
$RedisExe    = Join-Path $RedisDir 'redis-server.exe'
$RedisCli    = Join-Path $RedisDir 'redis-cli.exe'
$ZlmDir      = Join-Path $VendorDir 'zlm'
$ZlmExe      = Join-Path $ZlmDir 'MediaServer.exe'
$ZlmIni      = Join-Path $ZlmDir 'config.ini'
$NginxDir    = Join-Path $VendorDir 'nginx'
$NginxExe    = Join-Path $NginxDir 'nginx.exe'
$NginxConf   = Join-Path $NginxDir 'conf\nginx.conf'
$NginxTpl    = Join-Path $NginxDir 'conf\nginx.conf.template'
$NginxCrt    = Join-Path $NginxDir 'conf\uvp.crt'
$NginxKey    = Join-Path $NginxDir 'conf\uvp.key'
# ⛔ ZLM 的 SSL 证书刻意放在**它自己目录下**，启动时只传裸文件名：
#   MediaServer.exe 用窄字符路径开文件，包里带了中文/空格的安装目录会出问题；
#   裸文件名 + 工作目录是零路径编码风险的做法。
$ZlmSslPem   = Join-Path $ZlmDir 'uvp-ssl.pem'
$RedisConf   = Join-Path $DataDir 'redis\redis.conf'
$Baseline    = Join-Path $Root 'resource\baseline\baseline.sql'
$DbPath      = Join-Path $DataDir 'uvp.db'
$PublicDir   = Join-Path $Root 'resource\public'

$BackendPidFile = Join-Path $RunDir 'backend.pid'
$RedisPidFile   = Join-Path $RunDir 'redis.pid'
$ZlmPidFile     = Join-Path $RunDir 'zlm.pid'
$NginxPidFile   = Join-Path $RunDir 'nginx.pid'

# ---------------------------------------------------------------- 输出样式 ----
# 三级缩进：`[uvp-gb28181] 阶段行` / `（14 格）✓ 结果行` / `（14 格）· 明细行`
# ⛔ IND 必须等于标签宽度「[uvp-gb28181] 」= 14 格，三个平台（Linux/Windows/uvp-setup）共用这个宽度。
$script:IND = '              '
$script:UseColor = (-not [Console]::IsOutputRedirected) -and (-not $env:NO_COLOR)
$C = @{ rst = ''; tag = ''; ok = ''; warn = ''; err = ''; dim = ''; hl = '' }
if ($script:UseColor) {
    $C.rst  = [char]27 + '[0m'
    $C.tag  = [char]27 + '[36m'
    $C.ok   = [char]27 + '[32m'
    $C.warn = [char]27 + '[33m'
    $C.err  = [char]27 + '[31m'
    $C.dim  = [char]27 + '[90m'
    $C.hl   = [char]27 + '[1;32m'   # 收尾的访问地址：加粗亮绿（整个安装过程只此一处）
}

function Write-UvpLog  { param([string]$Message) Write-Host ("{0}[uvp-gb28181]{1} {2}" -f $C.tag, $C.rst, $Message) }
function Write-UvpDet  { param([string]$Message) Write-Host ("{0}{1}" -f $script:IND, $Message) }
function Write-UvpOk   { param([string]$Message) Write-Host ("{0}{1}✓{2} {3}" -f $script:IND, $C.ok, $C.rst, $Message) }
function Write-UvpWarn { param([string]$Message) Write-Host ("{0}{1}!{2} {3}" -f $script:IND, $C.warn, $C.rst, $Message) }
function Write-UvpErr  { param([string]$Message) Write-Host ("{0}{1}✗{2} {3}" -f $script:IND, $C.err, $C.rst, $Message) }

function Fail {
    param([string]$Message)
    Write-Host ("{0}[uvp-gb28181][ERROR]{1} {2}" -f $C.err, $C.rst, $Message)
    exit 1
}

# 写 UTF-8 **不带 BOM**。⛔ PowerShell 5.1 的 `Set-Content -Encoding UTF8` 会**加 BOM**，
# 而 BOM 一旦落进 redis.conf 这种逐行解析的配置文件里，第一行就变成 `\ufeff#...`
# —— 有些解析器认，有些直接报错；干脆统一写成无 BOM。
function Write-Utf8NoBom {
    param([string]$Path, [string]$Text)
    [System.IO.File]::WriteAllText($Path, $Text, (New-Object System.Text.UTF8Encoding $false))
}

# 跑一个原生命令，返回合并后的输出（stdout+stderr），**且绝不因它写 stderr 而中断脚本**。
#
# ⛔⛔ 为什么不能简单地写 `& cmd *> $null` 或 `& cmd 2>&1 | Out-Null`：
#   PS 5.1 里原生命令的 stderr **只要被重定向**就会被包装成 ErrorRecord，而本脚本开头是
#   `$ErrorActionPreference = 'Stop'` ⇒ 它被升格成**终止性异常**，脚本当场中断。
#   `2>&1 | Out-Null` 也救不了 —— 合并后的 ErrorRecord 照样归 ErrorActionPreference 管
#   （这一点实测确认过，别以为加了 `2>&1` 就没事）。
#   对照：**不加任何重定向**的原生命令，stderr 直通控制台、不生成 ErrorRecord，
#   所以下面 uvp-setup 那几处调用能正常走到 `$LASTEXITCODE` 判断。
#   实测代价：stop 一上来就死在 `nginx -s quit` 上，连"管理页面已停止"都打不出来，
#   退出码却是 1，看起来像"停止功能坏了"。⇒ 唯一可靠的做法是**临时降级**再执行。
function Invoke-NativeText {
    param([string]$Exe, [string[]]$Arguments = @())
    $prev = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        return (& $Exe @Arguments 2>&1 | Out-String)
    } finally {
        $ErrorActionPreference = $prev
    }
}

# 同上，但**丢弃输出**。停止路径上的 taskkill / nginx -s quit / redis shutdown 用它。
function Invoke-NativeQuiet {
    param([string]$Exe, [string[]]$Arguments = @())
    $null = Invoke-NativeText -Exe $Exe -Arguments $Arguments
}

# 给原生命令行参数加引号。
#
# ⛔⛔ `Start-Process -ArgumentList` **不会**自动给含空格的参数加引号（它只是把数组
#   用空格拼起来），所以路径里的空格会把参数切碎 —— 必须自己加。
# ⛔⛔ 但**绝不能**对一个以反斜杠结尾的值加引号：Windows 命令行里 `"F:\a\"` 的
#   `\"` 会被读成**转义引号**，参数直接跑偏（本项目在 `nginx -s quit` 上踩过）。
#   这里的做法是把结尾的 `\` 去掉再加引号 —— nginx 的 `-p` 自己会补分隔符。
function Quote-NativeArg {
    param([string]$Value)
    return '"' + $Value.TrimEnd('\') + '"'
}

# ---------------------------------------------------------------- 端口规划 ----
# ⛔⛔ Windows 版用 **15500-15564**，与 Linux 版（51000-51064，见
#   deploy/standalone/PORTS.md）**刻意不一致** —— 这不是笔误，是被 Windows 逼出来的：
#
#   Windows 有一段「动态端口范围」（默认为 49152-65535，本机实测被改成了
#   50000-55000）。落在这段里的端口会被系统**预留**，任何程序去 bind 都直接返回
#   WSAEACCES(拒绝访问) —— 而且是**静默**的：
#     · ZLM 的 TCP 端口绑得上，一到 RTP 的 UDP 端口就 `bind sock6 permission denied`
#       ⇒ 进程起来 1 秒后自杀；
#     · SIP 的 UDP 端口同样绑不上 ⇒ 客户设备永远注册不上，而平台侧看不出任何错。
#   判断方法（管理员 CMD）：netsh int ipv4 show excludedportrange protocol=udp
#   ⇒ 打印出来的保留段里若含 50000-51064，那一整段就全废了。
#   ⚠️ 只有**不带 `*` 号**的那些段才真的挡住 bind；带 `*` 的（Administered）只影响
#     系统临时端口分配，手工 bind 照样能成——实测确认过，别拿它当"已经安全"的证据。
#
#   ⛔ 为什么是 15500 而不是 15000（本文件第一版选的就是 15000）：
#     **深信服终端软件**（SangforPromoteService.exe，政企内网机很常见）常驻监听
#     `127.0.0.1:15000` ⇒ 绿色包默认端口正好撞上它，装在那种机器上**开箱即挂**。
#     ⇒ 整段再上移 500，避开这一带。⚠️ 端口预检能拦住（会明确报"TCP 15500 被谁占"），
#       但"开箱即用"不该依赖用户先改配置。
#
#   ⇒ Windows 版整体挪到 **15500-15564**（在动态段之外，且避开常见软件占用）。
#     代价：与 Linux 版端口不同，**跨平台迁移要改 config\config.env**，
#     README 里有对照表。
#
# 结构仍是连续无空洞的 65 个端口，一条防火墙规则即可覆盖：
# ⛔⛔ 2026-10-19 端口重排（老板定）：**明文 15500 / HTTPS 15501**。
#   上一版正好相反（HTTPS 15500 / HTTP 15501）。这两个默认值必须**成对**改，
#   只改一个就等于让两个 listen 抢同一端口；且 Windows 与 Linux 的语义要一致
#   （Linux 是 51000 明文 / 51001 HTTPS），否则同一份文档两种说法。
$PortDefault = @{
    HTTP           = 15502   # 后端 HTTP
    REDIS          = 15503   # Redis（仅回环）
    HTTPS          = 15501   # nginx HTTPS（浏览器入口，默认关闭）
    NGINX_HTTP     = 15500   # nginx HTTP（默认入口；开 HTTPS 后跳转 + 明文 API）
    ZLM_HTTP       = 15504
    ZLM_SSL        = 15507
    ZLM_RTSP       = 15505
    ZLM_RTMP       = 15506
    ZLM_RTC        = 15508
    ZLM_SIGNALING  = 15509
    ZLM_SIGNALING_SSL = 15510
    ZLM_SRT        = 15511
    ZLM_ONVIF      = 15512
    ZLM_ICE        = 15513
    ZLM_RTP_PROXY  = 15514
}
$ZlmRtpRange = '15514-15563'
$SipPort     = 15564

# 用户可改的只有这 4 个（与 Linux 版一致）：其余由这几个派生。
$PortEnvKeys = @{
    HTTP       = 'UVP_HTTP_PORT'
    REDIS      = 'UVP_REDIS_PORT'
    HTTPS      = 'UVP_HTTPS_PORT'
    NGINX_HTTP = 'UVP_NGINX_HTTP_PORT'
}

function Read-EnvFileMap {
    param([string]$Path)
    $map = @{}
    if (-not (Test-Path -LiteralPath $Path)) { return $map }
    foreach ($line in (Get-Content -LiteralPath $Path -Encoding UTF8)) {
        $trimmed = $line.Trim()
        if ($trimmed -eq '' -or $trimmed.StartsWith('#')) { continue }
        $idx = $trimmed.IndexOf('=')
        if ($idx -lt 1) { continue }
        $map[$trimmed.Substring(0, $idx)] = $trimmed.Substring($idx + 1)
    }
    return $map
}

# Resolve-Port：环境变量 > config.env（**只认带 UVP_USER_SET_ 标记的**）> 默认值。
# ⛔ 为什么必须看标记：老包写进去的旧默认值不能当成「用户设的」，
#   否则升级包以后客户会莫名停在旧端口规划上（Linux 版为此踩过两次）。
function Resolve-Port {
    param([string]$Name, [hashtable]$EnvMap, [ref]$UserSetKeys)
    $envKey = $PortEnvKeys[$Name]
    $fromEnv = [Environment]::GetEnvironmentVariable($envKey)
    if ($fromEnv) {
        $UserSetKeys.Value += $envKey
        return [int]$fromEnv
    }
    if ($EnvMap.ContainsKey('UVP_USER_SET_' + $envKey) -and $EnvMap.ContainsKey($envKey)) {
        $UserSetKeys.Value += $envKey
        return [int]$EnvMap[$envKey]
    }
    return [int]$PortDefault[$Name]
}

function Get-Config {
    $envMap = Read-EnvFileMap -Path $EnvFile
    $userSet = @()
    $ref = [ref]$userSet
    $ports = @{
        HTTP          = Resolve-Port -Name 'HTTP'       -EnvMap $envMap -UserSetKeys $ref
        REDIS         = Resolve-Port -Name 'REDIS'      -EnvMap $envMap -UserSetKeys $ref
        HTTPS         = Resolve-Port -Name 'HTTPS'      -EnvMap $envMap -UserSetKeys $ref
        NGINX_HTTP    = Resolve-Port -Name 'NGINX_HTTP' -EnvMap $envMap -UserSetKeys $ref
    }
    foreach ($key in @('ZLM_HTTP','ZLM_SSL','ZLM_RTSP','ZLM_RTMP','ZLM_RTC','ZLM_SIGNALING',
                       'ZLM_SIGNALING_SSL','ZLM_SRT','ZLM_ONVIF','ZLM_ICE','ZLM_RTP_PROXY')) {
        $envName = 'UVP_' + $key
        $value = [Environment]::GetEnvironmentVariable($envName)
        if ($value) { $ports[$key] = [int]$value } else { $ports[$key] = [int]$PortDefault[$key] }
    }
    # ⛔ 必须 `@(...)` 包一层：`Resolve-Port` 一次都没命中时 `$ref.Value` 可能仍是
    #   空数组（也可能被 `+=` 折成单个字符串），包一层后 `.Count` 才可靠。
    # ⛔ 默认不开 HTTPS：新装的管理页走明文 HTTP，由管理员在「系统配置 → HTTPS」
    #   里自行开启。后端 uvp-setup https-render 首次启动会把这份默认落盘到
    #   data\https\settings.json，之后的启停与端口规划都以落盘值为准。
    #   （存量实例已落盘的不受影响；不要为了"默认开"改回 $true。）
    $script:HttpsEnabled = $false
    $httpsState = Join-Path $DataDir 'https\settings.json'
    if (Test-Path -LiteralPath $httpsState) {
        $savedHttps = Get-Content -LiteralPath $httpsState -Raw -Encoding UTF8 | ConvertFrom-Json
        if ($savedHttps.enabled -isnot [bool] -or $savedHttps.port -lt 1 -or $savedHttps.port -gt 65535) { Fail 'HTTPS 配置文件无效' }
        $script:HttpsEnabled = $savedHttps.enabled
        $ports.HTTPS = [int]$savedHttps.port

        # ⛔⛔ 端口重排（2026-10-19：明文 15500 / HTTPS 15501）后的存量迁移：
        #   上一版把 HTTPS 落盘成 15500，而 15500 现在是**明文端口** ⇒
        #   两个 listen 抢同一端口，nginx 直接起不来（或 https-render 报
        #   "HTTPS 端口不能与 HTTP 端口相同"），客户完全看不出是升级导致的。
        #   ⇒ 只在**撞车**时迁移；手工设过的 8443 之类一律不碰。
        if ($ports.HTTPS -eq $ports.NGINX_HTTP) {
            Write-UvpLog "HTTPS 端口 $($ports.HTTPS) 与本包明文端口相同（旧版遗留），自动迁移到 $($PortDefault.HTTPS)"
            & $SetupExe https-repair-port --root $Root `
                --conflict ([string]$ports.NGINX_HTTP) --fallback ([string]$PortDefault.HTTPS)
            if ($LASTEXITCODE -ne 0) { Fail "HTTPS 端口迁移失败（$httpsState）—— 两个监听不能同端口" }
            $ports.HTTPS = [int]$PortDefault.HTTPS
        }
    }
    return @{ Ports = $ports; EnvMap = $envMap; UserSet = @($ref.Value) }
}

# 砍掉 YAML 标量里**引号外**的行尾注释，再剥掉包裹的引号。
#   `"123456"  # 登录页默认密码`  →  `123456`
# ⛔ 两步的顺序不能反：口令里带 `#` 是合法的（客户完全可能设成 `P#ssw0rd`），
#   先剥引号就会把它当成注释起点、把口令截断。
function ConvertFrom-YamlScalar {
    param([string]$Raw)
    $value = $Raw
    $cut = $value.Length
    $quote = ''
    for ($i = 0; $i -lt $value.Length; $i++) {
        $ch = $value[$i]
        if ($quote -ne '') {
            if ($ch -eq $quote) { $quote = '' }
        } elseif ($ch -eq '"' -or $ch -eq "'") {
            $quote = $ch
        } elseif ($ch -eq '#') {
            $cut = $i
            break
        }
    }
    $value = $value.Substring(0, $cut).Trim()
    if ($value.Length -ge 2) {
        $first = $value.Substring(0, 1)
        $last = $value.Substring($value.Length - 1, 1)
        if (($first -eq '"' -and $last -eq '"') -or ($first -eq "'" -and $last -eq "'")) {
            $value = $value.Substring(1, $value.Length - 2)
        }
    }
    return $value
}

# 读 config.yml 里 `server.demoaccount` 的出厂账号与口令 —— **只用于给用户提示**，
# 不参与任何业务逻辑。
#
# ⛔⛔ 为什么要读配置、而不是继续写死（2026-10-15 修）：本文件的启动横幅原先印的是
#   `admin / admin123`，而 2026-10-08 之后出厂口令已改成由后端读
#   config.yml 的 `server.demoaccount.defaultpassword`（当前是 `123456`；seed 里 admin 的
#   bcrypt 哈希就是它的哈希，契约见 server/resource/database/password_bootstrap_contract_test.go）。
#   ⇒ 那行文案是**错的**：客户照着试会白扣两次尝试次数，第 3 次直接锁 600 秒，
#   然后来报「初始口令不对 / 登不进去」。改成从配置读之后，配置改了横幅自动跟着变。
function Get-DemoCredential {
    $user = ''
    $pass = ''
    if (-not (Test-Path -LiteralPath $ConfFile)) { return @{ User = $user; Password = $pass } }
    $inSection = $false
    $sectionIndent = 0
    foreach ($line in (Get-Content -LiteralPath $ConfFile -Encoding UTF8)) {
        $trimmed = $line.Trim()
        if ($trimmed -eq '' -or $trimmed.StartsWith('#')) { continue }
        if ($trimmed -match '^demoaccount\s*:\s*(#.*)?$') {
            # ⛔ 按**缩进层级**定位，不能只认顶格：config.yml 里 demoaccount 是嵌套段
            #   （server: → demoaccount:，2 空格缩进），认顶格的话永远找不到。
            $inSection = $true
            $sectionIndent = $line.Length - $line.TrimStart().Length
            continue
        }
        if (-not $inSection) { continue }
        $indent = $line.Length - $line.TrimStart().Length
        if ($indent -le $sectionIndent) { $inSection = $false; continue }
        if ($trimmed -match '^defaultusername\s*:\s*(.*)$') {
            $user = ConvertFrom-YamlScalar -Raw $Matches[1]
        } elseif ($trimmed -match '^defaultpassword\s*:\s*(.*)$') {
            $pass = ConvertFrom-YamlScalar -Raw $Matches[1]
        }
    }
    return @{ User = $user; Password = $pass }
}

# ---------------------------------------------------------------- 组件基元 ----
function Get-Version {
    $versionFile = Join-Path $Root 'version.json'
    if (Test-Path -LiteralPath $versionFile) {
        try {
            $json = Get-Content -LiteralPath $versionFile -Raw -Encoding UTF8 | ConvertFrom-Json
            if ($json.version) { return [string]$json.version }
        } catch { }
    }
    return ''
}

function Test-TcpPort {
    param([int]$Port, [int]$TimeoutMs = 400)
    $client = New-Object System.Net.Sockets.TcpClient
    try {
        $task = $client.ConnectAsync('127.0.0.1', $Port)
        if (-not $task.Wait($TimeoutMs)) { return $false }
        return $client.Connected
    } catch {
        return $false
    } finally {
        $client.Close()
    }
}

function Get-PidFromFile {
    param([string]$Path)
    if (-not (Test-Path -LiteralPath $Path)) { return 0 }
    $raw = (Get-Content -LiteralPath $Path -ErrorAction SilentlyContinue | Select-Object -First 1)
    $pidValue = 0
    if ([int]::TryParse(("$raw").Trim(), [ref]$pidValue)) { return $pidValue }
    return 0
}

# 端口的「真·可绑定性」探测 —— 这是端口占用扫描**看不见**的一类故障。
#
# ⛔⛔ 为什么光有 Get-NetTCPConnection / Get-NetUDPEndpoint 不够：
#   它们只能列出「**已经有进程占着**」的端口。而 Windows 另有一段「动态端口范围」
#   （默认 49152-65535，实测本机被改成了 50000-55000），落进这段的端口会被系统
#   **预留**（excluded port range）：
#     · 没有任何进程占用 ⇒ 占用扫描完全看不见；
#     · 任何程序去 bind 都直接 WSAEACCES「拒绝访问」。
#   后果（2026-10-10 实测，Windows 包的第一个产品级阻塞）：
#     ZLM 的 TCP 端口（HTTP/RTSP/RTMP）全绑得上，一到 RTP 的 UDP 就
#     `bind_sock6 | Bind socket failed: permission denied` ⇒ **进程起来 1 秒后自杀**；
#     SIP 的 UDP 端口同样绑不上 ⇒ 客户设备永远注册不上，而平台侧看不出任何错。
#   ⛔ 这种故障**换机器、重启系统都可能照样存在**（Hyper-V / WSL / Docker / 某些
#     安全软件会抢占并保留整段），所以必须能提前查出来、并说清怎么绕。
#
# 返回值：ok（能绑） / reserved（被系统保留，AccessDenied） / busy（被进程占用） / error
function Get-PortBindState {
    param([int]$Port, [ValidateSet('UDP', 'TCP')][string]$Protocol = 'UDP')
    $sock = $null
    try {
        # ⛔ 用 `[类型]::new(...)` 而不是 `New-Object -ArgumentList`：后者要靠反射去猜
        #   构造函数重载，碰上「三个枚举参数」这种签名有猜错的可能（报一堆找不到重载），
        #   而 PowerShell 5.1 已经支持 `::new()`，没有理由冒这个险。
        $af = [System.Net.Sockets.AddressFamily]::InterNetworkV6
        $sockType = if ($Protocol -eq 'UDP') {
            [System.Net.Sockets.SocketType]::Dgram
        } else {
            [System.Net.Sockets.SocketType]::Stream
        }
        $proto = if ($Protocol -eq 'UDP') {
            [System.Net.Sockets.ProtocolType]::Udp
        } else {
            [System.Net.Sockets.ProtocolType]::Tcp
        }
        # ⛔ 必须探 **IPv6 双栈**（`::`）：ZLM 起监听就是 `:::15514`，
        #   只探 127.0.0.1 会漏掉「IPv6 侧被保留」的情况。
        $sock = [System.Net.Sockets.Socket]::new($af, $sockType, $proto)
        $sock.Bind([System.Net.IPEndPoint]::new([System.Net.IPAddress]::IPv6Any, $Port))
        return 'ok'
    } catch [System.Net.Sockets.SocketException] {
        if ($_.Exception.SocketErrorCode -eq [System.Net.Sockets.SocketError]::AccessDenied) {
            return 'reserved'
        }
        return 'busy'
    } catch {
        return 'error'
    } finally {
        if ($sock) { try { $sock.Close() } catch { } }
    }
}

# 日志里有没有 ZLM「绑不上端口」的特征串。
# ⛔ ZLM 是**先绑 TCP 再绑 UDP**，UDP 一失败就整体退出 —— 所以 Wait-Port 探到 TCP
#   已经通的那一刻，进程其实已经在死了。光看端口会得出完全错误的结论。
function Test-ZlmBindFailure {
    param([string]$LogPath)
    if (-not (Test-Path -LiteralPath $LogPath)) { return $false }
    $tail = (Get-Content -LiteralPath $LogPath -Tail 80 -ErrorAction SilentlyContinue) -join "`n"
    # 硬特征串（ZLM 源码里的固定文案，出现即说明是绑定失败）
    if ($tail -match 'bind_sock|Bind socket failed|Start server failed|Unknown system error') { return $true }
    # ⚠️ `permission denied` 单看不够 —— 别的地方也可能出现这个词（例如访问某个文件被拒）。
    #   这里要求它**与 bind/listen/socket 出现在同一行**，才算端口绑定失败。
    if ($tail -match '(?i)(bind|listen|socket)[^\r\n]{0,120}permission denied') { return $true }
    return $false
}

function Test-PidAlive {
    param([int]$ProcessId)
    if ($ProcessId -le 0) { return $false }
    return [bool](Get-Process -Id $ProcessId -ErrorAction SilentlyContinue)
}

function Wait-Port {
    param([int]$Port, [int]$Tries = 100, [int]$SleepMs = 200, [string]$LogPath)
    for ($i = 0; $i -lt $Tries; $i++) {
        if (Test-TcpPort -Port $Port) { return $true }
        if ($LogPath -and (Test-Path -LiteralPath $LogPath)) {
            $tail = Get-Content -LiteralPath $LogPath -Tail 12 -ErrorAction SilentlyContinue
            if ($tail -match 'panic|FATAL|fatal error|startupFail') { return $false }
        }
        Start-Sleep -Milliseconds $SleepMs
    }
    return $false
}

function Start-Child {
    param(
        [string]$Name,
        [string]$Exe,
        [string[]]$Arguments,
        [string]$WorkDir,
        [string]$LogPath,
        [string]$PidPath
    )
    $out = $LogPath
    $err = "$LogPath.err"
    # ⛔⛔ `Start-Process -ArgumentList` 收到**空数组**会直接抛
    #   ParameterBindingValidationException：
    #   "The argument is null, empty, or an element of the argument collection contains a null value."
    #   —— 而**后端恰恰是无参数启动的**（调用点传 `-Arguments @()`），所以这条在首次启动
    #   必然命中，症状极具误导性：ZLM / nginx / Redis 全绿，一到后端整个脚本就终止、
    #   退出码 1，**却不打任何 `[uvp-gb28181][ERROR]` 行**（因为异常在 Fail 之前就抛了）。
    #   ⇒ 无参数时干脆**不传** -ArgumentList（改用 splat 组装）。
    #     数组里混进 $null 元素报的是同一句话，这里一并过滤掉。
    $argList = @($Arguments | Where-Object { $null -ne $_ })
    $sp = @{
        FilePath               = $Exe
        WorkingDirectory       = $WorkDir
        WindowStyle            = 'Hidden'
        PassThru               = $true
        RedirectStandardOutput = $out
        RedirectStandardError  = $err
    }
    if ($argList.Count -gt 0) { $sp['ArgumentList'] = $argList }
    $proc = Start-Process @sp
    if ($PidPath) { Set-Content -LiteralPath $PidPath -Value $proc.Id -Encoding ASCII }
    return $proc
}

function Stop-ProcessTree {
    param([int]$ProcessId, [int]$GraceMs = 4000)
    if ($ProcessId -le 0) { return }
    if (-not (Test-PidAlive -ProcessId $ProcessId)) { return }
    # ⛔ 先礼貌后强杀：Windows 没有 SIGTERM，Stop-Process 就是 TerminateProcess；
    #   用 taskkill /T 是为了把子进程（Cygwin 版 redis 的子 shell 等）一起带走。
    # ⛔⛔ 必须走 Invoke-NativeQuiet（它内部临时降 ErrorActionPreference）——
    #   直接 `& taskkill ... *> $null` 或 `2>&1 | Out-Null` 都会让 taskkill 写往 stderr 的
    #   「没有找到进程」被升格成终止性异常，详见 Invoke-NativeQuiet 的注释。
    Invoke-NativeQuiet -Exe 'taskkill.exe' -Arguments @('/PID', "$ProcessId", '/T')
    $deadline = (Get-Date).AddMilliseconds($GraceMs)
    while ((Get-Date) -lt $deadline) {
        if (-not (Test-PidAlive -ProcessId $ProcessId)) { return }
        Start-Sleep -Milliseconds 200
    }
    Invoke-NativeQuiet -Exe 'taskkill.exe' -Arguments @('/PID', "$ProcessId", '/T', '/F')
}

# 按可执行文件路径兜底找进程（pid 文件丢了的时候用）
function Get-PidByExe {
    param([string]$ExePath)
    $name = [System.IO.Path]::GetFileNameWithoutExtension($ExePath)
    $found = @()
    foreach ($p in (Get-Process -Name $name -ErrorAction SilentlyContinue)) {
        try {
            if ($p.Path -and ($p.Path -ieq $ExePath)) { $found += $p.Id }
        } catch { }
    }
    return $found
}

function Stop-Component {
    param([string]$Label, [string]$PidPath, [string]$ExePath)
    $pidValue = Get-PidFromFile -Path $PidPath
    $killed = $false
    if ($pidValue -gt 0) {
        if (Test-PidAlive -ProcessId $pidValue) {
            Stop-ProcessTree -ProcessId $pidValue
            $killed = $true
        }
        Remove-Item -LiteralPath $PidPath -Force -ErrorAction SilentlyContinue
    }
    if (-not $killed -and $ExePath) {
        foreach ($extra in (Get-PidByExe -ExePath $ExePath)) {
            Stop-ProcessTree -ProcessId $extra
            $killed = $true
        }
    }
    if ($killed) { Write-UvpDet "$Label 已停止" } else { Write-UvpDet "$Label 未在运行" }
}

# ---------------------------------------------------------------- 横幅/汇总 ----
function Write-Banner {
    $version = Get-Version
    $title = 'UVP-GB28181  统一视频接入平台'
    if ($version) { $title = "$title  版本号 v$version" }
    $line = '=' * 64
    Write-Host ''
    Write-Host ("{0}{1}{2}" -f $C.dim, $line, $C.rst)
    Write-Host ("  {0}" -f $title)
    Write-Host ("{0}{1}{2}" -f $C.dim, $line, $C.rst)
    Write-Host ''
}

function Get-LanIP {
    $candidates = @()
    try {
        $route = Get-NetRoute -DestinationPrefix '0.0.0.0/0' -ErrorAction Stop |
            Sort-Object -Property RouteMetric | Select-Object -First 1
        if ($route) {
            $addr = Get-NetIPAddress -InterfaceIndex $route.InterfaceIndex -AddressFamily IPv4 -ErrorAction SilentlyContinue |
                Where-Object { $_.IPAddress -notlike '169.254.*' } | Select-Object -First 1
            if ($addr) { $candidates += $addr.IPAddress }
        }
    } catch { }
    foreach ($addr in ([System.Net.Dns]::GetHostAddresses([System.Net.Dns]::GetHostName()))) {
        if ($addr.AddressFamily -eq [System.Net.Sockets.AddressFamily]::InterNetwork) {
            $candidates += $addr.IPAddressToString
        }
    }
    # 优先 192.168./10. 段，其次任意非回环
    foreach ($ip in $candidates) {
        if ($ip -like '192.168.*' -or $ip -like '10.*') { return $ip }
    }
    foreach ($ip in $candidates) {
        if ($ip -ne '127.0.0.1') { return $ip }
    }
    return '127.0.0.1'
}

function Write-PortPlan {
    param([hashtable]$Ports)
    Write-Host ''
    Write-UvpDet '端口规划（局域网访问需要放行）：'
    if ($script:HttpsEnabled) {
        Write-UvpDet ("  {0} 管理页面（HTTPS）      http://<本机IP>:{1}  → https://<本机IP>:{2}" -f '·', $Ports.NGINX_HTTP, $Ports.HTTPS)
    } else {
        Write-UvpDet ("  {0} 管理页面（HTTP；HTTPS 已关闭） http://<本机IP>:{1}" -f '·', $Ports.NGINX_HTTP)
    }
    Write-UvpDet ("  {0} 后端 API（仅本机）     http://127.0.0.1:{1}" -f '·', $Ports.HTTP)
    Write-UvpDet ("  {0} Redis（仅本机）        127.0.0.1:{1}" -f '·', $Ports.REDIS)
    Write-UvpDet ("  {0} 流媒体（ZLM）          {1}-{2} / RTP {3} / SIP {4}" -f '·', $Ports.ZLM_HTTP, $Ports.ZLM_ICE, $ZlmRtpRange, $SipPort)
    Write-Host ''
    $tcp = "$($Ports.HTTPS)-$SipPort"
    # ⛔ 不要把这段写成字面量：端口段一平移，文档就与脚本对不上（本文件已踩过）。
    # ⛔⛔ RTP 段的**起点**必须从 `$ZlmRtpRange` 里拆出来，绝不能再接一个 `-$SipPort`：
    #   `$ZlmRtpRange` 本身已经带连字符（`15514-15563`），拼出来会变成
    #   `15514-15563-15564` —— **不是合法的端口范围**，客户照抄这条
    #   New-NetFirewallRule 会直接报参数错。（原字面量 `51014-51064` 用的是
    #   「RTP 起点 → SIP 端口」合并成一段的写法，这里要还原同样的语义。）
    #   ⭐ 这个 bug 实测出现过一次（2026-10-10 验收输出里捞出来的），所以留个记号。
    $rtpStart = $ZlmRtpRange.Split('-')[0]
    $udp = "$($Ports.ZLM_SRT),$($Ports.ZLM_ICE),$rtpStart-$SipPort"
    Write-UvpDet '防火墙放行命令（管理员 PowerShell，只需执行一次）：'
    Write-UvpDet ("  New-NetFirewallRule -DisplayName 'UVP' -Direction Inbound -Action Allow " +
                  "-Protocol TCP -LocalPort $tcp")
    Write-UvpDet ("  New-NetFirewallRule -DisplayName 'UVP-UDP' -Direction Inbound -Action Allow " +
                  "-Protocol UDP -LocalPort $udp")
}

function Write-StartSummary {
    param([hashtable]$Ports, [string]$LanIP)
    $scheme = 'https'; $entryPort = $Ports.HTTPS
    if (-not $script:HttpsEnabled) { $scheme = 'http'; $entryPort = $Ports.NGINX_HTTP }
    # ⛔⛔ 这是整段安装输出**最后**一块，用户装完第一眼就找这个地址 ⇒ 用不封口的
    #   横线框圈出来、地址行加粗（$C.hl）。不能只靠颜色：输出重定向成日志时
    #   $script:UseColor 为假，那时只剩框线与空行，也得能看出"到这里就装完了"。
    #   不画右边线：中文双宽，PowerShell 也算不出列宽，封口框换字体必歪。
    $rule = '-' * 64
    Write-Host ''
    Write-UvpLog '启动完成'
    Write-Host ''
    Write-Host ("{0}{1}{2}" -f $C.tag, $rule, $C.rst)
    Write-Host ("{0}{1}访问地址  {2}://{3}:{4}{5}" -f $C.hl, $script:IND, $scheme, $LanIP, $entryPort, $C.rst)
    if ($LanIP -ne '127.0.0.1') {
        Write-UvpDet ("本机访问  {0}://127.0.0.1:{1}" -f $scheme, $entryPort)
    } else {
        Write-UvpDet '提示      未识别到对外网卡，其它电脑请改用本机 IP 访问'
    }
    $demo = Get-DemoCredential
    if ($demo.Password) {
        Write-UvpDet ("登录账号  {0} / {1}（登录后请立即修改）" -f $demo.User, $demo.Password)
    } else {
        Write-UvpDet '登录账号  admin（口令见 config\config.yml 的 server.demoaccount.defaultpassword，登录后请立即修改）'
    }
    Write-Host ("{0}{1}{2}" -f $C.tag, $rule, $C.rst)
}

# ---------------------------------------------------------------- 阶段 1 ---- ----
function Test-Preconditions {
    $missing = @()
    foreach ($item in @(
            @{ Path = $ServerExe; Name = 'bin\uvp-server.exe' },
            @{ Path = $SetupExe;  Name = 'bin\uvp-setup.exe' },
            @{ Path = $ConfFile;  Name = 'config\config.yml' },
            @{ Path = $Baseline;  Name = 'resource\baseline\baseline.sql' },
            @{ Path = $RedisExe;  Name = 'vendor\redis\redis-server.exe' },
            @{ Path = $ZlmExe;    Name = 'vendor\zlm\MediaServer.exe' },
            @{ Path = $ZlmIni;    Name = 'vendor\zlm\config.ini' },
            @{ Path = $NginxExe;  Name = 'vendor\nginx\nginx.exe' }
        )) {
        if (-not (Test-Path -LiteralPath $item.Path)) { $missing += $item.Name }
    }
    $hasFrontend = (Test-Path -LiteralPath (Join-Path $PublicDir 'index.html'))
    if (-not $hasFrontend) { $missing += 'resource\public\index.html（前端产物）' }
    if ($missing.Count -gt 0) {
        Fail ("包不完整，缺少：`n        - " + ($missing -join "`n        - ") + "`n      请重新解压完整安装包。")
    }
    Write-UvpOk '文件完整性检查通过'

    $arch = $env:PROCESSOR_ARCHITECTURE
    if ($arch -ne 'AMD64') {
        Fail "本包只支持 64 位 Windows（当前 PROCESSOR_ARCHITECTURE=$arch）。"
    }
    Write-UvpOk "架构检查通过（$arch）"
    Write-UvpDet "（提示：SIP 端口 $SipPort 由平台内的引导页录入，不在本预检范围内）"
}

function Test-PortsFree {
    param([hashtable]$Ports)
    $tcpPorts = New-Object System.Collections.Generic.List[int]
    $udpPorts = New-Object System.Collections.Generic.List[int]
    foreach ($p in @($Ports.HTTPS, $Ports.NGINX_HTTP, $Ports.HTTP, $Ports.REDIS,
                     $Ports.ZLM_HTTP, $Ports.ZLM_RTSP, $Ports.ZLM_RTMP, $Ports.ZLM_SSL,
                     $Ports.ZLM_RTC, $Ports.ZLM_SIGNALING, $Ports.ZLM_SIGNALING_SSL,
                     $Ports.ZLM_ONVIF, $Ports.ZLM_ICE)) { $tcpPorts.Add([int]$p) }
    if (-not $script:HttpsEnabled) { $tcpPorts.Remove([int]$Ports.HTTPS) | Out-Null }
    $udpPorts.Add([int]$Ports.ZLM_SRT)
    $udpPorts.Add([int]$Ports.ZLM_ICE)
    $rtpStart = [int]($ZlmRtpRange.Split('-')[0])
    $rtpEnd = [int]($ZlmRtpRange.Split('-')[1])
    for ($p = $rtpStart; $p -le $rtpEnd; $p++) {
        $tcpPorts.Add($p)
        $udpPorts.Add($p)
    }
    # ⛔ SIP（$SipPort）**不在预检内**：它的端口由平台引导页录入（存 gb_sip_config 表），
    #   包内无从得知客户要填哪个，Linux 版同样把它排除。

    $tcpSet = @{}
    $udpSet = @{}
    try {
        foreach ($c in (Get-NetTCPConnection -State Listen -ErrorAction Stop)) { $tcpSet[[int]$c.LocalPort] = $c.OwningProcess }
        foreach ($c in (Get-NetUDPEndpoint -ErrorAction Stop)) { $udpSet[[int]$c.LocalPort] = $c.OwningProcess }
    } catch {
        Write-UvpWarn "无法用 Get-NetTCPConnection 预检端口（$($_.Exception.Message)），跳过端口占用检查"
        return
    }

    $conflicts = @()
    foreach ($p in $tcpPorts) {
        if ($tcpSet.ContainsKey($p)) {
            $owner = ''
            try { $owner = (Get-Process -Id $tcpSet[$p] -ErrorAction SilentlyContinue).ProcessName } catch { }
            $conflicts += "TCP $p（被 $owner 占用）"
        }
    }
    foreach ($p in $udpPorts) {
        if ($udpSet.ContainsKey($p)) {
            $owner = ''
            try { $owner = (Get-Process -Id $udpSet[$p] -ErrorAction SilentlyContinue).ProcessName } catch { }
            $conflicts += "UDP $p（被 $owner 占用）"
        }
    }
    if ($conflicts.Count -gt 0) {
        # ⛔ 端口被别人占着而照样启动的后果：nginx 会连到别的服务上，而 status 仍报「运行中」，
        #   客户看到的是**别的系统的页面**，全程零报错。
        Fail ("以下端口已被占用，请先释放（或先执行本目录的 stop.cmd 停掉本平台）：`n        - " +
              (($conflicts | Select-Object -First 12) -join "`n        - "))
    }

    # ------------------------------------------------------------------
    # 第二道：**真·绑定探测** —— 专抓「被 Windows 保留」的端口。
    #
    # ⛔⛔ 上面那轮扫描只看得到「有进程占着」，看不到「被系统预留」。而被预留的端口
    #   bind 必然 WSAEACCES，足以让 ZLM 秒退、SIP 永远注册不上 —— 且表现是
    #   「预检通过 ✓ / 服务已启动 ✓ / 一秒后 status 说不在了」，排查要绕很远。
    #   ⇒ 这里对计划用到的每个端口**逐个真绑一次**（IPv6 双栈，与 ZLM 一致），
    #     只把 reserved 这一类拦下来（busy 已由上一轮报告过）。
    #   ⚠️ 探测顺序固定（先 TCP 后 UDP、端口升序），失败信息可复现。
    # ------------------------------------------------------------------
    $reserved = @()
    foreach ($p in ($udpPorts | Sort-Object -Unique)) {
        if ((Get-PortBindState -Port $p -Protocol 'UDP') -eq 'reserved') { $reserved += "UDP $p" }
    }
    foreach ($p in ($tcpPorts | Sort-Object -Unique)) {
        if ((Get-PortBindState -Port $p -Protocol 'TCP') -eq 'reserved') { $reserved += "TCP $p" }
    }
    if ($reserved.Count -gt 0) {
        $shown = $reserved | Select-Object -First 8
        Fail ("以下端口被 **Windows 系统保留**，任何程序都绑不上（不是被别的软件占用）：`n        - " +
              (($shown) -join "`n        - ") + "`n" +
              "              原因：本机的「动态端口范围」覆盖了这一段。查看当前保留区（管理员 CMD）：`n" +
              "                netsh int ipv4 show excludedportrange protocol=udp`n" +
              "                netsh int ipv4 show excludedportrange protocol=tcp`n" +
              "              ⇒ 把 config\config.env 里的端口段挪出保留区（见包内 README 第四节），`n" +
              "                再执行 stop.cmd → start.cmd。")
    }
    Write-UvpOk ("端口预检通过（TCP $($Ports.HTTPS)-$SipPort + UDP " +
                 "$($Ports.ZLM_SRT)/$($Ports.ZLM_ICE)/$($ZlmRtpRange.Split('-')[0])-$SipPort，均实测可绑定）")

    # --------------------------------------------------------------
    # SIP 端口：只**告警**、不拦启动。
    # ⛔ 为什么不当失败：SIP 的最终端口由引导页录入（存 gb_sip_config 表），包内只知道
    #   默认值；客户完全可能填别的端口 ⇒ 拿默认值判失败会误伤。
    # ⭐ 但为什么还要查：默认值就是引导页的**预填值**，它若被系统保留，管理员点保存
    #   必然 bind 失败（SIP 用 UDP+TCP 双栈，两个都得能绑），而那条报错离"端口被保留"
    #   很远，现场很可能一路查到设备侧去。
    # --------------------------------------------------------------
    if ((Get-PortBindState -Port $SipPort -Protocol 'UDP') -eq 'reserved' -or
        (Get-PortBindState -Port $SipPort -Protocol 'TCP') -eq 'reserved') {
        Write-UvpWarn ("默认 SIP 端口 $SipPort 被 **Windows 系统保留**，绑定必失败 ——" +
                       "引导页里请改成一个可用的端口（否则国标设备永远注册不上）。")
    }
}

# ---------------------------------------------------------------- 阶段 2 ---- ----
function Invoke-DatabaseInit {
    if (Test-Path -LiteralPath $DbPath) {
        # 幂等由 uvp-setup 判（看 sys_users 表在不在），这里只提示。
        Write-UvpDet '检测到已有数据库，校验中…'
    } else {
        Write-UvpLog '      首次运行：正在初始化数据库（111 张表）…'
    }
    & $SetupExe db-init --baseline $Baseline --db $DbPath
    if ($LASTEXITCODE -ne 0) { Fail "数据库初始化失败（uvp-setup 退出码 $LASTEXITCODE）" }
}

function Invoke-PortsSync {
    param([hashtable]$Config)
    $ports = $Config.Ports
    $syncArgs = @(
        'ports-sync',
        '--env', $EnvFile, '--yml', $ConfFile, '--ini', $ZlmIni,
        '--http', [string]$ports.HTTP, '--redis', [string]$ports.REDIS,
        '--https', [string]$ports.HTTPS, '--nginx-http', [string]$ports.NGINX_HTTP,
        '--zlm-http', [string]$ports.ZLM_HTTP, '--zlm-ssl', [string]$ports.ZLM_SSL,
        '--zlm-rtsp', [string]$ports.ZLM_RTSP, '--zlm-rtmp', [string]$ports.ZLM_RTMP,
        '--zlm-rtc', [string]$ports.ZLM_RTC, '--zlm-signaling', [string]$ports.ZLM_SIGNALING,
        '--zlm-signaling-ssl', [string]$ports.ZLM_SIGNALING_SSL, '--zlm-srt', [string]$ports.ZLM_SRT,
        '--zlm-onvif', [string]$ports.ZLM_ONVIF, '--zlm-ice', [string]$ports.ZLM_ICE,
        '--zlm-rtp-proxy', [string]$ports.ZLM_RTP_PROXY, '--zlm-rtp-range', $ZlmRtpRange
    )
    # ⛔⛔ `--user-set` 必须**非空才追加**：PowerShell 在拼原生命令行时会把
    #   **空字符串参数整条丢掉**，于是 uvp-setup 收到一个没有值的 `--user-set`，
    #   Go 的 flag 包直接报 `flag needs an argument: -user-set` 退出 ——
    #   而这一条在"用户没改过任何端口"的首次启动里**100% 命中**，
    #   表现为「数据库建好了、端口同步失败」，极容易被误判成 uvp-setup 的 bug。
    if ($Config.UserSet.Count -gt 0) {
        $syncArgs += @('--user-set', ($Config.UserSet -join ','))
    }
    & $SetupExe @syncArgs
    if ($LASTEXITCODE -ne 0) { Fail "端口同步失败（uvp-setup 退出码 $LASTEXITCODE）" }
}

function Invoke-TlsMaterial {
    foreach ($dir in @((Split-Path -Parent $NginxCrt), (Split-Path -Parent $ZlmSslPem))) {
        if (-not (Test-Path -LiteralPath $dir)) { New-Item -ItemType Directory -Force -Path $dir | Out-Null }
    }
    # ⛔ 2026-10-19 起**优先用包内自带证书**（resource\cert）：安装时只做拷贝，
    #   不再现场生成 —— 生成失败只写在日志里，现场表现就是"https 打不开"。
    # ⛔ 只在目标文件缺失时拷贝：客户上传过真证书的机器，升级后不能被内置证书盖掉。
    $bundledCrt = Join-Path $Root 'resource\cert\uvp.crt'
    $bundledKey = Join-Path $Root 'resource\cert\uvp.key'
    if ((-not (Test-Path -LiteralPath $NginxCrt)) -and (-not (Test-Path -LiteralPath $NginxKey)) -and
        (Test-Path -LiteralPath $bundledCrt) -and (Test-Path -LiteralPath $bundledKey)) {
        Copy-Item -LiteralPath $bundledCrt -Destination $NginxCrt -Force
        Copy-Item -LiteralPath $bundledKey -Destination $NginxKey -Force
    }
    $ips = @(Get-LanIP)
    # ⛔ certgen 是**幂等**的：证书已存在就跳过生成，只负责把 ZLM 要的那份
    #   "私钥+证书合一" PEM 补出来（ZLM 只认单文件）。所以内置证书就位后它也不会覆盖。
    & $SetupExe certgen --dir (Split-Path -Parent $NginxCrt) --zlm-pem $ZlmSslPem --cn 'uvp-local' --ip ($ips -join ',')
    if ($LASTEXITCODE -ne 0) { Write-UvpWarn '证书生成失败（不影响启动，但浏览器只能走 HTTP 明文端口）' }
}
function Invoke-NginxConfRender {
    if (-not (Test-Path -LiteralPath $NginxTpl)) {
        Write-UvpWarn "缺少 nginx 配置模板（$NginxTpl），沿用已有的 nginx.conf"
        return
    }
    $text = Get-Content -LiteralPath $NginxTpl -Raw -Encoding UTF8
    $config = $script:CurrentConfig
    $text = $text.Replace('@ROOT@', ($Root -replace '\\', '/'))
    $text = $text.Replace('@HTTPS_PORT@', [string]$config.Ports.HTTPS)
    $text = $text.Replace('@PLAIN_HTTP_PORT@', [string]$config.Ports.NGINX_HTTP)
    $text = $text.Replace('@BACKEND_PORT@', [string]$config.Ports.HTTP)
    # ⛔⛔ 必须写**无 BOM**（用 Write-Utf8NoBom，别用 Set-Content -Encoding UTF8）。
    #   PS 5.1 的 `-Encoding UTF8` 会往文件头塞 EF BB BF，而 nginx 不认 BOM：
    #   它把首行读成 `\ufeff# ...`，于是**第 1 行那句注释不再是注释**，词法器一路往下
    #   吞到第一个 `;` 才报错 —— 实测报的是
    #   `[emerg] unknown directive "\ufeff#" in ...\nginx.conf:22`（行号指向被吞掉的
    #   那条指令 worker_processes 的结尾），**极易误导成"第 22 行有问题"**。
    Write-Utf8NoBom -Path $NginxConf -Text $text
    & $SetupExe https-render --root $Root
    if ($LASTEXITCODE -ne 0) { Fail '应用 HTTPS 配置失败' }
    # BOM 属于"静默坏"：不主动检查就只能等 nginx 报错才暴露，所以渲染完立刻回读自检。
    $back = [System.IO.File]::ReadAllText($NginxConf, (New-Object System.Text.UTF8Encoding $false))
    if ($back.Length -gt 0 -and [int]$back[0] -eq 0xFEFF) {
        Fail "nginx.conf 渲染结果带 UTF-8 BOM，nginx 会拒绝解析（$NginxConf）"
    }
    if ($back -match '@[A-Z_]+@') {
        Fail "nginx.conf 渲染后仍有未替换的占位符（$NginxConf）"
    }
}

# ---------------------------------------------------------------- 阶段 3 ---- ----
function Start-Redis {
    param([hashtable]$Ports)
    $redisDataDir = Join-Path $DataDir 'redis'
    if (-not (Test-Path -LiteralPath $redisDataDir)) { New-Item -ItemType Directory -Force -Path $redisDataDir | Out-Null }

    # ⛔⛔ 用**配置文件**拉起 redis，而不是把一串 `--key value` 塞进命令行。
    #   理由（实测过的坑）：包内 redis 是 Cygwin 版（依赖 cygwin1.dll），它的 argv 由
    #   cygwin1.dll 自己从 Windows 命令行重解析 —— 空字符串参数（`--save ""`）、
    #   含空格/中文的绝对路径在这里都极易被解析坏，而失败表现极具误导性：
    #   **进程起来了、端口却不对 / 数据目录跑到别处**，看上去像"redis 正常"。
    #   ⇒ 配置文件 + 裸文件名 + 工作目录（`dir .`）是最稳的写法，Windows 7.2.16 已验收
    #     （见 UVP-REN/deploy/windows/redis-probe/probe.go 的 newRedisInstance）。
    $confText = (@(
            '# UVP-GB28181 Redis 配置 —— 由 uvp-gb28181-ctl.ps1 每次启动时重写',
            '# ⛔ 不要手工改这个文件；要改端口请改 config\config.env 里的 UVP_REDIS_PORT。',
            'bind 127.0.0.1',
            "port $($Ports.REDIS)",
            'protected-mode yes',
            'daemonize no',
            'supervised no',
            'loglevel notice',
            'logfile ""',
            'save ""',
            'appendonly yes',
            'appendfsync everysec',
            'appendfilename "appendonly.aof"',
            'appenddirname "appendonlydir"',
            'dbfilename "dump.rdb"',
            'maxmemory 0',
            'maxmemory-policy noeviction',
            'dir .'
        ) -join "`r`n") + "`r`n"
    Write-Utf8NoBom -Path $RedisConf -Text $confText

    $log = Join-Path $LogsDir 'redis.log'
    # ⛔ 传的是**裸文件名**、工作目录设成 data\redis：`dir .` 于是落在包内，
    #   AOF 也就写在 data\redis\ 下（不会跑到进程启动目录）。
    Start-Child -Name 'redis' -Exe $RedisExe -WorkDir $redisDataDir -LogPath $log -PidPath $RedisPidFile `
        -Arguments @([System.IO.Path]::GetFileName($RedisConf)) | Out-Null
    $ok = $false
    for ($i = 0; $i -lt 40; $i++) {
        Start-Sleep -Milliseconds 250
        # ⛔ 别用 `2>$null`：那同样会把 native stderr 包成 ErrorRecord，在本脚本的
        #   `$ErrorActionPreference = 'Stop'` 下直接中断（redis 还没起完时必然踩）。
        #   ⇒ 走 Invoke-NativeText（内部临时降级后合并输出）；连不上时 redis-cli 的
        #     报错文本里不会出现 PONG，所以判断语义与原来完全一致。
        $pong = Invoke-NativeText -Exe $RedisCli -Arguments @('-p', [string]$Ports.REDIS, 'ping')
        if ($pong -match 'PONG') { $ok = $true; break }
    }
    if (-not $ok) { Fail "Redis 启动失败，详见 $log" }
    Write-UvpOk "Redis 已启动（127.0.0.1:$($Ports.REDIS)，AOF 落盘 $redisDataDir）"
}

# 把「进程权威目录」的 ACL 收紧到只剩受信账户，并**断开权限继承**。
#
# ⛔⛔ 为什么必须在启动前做：后端 `app/openapi/processauthority/local_lock_windows.go` 的
#   `secureLocalFile()` 会遍历目录/锁文件的 DACL，**只要发现任何一个非受信 SID
#   （受信 = 当前用户 / SYSTEM / Administrators）持有写权限就直接拒绝**。
#   而 Windows 对**数据盘根目录**的默认 ACL 是
#     `NT AUTHORITY\Authenticated Users:(M)`（修改，含写/删）
#   —— 新建的子目录会**继承**它。⇒ 客户把绿色包解压到 `D:\` / `E:\` / `F:\` 这类盘上
#   （最自然的做法），后端 **100% 起不来**。故障表现极其隐晦（2026-10-10 实测）：
#     · 进程静默退出，stderr 只有一行不含 message 的 `{"event":"startup.failed","phase":"logging"}` 之后再无输出；
#     · 应用日志里只有 `phase=process_authority reason=state_unavailable`；
#     · 而此刻**配置 / 日志 / 数据库 / casbin / 调度器都已经就绪**，倒在最后一步，
#       连"是不是权限问题"都看不出来（真正的 `ErrLocalAuthorityUnavailable` 被
#       ErrorClass 刻意省略了 message）。
#   ⇒ 启动时主动断开继承、只保留 {当前用户, SYSTEM, Administrators}。
#
# ⚠️ 用**当前用户 SID**（`*S-1-5-…`）而不是 `%USERDOMAIN%\%USERNAME%`：
#   工作组机器上 `%USERDOMAIN%` 是 `WORKGROUP`，拼出来的账户名不存在，icacls 会报
#   `No mapping between account names and security IDs was done`（错误 1332）—— 实测踩过。
# ⚠️ `-S-1-5-18` = SYSTEM，`-S-1-5-32-544` = Administrators；用 SID 是为了不依赖系统语言。
function Initialize-ProcessAuthorityDir {
    param([string]$Path)
    if (-not (Test-Path -LiteralPath $Path)) {
        New-Item -ItemType Directory -Force -Path $Path | Out-Null
    }
    try {
        $sid = [System.Security.Principal.WindowsIdentity]::GetCurrent().User.Value
    } catch {
        Write-UvpWarn "取不到当前用户 SID，跳过进程权威目录的权限收紧（后端可能拒绝启动）：$($_.Exception.Message)"
        return
    }
    $ace = '*' + $sid + ':(OI)(CI)F'
    # /T 是为了照顾"上一次启动已留下继承旧的 ACL 的文件"；/C 让个别失败不中断整体。
    $out = Invoke-NativeText -Exe 'icacls.exe' -Arguments @(
        $Path, '/inheritance:r',
        '/grant:r', $ace,
        '/grant:r', '*S-1-5-18:(OI)(CI)F',
        '/grant:r', '*S-1-5-32-544:(OI)(CI)F',
        '/T', '/C')
    if ($out -match 'Failed processing\s+[1-9]') {
        Write-UvpWarn "进程权威目录权限收紧部分失败，后端可能拒绝启动（$Path）"
    }
}

function Start-Backend {
    param([hashtable]$Ports)
    $envMap = Read-EnvFileMap -Path $EnvFile
    # ⛔ 两个密钥只走环境变量（不进 config.yml），必须在这里显式注入子进程环境。
    if ($envMap.ContainsKey('UVP_SIP_TRACE_ENCRYPTION_KEY')) {
        $env:UVP_SIP_TRACE_ENCRYPTION_KEY = $envMap['UVP_SIP_TRACE_ENCRYPTION_KEY']
    }
    if ($envMap.ContainsKey('UVP_OPENAPI_MASTER_KEY')) {
        $env:UVP_OPENAPI_MASTER_KEY = $envMap['UVP_OPENAPI_MASTER_KEY']
    }
    # ⛔⛔ 进程权威目录必须**显式指到包内**，绝不能让它走平台默认值。
    #   后端 `processauthority.PrepareStateDir()` 的解析顺序是：
    #     ① config.yml 的 `processauthority.state_dir`（非空则**原样返回**，不创建、不绝对化）
    #     ② 环境变量 `UVP_PROCESS_AUTHORITY_DIR`   ← 本行走这条
    #     ③ 平台默认：Windows = `%LOCALAPPDATA%\UVP-GB28181\process-authority`
    #   走 ③ 有两个真实代价（2026-10-10 实测首例）：
    #     a) **非交互会话（服务 / ssh / 计划任务）下 LOCALAPPDATA 可能取不到** ⇒ 后端报
    #        `phase=process_authority reason=state_unavailable` 退出。最坑的是时机：
    #        **配置、日志、数据库、casbin、调度器全都已经就绪**，倒在最后一步，
    #        而 stderr 只有一行不含 message 的 startup.failed JSON，日志里也只有
    #        `phase=process_authority`（真正的 errors.New("LOCALAPPDATA is unavailable")
    #        被 ErrorClass 刻意省略）—— 排查要绕很远。
    #     b) 绿色包不再自包含：状态落到用户 AppData，卸载/迁移会漏掉，与 Linux 版
    #        （数据都在包内）行为也不一致。
    #   ⚠️ 为什么不写进 config.yml：出包时不知道客户的解压路径，写不了绝对路径；
    #     而 PrepareStateDir 对**显式值不做绝对化**，塞相对路径会埋雷。⇒ 只能运行时注入。
    $env:UVP_STANDALONE_ROOT = $Root
    $env:UVP_PROCESS_AUTHORITY_DIR = Join-Path $DataDir 'process-authority'
    Initialize-ProcessAuthorityDir -Path $env:UVP_PROCESS_AUTHORITY_DIR
    $log = Join-Path $LogsDir 'backend.log'
    # ⛔ 工作目录必须是包根：后端按 cwd 解析 config/config.yml 与 ./data/uvp.db。
    Start-Child -Name 'backend' -Exe $ServerExe -WorkDir $Root -LogPath $log -PidPath $BackendPidFile -Arguments @() | Out-Null
    if (-not (Wait-Port -Port $Ports.HTTP -Tries 150 -SleepMs 200 -LogPath $log)) {
        Fail "后端启动失败或未在 $($Ports.HTTP) 端口监听，详见 $log（另见 $(Join-Path $LogsDir 'backend.log.err')）"
    }
    Write-UvpOk "后端已启动（http://127.0.0.1:$($Ports.HTTP)）"
}

function Start-Zlm {
    param([hashtable]$Ports)
    $log = Join-Path $LogsDir 'zlm.log'
    # ⛔⛔ 与 Redis 同一个道理：传**裸文件名** + 工作目录，别把绝对路径塞进命令行。
    #   MediaServer.exe 用窄字符（ANSI/UTF-8）打开路径，安装目录一旦带中文或空格
    #   （客户的 `D:\接活\…` 就是），绝对路径会静默开错文件甚至起不来。
    #   （UVP-REN/deploy/windows/zlm-probe 就是这么拉的，并专门在
    #     「含中文和空格的路径」下验收过。）
    $args = @('-c', 'config.ini')
    # ZLM 的 SSL 只从 `-s` 指定的**单个** PEM 读（私钥+证书合一，由 certgen 合成）。
    # 同样只给裸文件名 —— 它就在 vendor\zlm\ 下。
    if (Test-Path -LiteralPath $ZlmSslPem) { $args += @('-s', [System.IO.Path]::GetFileName($ZlmSslPem)) }
    Start-Child -Name 'zlm' -Exe $ZlmExe -WorkDir $ZlmDir -LogPath $log -PidPath $ZlmPidFile -Arguments $args | Out-Null
    if (-not (Wait-Port -Port $Ports.ZLM_HTTP -Tries 100 -SleepMs 200 -LogPath $log)) {
        Fail "流媒体服务（ZLM）启动失败，详见 $log"
    }
    # ⛔⛔ 「TCP 端口通了」**不等于启动成功** —— 必须再等一拍，看它有没有立刻自杀。
    #   ZLM 的启动顺序是「先绑 TCP（HTTP/RTSP/RTMP 都绑得上）→ 再绑 RTP/SRT/ICE 的 UDP」，
    #   UDP 一失败就整体退出，全程 < 1 秒。而 Wait-Port 只探 TCP ⇒ 它返回 true 的那一刻
    #   进程其实已经在死了，ctl 却打出「✓ 流媒体服务已启动」，**1 秒后 status 就说不在了**。
    #   （2026-10-10 实测原话：「报成功却不在了」。）
    #   三条独立判据，命中任意一条即判失败（并把线索直接给出来，别让现场猜）：
    #     ① 进程没了 —— pid 文件里那个 pid 已经不存活
    #     ② TCP 端口重新连不上
    #     ③ 日志里出现 ZLM 自己的端口绑定失败特征串
    Start-Sleep -Milliseconds 1200
    $zlmPid = Get-PidFromFile -Path $ZlmPidFile
    $why = @()
    if (-not (Test-PidAlive -ProcessId $zlmPid)) { $why += '进程已退出' }
    if (-not (Test-TcpPort -Port $Ports.ZLM_HTTP)) { $why += "TCP $($Ports.ZLM_HTTP) 已不再监听" }
    if (Test-ZlmBindFailure -LogPath $log) { $why += '日志中出现端口绑定失败' }
    if ($why.Count -gt 0) {
        Fail ("流媒体服务（ZLM）启动后立即退出（$($why -join '；')），详见 $log`n" +
              "              最常见原因：RTP / SIP 那一段 **UDP 端口被 Windows 保留**——不是被别的软件占用。`n" +
              "              查：netsh int ipv4 show excludedportrange protocol=udp`n" +
              "              处理：把 config\config.env 的端口段挪出保留区（见包内 README 第四节），再 stop → start。")
    }
    Write-UvpOk "流媒体服务已启动（ZLM HTTP $($Ports.ZLM_HTTP)）"
}

function Start-Nginx {
    param([hashtable]$Ports)
    foreach ($tempDir in @('nginx-client-body', 'nginx-proxy', 'nginx-fastcgi', 'nginx-uwsgi', 'nginx-scgi')) {
        $p = Join-Path $RunDir $tempDir
        if (-not (Test-Path -LiteralPath $p)) { New-Item -ItemType Directory -Force -Path $p | Out-Null }
    }
    # ⛔ pid 文件先删：nginx 在 Windows 上不会覆盖已存在的 pid 文件。
    Remove-Item -LiteralPath $NginxPidFile -Force -ErrorAction SilentlyContinue
    # ⛔ -p / -c 给的是**绝对路径**（nginx 的 prefix 与配置文件路径没法用裸文件名规避），
    #   所以必须自己加引号，否则安装目录一带空格就被切碎（详见 Quote-NativeArg）。
    Start-Child -Name 'nginx' -Exe $NginxExe -WorkDir $NginxDir -LogPath (Join-Path $LogsDir 'nginx-console.log') `
        -PidPath $null -Arguments @('-p', (Quote-NativeArg $NginxDir), '-c', (Quote-NativeArg $NginxConf)) | Out-Null
    $entryPort = $Ports.HTTPS
    if (-not $script:HttpsEnabled) { $entryPort = $Ports.NGINX_HTTP }
    if (-not (Wait-Port -Port $entryPort -Tries 50 -SleepMs 200 -LogPath (Join-Path $LogsDir 'nginx-error.log'))) {
        $detail = ''
        $errLog = Join-Path $LogsDir 'nginx-error.log'
        if (Test-Path -LiteralPath $errLog) {
            $detail = (Get-Content -LiteralPath $errLog -Tail 8 | Out-String).Trim()
        }
        Fail "nginx 启动失败，详见 $errLog`n$detail"
    }
    Write-UvpOk "管理页面已就绪（HTTPS 开启=$script:HttpsEnabled，入口端口 $entryPort）"
}

# ---------------------------------------------------------------- 动作 ----
function Invoke-Start {
    Write-Banner
    $config = Get-Config
    $script:CurrentConfig = $config

    Write-UvpLog '阶段 1/3 · 环境检查'
    Test-Preconditions
    Test-PortsFree -Ports $config.Ports

    Write-UvpLog '阶段 2/3 · 初始化数据与配置'
    foreach ($dir in @($LogsDir, $RunDir, $DataDir, (Join-Path $LogsDir 'app'))) {
        if (-not (Test-Path -LiteralPath $dir)) { New-Item -ItemType Directory -Force -Path $dir | Out-Null }
    }
    Invoke-DatabaseInit
    Invoke-PortsSync -Config $config
    Invoke-NginxConfRender
    Invoke-TlsMaterial

    Write-UvpLog '阶段 3/3 · 启动服务'
    Start-Zlm -Ports $config.Ports
    Start-Nginx -Ports $config.Ports
    Start-Redis -Ports $config.Ports
    Start-Backend -Ports $config.Ports

    # 收尾：先给**端口规划**（技术明细，客户要拿去开防火墙），
    # 最后才是「启动完成 + 访问地址」—— 让地址停在屏幕最后、不被一堆端口号冲掉。
    Write-PortPlan -Ports $config.Ports
    Write-StartSummary -Ports $config.Ports -LanIP (Get-LanIP)
    Write-Host ''
}

function Invoke-Stop {
    Write-Banner
    Write-UvpLog '正在停止服务（顺序：管理页面 → 后端 → 流媒体 → 缓存）'
    # nginx 走 -s quit：让 master 通知 worker 收尾，避免留下占端口的孤儿 worker。
    # ⛔ 必须带 -p（否则 nginx 找不到 pid 文件，报 OpenEvent("Global\ngx_quit_…") failed (2)），
    #   且 -p 的值要去掉结尾反斜杠再加引号（见 Quote-NativeArg）。
    if (Test-Path -LiteralPath $NginxExe) {
        # ⛔⛔ 必须走 Invoke-NativeQuiet（内部临时降 ErrorActionPreference），
        #   不能写 `*> $null` 也不能写 `2>&1 | Out-Null` —— 后者实测**仍然会**把 nginx
        #   写往 stderr 的内容（如 OpenEvent failed / 配置告警）升格为终止性异常：
        #   stop 一上来就中断，连"管理页面（nginx）已停止"都打不出来，退出码却是 1。
        #   失败也不要紧 —— 紧随其后的 Get-PidByExe + Stop-ProcessTree 会按可执行文件
        #   路径兜底强杀。
        Invoke-NativeQuiet -Exe $NginxExe -Arguments @(
            '-p', (Quote-NativeArg $NginxDir), '-c', (Quote-NativeArg $NginxConf), '-s', 'quit')
    }
    Start-Sleep -Milliseconds 600
    foreach ($pidValue in (Get-PidByExe -ExePath $NginxExe)) { Stop-ProcessTree -ProcessId $pidValue }
    Remove-Item -LiteralPath $NginxPidFile -Force -ErrorAction SilentlyContinue
    Write-UvpDet '管理页面（nginx）已停止'

    Stop-Component -Label '后端' -PidPath $BackendPidFile -ExePath $ServerExe
    Stop-Component -Label '流媒体服务（ZLM）' -PidPath $ZlmPidFile -ExePath $ZlmExe

    # Redis 先走 redis-cli shutdown（会刷 AOF），再兜底杀进程
    $envMap = Read-EnvFileMap -Path $EnvFile
    # ⛔ PowerShell 不支持 `$函数名(参数)` 这种调用形式（那是 C 系语言的写法），
    #   必须写成 `(函数名 -参数 值)`；写成前者是**解析期语法错误**，不是运行期报错。
    $redisPort = [int](PortEnvDefaultSafe -EnvMap $envMap)
    if ((Test-Path -LiteralPath $RedisCli) -and (Test-TcpPort -Port $redisPort)) {
        Invoke-NativeQuiet -Exe $RedisCli -Arguments @('-p', "$redisPort", 'shutdown')
        Start-Sleep -Milliseconds 400
    }
    Stop-Component -Label 'Redis' -PidPath $RedisPidFile -ExePath $RedisExe

    # 兜底：把本包目录下的残留进程再扫一遍（pid 文件丢失场景）
    $leftover = @()
    foreach ($pair in @(@($ServerExe, 'uvp-server'), @($ZlmExe, 'MediaServer'), @($RedisExe, 'redis-server'), @($NginxExe, 'nginx'))) {
        $extra = Get-PidByExe -ExePath $pair[0]
        if ($extra.Count -gt 0) { $leftover += "$($pair[1]) (PID $($extra -join ','))" }
    }
    if ($leftover.Count -gt 0) {
        Write-UvpWarn ("仍有残留进程：" + ($leftover -join ' / '))
    } else {
        Write-UvpOk '已全部停止，无残留进程'
    }
    Write-Host ''
}

function PortEnvDefaultSafe {
    param([hashtable]$EnvMap)
    if ($EnvMap.ContainsKey('UVP_USER_SET_UVP_REDIS_PORT') -and $EnvMap.ContainsKey('UVP_REDIS_PORT')) {
        return [int]$EnvMap['UVP_REDIS_PORT']
    }
    return [int]$PortDefault['REDIS']
}

function Invoke-Status {
    $config = Get-Config
    $ports = $config.Ports
    Write-Host ''
    Write-UvpDet ("后端        " + (Format-Status -Alive (Test-TcpPort -Port $ports.HTTP) -Detail "端口 $($ports.HTTP)"))
    Write-UvpDet ("Redis       " + (Format-Status -Alive (Test-TcpPort -Port $ports.REDIS) -Detail "端口 $($ports.REDIS)"))
    Write-UvpDet ("流媒体 ZLM  " + (Format-Status -Alive (Test-TcpPort -Port $ports.ZLM_HTTP) -Detail "端口 $($ports.ZLM_HTTP)"))
    $scheme = 'https'; $entryPort = $ports.HTTPS
    if (-not $script:HttpsEnabled) { $scheme = 'http'; $entryPort = $ports.NGINX_HTTP }
    Write-UvpDet ("管理页面    " + (Format-Status -Alive (Test-TcpPort -Port $entryPort) -Detail "${scheme}://<本机IP>:$entryPort"))
    Write-Host ''
    if (-not (Test-TcpPort -Port $entryPort)) {
        Write-UvpDet "（管理页面未就绪时，可临时直连后端明文端口 http://127.0.0.1:$($ports.HTTP) 排查）"
        Write-Host ''
    }
}

function Format-Status {
    param([bool]$Alive, [string]$Detail)
    if ($Alive) { return ("{0}运行中{1}（{2}）" -f $C.ok, $C.rst, $Detail) }
    return ("{0}未运行{1}" -f $C.err, $C.rst)
}

# 忘记了登录口令时，把管理员口令还原成**出厂口令**并重新要求首次登录改密。
#
# 为什么需要它：平台上没有自助找回入口（登录页的「忘记密码」是注释掉的死链、
# 后端也没有重置接口），而管理员在首次登录时改成自己的口令后就只剩改库一条路。
# ⛔ Windows 客户机**不保证有 python3、包里也不带 sqlite3.exe** —— 以前只能让客户
#   把 data\uvp.db（含全部业务数据）发回来，或远程连进去手改；现在一条命令即可。
# 实现放在 uvp-setup.exe（纯 Go、离线、无外部依赖），本函数只负责拼参数与提示。
function Invoke-ResetPassword {
    Write-Host ''
    Write-UvpLog '重置登录口令'
    if (-not (Test-Path -LiteralPath $DbPath)) {
        Fail "找不到数据库 $DbPath —— 这台机器还没执行过 start（库里还没有账号）"
    }
    $demo = Get-DemoCredential
    # ⛔ 变量不能叫 `$args`：那是 PowerShell 的自动变量（函数实参），赋值会踩到它。
    $resetArgs = @('reset-admin', '--root', $Root, '--db', $DbPath, '--config', $ConfFile)
    if ($demo.User) { $resetArgs += @('--username', $demo.User) }
    & $SetupExe @resetArgs
    if ($LASTEXITCODE -ne 0) { Fail "重置口令失败（uvp-setup 退出码 $LASTEXITCODE）" }
    Write-Host ''
    # 口令是**每次登录现读库**的，所以不必重启；写"若仍失败再重启"是为了兜住
    # "库被占用导致写入没生效"这种极少数情况，别让现场卡在"到底要不要重启"上。
    Write-UvpDet '改完即可直接登录（无需重启）；若仍登不上，执行 .\uvp-gb28181-ctl.ps1 restart 后再试'
    Write-Host ''
}

function Show-Usage {
    Write-Banner
    Write-Host '用法: .\uvp-gb28181-ctl.ps1 <start|stop|restart|status|reset-password>'
    Write-Host '  也可以双击同目录的 start.cmd / stop.cmd / status.cmd / reset-password.cmd'
    Write-Host ''
}

switch ($Action.ToLower()) {
    'start'          { Invoke-Start }
    'stop'           { Invoke-Stop }
    'restart'        { Invoke-Stop; Invoke-Start }
    'restart-zlm'    { $config = Get-Config; Stop-Component -Label 'ZLM' -PidPath $ZlmPidFile -ExePath $ZlmExe; Start-Zlm -Ports $config.Ports }
    'status'         { Invoke-Status }
    'reset-password' { Invoke-ResetPassword }
    default          { Show-Usage; exit 2 }
}

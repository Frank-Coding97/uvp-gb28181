#Requires -Version 5.1
<#
  UVP-GB28181 统一视频接入平台 · Windows 绿色安装包 —— 控制脚本

  用法（包目录下执行）：
      .\uvp-gb28181-ctl.ps1 start      启动全部组件
      .\uvp-gb28181-ctl.ps1 stop       停止全部组件（零残留）
      .\uvp-gb28181-ctl.ps1 restart    重启
      .\uvp-gb28181-ctl.ps1 status     查看运行状态
  也可以直接双击同目录的 start.cmd / stop.cmd / status.cmd。

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
# 三级缩进：`[uvp] 阶段行` / `      ✓ 结果行` / `        · 明细行`（6 格 = "[uvp] " 宽度）
$script:UseColor = (-not [Console]::IsOutputRedirected) -and (-not $env:NO_COLOR)
$C = @{ rst = ''; tag = ''; ok = ''; warn = ''; err = ''; dim = '' }
if ($script:UseColor) {
    $C.rst  = [char]27 + '[0m'
    $C.tag  = [char]27 + '[36m'
    $C.ok   = [char]27 + '[32m'
    $C.warn = [char]27 + '[33m'
    $C.err  = [char]27 + '[31m'
    $C.dim  = [char]27 + '[90m'
}

function Write-UvpLog  { param([string]$Message) Write-Host ("{0}[uvp]{1} {2}" -f $C.tag, $C.rst, $Message) }
function Write-UvpDet  { param([string]$Message) Write-Host ("      {0}" -f $Message) }
function Write-UvpOk   { param([string]$Message) Write-Host ("      {0}✓{1} {2}" -f $C.ok, $C.rst, $Message) }
function Write-UvpWarn { param([string]$Message) Write-Host ("      {0}!{1} {2}" -f $C.warn, $C.rst, $Message) }
function Write-UvpErr  { param([string]$Message) Write-Host ("      {0}✗{1} {2}" -f $C.err, $C.rst, $Message) }

function Fail {
    param([string]$Message)
    Write-Host ("{0}[uvp][ERROR]{1} {2}" -f $C.err, $C.rst, $Message)
    exit 1
}

# 写 UTF-8 **不带 BOM**。⛔ PowerShell 5.1 的 `Set-Content -Encoding UTF8` 会**加 BOM**，
# 而 BOM 一旦落进 redis.conf 这种逐行解析的配置文件里，第一行就变成 `\ufeff#...`
# —— 有些解析器认，有些直接报错；干脆统一写成无 BOM。
function Write-Utf8NoBom {
    param([string]$Path, [string]$Text)
    [System.IO.File]::WriteAllText($Path, $Text, (New-Object System.Text.UTF8Encoding $false))
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
# ⛔ 必须与 deploy/standalone/PORTS.md、Linux 版 ctl 的默认值**完全一致**：
#   51000-51064 连续无空洞，防火墙一条规则即可覆盖。
$PortDefault = @{
    HTTP           = 51002   # 后端 HTTP
    REDIS          = 51003   # Redis（仅回环）
    HTTPS          = 51000   # nginx HTTPS（浏览器入口）
    NGINX_HTTP     = 51001   # nginx HTTP（跳 HTTPS + 明文 API）
    ZLM_HTTP       = 51004
    ZLM_SSL        = 51007
    ZLM_RTSP       = 51005
    ZLM_RTMP       = 51006
    ZLM_RTC        = 51008
    ZLM_SIGNALING  = 51009
    ZLM_SIGNALING_SSL = 51010
    ZLM_SRT        = 51011
    ZLM_ONVIF      = 51012
    ZLM_ICE        = 51013
    ZLM_RTP_PROXY  = 51014
}
$ZlmRtpRange = '51014-51063'
$SipPort     = 51064

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
    return @{ Ports = $ports; EnvMap = $envMap; UserSet = @($ref.Value) }
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
    $proc = Start-Process -FilePath $Exe -ArgumentList $Arguments -WorkingDirectory $WorkDir `
        -WindowStyle Hidden -PassThru -RedirectStandardOutput $out -RedirectStandardError $err
    if ($PidPath) { Set-Content -LiteralPath $PidPath -Value $proc.Id -Encoding ASCII }
    return $proc
}

function Stop-ProcessTree {
    param([int]$ProcessId, [int]$GraceMs = 4000)
    if ($ProcessId -le 0) { return }
    if (-not (Test-PidAlive -ProcessId $ProcessId)) { return }
    # ⛔ 先礼貌后强杀：Windows 没有 SIGTERM，Stop-Process 就是 TerminateProcess；
    #   用 taskkill /T 是为了把子进程（Cygwin 版 redis 的子 shell 等）一起带走。
    & taskkill.exe /PID $ProcessId /T *> $null
    $deadline = (Get-Date).AddMilliseconds($GraceMs)
    while ((Get-Date) -lt $deadline) {
        if (-not (Test-PidAlive -ProcessId $ProcessId)) { return }
        Start-Sleep -Milliseconds 200
    }
    & taskkill.exe /PID $ProcessId /T /F *> $null
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
    Write-UvpDet ("  {0} 管理页面（HTTPS）      http://<本机IP>:{1}  → https://<本机IP>:{2}" -f '·', $Ports.NGINX_HTTP, $Ports.HTTPS)
    Write-UvpDet ("  {0} 后端 API（仅本机）     http://127.0.0.1:{1}" -f '·', $Ports.HTTP)
    Write-UvpDet ("  {0} Redis（仅本机）        127.0.0.1:{1}" -f '·', $Ports.REDIS)
    Write-UvpDet ("  {0} 流媒体（ZLM）          {1}-{2} / RTP {3} / SIP {4}" -f '·', $Ports.ZLM_HTTP, $Ports.ZLM_ICE, $ZlmRtpRange, $SipPort)
    Write-Host ''
    $tcp = "$($Ports.HTTPS)-$SipPort"
    Write-UvpDet '防火墙放行命令（管理员 PowerShell，只需执行一次）：'
    Write-UvpDet ("  New-NetFirewallRule -DisplayName 'UVP' -Direction Inbound -Action Allow " +
                  "-Protocol TCP -LocalPort $tcp")
    Write-UvpDet ("  New-NetFirewallRule -DisplayName 'UVP-UDP' -Direction Inbound -Action Allow " +
                  "-Protocol UDP -LocalPort 51011,51013,51014-51064")
}

function Write-StartSummary {
    param([hashtable]$Ports, [string]$LanIP)
    Write-Host ''
    Write-UvpDet ("访问地址：https://{0}:{1}   （自签证书，浏览器首次会提示不受信任，选择继续即可）" -f $LanIP, $Ports.HTTPS)
    Write-UvpDet '首次登录：admin / admin123（登录后请立即修改密码）'
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
    Write-UvpDet '（提示：SIP 端口 51064 由平台内的引导页录入，不在本预检范围内）'
}

function Test-PortsFree {
    param([hashtable]$Ports)
    $tcpPorts = New-Object System.Collections.Generic.List[int]
    $udpPorts = New-Object System.Collections.Generic.List[int]
    foreach ($p in @($Ports.HTTPS, $Ports.NGINX_HTTP, $Ports.HTTP, $Ports.REDIS,
                     $Ports.ZLM_HTTP, $Ports.ZLM_RTSP, $Ports.ZLM_RTMP, $Ports.ZLM_SSL,
                     $Ports.ZLM_RTC, $Ports.ZLM_SIGNALING, $Ports.ZLM_SIGNALING_SSL,
                     $Ports.ZLM_ONVIF, $Ports.ZLM_ICE)) { $tcpPorts.Add([int]$p) }
    $udpPorts.Add([int]$Ports.ZLM_SRT)
    $udpPorts.Add([int]$Ports.ZLM_ICE)
    $rtpStart = [int]($ZlmRtpRange.Split('-')[0])
    $rtpEnd = [int]($ZlmRtpRange.Split('-')[1])
    for ($p = $rtpStart; $p -le $rtpEnd; $p++) {
        $tcpPorts.Add($p)
        $udpPorts.Add($p)
    }
    # ⛔ SIP（51064）**不在预检内**：它的端口由平台引导页录入（存 gb_sip_config 表），
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
    Write-UvpOk "端口预检通过（TCP $($Ports.HTTPS)-$SipPort + UDP 51011/51013/51014-$SipPort）"
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
    $ips = @(Get-LanIP)
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
        $pong = & $RedisCli -p $Ports.REDIS ping 2>$null
        if ($pong -match 'PONG') { $ok = $true; break }
    }
    if (-not $ok) { Fail "Redis 启动失败，详见 $log" }
    Write-UvpOk "Redis 已启动（127.0.0.1:$($Ports.REDIS)，AOF 落盘 $redisDataDir）"
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
    if (-not (Wait-Port -Port $Ports.HTTPS -Tries 50 -SleepMs 200 -LogPath (Join-Path $LogsDir 'nginx-error.log'))) {
        $detail = ''
        $errLog = Join-Path $LogsDir 'nginx-error.log'
        if (Test-Path -LiteralPath $errLog) {
            $detail = (Get-Content -LiteralPath $errLog -Tail 8 | Out-String).Trim()
        }
        Fail "nginx 启动失败，详见 $errLog`n$detail"
    }
    Write-UvpOk "管理页面已就绪（https://<本机IP>:$($Ports.HTTPS)）"
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

    Write-StartSummary -Ports $config.Ports -LanIP (Get-LanIP)
    Write-PortPlan -Ports $config.Ports
    Write-Host ("{0}[uvp]{1} 启动完成。" -f $C.ok, $C.rst)
    Write-Host ''
}

function Invoke-Stop {
    Write-Banner
    Write-UvpLog '正在停止服务（顺序：管理页面 → 后端 → 流媒体 → 缓存）'
    # nginx 走 -s quit：让 master 通知 worker 收尾，避免留下占端口的孤儿 worker。
    # ⛔ 必须带 -p（否则 nginx 找不到 pid 文件，报 OpenEvent("Global\ngx_quit_…") failed (2)），
    #   且 -p 的值要去掉结尾反斜杠再加引号（见 Quote-NativeArg）。
    if (Test-Path -LiteralPath $NginxExe) {
        & $NginxExe -p (Quote-NativeArg $NginxDir) -c (Quote-NativeArg $NginxConf) -s quit *> $null
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
        & $RedisCli -p $redisPort shutdown *> $null
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
    Write-UvpDet ("管理页面    " + (Format-Status -Alive (Test-TcpPort -Port $ports.HTTPS) -Detail "https://<本机IP>:$($ports.HTTPS)"))
    Write-Host ''
    if (-not (Test-TcpPort -Port $ports.HTTPS)) {
        Write-UvpDet "（管理页面未就绪时，可临时直连后端明文端口 http://127.0.0.1:$($ports.HTTP) 排查）"
        Write-Host ''
    }
}

function Format-Status {
    param([bool]$Alive, [string]$Detail)
    if ($Alive) { return ("{0}运行中{1}（{2}）" -f $C.ok, $C.rst, $Detail) }
    return ("{0}未运行{1}" -f $C.err, $C.rst)
}

function Show-Usage {
    Write-Banner
    Write-Host '用法: .\uvp-gb28181-ctl.ps1 <start|stop|restart|status>'
    Write-Host '  也可以双击同目录的 start.cmd / stop.cmd / status.cmd'
    Write-Host ''
}

switch ($Action.ToLower()) {
    'start'   { Invoke-Start }
    'stop'    { Invoke-Stop }
    'restart' { Invoke-Stop; Invoke-Start }
    'status'  { Invoke-Status }
    default   { Show-Usage; exit 2 }
}

#Requires -Version 5.1
<#
  UVP-GB28181 统一视频接入平台 · Windows 绿色安装包 —— 出包脚本

  用法（在 Windows 出包机上，普通 PowerShell 即可）：
      # 最简：给一个装着第三方二进制的目录，其余全自动
      .\build-standalone.ps1 -VendorDir F:\uvp-release\vendor-src

      # 带自带工具链（Go / Node 不在 PATH 里时）
      .\build-standalone.ps1 -VendorDir F:\uvp-release\vendor-src -ToolchainDir F:\uvp-release\toolchain

      # 只改配置重出包（跳过编译，但新鲜度断言仍会跑）
      .\build-standalone.ps1 -VendorDir ... -SkipBuild -SkipFrontend

  产物：
      <OutDir>\uvp-gb28181-windows-amd64-<版本>.zip
      <OutDir>\uvp-gb28181-windows-amd64-<版本>.zip.sha256

  -VendorDir 里必须有三样（Linux 版对应 deploy/standalone/bin/{redis,nginx,zlm}，
  Windows 版不入库，按平台单独准备）：
      redis\redis-server.exe  redis\redis-cli.exe  redis\*.dll   （Cygwin 版 redis）
      zlm\MediaServer.exe     zlm\config.ini       zlm\www\      zlm\default.pem
      nginx\nginx.exe         nginx\conf\  nginx\html\ ...       （nginx for Windows 发行包）

  ⛔⛔ 包内契约与 Linux 版**逐条对齐**（deploy/standalone/uvp-gb28181-ctl.sh 顶部注释）：
      bin\      只放我们自己编的程序（uvp-server.exe / uvp-setup.exe）
      vendor\   第三方运行时，每个组件自成一体（redis / nginx / zlm）
      config\   config.yml（后端读）+ config.env（只走环境变量，ctl 注入子进程）
      data\     uvp.db + redis AOF + secrets\
      logs\ run\ resource\
      顶层只允许：uvp-gb28181-ctl.ps1、start.cmd / stop.cmd / status.cmd、
                  README.md、version.json

  ⛔⛔ 本文件与 uvp-gb28181-ctl.ps1 **必须保存为 UTF-8 with BOM**。
     Windows PowerShell 5.1 读 .ps1 时若没有 BOM，会按系统 ANSI 代码页（简中=GBK）
     解码 —— 中文注释与字符串变成乱码，轻则输出花屏、重则**语法错误**（引号被吃掉）。
     脚本末尾有硬校验：包内任何 .ps1 缺 BOM 一律拒绝打包。
#>
[CmdletBinding()]
param(
    # 装着 redis\ zlm\ nginx\ 三个子目录的目录。默认取本目录下的 bin\。
    [string]$VendorDir = '',
    # 出包产物目录，默认 <仓库根>\release-output
    [string]$OutDir = '',
    # 包版本号，默认读 server\version.json
    [string]$Version = '',
    # 可选：自带 Go / Node 工具链目录（含 go\bin\go.exe 与 node\node.exe）
    [string]$ToolchainDir = '',
    # Python 解释器（供 make-config.py 生成 config.yml 用）
    [string]$PythonExe = 'python',
    # 扫码接入基址里「设备可达的地址」；留空则由 make-config.py 探测本机局域网 IP
    [string]$QrProvisionHost = '',
    # 跳过后端编译（产物新鲜度断言仍会执行）
    [switch]$SkipBuild,
    # 跳过前端构建（复用 web\dist）
    [switch]$SkipFrontend
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
try { [Console]::OutputEncoding = New-Object System.Text.UTF8Encoding $false } catch { }
$OutputEncoding = [Console]::OutputEncoding

# ------------------------------------------------------------------ 输出样式 ----
# 三级缩进与包内 ctl.ps1 完全一致：`[build] 阶段行` / `      ✓ 结果` / `        · 明细`
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

function Log  { param([string]$m) Write-Host ("{0}[build]{1} {2}" -f $C.tag, $C.rst, $m) }
function Det  { param([string]$m) Write-Host ("      {0}" -f $m) }
function Ok   { param([string]$m) Write-Host ("      {0}✓{1} {2}" -f $C.ok, $C.rst, $m) }
function Warn { param([string]$m) Write-Host ("      {0}!{1} {2}" -f $C.warn, $C.rst, $m) }
function Fail {
    param([string]$m)
    Write-Host ("{0}[build][ERROR]{1} {2}" -f $C.err, $C.rst, $m)
    exit 1
}

# 跑外部命令并检查退出码。
# ⛔ 必须临时把 ErrorActionPreference 降回 Continue：PS 5.1 下若在 Stop 状态下
#   把原生命令的 stderr 重定向（2>&1）或子进程往 stderr 写，会被当成**终止性错误**
#   抛出 —— 而 go/vite 正常运行时也会往 stderr 写进度，于是"构建明明成功却被判失败"。
function Invoke-Native {
    param([string]$FilePath, [string[]]$Arguments, [string]$What)
    $prev = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        & $FilePath @Arguments
        $code = $LASTEXITCODE
    } finally {
        $ErrorActionPreference = $prev
    }
    if ($code -ne 0) { Fail "$What 失败（$FilePath 退出码 $code）" }
}

function Write-Utf8NoBom {
    param([string]$Path, [string]$Text)
    [System.IO.File]::WriteAllText($Path, $Text, (New-Object System.Text.UTF8Encoding $false))
}

function Test-Utf8Bom {
    param([string]$Path)
    $bytes = [System.IO.File]::ReadAllBytes($Path)
    return ($bytes.Length -ge 3 -and $bytes[0] -eq 0xEF -and $bytes[1] -eq 0xBB -and $bytes[2] -eq 0xBF)
}

# 递归复制目录树（源目录的**内容** → 目标目录）。
#
# ⛔⛔ 为什么不用 `Copy-Item -Path '<src>\*' -Destination <dst> -Recurse -Force`：
#   PowerShell 5.1 在「通配符源 + 目标目录尚不存在」时会走一套很坑的推断 ——
#   它把**第一个源项**当成目标本身（相当于重命名），后续项再往里塞时就会抛
#     `PSArgumentException: 无法将容器复制到现有叶项。`
#   实测就死在这一步（出包走到"复制前端产物"直接中断，包只装进去一个 loading.css）。
#   这里是显式按文件/目录逐个建 + 逐个拷，语义完全确定，不再依赖 PS 的推断。
function Copy-Tree {
    param([string]$Source, [string]$Destination)
    if (-not (Test-Path -LiteralPath $Source)) { Fail "Copy-Tree 源不存在：$Source" }
    if (-not (Test-Path -LiteralPath $Destination)) {
        New-Item -ItemType Directory -Force -Path $Destination | Out-Null
    }
    $srcLen = $Source.Length + 1
    foreach ($item in (Get-ChildItem -LiteralPath $Source -Recurse -Force)) {
        $rel = $item.FullName.Substring($srcLen)
        $target = Join-Path $Destination $rel
        if ($item.PSIsContainer) {
            if (-not (Test-Path -LiteralPath $target)) { New-Item -ItemType Directory -Force -Path $target | Out-Null }
        } else {
            $parent = Split-Path -Parent $target
            if (-not (Test-Path -LiteralPath $parent)) { New-Item -ItemType Directory -Force -Path $parent | Out-Null }
            Copy-Item -LiteralPath $item.FullName -Destination $target -Force
        }
    }
}

function Format-Size {
    param([long]$Bytes)
    if ($Bytes -ge 1GB) { return ("{0:N2} GB" -f ($Bytes / 1GB)) }
    if ($Bytes -ge 1MB) { return ("{0:N1} MB" -f ($Bytes / 1MB)) }
    if ($Bytes -ge 1KB) { return ("{0:N0} KB" -f ($Bytes / 1KB)) }
    return "$Bytes B"
}

function New-RandomBase64 {
    param([int]$ByteCount = 32)
    $buf = New-Object byte[] $ByteCount
    $rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
    try { $rng.GetBytes($buf) } finally { $rng.Dispose() }
    return [Convert]::ToBase64String($buf)
}

# OpenAPI 主密钥的格式是硬约束：后端用 base64.RawURLEncoding.Strict() 解码，
# 要求 **32 字节的无填充 base64url** —— 标准 base64 的 + / = 会被直接拒。
function New-RandomBase64Url {
    param([int]$ByteCount = 32)
    return (New-RandomBase64 -ByteCount $ByteCount).TrimEnd('=').Replace('+', '-').Replace('/', '_')
}

# ------------------------------------------------------------------ 常量 ----
# ⛔ 必须与 deploy/standalone/PORTS.md、两个平台的 ctl 默认值**完全一致**：
#   51000-51064 连续无空洞，一条防火墙规则即可覆盖。
$HttpsPort       = 51000   # nginx HTTPS（浏览器入口）
$PlainHttpPort   = 51001   # nginx HTTP（跳 HTTPS + 明文 API，供设备扫码）
$HttpPort        = 51002   # 后端 HTTP
$RedisPort       = 51003   # Redis（仅回环）
$SipPort         = 51064   # SIP 信令（由平台引导页录入，不在端口预检内）
$ZlmRtpRange     = '51014-51063'

# ZLM 身份固定值：与 Linux 版**逐字相同**（config.ini 与 config.yml 必须对得上，
# 否则平台调 ZLM 全被拒，而 ZLM 进程本身看起来完全正常）。
$ZlmSecret        = 'uvp-9f3c7a1e5d84b206c1a9e4f7b2d6c805'
$ZlmMediaServerId = 'uvp-media-server-0001'

$OpenApiMasterKeyId = 'uvp-openapi-k1'

# ------------------------------------------------------------------ 路径 ----
$RepoRoot  = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..\..')).Path
$ServerDir = Join-Path $RepoRoot 'server'
$WebDir    = Join-Path $RepoRoot 'web'
$DeployDir = Join-Path $RepoRoot 'deploy\standalone'
$WinDir    = $PSScriptRoot

$SqliteDir = Join-Path $ServerDir 'resource\database\sqlitebaseline'
$VersionJson = Join-Path $ServerDir 'version.json'
$ExampleYml  = Join-Path $ServerDir 'config\config.example.yml'
$MakeConfig  = Join-Path $DeployDir 'make-config.py'

if (-not $VendorDir) { $VendorDir = Join-Path $WinDir 'bin' }
$VendorDir = [System.IO.Path]::GetFullPath($VendorDir)
$VendorRedis = Join-Path $VendorDir 'redis'
$VendorZlm   = Join-Path $VendorDir 'zlm'
$VendorNginx = Join-Path $VendorDir 'nginx'

if (-not $OutDir) { $OutDir = Join-Path $RepoRoot 'release-output' }
$OutDir = [System.IO.Path]::GetFullPath($OutDir)

if ($ToolchainDir) {
    $ToolchainDir = [System.IO.Path]::GetFullPath($ToolchainDir)
    $goBinDir   = Join-Path $ToolchainDir 'go\bin'
    $nodeBinDir = Join-Path $ToolchainDir 'node'
    $env:PATH = $goBinDir + ';' + $nodeBinDir + ';' + $env:PATH
    $env:GOROOT = Join-Path $ToolchainDir 'go'
}
# 目标平台写死成 windows/amd64：包名里的架构与真正编出来的架构必须同源。
# CGO_ENABLED=0：SQLite 引擎是纯 Go（modernc.org/sqlite），不需要 libcgo。
$env:GOTOOLCHAIN = 'local'
$env:GOFLAGS     = '-mod=mod'
$env:CGO_ENABLED = '0'
$env:GOOS        = 'windows'
$env:GOARCH      = 'amd64'

# ⛔⛔ Windows 上 Python 的 stdout/stderr 默认按**系统 ANSI 代码页**编码（简中 = GBK），
#   而 deploy\standalone\make-config.py 会打印 ✅ / ❌ 这类非 GBK 字符 ——
#   于是它在**最后一行**抛 `UnicodeEncodeError: 'gbk' codec can't encode character '\u2705'`，
#   把出包硬生生打断。报错栈还在 python 内部，看上去像"那个脚本坏了"，
#   其实是**终端编码**问题，跟脚本逻辑无关。
#   ⇒ 这里把 Python 的 IO 编码钉成 UTF-8。这是出包机的环境问题，不该去改共享脚本
#     （make-config.py 在 Linux/macOS 上跑得好好的，为 Windows 改它属于本末倒置）。
$env:PYTHONUTF8       = '1'   # Python 3.7+ 的 UTF-8 模式
$env:PYTHONIOENCODING = 'utf-8'

if (-not $Version) {
    if (-not (Test-Path -LiteralPath $VersionJson)) { Fail "找不到 $VersionJson" }
    $Version = [string]((Get-Content -LiteralPath $VersionJson -Raw -Encoding UTF8 | ConvertFrom-Json).version)
}
if ([string]::IsNullOrWhiteSpace($Version)) { Fail '读不到版本号（server\version.json 里的 version 字段）' }

$PkgStem  = "uvp-gb28181-windows-amd64-$Version"
$ZipPath  = Join-Path $OutDir "$PkgStem.zip"
$ShaPath  = Join-Path $OutDir "$PkgStem.zip.sha256"

# ------------------------------------------------------------------ 前置检查 ----
Log '阶段 0/4 · 前置检查'

$missing = @()
foreach ($item in @(
        @{ P = $ServerDir;        N = "仓库后端目录 server\" },
        @{ P = $WebDir;           N = "仓库前端目录 web\" },
        @{ P = $ExampleYml;       N = "server\config\config.example.yml" },
        @{ P = $MakeConfig;       N = "deploy\standalone\make-config.py" },
        @{ P = $VersionJson;      N = "server\version.json" },
        @{ P = (Join-Path $SqliteDir 'baseline.sql'); N = "server\resource\database\sqlitebaseline\baseline.sql" },
        @{ P = (Join-Path $WinDir 'uvp-gb28181-ctl.ps1'); N = "deploy\standalone-windows\uvp-gb28181-ctl.ps1" },
        @{ P = (Join-Path $WinDir 'nginx.conf.template'); N = "deploy\standalone-windows\nginx.conf.template" },
        @{ P = (Join-Path $WinDir 'start.cmd');   N = "deploy\standalone-windows\start.cmd" },
        @{ P = (Join-Path $WinDir 'stop.cmd');    N = "deploy\standalone-windows\stop.cmd" },
        @{ P = (Join-Path $WinDir 'status.cmd');  N = "deploy\standalone-windows\status.cmd" },
        @{ P = (Join-Path $VendorRedis 'redis-server.exe'); N = "-VendorDir\redis\redis-server.exe" },
        @{ P = (Join-Path $VendorRedis 'redis-cli.exe');    N = "-VendorDir\redis\redis-cli.exe" },
        @{ P = (Join-Path $VendorZlm 'MediaServer.exe');    N = "-VendorDir\zlm\MediaServer.exe" },
        @{ P = (Join-Path $VendorZlm 'config.ini');         N = "-VendorDir\zlm\config.ini" },
        @{ P = (Join-Path $VendorNginx 'nginx.exe');        N = "-VendorDir\nginx\nginx.exe" }
    )) {
    if (-not (Test-Path -LiteralPath $item.P)) { $missing += $item.N }
}
if ($missing.Count -gt 0) {
    Fail ("缺少必需文件：`n        - " + ($missing -join "`n        - ") +
          "`n      -VendorDir 需要含 redis\ zlm\ nginx\ 三个子目录（分别来自" +
          "酒酒平台 Windows 包回提的 redis、二开 ZLM、nginx for Windows 发行包）。")
}
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Fail '找不到 go 工具链（用 -ToolchainDir 指定，或把它加进 PATH）'
}
if (-not (Get-Command $PythonExe -ErrorAction SilentlyContinue)) {
    Fail "找不到 Python（$PythonExe）—— 生成 config.yml 需要它（make-config.py）"
}
Ok "仓库结构 / 第三方二进制 / 工具链齐备（vendor: $VendorDir）"

# ------------------------------------------------------------------ 编译 ----
$ServerExeOut = Join-Path $ServerDir 'bin\uvp-server.exe'
$SetupExeOut  = Join-Path $ServerDir 'bin\uvp-setup.exe'
$DistIndex    = Join-Path $WebDir 'dist\index.html'

if ($SkipBuild) {
    Warn '已跳过编译（-SkipBuild）—— **包里的产物可能比源码旧**，下面靠新鲜度断言兜底'
} else {
    Log '阶段 1/4 · 编译后端二进制（windows/amd64）'
    $devBin = Join-Path $ServerDir 'bin'
    if (-not (Test-Path -LiteralPath $devBin)) { New-Item -ItemType Directory -Force -Path $devBin | Out-Null }
    $goExe = (Get-Command go).Source
    Push-Location $ServerDir
    try {
        Invoke-Native -FilePath $goExe -What '后端编译（uvp-server）' `
            -Arguments @('build', '-ldflags', '-s -w', '-o', $ServerExeOut, '.')
        Invoke-Native -FilePath $goExe -What '辅助工具编译（uvp-setup）' `
            -Arguments @('build', '-ldflags', '-s -w', '-o', $SetupExeOut, '.\cmd\uvp-setup')
    } finally { Pop-Location }
    Ok "uvp-server.exe $(Format-Size (Get-Item $ServerExeOut).Length) · uvp-setup.exe $(Format-Size (Get-Item $SetupExeOut).Length)"

    Log '阶段 1/4 · 构建前端产物（SIP 默认端口 51064 构建期注入）'
    $viteJs = Join-Path $WebDir 'node_modules\vite\bin\vite.js'
    if (-not (Test-Path -LiteralPath $viteJs)) {
        Fail "找不到 $viteJs —— 先在 web\ 下执行 pnpm install"
    }
    $nodeCmd = Get-Command node -ErrorAction SilentlyContinue
    if (-not $nodeCmd) { Fail '找不到 node（用 -ToolchainDir 指定，或把它加进 PATH）' }
    $nodeExe = $nodeCmd.Source
    $distDir = Join-Path $WebDir 'dist'
    if (Test-Path -LiteralPath $distDir) { Remove-Item -LiteralPath $distDir -Recurse -Force }
    # ⛔ 用 .env.production.local 注入（vite 只在 .env.[mode] / .env.[mode].local 里保证读得到，
    #   而 vite build 的 mode 就是 production）；`.local` 已被 web\.gitignore 的 *.local 忽略
    #   ⇒ 不会污染工作区。构建结束（无论成败）一定要删掉它。
    $envLocal = Join-Path $WebDir '.env.production.local'
    Write-Utf8NoBom -Path $envLocal -Text "VITE_DEFAULT_SIP_PORT=$SipPort`r`n"
    try {
        Push-Location $WebDir
        try {
            # ⛔ 直接调 node 跑 vite.js，**不走 `pnpm run build:prod`** ——
            #   那条脚本前面挂着 vue-tsc（全量类型检查），与 Linux 版行为不一致
            #   且会把既有的类型报错变成出包失败。
            Invoke-Native -FilePath $nodeExe -What '前端构建（vite build）' -Arguments @($viteJs, 'build')
        } finally { Pop-Location }
    } finally {
        Remove-Item -LiteralPath $envLocal -Force -ErrorAction SilentlyContinue
    }
    if (-not (Test-Path -LiteralPath $DistIndex)) { Fail "前端构建后仍没有 $DistIndex" }
    # ⛔⛔ 读回断言：注入没生效必须**当场报错**。否则包照常出、引导页预填的还是 5061，
    #   而"SIP 在 51064"只存在于文档里，现场根本发现不了。
    $hit = Get-ChildItem -LiteralPath $distDir -Recurse -File -ErrorAction SilentlyContinue |
        Select-String -Pattern $SipPort -SimpleMatch -List -ErrorAction SilentlyContinue
    if (-not $hit) {
        Fail "前端产物里找不到注入的 SIP 默认端口 $SipPort —— VITE_DEFAULT_SIP_PORT 未生效" +
             "（变量名或 vite 读 env 的方式有变）。"
    }
    Ok "前端产物已生成，SIP 默认端口 $SipPort 读回校验通过"
}

# ---- 产物新鲜度断言（无条件执行）----
#
# ⛔ 为什么 -SkipBuild 也不能跳过：跳过编译恰恰是「产物可能没重建」的场景。
# ⭐ 判据用「产物 mtime 是否早于**最新改动的源码**」，而不是「早于脚本运行时间」——
#   后者在刚 build 完时会误报，也抓不到"构建了但漏编了某文件"。
# ⛔ 必须排除构建期自动生成的声明文件（web\src\auto-import.d.ts 由 vite 插件每次重写，
#   而后端二进制在前端构建**之前**编译 ⇒ 拿它当判据每次出包都误报）。
# ⛔ 也必须排除 _test.go / *.test.ts / *.spec.ts：测试文件不编进二进制，
#   但提交钩子会刷新它们的 mtime ⇒ 出包被永久卡死。
function Assert-Fresh {
    param([string]$Artifact, [string]$Label)
    if (-not (Test-Path -LiteralPath $Artifact)) { Fail "$Label 不存在：$Artifact" }
    $cutoff = (Get-Item -LiteralPath $Artifact).LastWriteTimeUtc
    $newest = $null
    foreach ($root in @($ServerDir, (Join-Path $WebDir 'src'))) {
        if (-not (Test-Path -LiteralPath $root)) { continue }
        $cand = Get-ChildItem -LiteralPath $root -Recurse -File -ErrorAction SilentlyContinue |
            Where-Object {
                $_.Extension -in '.go', '.vue', '.ts', '.scss' -and
                $_.Name -notlike '*.d.ts' -and
                $_.Name -notlike '*_test.go' -and
                $_.Name -notlike '*.test.ts' -and
                $_.Name -notlike '*.spec.ts' -and
                $_.LastWriteTimeUtc -gt $cutoff
            } | Sort-Object -Property LastWriteTimeUtc -Descending | Select-Object -First 1
        if ($cand) { $newest = $cand; break }
    }
    if ($newest) {
        Fail ("$Label 比源码旧，拒绝出包（否则会打出一个「看起来全新、跑起来是旧的」包）。`n" +
              "     产物: $Artifact`n" +
              "     更新的源码: $($newest.FullName)`n" +
              "     解决：去掉 -SkipBuild 让脚本重新编译。")
    }
    Ok "$Label 新鲜度 OK"
}
Log '阶段 1/4 · 产物新鲜度断言'
Assert-Fresh -Artifact $ServerExeOut -Label '后端二进制'
Assert-Fresh -Artifact $SetupExeOut  -Label 'uvp-setup 二进制'
Assert-Fresh -Artifact $DistIndex    -Label '前端产物'

# ------------------------------------------------------------------ 装配 ----
Log '阶段 2/4 · 装配包目录'

$Stage = Join-Path ([System.IO.Path]::GetTempPath()) ("uvp-standalone-win-" + [guid]::NewGuid().ToString('N'))
$Pkg   = Join-Path $Stage $PkgStem
# ⛔ 临时校验脚本必须放在 $Stage **外面**：打包时是按 $Stage 递归枚举条目的，
#   放在里面会被一起压进包根（`_avatar_check.py` 出现在客户包里）。
$HelperDir = Join-Path ([System.IO.Path]::GetTempPath()) ("uvp-build-helpers-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Force -Path $Stage, $HelperDir | Out-Null
foreach ($d in @('bin', 'config', 'data\redis', 'logs', 'run',
                 'resource\public', 'resource\baseline',
                 'vendor\redis', 'vendor\nginx', 'vendor\zlm')) {
    New-Item -ItemType Directory -Force -Path (Join-Path $Pkg $d) | Out-Null
}

# ---- bin\：只放我们自己编的程序 ----
Copy-Item -LiteralPath $ServerExeOut -Destination (Join-Path $Pkg 'bin\uvp-server.exe') -Force
Copy-Item -LiteralPath $SetupExeOut  -Destination (Join-Path $Pkg 'bin\uvp-setup.exe')  -Force
Det 'bin\uvp-server.exe · bin\uvp-setup.exe'

# ---- vendor\redis：包内自带，只连回环 ----
Copy-Tree -Source $VendorRedis -Destination (Join-Path $Pkg 'vendor\redis')
Det 'vendor\redis\（redis-server / redis-cli / cygwin 运行库）'

# ---- vendor\zlm：二开 ZLM。Windows 版是**静态链接**，没有 Linux 那份 lib\ ----
Copy-Item -Path (Join-Path $VendorZlm 'MediaServer.exe') -Destination (Join-Path $Pkg 'vendor\zlm\') -Force
Copy-Item -LiteralPath (Join-Path $VendorZlm 'config.ini') -Destination (Join-Path $Pkg 'vendor\zlm\config.ini') -Force
if (Test-Path -LiteralPath (Join-Path $VendorZlm 'default.pem')) {
    Copy-Item -LiteralPath (Join-Path $VendorZlm 'default.pem') -Destination (Join-Path $Pkg 'vendor\zlm\default.pem') -Force
}
if (Test-Path -LiteralPath (Join-Path $VendorZlm 'www')) {
    Copy-Tree -Source (Join-Path $VendorZlm 'www') -Destination (Join-Path $Pkg 'vendor\zlm\www')
}
Det 'vendor\zlm\（MediaServer.exe / config.ini / www）'

# ---- ZLM 身份在**源头侧**固定 ----
# ⛔ 必须改包内这份 config.ini，而不是靠启动时对齐：mediaServerId 的默认值是占位符
#   `your_server_id`，而 ZLM 的 setServerConfig 会把平台下发的值**持久化回这个文件**
#   —— 一旦落盘就固化。secret 同理：它必须与 config.yml 逐字相同。
# ⭐ 复用 uvp-setup ports-sync 的 INI 段落内替换（与运行期同一份实现，不重复造轮子）：
#   这里只传 --secret / --media-server-id / --zlm-ffmpeg，所有端口类编辑都因「值为 0」被跳过。
#
# ⛔ 为什么要动 [ffmpeg] bin：包内 config.ini 是从 Linux 那份直接搬来的，
#   里面写着 `bin=/usr/bin/ffmpeg` —— 在 Windows 上**永远解不开**。
#   改成相对路径 ./ffmpeg.exe 后：客户往 vendor\zlm\ 丢一个 ffmpeg.exe 就能启用
#   依赖 ffmpeg 的功能（通道抓拍走 ZLM 的 getSnap）。这不是"顺手改改"：
#   r19 之前一直带着那个 Linux 路径发出去，等于抓拍在这条线上从来就没可用过。
#   ⚠️ 包内**不打包** ffmpeg.exe（Linux 绿色包同样不带，只带 libav*.so 依赖库）。
Invoke-Native -FilePath $SetupExeOut -What '固定 ZLM secret 与节点标识' -Arguments @(
    'ports-sync', '--ini', (Join-Path $Pkg 'vendor\zlm\config.ini'),
    '--secret', $ZlmSecret, '--media-server-id', $ZlmMediaServerId,
    '--zlm-ffmpeg', './ffmpeg.exe')
Ok 'ZLM 身份已固定（[api] secret / [general] mediaServerId / [ffmpeg] bin → ./ffmpeg.exe）'

# ---- vendor\nginx ----
Copy-Tree -Source $VendorNginx -Destination (Join-Path $Pkg 'vendor\nginx')
# ⛔ 配置模板以**仓库版本**为唯一真源：nginx 发行包 conf\ 下那份 nginx.conf 是给
#   "单机跑个静态站"用的，root 指向 html\、监听 80 —— 打进包会让客户看到 nginx 欢迎页。
$tplDst = Join-Path $Pkg 'vendor\nginx\conf\nginx.conf.template'
Copy-Item -LiteralPath (Join-Path $WinDir 'nginx.conf.template') -Destination $tplDst -Force
if ((Get-Content -LiteralPath $tplDst -Raw -Encoding UTF8) -match '@ROOT@/bin/nginx') {
    Fail 'nginx 配置模板里仍是旧路径（@ROOT@/bin/nginx）——仓库模板没覆盖进去'
}
# 同时按默认端口渲染一份 conf\nginx.conf 作初值（ctl 启动时会按实际端口重渲染），
# 这样客户手工 `nginx -t` 也能过，不至于第一眼就是发行包那份指向 html\ 的配置。
$tplText = Get-Content -LiteralPath $tplDst -Raw -Encoding UTF8
$tplText = $tplText.Replace('@ROOT@', ($Pkg -replace '\\', '/'))
$tplText = $tplText.Replace('@HTTPS_PORT@', [string]$HttpsPort)
$tplText = $tplText.Replace('@PLAIN_HTTP_PORT@', [string]$PlainHttpPort)
$tplText = $tplText.Replace('@BACKEND_PORT@', [string]$HttpPort)
Write-Utf8NoBom -Path (Join-Path $Pkg 'vendor\nginx\conf\nginx.conf') -Text $tplText
Det 'vendor\nginx\（nginx.exe / conf / html）+ 配置模板已用仓库版本覆盖'

# ---- resource\public：前端产物 + 后端静态资源 + admin 头像种子 ----
Log '阶段 2/4 · 复制前端产物与后端资源'
$publicDir = Join-Path $Pkg 'resource\public'
Copy-Tree -Source (Join-Path $WebDir 'dist') -Destination $publicDir
$srvPublic = Join-Path $ServerDir 'resource\public'
if (Test-Path -LiteralPath $srvPublic) {
    Copy-Tree -Source $srvPublic -Destination $publicDir
}
# ⛔⛔ 不能直接用 server\resource\public\uploads：那个目录被 gitignore，是**运行时产物**，
#   从 git 拉代码的构建机上它是空的 ⇒ 头像进不了包，而 nginx 的 SPA 回落会把
#   index.html 返回给图片请求 ⇒ HTTP 200 而内容是 HTML，浏览器静默显示不出图。
$avatarSeed = Join-Path $ServerDir 'resource\seed-assets\avatar\admin.png'
if (Test-Path -LiteralPath $avatarSeed) {
    $seedDir = Join-Path $publicDir 'uploads\seed'
    New-Item -ItemType Directory -Force -Path $seedDir | Out-Null
    Copy-Item -LiteralPath $avatarSeed -Destination (Join-Path $seedDir 'admin.png') -Force
    Det 'admin 默认头像已随包（resource\public\uploads\seed\admin.png）'
} else {
    Warn "未找到 $avatarSeed，admin 将无默认头像（不阻断出包）"
}

# ---- resource\baseline：SQLite 建库脚本（与 Linux 版同源同目录结构）----
Copy-Tree -Source $SqliteDir -Destination (Join-Path $Pkg 'resource\baseline')
Copy-Item -LiteralPath $VersionJson -Destination (Join-Path $Pkg 'version.json') -Force
Det 'resource\baseline\（baseline.sql 等）· version.json'

# ---- 出包前断言：**库里有头像路径 ⇒ 包里必须有那个文件** ----
#   这类"数据库说有、磁盘上没有"的组合最坑：nginx 的 SPA 回落会把 index.html 返回给
#   图片请求 ⇒ HTTP 200 而内容是 HTML，浏览器静默显示不出图，**没有任何一行报错**。
$avatarCheck = @'
import os
import re
import sys

baseline, public = sys.argv[1], sys.argv[2]
sql = open(baseline, encoding="utf-8", errors="replace").read()
m = re.search(r"INSERT INTO [\"`]?sys_users[\"`]?\s*\([^)]*\)\s*VALUES(.*?);", sql, re.S)
urls = re.findall(r"'(/public/uploads/[^']+)'", m.group(1)) if m else []
problems = []
for url in urls:
    rel = url.lstrip("/").split("public/", 1)[-1]
    if not os.path.isfile(os.path.join(public, rel)):
        problems.append("库里有 " + url + "，但包内 resource/public/" + rel + " 不存在")
if problems:
    sys.exit("avatar resources inconsistent with baseline:\n  - " + "\n  - ".join(problems))
print("      ok  avatar check passed (%d references)" % len(urls))
'@
$avatarCheckPath = Join-Path $HelperDir 'avatar_check.py'
Write-Utf8NoBom -Path $avatarCheckPath -Text $avatarCheck
Invoke-Native -FilePath $PythonExe -What '头像资源一致性校验' `
    -Arguments @($avatarCheckPath, (Join-Path $Pkg 'resource\baseline\baseline.sql'), $publicDir)

# ---- config\config.yml ----
Log '阶段 2/4 · 生成生产配置'
$mkArgs = @(
    $MakeConfig,
    '--source', $ExampleYml,
    '--target', (Join-Path $Pkg 'config\config.yml'),
    '--http-port', [string]$HttpPort,
    '--redis-port', [string]$RedisPort,
    '--db-path', './data/uvp.db',
    '--log-path', './logs/app/uvp-gb28181.log',
    '--zlm-secret', $ZlmSecret,
    '--zlm-media-server-id', $ZlmMediaServerId,
    '--openapi-enabled', 'true',
    '--openapi-master-key-id', $OpenApiMasterKeyId
)
if ($QrProvisionHost) { $mkArgs += @('--qr-provision-host', $QrProvisionHost) }
Invoke-Native -FilePath $PythonExe -What '生成 config.yml（make-config.py）' -Arguments $mkArgs
Ok 'config.yml 已生成（SQLite + 本机 Redis + ZLM 身份 + 扫码基址）'

# ---- config\config.env：两个只走环境变量的密钥（每份包一份随机）----
#
# ⛔⛔⛔ 必须在**出包时**生成，不能让客户自己填：
#   · SIP 报文诊断密钥缺失时后端 fail-closed 不落库 ⇒ 现场看到的是
#     「菜单能进、列表永远空」，health 只报 invalid key，不说该怎么修。
#   · OpenAPI 主密钥缺失 ⇒ 后端五道前置检查不过，客户端页统一 503 且不说是哪一道。
#   安全取舍与 Linux 版一致：密钥明文随包分发（与初始口令同级）。
$sipTraceKey = New-RandomBase64 -ByteCount 32
$openApiKey  = New-RandomBase64Url -ByteCount 32
if (-not $sipTraceKey) { Fail '生成 SIP 报文诊断密钥失败' }
if (-not $openApiKey)  { Fail '生成 OpenAPI 主密钥失败' }

$envBody = @(
    '# ============================================================',
    '# UVP-GB28181 环境变量（本文件由出包脚本生成）',
    '#',
    '# 后端进程启动时由 uvp-gb28181-ctl.ps1 读进环境变量再传给子进程。',
    '# ⛔ 端口那 4 个键由 ctl 托管（会自动按 UVP_USER_SET_ 标记判断是不是用户改的），',
    '#   其余键（含你自己加的行）一定会被原样保留 —— 放心往下加。',
    '# ============================================================',
    '',
    '# ---- SIP 报文诊断加密密钥（打包时生成，每份包不同）----',
    '# 报文里的密码字段用它加密后落库。缺失或无效则**整条报文不落库**',
    '# （安全优先，但功能不可用，且 health 只报 invalid key、不提示怎么修）。',
    '# ⚠️ 换密钥后历史密文解不开，需能接受丢历史再换。',
    "UVP_SIP_TRACE_ENCRYPTION_KEY=$sipTraceKey",
    '',
    '# ---- OpenAPI 主密钥（打包时生成，每份包不同）----',
    '# 格式必须是 **32 字节的无填充 base64url**（后端用 Strict 解码，',
    '# 标准 base64 的 + / = 会被直接拒 ⇒ 表现为 OpenAPI 客户端页 503）。',
    '# ⚠️ 换主密钥后**已签发的客户端凭据全部失效**，需重新下发。',
    "UVP_OPENAPI_MASTER_KEY=$openApiKey",
    ''
) -join "`r`n"
Write-Utf8NoBom -Path (Join-Path $Pkg 'config\config.env') -Text $envBody
Ok 'config.env 已写入（SIP 报文诊断密钥 / OpenAPI 主密钥）'

# ---- 顶层脚本与说明 ----
foreach ($f in @('uvp-gb28181-ctl.ps1', 'start.cmd', 'stop.cmd', 'status.cmd')) {
    $src = Join-Path $WinDir $f
    if (-not (Test-Path -LiteralPath $src)) { Fail "缺少 $f（deploy\standalone-windows\ 下应有）" }
    Copy-Item -LiteralPath $src -Destination (Join-Path $Pkg $f) -Force
}
if (Test-Path -LiteralPath (Join-Path $WinDir 'README.md')) {
    Copy-Item -LiteralPath (Join-Path $WinDir 'README.md') -Destination (Join-Path $Pkg 'README.md') -Force
}
Det '顶层：uvp-gb28181-ctl.ps1 / start.cmd / stop.cmd / status.cmd / version.json'

# ---- 空目录占位（zip 不带目录项时会被丢掉；ctl 虽然会自建，但保持布局一致更好读）----
foreach ($d in @('data', 'data\redis', 'logs', 'run')) {
    Write-Utf8NoBom -Path (Join-Path $Pkg "$d\.keep") `
        -Text "本文件只是为了在压缩包里保留这个空目录，可安全删除。`r`n"
}

# ------------------------------------------------------------------ 校验 ----
Log '阶段 3/4 · 包内契约校验'

# ⛔⛔ 所有 .ps1 必须带 UTF-8 BOM。PowerShell 5.1 无 BOM 时按系统 ANSI 代码页
#   （简中=GBK）解码，中文注释/字符串会乱码，**重则语法错误**（引号被吃掉）。
$ps1Files = Get-ChildItem -LiteralPath $Pkg -Recurse -File -Filter '*.ps1'
foreach ($f in $ps1Files) {
    if (-not (Test-Utf8Bom $f.FullName)) {
        Fail "包内 .ps1 缺少 UTF-8 BOM：$($f.FullName.Substring($Pkg.Length + 1))`n" +
             "      Windows PowerShell 5.1 会按 GBK 解码它 ⇒ 中文乱码甚至语法错误。" +
             "`n      修法：用记事本另存为「UTF-8 带 BOM」，或在出包机上执行" +
             "`n        [IO.File]::WriteAllText(`$p, (Get-Content -Raw `$p), (New-Object Text.UTF8Encoding `$true))"
    }
}
Ok "全部 .ps1 都是 UTF-8 with BOM（$($ps1Files.Count) 个）"

# ⛔ .cmd 必须纯 ASCII：cmd.exe 按 OEM 代码页（简中=936）解码 .cmd，
#   存成 UTF-8 的中文会变成乱码，而乱码出现在 `echo` 里还好、出现在 `if`/`goto` 里就是语法错。
foreach ($f in (Get-ChildItem -LiteralPath $Pkg -Recurse -File -Filter '*.cmd')) {
    $bytes = [System.IO.File]::ReadAllBytes($f.FullName)
    $bad = $false
    foreach ($b in $bytes) { if ($b -gt 127) { $bad = $true; break } }
    if ($bad) { Fail "包内 .cmd 含非 ASCII 字节：$($f.Name) —— cmd.exe 会按 OEM 代码页解码导致乱码" }
}
Ok '.cmd 入口均为纯 ASCII'

# 关键文件逐个确认
foreach ($item in @(
        @{ P = 'bin\uvp-server.exe';                 N = '后端二进制' },
        @{ P = 'bin\uvp-setup.exe';                  N = '辅助工具' },
        @{ P = 'config\config.yml';                  N = '后端配置' },
        @{ P = 'config\config.env';                  N = '环境变量（密钥）' },
        @{ P = 'resource\baseline\baseline.sql';     N = 'SQLite 建库脚本' },
        @{ P = 'resource\public\index.html';         N = '前端产物' },
        @{ P = 'vendor\redis\redis-server.exe';      N = 'Redis' },
        @{ P = 'vendor\zlm\MediaServer.exe';         N = 'ZLM' },
        @{ P = 'vendor\zlm\config.ini';              N = 'ZLM 配置' },
        @{ P = 'vendor\nginx\nginx.exe';             N = 'nginx' },
        @{ P = 'vendor\nginx\conf\nginx.conf.template'; N = 'nginx 配置模板' },
        @{ P = 'uvp-gb28181-ctl.ps1';                N = '控制脚本' }
    )) {
    if (-not (Test-Path -LiteralPath (Join-Path $Pkg $item.P))) { Fail "包内缺少 $($item.N)：$($item.P)" }
}
Ok '关键文件齐备'

# config.yml 解析后断言 ZLM 身份真的进去了（字符串出现过≠生效）。
$zlmCheck = @'
import sys
import yaml

path, want_secret, want_id = sys.argv[1:4]
zlm = (yaml.safe_load(open(path, encoding="utf-8")) or {}).get("gb28181", {}).get("zlm", {})
problems = []
if zlm.get("secret") != want_secret:
    problems.append("secret=%r（应为 %r）" % (zlm.get("secret"), want_secret))
if zlm.get("mediaserverid") != want_id:
    problems.append("mediaserverid=%r（应为 %r）" % (zlm.get("mediaserverid"), want_id))
if problems:
    sys.exit("ZLM identity not written into config.yml: " + "; ".join(problems))
print("      ok  ZLM identity pinned (secret %d chars / id %d chars)" % (len(want_secret), len(want_id)))
'@
$zlmCheckPath = Join-Path $HelperDir 'zlm_check.py'
Write-Utf8NoBom -Path $zlmCheckPath -Text $zlmCheck
Invoke-Native -FilePath $PythonExe -What 'ZLM 身份配置校验' `
    -Arguments @($zlmCheckPath, (Join-Path $Pkg 'config\config.yml'), $ZlmSecret, $ZlmMediaServerId)

# ZLM config.ini 侧也要对得上（两侧任一漏掉都是「平台调 ZLM 全被拒」）
$iniRaw = Get-Content -LiteralPath (Join-Path $Pkg 'vendor\zlm\config.ini') -Raw -Encoding UTF8
if ($iniRaw -notmatch [regex]::Escape($ZlmSecret)) { Fail 'config.ini 里没写进 ZLM secret' }
if ($iniRaw -notmatch [regex]::Escape($ZlmMediaServerId)) { Fail 'config.ini 里没写进 mediaServerId' }
Ok 'config.ini 与 config.yml 的 ZLM 身份一致'

# nginx 配置：**不做 `nginx -t`**，只断言渲染结果里没有残留占位符。
#
# ⛔ 为什么不做 -t：nginx 在 `-t` 时会校验 ssl_certificate 指向的文件存在，
#   而证书是**首次 start 时由 certgen 生成**的 —— 出包阶段必然不存在
#   ⇒ `nginx -t` 100% 失败。让它失败却只 Warn，等于给一个永远为红的指示灯，
#   真出问题时反而没人看。语法正确性由包内 ctl 首次 start 负责（那一层会硬失败）。
# ⭐ 真正值得在出包时拦的是**占位符没被替换**：模板改了占位符名而脚本没跟上时，
#   nginx 会拿着字面量 `@ROOT@/...` 去找目录，报的是一堆找不到路径的错。
$rendered = Get-Content -LiteralPath (Join-Path $Pkg 'vendor\nginx\conf\nginx.conf') -Raw -Encoding UTF8
$leftover = [regex]::Matches($rendered, '@[A-Z_]+@') | ForEach-Object { $_.Value } | Sort-Object -Unique
if ($leftover) {
    Fail ("nginx.conf 里还有未替换的占位符：" + ($leftover -join ', ') +
          "`n      说明模板里的占位符名与出包脚本/ctl 脚本里的替换列表对不上。")
}
if ($rendered -notmatch [regex]::Escape("$($HttpsPort)")) {
    Fail "渲染后的 nginx.conf 里看不到 HTTPS 端口 $HttpsPort"
}
Ok "nginx.conf 已按默认端口渲染，无残留占位符（模板：$($tplDst.Substring($Pkg.Length + 1))）"

# ⛔⛔ 文本配置文件**必须无 BOM**。这里刻意**按字节**判断而不是 Get-Content：
#   PowerShell 5.1 的 `Get-Content -Encoding UTF8` 会自动**剥掉** BOM，所以上面那段
#   占位符检查永远看不到 BOM —— 这正是本项目「出包校验全绿、首次 start 才炸」的原因。
#   实测代价（2026-10-10）：ctl.ps1 用 `Set-Content -Encoding UTF8` 渲染 nginx.conf 后，
#   nginx 报 `[emerg] unknown directive "\ufeff#" in ...\nginx.conf:22` —— 首行那句注释
#   因为前面多了 BOM 不再是注释，词法器一路吞到第一个 `;` 才报错，于是**行号指向被吞掉的
#   那条指令结尾**，极易误导成"第 22 行有问题"。
#   同类后果：① ZLM 的 .ini 首个 key 名多一个 \ufeff 而匹配不上；② Go 的 yaml/json 解析器
#   对首键/首 token 报错；③ SQLite 建库脚本首个语句语法错。
foreach ($item in @(
        @{ P = 'vendor\nginx\conf\nginx.conf';   N = 'nginx 配置（渲染产物）' },
        @{ P = 'vendor\nginx\conf\nginx.conf.template'; N = 'nginx 配置模板' },
        @{ P = 'vendor\zlm\config.ini';          N = 'ZLM 配置' },
        @{ P = 'config\config.env';              N = '环境变量' },
        @{ P = 'config\config.yml';              N = '后端配置' },
        @{ P = 'resource\baseline\baseline.sql'; N = '建库脚本' },
        @{ P = 'version.json';                   N = '版本信息' }
    )) {
    $fp = Join-Path $Pkg $item.P
    if (-not (Test-Path -LiteralPath $fp)) { continue }
    $head = [System.IO.File]::ReadAllBytes($fp)
    if ($head.Length -ge 3 -and $head[0] -eq 0xEF -and $head[1] -eq 0xBB -and $head[2] -eq 0xBF) {
        Fail ("包内 $($item.N) 带 UTF-8 BOM：$($item.P)" +
              "`n      原生解析器（nginx / ZLM / Go）不剥 BOM，会把它当成内容的一部分。" +
              "`n      修法：改用 Write-Utf8NoBom 写出，不要用 Set-Content -Encoding UTF8。")
    }
}
Ok '文本配置文件均无 BOM（按字节校验）'

# ------------------------------------------------------------------ 打包 ----
Log '阶段 4/4 · 打包'
if (-not (Test-Path -LiteralPath $OutDir)) { New-Item -ItemType Directory -Force -Path $OutDir | Out-Null }
Remove-Item -LiteralPath $ZipPath -Force -ErrorAction SilentlyContinue
Remove-Item -LiteralPath $ShaPath -Force -ErrorAction SilentlyContinue

Add-Type -AssemblyName System.IO.Compression
Add-Type -AssemblyName System.IO.Compression.FileSystem
# ⛔ 手工建 entry 而不用 CreateFromDirectory：后者按**文件**枚举，
#   空目录（data\ logs\ run\）会整体丢掉，客户解开看不到这几个目录会以为包残缺。
#   这里连目录项一起写，条目名统一用 '/'（Windows 资源管理器两种都能认）。
$zip = [System.IO.Compression.ZipFile]::Open($ZipPath, [System.IO.Compression.ZipArchiveMode]::Create)
$entryCount = 0
try {
    $parentLen = $Stage.Length + 1
    foreach ($item in (Get-ChildItem -LiteralPath $Stage -Recurse -Force)) {
        $rel = $item.FullName.Substring($parentLen) -replace '\\', '/'
        if ($rel.StartsWith('/')) { $rel = $rel.TrimStart('/') }
        if ($item.PSIsContainer) {
            [void]$zip.CreateEntry($rel + '/')
        } else {
            [void][System.IO.Compression.ZipFileExtensions]::CreateEntryFromFile(
                $zip, $item.FullName, $rel, [System.IO.Compression.CompressionLevel]::Optimal)
        }
        $entryCount++
    }
} finally { $zip.Dispose() }

if (-not (Test-Path -LiteralPath $ZipPath) -or (Get-Item $ZipPath).Length -eq 0) { Fail '产物为空' }
$sha = (Get-FileHash -LiteralPath $ZipPath -Algorithm SHA256).Hash.ToLower()
# 与 Linux 版同格式：`<sha>  <文件名>`
Write-Utf8NoBom -Path $ShaPath -Text ("$sha  $PkgStem.zip`r`n")

$pkgBytes = (Get-ChildItem -LiteralPath $Pkg -Recurse -Force -File | Measure-Object -Property Length -Sum).Sum
Remove-Item -LiteralPath $Stage -Recurse -Force -ErrorAction SilentlyContinue
Remove-Item -LiteralPath $HelperDir -Recurse -Force -ErrorAction SilentlyContinue

Write-Host ''
Ok "完成：$ZipPath（$(Format-Size (Get-Item $ZipPath).Length)，未压缩 $(Format-Size $pkgBytes)）"
Ok "校验：$sha"
Det "包内条目：$entryCount 项"
Det "校验文件：$ShaPath"
Log '下一步：把 zip 拷到目标机解压 → 双击 start.cmd（首次会自动建库、发证书、渲染配置）'
Write-Host ''

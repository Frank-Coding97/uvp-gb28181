#Requires -Version 5.1
<#
  UVP-GB28181 增量升级包（Windows）

  用法（在解压后的包目录里执行）：
      .\apply.ps1                  自动向上探测安装根
      .\apply.ps1 -Root D:\uvp     指定安装根（目录里应有 uvp-gb28181-ctl.ps1）
      .\apply.ps1 -DryRun          演练：只校验 + 打印，不动任何文件
      .\apply.cmd [...]            等价入口（绕过 PowerShell 执行策略）

  顺序固定：版本核对 → 校验 → 停服 → 备份 → 落文件 → 删旧文件 → 起服 → 复核。
  任何一步失败即中止；文件改动之前一定先做备份。
  config\ 与 data\ 不在升级包内 —— 升级不会改动现场配置与数据库。
#>
[CmdletBinding()]
param(
    [string]$Root = '',
    [switch]$DryRun
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
try { [Console]::OutputEncoding = New-Object System.Text.UTF8Encoding $false } catch { }
$OutputEncoding = [Console]::OutputEncoding

$FromVersion = '__FROM_VERSION__'
$ToVersion = '__TO_VERSION__'
$Here = Split-Path -Parent $MyInvocation.MyCommand.Path

function Say([string]$Message) { Write-Host $Message }
function Warn([string]$Message) { Write-Host $Message -ForegroundColor Yellow }
function Die([string]$Message) {
    Write-Host "[失败] $Message" -ForegroundColor Red
    exit 1
}

foreach ($required in @('manifest.json', 'payload')) {
    if (-not (Test-Path -LiteralPath (Join-Path $Here $required))) {
        Die "包不完整：缺少 ${required}"
    }
}

# ---- 1 定位安装根与当前版本 -------------------------------------------------
if (-not $Root) {
    $probe = $Here
    while ($true) {
        $parent = Split-Path -Parent $probe
        if (-not $parent -or $parent -eq $probe) { break }
        $probe = $parent
        if (Test-Path -LiteralPath (Join-Path $probe 'uvp-gb28181-ctl.ps1')) { $Root = $probe; break }
    }
}
if (-not $Root) {
    Die '没找到安装根，请用 -Root <安装目录> 指定（目录里应有 uvp-gb28181-ctl.ps1）'
}
$Root = (Resolve-Path -LiteralPath $Root).Path
$Ctl = Join-Path $Root 'uvp-gb28181-ctl.ps1'
if (-not (Test-Path -LiteralPath $Ctl)) {
    Die "${Root} 不是安装根：缺少 uvp-gb28181-ctl.ps1"
}
$VersionFile = Join-Path $Root 'version.json'
if (-not (Test-Path -LiteralPath $VersionFile)) {
    Die "${Root} 缺少 version.json，无法确认当前版本"
}

$current = ''
try {
    $current = [string](Get-Content -LiteralPath $VersionFile -Raw -Encoding UTF8 | ConvertFrom-Json).version
} catch { }
if (-not $current) { Die "读不出 ${VersionFile} 里的版本号" }
if ($current -ne $FromVersion) {
    Die "版本不匹配：当前 ${current}，本包只能升级 ${FromVersion} → ${ToVersion}。`n     请换对应版本的升级包，或直接用完整安装包覆盖安装。"
}

# ---- 2 校验 payload --------------------------------------------------------
Say '==> 校验升级包完整性'
$ChecksumFile = Join-Path $Here 'files.sha256'
if ((Test-Path -LiteralPath $ChecksumFile) -and ((Get-Item -LiteralPath $ChecksumFile).Length -gt 0)) {
    foreach ($line in (Get-Content -LiteralPath $ChecksumFile -Encoding UTF8)) {
        $text = "$line".Trim()
        if (-not $text) { continue }
        $parts = $text -split '\s+', 2
        if ($parts.Count -lt 2) { continue }
        $expected = $parts[0].ToLower()
        $rel = $parts[1]
        $file = Join-Path $Here ($rel -replace '/', '\')
        if (-not (Test-Path -LiteralPath $file)) { Die "包不完整：缺少 ${rel}" }
        $actual = (Get-FileHash -LiteralPath $file -Algorithm SHA256).Hash.ToLower()
        if ($actual -ne $expected) { Die "校验失败：${rel} 与清单不符，包已损坏，请重新下载" }
    }
}
Say '    通过'

if ($DryRun) {
    # 演练也要给出"会动多少东西"的量化结论，否则用户看不出这次升级的规模。
    $counts = $null
    $manifestPath = Join-Path $Here 'manifest.json'
    if (Test-Path -LiteralPath $manifestPath) {
        try { $counts = (Get-Content -LiteralPath $manifestPath -Raw -Encoding UTF8 | ConvertFrom-Json).counts } catch { }
    }
    $payloadDir = Join-Path $Here 'payload'
    $payloadBytes = 0
    $overwrite = 0
    foreach ($file in (Get-ChildItem -LiteralPath $payloadDir -Recurse -File)) {
        $payloadBytes += $file.Length
        $rel = $file.FullName.Substring($payloadDir.Length).TrimStart('\')
        if ($rel.ToLower().EndsWith('.new')) { $rel = $rel.Substring(0, $rel.Length - 4) }
        if (Test-Path -LiteralPath (Join-Path $Root $rel)) { $overwrite += 1 }
    }
    $removeCount = 0
    $DryDelFile = Join-Path $Here 'deletes.txt'
    if ((Test-Path -LiteralPath $DryDelFile) -and ((Get-Item -LiteralPath $DryDelFile).Length -gt 0)) {
        foreach ($line in (Get-Content -LiteralPath $DryDelFile -Encoding UTF8)) {
            $rel = "$line".Trim()
            if (-not $rel) { continue }
            if (Test-Path -LiteralPath (Join-Path $Root ($rel -replace '/', '\'))) { $removeCount += 1 }
        }
    }
    Say '==> 演练模式（不做任何改动）'
    Say "    安装根   ${Root}"
    Say "    版本     ${FromVersion} → ${ToVersion}"
    if ($counts) {
        $mb = [math]::Round($payloadBytes / 1e6, 1)
        Say "    包内容   新增 $($counts.add) / 更新 $($counts.modify) / 删除 $($counts.delete)，解开约 ${mb} MB"
    }
    Say "    将执行   覆盖 ${overwrite} 个现有文件，清理 ${removeCount} 个旧文件"
    Say "    备份位置 $(Split-Path -Parent $Root)\uvp-update-backup-<时间戳>.zip"
    exit 0
}

# ---- 3 停服 ----------------------------------------------------------------
# Windows 版没有 systemd 托管路径，统一走控制脚本 stop；调用方是新进程，
# 脚本文件本身的句柄已经释放，后面覆盖 uvp-gb28181-ctl.ps1 不会撞 sharing violation。
Say '==> 停止服务'
& powershell -NoProfile -ExecutionPolicy Bypass -File $Ctl stop
if ($LASTEXITCODE -ne 0) { Die '停服失败，已中止（未改动任何文件）' }

# 二进制被占用时直接复制会报 sharing violation，所以先确认后端进程真的退了。
$PidFile = Join-Path $Root 'run\backend.pid'
$deadline = (Get-Date).AddSeconds(30)
while ((Get-Date) -lt $deadline) {
    if (-not (Test-Path -LiteralPath $PidFile)) { break }
    $raw = (Get-Content -LiteralPath $PidFile -ErrorAction SilentlyContinue | Select-Object -First 1)
    $procId = 0
    if (-not [int]::TryParse("$raw".Trim(), [ref]$procId)) { break }
    if (-not (Get-Process -Id $procId -ErrorAction SilentlyContinue)) { break }
    Start-Sleep -Milliseconds 500
}
$rawFinal = (Get-Content -LiteralPath $PidFile -ErrorAction SilentlyContinue | Select-Object -First 1)
$finalId = 0
if ($rawFinal -and [int]::TryParse("$rawFinal".Trim(), [ref]$finalId)) {
    if (Get-Process -Id $finalId -ErrorAction SilentlyContinue) {
        Die '后端进程等待 30 秒仍在运行，请手工停止后重试（未改动任何文件）'
    }
}

# ---- 4 备份 ----------------------------------------------------------------
$PayloadDir = Join-Path $Here 'payload'
$DeletesFile = Join-Path $Here 'deletes.txt'
$stamp = (Get-Date).ToUniversalTime().ToString('yyyyMMddHHmmss')
$backupDir = Split-Path -Parent $Root
if (-not $backupDir -or -not (Test-Path -LiteralPath $backupDir)) { $backupDir = $env:TEMP }
$Backup = Join-Path $backupDir "uvp-update-backup-${stamp}.zip"

$targets = New-Object System.Collections.Generic.List[string]
foreach ($file in (Get-ChildItem -LiteralPath $PayloadDir -Recurse -File)) {
    $rel = $file.FullName.Substring($PayloadDir.Length).TrimStart('\')
    if ($rel.ToLower().EndsWith('.new')) { $rel = $rel.Substring(0, $rel.Length - 4) }
    $target = Join-Path $Root $rel
    if (Test-Path -LiteralPath $target) { $targets.Add($target) }
}
if ((Test-Path -LiteralPath $DeletesFile) -and ((Get-Item -LiteralPath $DeletesFile).Length -gt 0)) {
    foreach ($line in (Get-Content -LiteralPath $DeletesFile -Encoding UTF8)) {
        $rel = "$line".Trim()
        if (-not $rel) { continue }
        $target = Join-Path $Root ($rel -replace '/', '\')
        if (Test-Path -LiteralPath $target) { $targets.Add($target) }
    }
}

$unique = @($targets | Sort-Object -Unique)
if ($unique.Count -gt 0) {
    Say "==> 备份 $($unique.Count) 个将被覆盖或删除的文件到 ${Backup}"
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $zip = [System.IO.Compression.ZipFile]::Open($Backup, 'Create')
    try {
        foreach ($target in $unique) {
            $relInZip = $target.Substring($Root.Length).TrimStart('\')
            [void][System.IO.Compression.ZipFileExtensions]::CreateEntryFromFile($zip, $target, $relInZip, 'Optimal')
        }
    } catch {
        $zip.Dispose()
        Remove-Item -LiteralPath $Backup -Force -ErrorAction SilentlyContinue
        Die "备份失败，已中止（未改动任何文件）：$($_.Exception.Message)"
    }
    $zip.Dispose()
} else {
    Say '==> 没有需要备份的现有文件'
    $Backup = ''
}

# ---- 5 落新文件 ------------------------------------------------------------
Say '==> 写入新文件'
try {
    foreach ($file in (Get-ChildItem -LiteralPath $PayloadDir -Recurse -File)) {
        $rel = $file.FullName.Substring($PayloadDir.Length).TrimStart('\')
        $dst = Join-Path $Root $rel
        $parentDir = Split-Path -Parent $dst
        if (-not (Test-Path -LiteralPath $parentDir)) {
            New-Item -ItemType Directory -Force -Path $parentDir | Out-Null
        }
        Copy-Item -LiteralPath $file.FullName -Destination $dst -Force
    }
} catch {
    Die "写入失败，请用备份回滚：$($Backup)；原因：$($_.Exception.Message)"
}

# ---- 6 删除旧文件 ----------------------------------------------------------
if ((Test-Path -LiteralPath $DeletesFile) -and ((Get-Item -LiteralPath $DeletesFile).Length -gt 0)) {
    $removed = 0
    foreach ($line in (Get-Content -LiteralPath $DeletesFile -Encoding UTF8)) {
        $rel = "$line".Trim()
        if (-not $rel) { continue }
        $target = Join-Path $Root ($rel -replace '/', '\')
        if (Test-Path -LiteralPath $target) {
            Remove-Item -LiteralPath $target -Force -ErrorAction SilentlyContinue
            $removed += 1
        }
    }
    Say "==> 清理本版本已移除的文件：${removed} 个"
}

# ---- 7 起服 + 复核 ---------------------------------------------------------
Say '==> 启动服务'
& powershell -NoProfile -ExecutionPolicy Bypass -File $Ctl start
if ($LASTEXITCODE -ne 0) {
    $hint = ''
    if ($Backup) { $hint = "备份在 ${Backup}" } else { $hint = '（无备份）' }
    Die "启动失败。文件已更新，排查请见 ${Root}\logs；${hint}"
}

$after = ''
try {
    $after = [string](Get-Content -LiteralPath $VersionFile -Raw -Encoding UTF8 | ConvertFrom-Json).version
} catch { }
if ($after -ne $ToVersion) {
    Die "升级后版本号是 ${after}，期望 ${ToVersion}（文件可能没覆盖成功）"
}

Say ''
Say "升级完成：${FromVersion} → ${ToVersion}"
Say "安装根：${Root}"
if ($Backup) { Say "回滚备份：${Backup}" }
Say '配置与数据库未改动（config\ 与 data\ 不在升级包内）。'

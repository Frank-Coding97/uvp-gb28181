#requires -Version 5.1
<#
T14-C native Windows acceptance for atomic standalone configuration.

The script owns one newly-created VHD and one newly-created test directory on
that VHD. It never selects a physical disk and it leaves the VHD/work evidence
in place after detaching the VHD. Only the fill directory created by this run
is removed.
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$ProbeExe,
    [Parameter(Mandatory = $true)][string]$WorkRoot,
    [string]$VhdPath,
    [ValidatePattern('^[D-Zd-z]$')][string]$DriveLetter = 'V'
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false)
$OutputEncoding = [Console]::OutputEncoding

$envNames = @(
    'UVP_INSTALL_DIR', 'UVP_CONFIG_DIR', 'UVP_RESOURCE_DIR',
    'UVP_WEB_DIR', 'UVP_DATA_DIR', 'UVP_RECORDINGS_DIR'
)
$previousEnv = @{}
foreach ($name in $envNames) {
    $previousEnv[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
}

$work = $null
$vhd = $null
$probe = $null
$drive = $DriveLetter.ToUpperInvariant()
$driveRoot = $drive + ':\'
$fillDir = $null
$instanceRoot = $null
$vhdAttached = $false
$vhdOwned = $false
$report = $null
$failureMessage = $null
$cleanupErrors = New-Object 'System.Collections.Generic.List[string]'
$maxFillBytes = [uint64](128 * 1024 * 1024)
$maxVolumeBytes = [uint64](128 * 1024 * 1024)

function Resolve-LocalAbsolutePath([string]$Path, [string]$Name) {
    if ([string]::IsNullOrWhiteSpace($Path) -or $Path -match '["\r\n]' -or $Path -match '^\\\\') {
        throw "$Name must be a local absolute path"
    }
    try {
        $full = [IO.Path]::GetFullPath($Path)
    } catch {
        throw "$Name is not a valid path"
    }
    if ($full -notmatch '^[A-Za-z]:\\') {
        throw "$Name must be a local drive path"
    }
    return $full
}

function Assert-ExistingDirectory([string]$Path, [string]$Name) {
    if (-not (Test-Path -LiteralPath $Path -PathType Container)) {
        throw "$Name parent directory must already exist"
    }
    $item = Get-Item -LiteralPath $Path -Force
    if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
        throw "$Name parent directory must not be a reparse point"
    }
}

function Assert-FreeDriveLetter([string]$Letter) {
    $root = $Letter + ':\'
    if (Test-Path -LiteralPath $root) {
        throw "drive letter $Letter is already in use"
    }
    $volume = Get-Volume -DriveLetter $Letter -ErrorAction SilentlyContinue
    if ($null -ne $volume) {
        throw "drive letter $Letter already has a volume"
    }
}

function Invoke-DiskPart([string[]]$Commands, [string]$Name) {
    $scriptPath = Join-Path $work ("diskpart-$Name-$([Guid]::NewGuid().ToString('N')).txt")
    $logPath = Join-Path $work ("diskpart-$Name.log")
    try {
        $Commands | Set-Content -LiteralPath $scriptPath -Encoding ASCII
        & "$env:SystemRoot\System32\diskpart.exe" /s $scriptPath *> $logPath
        $exitCode = $LASTEXITCODE
        if ($exitCode -ne 0) {
            throw "diskpart $Name failed with exit=$exitCode; see $logPath"
        }
        return [ordered]@{ exit_code = $exitCode; log = $logPath }
    } finally {
        if (Test-Path -LiteralPath $scriptPath) {
            Remove-Item -LiteralPath $scriptPath -Force
        }
    }
}

function Get-TestVolume() {
    if (-not (Test-Path -LiteralPath $driveRoot -PathType Container)) {
        throw "dedicated VHD was not mounted at $driveRoot"
    }
    $volume = Get-Volume -DriveLetter $drive -ErrorAction Stop
    if ($volume.FileSystemLabel -ne 'UVP_T14_TEST') {
        throw "unexpected test volume label"
    }
    $size = [uint64]$volume.Size
    if ($size -le 0 -or $size -gt $maxVolumeBytes) {
        throw "unexpected test volume size"
    }
    return $volume
}

function Get-Win32Code([Exception]$Exception) {
    for ($current = $Exception; $null -ne $current; $current = $current.InnerException) {
        if ($current -is [ComponentModel.Win32Exception]) {
            return [int]$current.NativeErrorCode
        }
        $code = [int]($current.HResult -band 0xffff)
        if ($code -eq 39 -or $code -eq 112) {
            return $code
        }
    }
    return 0
}

function Test-DiskFullException([Exception]$Exception) {
    $code = Get-Win32Code $Exception
    return ($code -eq 39 -or $code -eq 112)
}

function Get-FillBytes([string]$Directory) {
    $files = @(Get-ChildItem -LiteralPath $Directory -File -Force -ErrorAction SilentlyContinue)
    if ($files.Count -eq 0) {
        return [uint64]0
    }
    $sum = ($files | Measure-Object -Property Length -Sum).Sum
    if ($null -eq $sum) {
        return [uint64]0
    }
    return [uint64]$sum
}

function Fill-UntilDiskFull([string]$Directory) {
    $chunkBytes = 1024 * 1024
    $buffer = New-Object byte[] $chunkBytes
    $attempted = [uint64]0
    $index = 0
    $reachedNoSpace = $false
    $errorCode = 0
    while ($attempted -lt $maxFillBytes) {
        $remaining = $maxFillBytes - $attempted
        $target = [int][Math]::Min([uint64]$chunkBytes, $remaining)
        $path = Join-Path $Directory ("fill-{0:D4}.bin" -f $index)
        $stream = $null
        try {
            $stream = [IO.File]::Open($path, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::None)
            try {
                $stream.Write($buffer, 0, $target)
                $stream.Flush($true)
            } finally {
                if ($null -ne $stream) {
                    $stream.Dispose()
                    $stream = $null
                }
            }
            $attempted += [uint64]$target
            $index++
        } catch {
            if (Test-DiskFullException $_.Exception) {
                $reachedNoSpace = $true
                $errorCode = Get-Win32Code $_.Exception
                break
            }
            throw "fill write failed with a non-disk-full error"
        } finally {
            if ($null -ne $stream) {
                $stream.Dispose()
            }
        }
    }
    $actual = Get-FillBytes $Directory
    if ($actual -gt $maxFillBytes) {
        throw "fill exceeded the 128 MiB safety cap"
    }
    return [ordered]@{
        bytes_written = $actual
        attempted_bytes = $attempted
        files = @(Get-ChildItem -LiteralPath $Directory -File -Force).Count
        cap_bytes = $maxFillBytes
        reached_no_space = $reachedNoSpace
        error_code = $errorCode
    }
}

function Get-SHA256([string]$Path) {
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        throw "expected file is missing"
    }
    return (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant()
}

function Invoke-ConfigProbe([string]$Name) {
    $stdout = Join-Path $work ("probe-$Name.stdout")
    $stderr = Join-Path $work ("probe-$Name.stderr")
    $process = Start-Process -FilePath $probe -WorkingDirectory "$env:SystemRoot\System32" -PassThru `
        -RedirectStandardOutput $stdout -RedirectStandardError $stderr -WindowStyle Hidden
    $processHandle = $process.Handle
    if (-not $process.WaitForExit(30000)) {
        try { $process.Kill() } catch { }
        throw "standalone-config-probe timed out"
    }
    $process.Refresh()
    return [ordered]@{
        name = $Name
        exit_code = [int]$process.ExitCode
        stdout_path = $stdout
        stderr_path = $stderr
        stdout_sha256 = Get-SHA256 $stdout
        stderr_sha256 = Get-SHA256 $stderr
    }
}

function Read-SuccessProbe([System.Collections.IDictionary]$Invocation, [string]$ExpectedConfig, [string]$ExpectedRedis, [string]$ExpectedZLM) {
    if ($Invocation.exit_code -ne 0) {
        throw "retry probe failed with exit=$($Invocation.exit_code)"
    }
    try {
        $json = [IO.File]::ReadAllText($Invocation.stdout_path) | ConvertFrom-Json
    } catch {
        throw "retry probe did not return JSON"
    }
    if ($json.created -ne $true -or [string]$json.config_sha256 -notmatch '^[a-fA-F0-9]{64}$') {
        throw "retry probe did not report a newly-created configuration"
    }
    try {
        $configPath = [IO.Path]::GetFullPath([string]$json.config_path)
        $redisPath = [IO.Path]::GetFullPath([string]$json.redis_config_path)
        $zlmPath = [IO.Path]::GetFullPath([string]$json.zlm_config_path)
    } catch {
        throw "retry probe returned invalid configuration paths"
    }
    if ($configPath -ne [IO.Path]::GetFullPath($ExpectedConfig) -or
        $redisPath -ne [IO.Path]::GetFullPath($ExpectedRedis) -or
        $zlmPath -ne [IO.Path]::GetFullPath($ExpectedZLM)) {
        throw "retry probe wrote outside the isolated instance"
    }
    return [ordered]@{
        name = $Invocation.name
        exit_code = $Invocation.exit_code
        created = $true
        config_sha256 = ([string]$json.config_sha256).ToLowerInvariant()
        stdout_sha256 = $Invocation.stdout_sha256
        stderr_sha256 = $Invocation.stderr_sha256
    }
}

function Set-ProbeEnvironment([string]$Root) {
    $values = @{
        UVP_INSTALL_DIR = $Root
        UVP_CONFIG_DIR = Join-Path $Root 'config'
        UVP_RESOURCE_DIR = Join-Path $Root 'resource'
        UVP_WEB_DIR = Join-Path $Root 'web'
        UVP_DATA_DIR = Join-Path $Root 'data'
        UVP_RECORDINGS_DIR = Join-Path $Root 'recordings'
    }
    foreach ($name in $envNames) {
        [Environment]::SetEnvironmentVariable($name, [string]$values[$name], 'Process')
    }
}

try {
    $probe = Resolve-LocalAbsolutePath $ProbeExe 'ProbeExe'
    if (-not (Test-Path -LiteralPath $probe -PathType Leaf) -or [IO.Path]::GetExtension($probe).ToLowerInvariant() -ne '.exe') {
        throw 'ProbeExe must be an existing .exe file'
    }
    $work = Resolve-LocalAbsolutePath $WorkRoot 'WorkRoot'
    if (Test-Path -LiteralPath $work) {
        throw 'WorkRoot must be a new isolated directory'
    }
    Assert-ExistingDirectory (Split-Path -Parent $work) 'WorkRoot'
    $null = New-Item -ItemType Directory -Path $work

    if ([string]::IsNullOrWhiteSpace($VhdPath)) {
        $vhd = Join-Path $work ("uvp-t14-config-disk-full-$([Guid]::NewGuid().ToString('N')).vhd")
    } else {
        $vhd = Resolve-LocalAbsolutePath $VhdPath 'VhdPath'
        if ([IO.Path]::GetExtension($vhd).ToLowerInvariant() -ne '.vhd') {
            throw 'VhdPath must have a .vhd extension'
        }
        Assert-ExistingDirectory (Split-Path -Parent $vhd) 'VhdPath'
    }
    if (Test-Path -LiteralPath $vhd) {
        throw 'VhdPath already exists; refusing to reuse it'
    }
    if ($vhd -match '[^\x00-\x7F]') {
        throw 'VhdPath must contain only ASCII characters for the DiskPart script'
    }
    Assert-FreeDriveLetter $drive
    if ([IO.Path]::GetPathRoot($probe).ToUpperInvariant() -eq $driveRoot.ToUpperInvariant()) {
        throw 'ProbeExe must be outside the dedicated test volume'
    }

    $vhdOwned = $true
    $null = Invoke-DiskPart @(
        "create vdisk file=`"$vhd`" maximum=64 type=expandable",
        "select vdisk file=`"$vhd`"",
        'attach vdisk',
        'create partition primary',
        'format fs=ntfs quick label=UVP_T14_TEST',
        "assign letter=$drive",
        'exit'
    ) 'create'
    $image = Get-DiskImage -ImagePath $vhd -ErrorAction Stop
    if (-not $image.Attached) {
        throw 'dedicated VHD is not attached'
    }
    $vhdAttached = $true
    $volume = Get-TestVolume
    $instanceRoot = Join-Path $driveRoot 'UVP T14 config 中文 # space'
    if (Test-Path -LiteralPath $instanceRoot) {
        throw 'isolated instance directory unexpectedly exists on the new VHD'
    }
    $fillDir = Join-Path $driveRoot ("uvp-t14-disk-full-fill-$([Guid]::NewGuid().ToString('N'))")
    $null = New-Item -ItemType Directory -Path $fillDir
    $fillPath = $fillDir
    $fill = Fill-UntilDiskFull $fillDir
    if (-not $fill.reached_no_space -or ($fill.error_code -ne 39 -and $fill.error_code -ne 112)) {
        throw 'test volume did not report an explicit disk-full error before the cap'
    }
    $fullVolume = Get-TestVolume
    Set-ProbeEnvironment $instanceRoot
    $fullProbe = Invoke-ConfigProbe 'full'
    if ($fullProbe.exit_code -eq 0) {
        throw 'configuration probe unexpectedly succeeded while the volume was full'
    }

    $expectedConfig = Join-Path $instanceRoot 'config\config.yml'
    $expectedRedis = Join-Path $instanceRoot 'config\redis.conf'
    $expectedZLM = Join-Path $instanceRoot 'config\zlm.ini'
    $artifactCandidates = @($expectedConfig, $expectedRedis, $expectedZLM)
    $artifacts = @($artifactCandidates | Where-Object { Test-Path -LiteralPath $_ -PathType Leaf })
    if ($artifacts.Count -ne 0) {
        throw 'disk-full initialization left a configuration artifact'
    }

    $null = Remove-Item -LiteralPath $fillDir -Recurse -Force
    $fillDir = $null
    $releasedVolume = Get-TestVolume
    if ([uint64]$releasedVolume.SizeRemaining -le 0) {
        throw 'test fill was removed but the volume still reports no free space'
    }
    $retryProbe = Invoke-ConfigProbe 'retry'
    $retry = Read-SuccessProbe $retryProbe $expectedConfig $expectedRedis $expectedZLM
    $fileHashes = [ordered]@{
        config_sha256 = Get-SHA256 $expectedConfig
        redis_sha256 = Get-SHA256 $expectedRedis
        zlm_sha256 = Get-SHA256 $expectedZLM
    }
    if ($fileHashes.config_sha256 -ne $retry.config_sha256) {
        throw 'retry probe hash does not match the published config file'
    }
    $report = [ordered]@{
        passed = $true
        test = 'T14-C'
        probe_exe_sha256 = Get-SHA256 $probe
        vhd_path = $vhd
        drive_letter = $drive
        volume_label = $volume.FileSystemLabel
        volume_size_bytes = [uint64]$volume.Size
        vhd_maximum_mib = 64
        fill = [ordered]@{
            path = $fillPath
            bytes_written = $fill.bytes_written
            attempted_bytes = $fill.attempted_bytes
            files = $fill.files
            cap_bytes = $fill.cap_bytes
            reached_no_space = $fill.reached_no_space
            error_code = $fill.error_code
            remaining_bytes_before_probe = [uint64]$fullVolume.SizeRemaining
            released = $true
            remaining_bytes_after_release = [uint64]$releasedVolume.SizeRemaining
        }
        full_probe = [ordered]@{
            exit_code = $fullProbe.exit_code
            stdout_sha256 = $fullProbe.stdout_sha256
            stderr_sha256 = $fullProbe.stderr_sha256
            left_config_artifacts = $false
        }
        retry_probe = $retry
        file_hashes = $fileHashes
    }
} catch {
    $failureMessage = $_.Exception.Message
} finally {
    if ($null -ne $fillDir -and (Test-Path -LiteralPath $fillDir)) {
        try {
            $fillItem = Get-Item -LiteralPath $fillDir -Force
            if (($fillItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
                throw 'fill directory became a reparse point; refusing deletion'
            }
            Remove-Item -LiteralPath $fillDir -Recurse -Force
        } catch {
            $null = $cleanupErrors.Add('remove test fill failed')
        }
    }
    if ($vhdOwned -and $null -ne $vhd -and (Test-Path -LiteralPath $vhd)) {
        try {
            $image = Get-DiskImage -ImagePath $vhd -ErrorAction Stop
            if ($image.Attached) {
                $null = Invoke-DiskPart @(
                    "select vdisk file=`"$vhd`"",
                    'detach vdisk',
                    'exit'
                ) 'detach'
                $afterDetach = Get-DiskImage -ImagePath $vhd -ErrorAction Stop
                if ($afterDetach.Attached) {
                    throw 'test VHD remains attached'
                }
                if (Test-Path -LiteralPath $driveRoot) {
                    throw 'test VHD drive letter remains assigned'
                }
            }
            $vhdAttached = $false
        } catch {
            $null = $cleanupErrors.Add('detach test VHD failed')
        }
    }
    foreach ($name in $envNames) {
        [Environment]::SetEnvironmentVariable($name, $previousEnv[$name], 'Process')
    }
}

if ($cleanupErrors.Count -gt 0) {
    $failureMessage = if ([string]::IsNullOrWhiteSpace($failureMessage)) { 'cleanup failed' } else { $failureMessage }
    $report = $null
}
if ($null -ne $report) {
    $report | ConvertTo-Json -Depth 8
    exit 0
}
[ordered]@{
    passed = $false
    test = 'T14-C'
    error = $failureMessage
    vhd_path = $vhd
    drive_letter = $drive
    vhd_attached = $vhdAttached
    cleanup_errors = @($cleanupErrors)
} | ConvertTo-Json -Depth 8
exit 1

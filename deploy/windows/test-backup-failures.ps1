#requires -Version 5.1
<#!
T24 native Windows backup failure and cross-account ACL acceptance.
The script creates one disposable 64 MiB VHD, leaves that VHD attached only
while it is running, and retains the detached VHD plus test evidence.
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$LauncherExe,
    [Parameter(Mandatory = $true)][string]$InstallDir,
    [Parameter(Mandatory = $true)][string]$RecordingsDir,
    [Parameter(Mandatory = $true)][string]$WorkRoot,
    [ValidatePattern('^[D-Zd-z]$')][string]$DriveLetter = 'V'
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false)
$OutputEncoding = [Console]::OutputEncoding

$work = $null
$vhd = $null
$fillDir = $null
$vhdAttached = $false
$vhdOwned = $false
$readerCreated = $false
$readerName = $null
$readerPassword = $null
$failure = $null
$cleanupErrors = New-Object 'System.Collections.Generic.List[string]'
$report = [ordered]@{ test = 'T24'; passed = $false }
$drive = $DriveLetter.ToUpperInvariant()
$driveRoot = $drive + ':\'
$volumeLabel = $null
$targetFreeBytes = [uint64](1 * 1024 * 1024)
$fillCapBytes = [uint64](128 * 1024 * 1024)
$databaseMinimumBytes = [uint64](2 * 1024 * 1024)

function Stop-Test([string]$Code) { throw $Code }

function Resolve-LocalPath([string]$Path, [string]$Name) {
    if ([string]::IsNullOrWhiteSpace($Path) -or $Path -match '["\r\n]' -or $Path -match '^\\\\') { Stop-Test "${Name}_invalid" }
    try { $full = [IO.Path]::GetFullPath($Path) } catch { Stop-Test "${Name}_invalid" }
    if ($full -notmatch '^[A-Za-z]:\\') { Stop-Test "${Name}_local_required" }
    return $full.TrimEnd('\')
}

function Assert-Directory([string]$Path, [string]$Name) {
    if (-not (Test-Path -LiteralPath $Path -PathType Container)) { Stop-Test "${Name}_missing" }
    try { $item = Get-Item -LiteralPath $Path -Force } catch { Stop-Test "${Name}_unreadable" }
    if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { Stop-Test "${Name}_reparse" }
}

function Assert-FreeDrive([string]$Letter) {
    if (Test-Path -LiteralPath ($Letter + ':\') -PathType Container) { Stop-Test 'drive_letter_in_use' }
    if ($null -ne (Get-Volume -DriveLetter $Letter -ErrorAction SilentlyContinue)) { Stop-Test 'drive_letter_in_use' }
}

function Invoke-DiskPart([string[]]$Commands, [string]$Stage) {
    $scriptPath = Join-Path $work ("diskpart-$Stage-$([Guid]::NewGuid().ToString('N')).txt")
    $logPath = Join-Path $work ("diskpart-$Stage.log")
    try {
        $Commands | Set-Content -LiteralPath $scriptPath -Encoding ASCII
        & "$env:SystemRoot\System32\diskpart.exe" /s $scriptPath *> $logPath
        if ($LASTEXITCODE -ne 0) { Stop-Test "diskpart_${Stage}_failed" }
    } finally {
        if (Test-Path -LiteralPath $scriptPath) { Remove-Item -LiteralPath $scriptPath -Force -ErrorAction SilentlyContinue }
    }
}

function Get-TestVolume {
    if (-not (Test-Path -LiteralPath $driveRoot -PathType Container)) { Stop-Test 'test_volume_missing' }
    $volume = Get-Volume -DriveLetter $drive -ErrorAction Stop
    if ([string]$volume.FileSystemLabel -ne $volumeLabel) { Stop-Test 'test_volume_label_mismatch' }
    $size = [uint64]$volume.Size
    if ($size -le 0 -or $size -gt [uint64](64 * 1024 * 1024)) { Stop-Test 'test_volume_size_mismatch' }
    return $volume
}

function Get-TreeFingerprint([string]$Root, [string]$Name) {
    Assert-Directory $Root $Name
    $base = (Get-Item -LiteralPath $Root -Force).FullName.TrimEnd('\') + '\'
    $lines = New-Object 'System.Collections.Generic.List[string]'
    foreach ($entry in @(Get-ChildItem -LiteralPath $Root -Recurse -Force -File | Sort-Object FullName)) {
        if (($entry.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { Stop-Test "${Name}_reparse" }
        try { $hash = (Get-FileHash -LiteralPath $entry.FullName -Algorithm SHA256).Hash.ToLowerInvariant() } catch { Stop-Test "${Name}_hash_failed" }
        $relative = $entry.FullName.Substring($base.Length).Replace('\', '/')
        $null = $lines.Add($relative + '|' + $hash)
    }
    $payload = [Text.UTF8Encoding]::new($false).GetBytes([string]::Join("`n", $lines))
    $digest = [Security.Cryptography.SHA256]::Create().ComputeHash($payload)
    return [ordered]@{ count = $lines.Count; sha256 = (($digest | ForEach-Object { $_.ToString('x2') }) -join '') }
}

function Assert-SourceStopped([string]$Root) {
    $markers = @(Get-ChildItem -LiteralPath $Root -Recurse -Force -File -Filter '.uvp-running.json')
    if ($markers.Count -ne 0) { Stop-Test 'source_running_marker' }
    $releaseRoot = Join-Path $Root 'releases'
    $componentPaths = New-Object 'System.Collections.Generic.HashSet[string]' ([StringComparer]::OrdinalIgnoreCase)
    $componentNames = New-Object 'System.Collections.Generic.HashSet[string]' ([StringComparer]::OrdinalIgnoreCase)
    if (Test-Path -LiteralPath $releaseRoot -PathType Container) {
        foreach ($release in @(Get-ChildItem -LiteralPath $releaseRoot -Directory -Force)) {
            foreach ($relative in @('backend\uvp-server.exe', 'media\MediaServer.exe', 'redis\redis-server.exe')) {
                $candidate = Join-Path $release.FullName $relative
                if (Test-Path -LiteralPath $candidate -PathType Leaf) {
                    $full = [IO.Path]::GetFullPath($candidate)
                    $null = $componentPaths.Add($full)
                    $null = $componentNames.Add((Split-Path -Leaf $full))
                }
            }
        }
    }
    foreach ($process in @(Get-CimInstance Win32_Process -ErrorAction Stop)) {
        if (-not $componentNames.Contains([string]$process.Name)) { continue }
        $path = [string]$process.ExecutablePath
        if ([string]::IsNullOrWhiteSpace($path)) { Stop-Test 'source_component_state_unknown' }
        if ($componentPaths.Contains([IO.Path]::GetFullPath($path))) { Stop-Test 'source_component_running' }
    }
}

function Fill-ToTargetFree([string]$Directory) {
    $before = [uint64](Get-TestVolume).SizeRemaining
    if ($before -le ($targetFreeBytes + $databaseMinimumBytes + 1MB)) { Stop-Test 'test_volume_too_small' }
    $path = Join-Path $Directory 'fill.bin'
    $buffer = New-Object byte[] (1MB)
    $stream = $null
    $written = [uint64]0
    try {
        $stream = [IO.FileStream]::new($path, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::None, 1, [IO.FileOptions]::WriteThrough)
        while ($written -lt $fillCapBytes) {
            $free = [uint64](Get-TestVolume).SizeRemaining
            if ($free -le $targetFreeBytes) { break }
            $need = $free - $targetFreeBytes
            [uint64]$chunk = [Math]::Min([double](1MB), [double]$need)
            if ($need -lt 1MB) { $chunk = [Math]::Min([double]64KB, [double]$need) }
            if ($need -lt 64KB) { $chunk = [Math]::Min([double]4KB, [double]$need) }
            if ($chunk -lt 1) { break }
            $stream.Write($buffer, 0, [int]$chunk)
            $stream.Flush($true)
            $written = [uint64]$stream.Length
        }
    } catch { Stop-Test 'test_volume_fill_failed' } finally {
        if ($null -ne $stream) { $stream.Dispose() }
    }
    $after = [uint64](Get-TestVolume).SizeRemaining
    if ($written -gt $fillCapBytes -or $after -lt 512KB -or $after -gt 2MB) { Stop-Test 'test_volume_free_target_mismatch' }
    return [ordered]@{ bytes_written = $written; free_before = $before; free_after = $after; target_free = $targetFreeBytes; cap = $fillCapBytes }
}

function Invoke-Backup([string]$Name, [string]$Output) {
    $stdout = Join-Path $work ("backup-$Name.stdout")
    $stderr = Join-Path $work ("backup-$Name.stderr")
    $args = @('backup', '--install-dir', ('"{0}"' -f $InstallDir), '--recordings-dir', ('"{0}"' -f $RecordingsDir), '--output', ('"{0}"' -f $Output))
    $process = Start-Process -FilePath $LauncherExe -ArgumentList $args -WorkingDirectory (Split-Path -Parent $LauncherExe) -PassThru -RedirectStandardOutput $stdout -RedirectStandardError $stderr -WindowStyle Hidden
    $processHandle = $process.Handle
    if (-not $process.WaitForExit(10 * 60 * 1000)) {
        try { $process.Kill() } catch { }
        try { $process.WaitForExit() } catch { }
        Stop-Test "backup_${Name}_timeout"
    }
    $process.Refresh()
    return [ordered]@{ name = $Name; exit_code = [int]$process.ExitCode; stdout_sha256 = (Get-FileHash -LiteralPath $stdout -Algorithm SHA256).Hash.ToLowerInvariant(); stderr_sha256 = (Get-FileHash -LiteralPath $stderr -Algorithm SHA256).Hash.ToLowerInvariant() }
}

Add-Type -TypeDefinition @'
using System;
using System.ComponentModel;
using System.IO;
using System.Runtime.InteropServices;
using System.Security.Principal;
public static class UvpT24AclProbe {
 [DllImport("advapi32.dll", CharSet=CharSet.Unicode, SetLastError=true)] static extern bool LogonUser(string user,string domain,string password,int type,int provider,out IntPtr token);
 [DllImport("kernel32.dll", SetLastError=true)] static extern bool CloseHandle(IntPtr handle);
 public sealed class Result { public bool LoggedOn; public string LogonError; public string Parent; public string Config; public string Database; public string Redis; }
 static string ErrorKind(Exception error) {
  if (error is UnauthorizedAccessException) return "access_denied";
  if (error is FileNotFoundException || error is DirectoryNotFoundException) return "path_missing";
  var win = error as Win32Exception;
  if (win != null && win.NativeErrorCode == 5) return "access_denied";
  if (win != null && (win.NativeErrorCode == 2 || win.NativeErrorCode == 3)) return "path_missing";
  return "other";
 }
 static string ReadFile(string path) {
  try { using (var stream = new FileStream(path, FileMode.Open, FileAccess.Read, FileShare.ReadWrite | FileShare.Delete)) { if (stream.Length > 0) stream.ReadByte(); } return "readable"; }
  catch (Exception error) { return ErrorKind(error); }
 }
 static string ReadDirectory(string path) {
  try { Directory.GetFileSystemEntries(path); return "readable"; }
  catch (Exception error) { return ErrorKind(error); }
 }
 public static Result Run(string user,string password,string parent,string config,string database,string redis) {
  var result = new Result(); IntPtr token = IntPtr.Zero;
  if (!LogonUser(user,".",password,2,0,out token)) { result.LogonError="logon_failed"; return result; }
  result.LoggedOn=true;
  try { using (var scope = WindowsIdentity.Impersonate(token)) { result.Parent=ReadDirectory(parent); result.Config=ReadFile(config); result.Database=ReadFile(database); result.Redis=ReadFile(redis); } }
  catch { result.Parent="other"; result.Config="other"; result.Database="other"; result.Redis="other"; }
  finally { CloseHandle(token); }
  return result;
 }
}
'@

try {
    $launcher = Resolve-LocalPath $LauncherExe 'launcher'
    $install = Resolve-LocalPath $InstallDir 'install'
    $recordings = Resolve-LocalPath $RecordingsDir 'recordings'
    $work = Resolve-LocalPath $WorkRoot 'work'
    if (Test-Path -LiteralPath $work) { Stop-Test 'work_must_be_new' }
    Assert-Directory (Split-Path -Parent $work) 'work_parent'
    if (-not (Test-Path -LiteralPath $launcher -PathType Leaf)) { Stop-Test 'launcher_missing' }
    if ([IO.Path]::GetExtension($launcher).ToLowerInvariant() -ne '.exe') { Stop-Test 'launcher_invalid' }
    Assert-Directory $install 'install'
    Assert-Directory (Join-Path $install 'config') 'source_config'
    Assert-Directory (Join-Path $install 'data') 'source_data'
    Assert-Directory $recordings 'recordings'
    if ([IO.Path]::GetPathRoot($launcher).ToUpperInvariant() -eq $driveRoot.ToUpperInvariant() -or [IO.Path]::GetPathRoot($install).ToUpperInvariant() -eq $driveRoot.ToUpperInvariant() -or [IO.Path]::GetPathRoot($recordings).ToUpperInvariant() -eq $driveRoot.ToUpperInvariant()) { Stop-Test 'source_must_be_outside_test_vhd' }
    $current = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = New-Object Security.Principal.WindowsPrincipal($current)
    if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) { Stop-Test 'administrator_required' }
    Assert-SourceStopped $install
    $databasePath = Join-Path $install 'data\uvp.db'
    $database = Get-Item -LiteralPath $databasePath -Force -ErrorAction SilentlyContinue
    if ($null -eq $database -or $database.Length -lt $databaseMinimumBytes) { Stop-Test 'source_database_too_small' }
    $sourceConfigBefore = Get-TreeFingerprint (Join-Path $install 'config') 'source_config'
    $sourceDataBefore = Get-TreeFingerprint (Join-Path $install 'data') 'source_data'
    $recordingsBefore = Get-TreeFingerprint $recordings 'recordings'

    New-Item -ItemType Directory -Path $work | Out-Null
    Assert-FreeDrive $drive
    $vhdOwned = $true
    $vhd = Join-Path $work ("uvp-t24-backup-$([Guid]::NewGuid().ToString('N')).vhd")
    if ($vhd -match '[^\x00-\x7F]' -or (Test-Path -LiteralPath $vhd)) { Stop-Test 'vhd_path_invalid' }
    $volumeLabel = 'UVP_T24_' + [Guid]::NewGuid().ToString('N').Substring(0, 12)
    Invoke-DiskPart @(
        "create vdisk file=`"$vhd`" maximum=64 type=expandable",
        "select vdisk file=`"$vhd`"",
        'attach vdisk',
        'create partition primary',
        "format fs=ntfs quick label=$volumeLabel",
        "assign letter=$drive",
        'exit'
    ) 'create'
    $image = Get-DiskImage -ImagePath $vhd -ErrorAction Stop
    if (-not $image.Attached) { Stop-Test 'vhd_not_attached' }
    $vhdAttached = $true
    $volume = Get-TestVolume
    $report.vhd_label = $volume.FileSystemLabel
    $report.vhd_size_bytes = [uint64]$volume.Size
    $fillDir = Join-Path $driveRoot ("uvp-t24-fill-$([Guid]::NewGuid().ToString('N'))")
    New-Item -ItemType Directory -Path $fillDir | Out-Null
    $fill = Fill-ToTargetFree $fillDir
    $failedOutput = Join-Path $driveRoot ("backup-enospc-$([Guid]::NewGuid().ToString('N'))")
    $retryOutput = Join-Path $driveRoot ("backup-retry-$([Guid]::NewGuid().ToString('N'))")
    $failed = Invoke-Backup 'enospc' $failedOutput
    if ($failed.exit_code -eq 0) { Stop-Test 'enospc_backup_unexpected_success' }
    $diskError = [IO.File]::ReadAllText((Join-Path $work 'backup-enospc.stderr'))
    if ($diskError -notmatch '(?i)not enough space|disk.{0,16}full|\u78c1\u76d8\u7a7a\u95f4\u4e0d\u8db3') { Stop-Test 'enospc_error_not_confirmed' }
    $failedComplete = Test-Path -LiteralPath (Join-Path $failedOutput 'complete.json') -PathType Leaf
    if ($failedComplete) { Stop-Test 'enospc_backup_published_complete' }
    $report.failed_backup = [ordered]@{ exit_code = $failed.exit_code; disk_full_confirmed = $true; complete_json = $false; stdout_sha256 = $failed.stdout_sha256; stderr_sha256 = $failed.stderr_sha256 }
    $fillItem = Get-Item -LiteralPath $fillDir -Force
    if (($fillItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { Stop-Test 'fill_reparse' }
    Remove-Item -LiteralPath $fillDir -Recurse -Force
    $fillDir = $null
    $released = Get-TestVolume
    $retry = Invoke-Backup 'retry' $retryOutput
    $completePath = Join-Path $retryOutput 'complete.json'
    $manifestPath = Join-Path $retryOutput 'manifest.json'
    if ($retry.exit_code -ne 0 -or -not (Test-Path -LiteralPath $completePath -PathType Leaf)) { Stop-Test 'retry_backup_failed' }
    if (-not (Test-Path -LiteralPath $manifestPath -PathType Leaf) -or -not (Test-Path -LiteralPath (Join-Path $retryOutput 'config\config.yml') -PathType Leaf) -or -not (Test-Path -LiteralPath (Join-Path $retryOutput 'data\uvp.db') -PathType Leaf) -or -not (Test-Path -LiteralPath (Join-Path $retryOutput 'data\redis') -PathType Container)) { Stop-Test 'retry_backup_missing_authoritative_data' }
    if (@(Get-ChildItem -LiteralPath $retryOutput -Recurse -Force -Filter '.uvp-running.json').Count -ne 0 -or (Test-Path -LiteralPath (Join-Path $retryOutput 'recordings'))) { Stop-Test 'retry_backup_contains_runtime_or_recordings' }
    $redisFile = Get-ChildItem -LiteralPath (Join-Path $retryOutput 'data\redis') -Recurse -Force -File | Select-Object -First 1
    if ($null -eq $redisFile) { Stop-Test 'retry_backup_missing_redis_file' }
    $backupAcl = Get-Acl -LiteralPath $retryOutput
    $ownerSid = $backupAcl.GetOwner([Security.Principal.SecurityIdentifier]).Value
    if ($ownerSid -notin @($current.User.Value, 'S-1-5-18', 'S-1-5-32-544') -or -not $backupAcl.AreAccessRulesProtected) { Stop-Test 'backup_owner_or_protection_mismatch' }
    foreach ($rule in $backupAcl.GetAccessRules($true, $true, [Security.Principal.SecurityIdentifier])) {
        if ($rule.AccessControlType -eq 'Allow' -and $rule.IdentityReference.Value -notin @($current.User.Value, 'S-1-5-18')) { Stop-Test 'backup_unexpected_reader' }
    }
    $readerName = 'uvpt24' + [Guid]::NewGuid().ToString('N').Substring(0, 10)
    $bytes = New-Object byte[] 32
    $rng = [Security.Cryptography.RandomNumberGenerator]::Create()
    try { $rng.GetBytes($bytes) } finally { $rng.Dispose() }
    $readerPassword = 'Aa1!' + [Convert]::ToBase64String($bytes)
    $secure = ConvertTo-SecureString $readerPassword -AsPlainText -Force
    $null = New-LocalUser -Name $readerName -Password $secure -Description 'Temporary UVP T24 backup ACL test'
    $readerCreated = $true
    $usersGroup = Get-LocalGroup -SID 'S-1-5-32-545'
    $null = Add-LocalGroupMember -Group $usersGroup -Member $readerName
    $adminGroup = Get-LocalGroup -SID 'S-1-5-32-544'
    if (@(Get-LocalGroupMember -Group $adminGroup -ErrorAction Stop | Where-Object { $_.SID.Value -eq (Get-LocalUser $readerName).SID.Value }).Count -ne 0) { Stop-Test 'reader_not_standard' }
    $aclRaw = [UvpT24AclProbe]::Run($readerName, $readerPassword, (Split-Path -Parent $retryOutput), (Join-Path $retryOutput 'config\config.yml'), (Join-Path $retryOutput 'data\uvp.db'), $redisFile.FullName)
    $report.acl = [ordered]@{ logged_on = [bool]$aclRaw.LoggedOn; parent = [string]$aclRaw.Parent; config = [string]$aclRaw.Config; database = [string]$aclRaw.Database; redis = [string]$aclRaw.Redis; owner_is_current = ($ownerSid -eq $current.User.Value); owner_is_allowed = $true; inheritance_protected = $true }
    if (-not $aclRaw.LoggedOn) { Stop-Test 'acl_logon_failed' }
    if ($aclRaw.Parent -ne 'readable') { Stop-Test ('acl_parent_' + [string]$aclRaw.Parent) }
    foreach ($status in @($aclRaw.Config, $aclRaw.Database, $aclRaw.Redis)) {
        if ($status -eq 'path_missing') { Stop-Test 'acl_path_missing' }
        if ($status -ne 'access_denied') { Stop-Test 'acl_read_failed' }
    }
    $sourceConfigAfter = Get-TreeFingerprint (Join-Path $install 'config') 'source_config'
    $sourceDataAfter = Get-TreeFingerprint (Join-Path $install 'data') 'source_data'
    $recordingsAfter = Get-TreeFingerprint $recordings 'recordings'
    $report.passed = ($sourceConfigBefore.sha256 -eq $sourceConfigAfter.sha256 -and $sourceDataBefore.sha256 -eq $sourceDataAfter.sha256 -and $recordingsBefore.sha256 -eq $recordingsAfter.sha256)
    if (-not $report.passed) { Stop-Test 'source_changed' }
    $report.source = [ordered]@{ config_unchanged = $true; data_unchanged = $true; recordings_unchanged = $true; config_files = $sourceConfigAfter.count; data_files = $sourceDataAfter.count; recording_files = $recordingsAfter.count }
    $report.retry_backup = [ordered]@{ exit_code = $retry.exit_code; complete_json = $true; stdout_sha256 = $retry.stdout_sha256; stderr_sha256 = $retry.stderr_sha256; config_present = $true; data_present = $true; redis_file_present = $true }
    $report.fill = $fill
    $report.released_free_bytes = [uint64]$released.SizeRemaining
    $report.reader_is_standard = $true
    $report.evidence_vhd_retained = $true
} catch {
    $message = [string]$_.Exception.Message
    if ($message -match '^[a-zA-Z0-9_]+$') { $failure = $message } else { $failure = 'test_failed' }
} finally {
    if ($null -ne $fillDir -and (Test-Path -LiteralPath $fillDir)) {
        try {
            $item = Get-Item -LiteralPath $fillDir -Force
            if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { $null = $cleanupErrors.Add('fill_reparse') } else { Remove-Item -LiteralPath $fillDir -Recurse -Force }
        } catch { $null = $cleanupErrors.Add('fill_cleanup_failed') }
    }
    if ($readerCreated) {
        try {
            $sid = (Get-LocalUser -Name $readerName -ErrorAction Stop).SID.Value
            Get-CimInstance Win32_UserProfile -Filter "SID='$sid'" -ErrorAction SilentlyContinue | Remove-CimInstance -ErrorAction Stop
        } catch { $null = $cleanupErrors.Add('reader_profile_cleanup_failed') }
        try { Remove-LocalUser -Name $readerName -ErrorAction Stop } catch { $null = $cleanupErrors.Add('reader_account_cleanup_failed') }
    }
    $readerPassword = $null
    if ($vhdOwned -and $null -ne $vhd -and (Test-Path -LiteralPath $vhd)) {
        try {
            $image = Get-DiskImage -ImagePath $vhd -ErrorAction Stop
            if ($image.Attached) {
                Invoke-DiskPart @("select vdisk file=`"$vhd`"", 'detach vdisk', 'exit') 'detach'
                if ((Get-DiskImage -ImagePath $vhd -ErrorAction Stop).Attached) { $null = $cleanupErrors.Add('vhd_still_attached') }
            }
            $vhdAttached = $false
        } catch { $null = $cleanupErrors.Add('vhd_detach_failed') }
    }
}

if ($null -eq $failure -and $cleanupErrors.Count -eq 0 -and $report.passed) {
    $report | ConvertTo-Json -Depth 8
    exit 0
}
$report.passed = $false
$report.error = if ($null -ne $failure) { $failure } else { 'cleanup_failed' }
$report.cleanup_errors = @($cleanupErrors)
$report.evidence_vhd_retained = ($vhdOwned -and $null -ne $vhd -and (Test-Path -LiteralPath $vhd))
$report.vhd_attached = $vhdAttached
$report | ConvertTo-Json -Depth 8
exit 1

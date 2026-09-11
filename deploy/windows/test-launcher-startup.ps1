param(
 [Parameter(Mandatory=$true)][string]$SourceRoot,
 [Parameter(Mandatory=$true)][string]$WorkRoot,
 [string]$LauncherName='UVP-t15-final.exe',
 [string]$BackendName='server-t15.exe'
)
$ErrorActionPreference='Stop'
$source=$SourceRoot
$root=$WorkRoot
if(Test-Path $root){throw 'Test root already exists'}
$release=Join-Path $root 'releases\t15-smoke'
New-Item -ItemType Directory -Path (Join-Path $release 'backend'),(Join-Path $release 'web'),(Join-Path $release 'resource') -Force|Out-Null
Copy-Item (Join-Path $source $LauncherName) (Join-Path $root 'UVP.exe')
Copy-Item (Join-Path $source $BackendName) (Join-Path $release 'backend\uvp-server.exe')
Copy-Item (Join-Path $source 'redis-package') (Join-Path $release 'redis') -Recurse
Copy-Item (Join-Path $source 'zlm-package-v3\media') (Join-Path $release 'media') -Recurse
Add-Type -AssemblyName System.IO.Compression.FileSystem
[IO.Compression.ZipFile]::ExtractToDirectory((Join-Path $source 'uvp-web-t17-v1.zip'),(Join-Path $release 'web'))
[IO.Compression.ZipFile]::ExtractToDirectory((Join-Path $source 'resource-t15.zip'),(Join-Path $release 'resource'))
$files=@(Get-ChildItem -LiteralPath $release -File -Recurse|ForEach-Object {@{path=$_.FullName.Substring($release.Length+1).Replace('\','/');sha256=(Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLower()}})
$manifest=@{format_version=1;version='t15-smoke';source_commit='1817c768-working-tree-smoke';schema_min=1;schema_max=2;files=$files}|ConvertTo-Json -Depth 6
[IO.File]::WriteAllText((Join-Path $release 'manifest.json'),$manifest,(New-Object Text.UTF8Encoding($false)))
'{"version":"t15-smoke"}'|Set-Content -LiteralPath (Join-Path $root 'current.json') -Encoding ASCII
$stdout=Join-Path $root 'launcher.stdout'
$stderr=Join-Path $root 'launcher.stderr'
$p=Start-Process -FilePath (Join-Path $root 'UVP.exe') -ArgumentList '-no-browser' -WorkingDirectory $root -PassThru -RedirectStandardOutput $stdout -RedirectStandardError $stderr
$ownedHandle=$p.Handle
$killed=$false
try {
$deadline=[DateTime]::UtcNow.AddSeconds(45)
$ready=$false
while([DateTime]::UtcNow -lt $deadline -and -not $p.HasExited){
 if((Get-Content -LiteralPath $stdout -Encoding UTF8 -Raw -ErrorAction SilentlyContinue) -match 'MediaReady'){$ready=$true;break}
 Start-Sleep -Milliseconds 200
 $p.Refresh()
}
$before=@(Get-CimInstance Win32_Process|Where-Object {$_.ExecutablePath -and $_.ExecutablePath.StartsWith($release,[StringComparison]::OrdinalIgnoreCase)}|Select-Object ProcessId,Name,ExecutablePath)
$secondCode=$null
if($ready){
 $second=Start-Process -FilePath (Join-Path $root 'UVP.exe') -ArgumentList '-no-browser' -WorkingDirectory $root -PassThru -Wait -RedirectStandardOutput (Join-Path $root 'second.stdout') -RedirectStandardError (Join-Path $root 'second.stderr')
 $secondCode=$second.ExitCode
}
} finally {
 if(-not $p.HasExited){$p.Kill();$p.WaitForExit();$killed=$true}
}
Start-Sleep -Milliseconds 500
$after=@(Get-CimInstance Win32_Process|Where-Object {$_.ExecutablePath -and $_.ExecutablePath.StartsWith($release,[StringComparison]::OrdinalIgnoreCase)}|Select-Object ProcessId,Name)
function SafeText($text){return [regex]::Replace([string]$text,'[A-Za-z0-9_-]{43,}','[REDACTED]')}
$secretValues=@([regex]::Matches((Get-Content -LiteralPath (Join-Path $root 'config\config.yml') -Raw),'[A-Za-z0-9_-]{43,}')|ForEach-Object {$_.Value})
$leaks=@()
$logs=@{}
Get-ChildItem -LiteralPath (Join-Path $root 'logs') -File -ErrorAction SilentlyContinue|ForEach-Object {$rawLog=Get-Content -LiteralPath $_.FullName -Encoding UTF8 -Raw; if(@($secretValues|Where-Object {$rawLog -and $rawLog.Contains($_)}).Count){$leaks+=$_.Name}; $logs[$_.Name]=SafeText ((Get-Content -LiteralPath $_.FullName -Encoding UTF8 -Tail 12)-join "`n")}
@{secret_leak_files=$leaks;root=$root;ready=$ready;second_exit=$secondCode;launcher_exit=$p.ExitCode;forced_cleanup=$killed;before=$before;after=$after;stdout=(SafeText (Get-Content -LiteralPath $stdout -Encoding UTF8 -Raw));stderr=(SafeText (Get-Content -LiteralPath $stderr -Encoding UTF8 -Raw));logs=$logs}|ConvertTo-Json -Depth 6

if(-not $ready -or $secondCode -ne 1 -or $before.Count -ne 3 -or $after.Count -ne 0 -or $leaks.Count -ne 0){exit 1}

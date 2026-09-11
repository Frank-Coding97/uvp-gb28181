param(
 [Parameter(Mandatory=$true)][string]$SourceRoot,
 [Parameter(Mandatory=$true)][string]$WorkRoot,
 [Parameter(Mandatory=$true)][string]$MediaDirectory,
 [string]$LauncherName='UVP-t16.exe',
 [string]$BackendName='uvp-backend-t16.exe'
)
$ErrorActionPreference='Stop'
if(Test-Path -LiteralPath $WorkRoot){throw 'Test root already exists'}
$release=Join-Path $WorkRoot 'releases\t16-stop'
New-Item -ItemType Directory -Path (Join-Path $release 'backend'),(Join-Path $release 'web'),(Join-Path $release 'resource') -Force|Out-Null
$exe=Join-Path $WorkRoot 'UVP.exe'
Copy-Item (Join-Path $SourceRoot $LauncherName) $exe
Copy-Item (Join-Path $SourceRoot $BackendName) (Join-Path $release 'backend\uvp-server.exe')
Copy-Item (Join-Path $SourceRoot 'redis-package') (Join-Path $release 'redis') -Recurse
Copy-Item $MediaDirectory (Join-Path $release 'media') -Recurse
Add-Type -AssemblyName System.IO.Compression.FileSystem
[IO.Compression.ZipFile]::ExtractToDirectory((Join-Path $SourceRoot 'uvp-web-t17-v1.zip'),(Join-Path $release 'web'))
[IO.Compression.ZipFile]::ExtractToDirectory((Join-Path $SourceRoot 'resource-t15.zip'),(Join-Path $release 'resource'))
$files=@(Get-ChildItem -LiteralPath $release -File -Recurse|ForEach-Object {@{path=$_.FullName.Substring($release.Length+1).Replace('\','/');sha256=(Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLower()}})
$manifest=@{format_version=1;version='t16-stop';source_commit='t16-native-stop-fixture';schema_min=1;schema_max=2;files=$files}|ConvertTo-Json -Depth 6
[IO.File]::WriteAllText((Join-Path $release 'manifest.json'),$manifest,(New-Object Text.UTF8Encoding($false)))
'{"version":"t16-stop"}'|Set-Content -LiteralPath (Join-Path $WorkRoot 'current.json') -Encoding ASCII
$marker=Join-Path $WorkRoot 'data\.uvp-running.json'
$results=@()
foreach($cycle in 1..3){
 $stdout=Join-Path $WorkRoot "cycle-$cycle.stdout"
 $stderr=Join-Path $WorkRoot "cycle-$cycle.stderr"
 $p=Start-Process -FilePath $exe -ArgumentList '-no-browser' -WorkingDirectory $WorkRoot -PassThru -RedirectStandardOutput $stdout -RedirectStandardError $stderr
 $ownedHandle=$p.Handle
 try {
  $deadline=[DateTime]::UtcNow.AddSeconds(60)
  $ready=$false
  while([DateTime]::UtcNow -lt $deadline -and -not $p.HasExited){
   if((Get-Content -LiteralPath $stdout -Encoding UTF8 -Raw -ErrorAction SilentlyContinue) -match '基础组件已就绪'){$ready=$true;break}
   Start-Sleep -Milliseconds 100
   $p.Refresh()
  }
  if(-not $ready){throw "Cycle $cycle did not reach Ready; inspect protected fixture logs"}
  if(-not (Test-Path -LiteralPath $marker)){throw 'Running instance lacks abnormal-exit marker'}
  $started=[DateTime]::UtcNow
  if($cycle -eq 1){
   $p.Kill();$p.WaitForExit()
   if(-not (Test-Path -LiteralPath $marker)){throw 'Forced exit removed marker'}
  } else {
   if($cycle -eq 2 -and (Get-Content -LiteralPath $stdout -Encoding UTF8 -Raw) -notmatch '检测到上次异常退出'){throw 'Previous abnormal exit was not reported'}
   if($cycle -eq 3 -and (Get-Content -LiteralPath $stdout -Encoding UTF8 -Raw) -match '检测到上次异常退出'){throw 'Clean previous exit was marked abnormal'}
   $stop=Start-Process -FilePath $exe -ArgumentList '-stop' -WorkingDirectory $WorkRoot -PassThru -RedirectStandardOutput (Join-Path $WorkRoot "stop-$cycle.stdout") -RedirectStandardError (Join-Path $WorkRoot "stop-$cycle.stderr")
   $stopHandle=$stop.Handle
   try {
    if(-not $stop.WaitForExit(70000)){$stop.Kill();$stop.WaitForExit();throw 'Stop command exceeded deadline'}
    if($stop.ExitCode -ne 0){throw 'Stop command failed; inspect protected fixture logs'}
   } finally {$stop.Dispose()}
   if(-not $p.WaitForExit(10000)){throw 'Launcher did not exit after acknowledgement'}
   if($p.ExitCode -ne 0){throw 'Launcher reported abnormal cleanup'}
   if(Test-Path -LiteralPath $marker){throw 'Successful stop left running marker'}
  }
  $deadline=[DateTime]::UtcNow.AddSeconds(5)
  do {
   $left=@(Get-CimInstance Win32_Process|Where-Object {$_.ExecutablePath -and $_.ExecutablePath.StartsWith($release+'\',[StringComparison]::OrdinalIgnoreCase)})
   if($left.Count -eq 0){break}
   Start-Sleep -Milliseconds 50
  } while([DateTime]::UtcNow -lt $deadline)
  if($left.Count -ne 0){throw 'Owned component remains after exit'}
  foreach($port in 8280,18080,16379){
   $listener=New-Object Net.Sockets.TcpListener([Net.IPAddress]::Loopback,$port)
   try {$listener.Start()} finally {$listener.Stop()}
  }
  $results+=@{cycle=$cycle;forced=($cycle -eq 1);exit=$p.ExitCode;seconds=([DateTime]::UtcNow-$started).TotalSeconds;owned_remaining=$left.Count;marker_present=(Test-Path -LiteralPath $marker)}
 } finally {
  if(-not $p.HasExited){$p.Kill();$p.WaitForExit()}
  $p.Dispose()
 }
}
@{root=$WorkRoot;cycles=$results;desktop_test=$false;active_media_test=$false}|ConvertTo-Json -Depth 5

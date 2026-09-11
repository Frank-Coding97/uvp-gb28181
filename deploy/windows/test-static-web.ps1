param(
 [Parameter(Mandatory=$true)][string]$TestExe,
 [Parameter(Mandatory=$true)][string]$WebZip,
 [Parameter(Mandatory=$true)][string]$WorkRoot,
 [Parameter(Mandatory=$true)][string]$ResultRoot
)
$ErrorActionPreference='Stop'
[Console]::OutputEncoding=New-Object System.Text.UTF8Encoding($false)
$OutputEncoding=[Console]::OutputEncoding
if(Test-Path $WorkRoot){throw 'Use a new isolated local work directory'}
New-Item -ItemType Directory -Path $WorkRoot|Out-Null
foreach($dir in @('public','uploads')){New-Item -ItemType Directory -Path (Join-Path $WorkRoot $dir)|Out-Null}
$webRoot=Join-Path $WorkRoot 'web'
New-Item -ItemType Directory -Path $webRoot|Out-Null
Add-Type -AssemblyName System.IO.Compression.FileSystem -ErrorAction Stop
[IO.Compression.ZipFile]::ExtractToDirectory($WebZip,$webRoot)
$env:UVP_NATIVE_WEB_WORK_ROOT=$WorkRoot
$server=Start-Process $TestExe -ArgumentList @('-test.run=^TestServeNativeStandaloneWeb$','-test.timeout=120s','-test.v') -PassThru -RedirectStandardOutput "$WorkRoot\server.stdout" -RedirectStandardError "$WorkRoot\server.stderr"
$serverHandle=$server.Handle
try {
 for($i=0;$i -lt 100 -and !(Test-Path "$WorkRoot\ready.txt");$i++){Start-Sleep -Milliseconds 100}
 if(!(Test-Path "$WorkRoot\ready.txt")){throw 'web fixture did not become ready'}
 $base=[IO.File]::ReadAllText("$WorkRoot\ready.txt")
 foreach($path in @('/api/missing','/index/hook/missing','/recordings/private.mp4','/data/uvp.db','/config/config.yml','/static/missing.js')){
  try {$r=Invoke-WebRequest -UseBasicParsing -Uri ($base+$path) -Headers @{Accept='text/html'};throw "unexpected status $($r.StatusCode) for $path"}
  catch [System.Net.WebException] {if([int]$_.Exception.Response.StatusCode -ne 404){throw}}
 }
 $edge="${env:ProgramFiles(x86)}\Microsoft\Edge\Application\msedge.exe"
 if(!(Test-Path $edge)){throw 'Microsoft Edge is required for this native browser test'}
 foreach($page in @(@{name='home';path='/'},@{name='deep';path='/gb28181/device'})){
  $name=$page.name
  $browserArgs=@('--headless=new','--no-first-run','--disable-background-networking','--disable-sync','--disable-gpu','--hide-scrollbars','--window-size=1365,900','--virtual-time-budget=7000','--dump-dom',"--user-data-dir=`"$WorkRoot\browser-$name`"","--screenshot=`"$WorkRoot\$name.png`"",($base+$page.path))
  $startInfo=New-Object System.Diagnostics.ProcessStartInfo
  $startInfo.FileName=$edge;$startInfo.Arguments=$browserArgs -join ' '
  $startInfo.UseShellExecute=$false;$startInfo.CreateNoWindow=$true
  $startInfo.RedirectStandardOutput=$true;$startInfo.RedirectStandardError=$true
  $startInfo.StandardOutputEncoding=New-Object Text.UTF8Encoding($false)
  $startInfo.StandardErrorEncoding=New-Object Text.UTF8Encoding($false)
  $browser=New-Object System.Diagnostics.Process
  $browser.StartInfo=$startInfo
  if(!$browser.Start()){throw 'Could not start Edge'}
  $outputTask=$browser.StandardOutput.ReadToEndAsync()
  $errorTask=$browser.StandardError.ReadToEndAsync()
  if(!$browser.WaitForExit(30000)){$browser.Kill();throw 'Edge browser test timed out'}
  if(![System.Threading.Tasks.Task]::WaitAll([System.Threading.Tasks.Task[]]@($outputTask,$errorTask),5000)){throw 'Edge output did not close within five seconds'}
  $dom=$outputTask.Result
  [IO.File]::WriteAllText("$WorkRoot\$name.html",$dom,(New-Object Text.UTF8Encoding($false)))
  [IO.File]::WriteAllText("$WorkRoot\$name.stderr",$errorTask.Result,(New-Object Text.UTF8Encoding($false)))
  $browserExit=$browser.ExitCode;$browser.Dispose()
  if($browserExit -ne 0 -or !(Test-Path "$WorkRoot\$name.png")){throw "Edge screenshot failed: $name"}
  if($dom -notmatch '登录' -or $dom -notmatch '<input'){throw "Vue login UI did not render: $name"}
 }
 [ordered]@{passed=$true;browser_version=(Get-Item $edge).VersionInfo.FileVersion;base_url=$base;pages=@('home','deep');missing_routes=6}|ConvertTo-Json
} finally {
 [IO.File]::WriteAllText("$WorkRoot\stop.txt",'stop')
 if(!$server.WaitForExit(10000)){$server.Kill();throw 'web fixture shutdown timed out'}
 New-Item -ItemType Directory -Path $ResultRoot -Force|Out-Null
 foreach($pattern in @('*.png','*.html','*.stdout','*.stderr')){Get-ChildItem $WorkRoot -Filter $pattern -File|Copy-Item -Destination $ResultRoot}
}
$server.Refresh()
if($server.ExitCode -ne 0){throw 'web fixture test failed'}

param(
 [Parameter(Mandatory=$true)][string]$ServerExe,
 [Parameter(Mandatory=$true)][string]$WorkRoot
)
$ErrorActionPreference='Stop'
[Console]::OutputEncoding=New-Object System.Text.UTF8Encoding($false)
$OutputEncoding=[Console]::OutputEncoding
if(Test-Path $WorkRoot){throw 'Use a fresh local test directory'}
$results=@()
foreach($debug in @($true,$false)){
 $root=Join-Path $WorkRoot ('debug-'+$debug.ToString().ToLower())
 foreach($dir in @($root,"$root\config","$root\resource","$root\web","$root\data","$root\recordings")){
  New-Item -ItemType Directory -Path $dir -Force | Out-Null
 }
 $config="server:`n  appdebug: $($debug.ToString().ToLower())`ngormv2:`n  usedbtype: sqlite`n"
 [IO.File]::WriteAllText("$root\config\config.yml",$config,(New-Object Text.UTF8Encoding($false)))
 $env:UVP_INSTALL_DIR=$root;$env:UVP_CONFIG_DIR="$root\config";$env:UVP_RESOURCE_DIR="$root\resource"
 $env:UVP_WEB_DIR="$root\web";$env:UVP_DATA_DIR="$root\data";$env:UVP_RECORDINGS_DIR="$root\recordings"
 $stdout="$root\check.stdout";$stderr="$root\check.stderr"
 $p=Start-Process -FilePath $ServerExe -ArgumentList '-db-check' -WorkingDirectory 'C:\Windows\System32' -PassThru -RedirectStandardOutput $stdout -RedirectStandardError $stderr
 $handle=$p.Handle
 if(!$p.WaitForExit(15000)){$p.Kill();throw 'configuration check timed out'}
 $p.Refresh()
 $errorText=[IO.File]::ReadAllText($stderr)
 $databaseCreated=Test-Path "$root\data\uvp.db"
 if($debug){
  if($p.ExitCode -eq 0 -or $errorText -notmatch 'server.appdebug must be false') {throw 'debug configuration was not explicitly rejected'}
  if($databaseCreated){throw 'debug rejection occurred after opening the database'}
 }else{
  if($p.ExitCode -ne 0 -or !$databaseCreated){throw 'valid non-debug configuration failed'}
 }
 if((Test-Path "$root\logs") -and @(Get-ChildItem "$root\logs" -File -Recurse).Count -gt 0){throw 'configuration check started business logging'}
 $results += [ordered]@{appdebug=$debug;exit_code=$p.ExitCode;database_created=$databaseCreated;passed=$true}
}
[ordered]@{passed=$true;results=$results}|ConvertTo-Json -Depth 4

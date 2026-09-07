param(
 [Parameter(Mandatory=$true)][string]$ServerExe,
 [Parameter(Mandatory=$true)][string]$WorkRoot
)
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false)
$OutputEncoding = [Console]::OutputEncoding
if (Test-Path $WorkRoot) { throw 'Use a new isolated work directory' }
$results = @()
function Set-Fixture([string]$Name, [string]$Config) {
 $root = Join-Path $WorkRoot $Name
 foreach ($dir in @($root,"$root\config","$root\resource","$root\web","$root\data","$root\recordings")) {
  New-Item -ItemType Directory -Path $dir -Force | Out-Null
 }
 [IO.File]::WriteAllText("$root\config\config.yml",$Config,(New-Object Text.UTF8Encoding($false)))
 $env:UVP_INSTALL_DIR=$root; $env:UVP_CONFIG_DIR="$root\config"; $env:UVP_RESOURCE_DIR="$root\resource"
 $env:UVP_WEB_DIR="$root\web"; $env:UVP_DATA_DIR="$root\data"; $env:UVP_RECORDINGS_DIR="$root\recordings"
 return $root
}
function Invoke-Check([string]$Root,[string]$Name,[bool]$Accept,[string]$ExpectedError='') {
 $stdout=Join-Path $Root "$Name.stdout"
 $stderr=Join-Path $Root "$Name.stderr"
 $p=Start-Process -FilePath $ServerExe -ArgumentList '-db-check' -WorkingDirectory 'C:\Windows\System32' -PassThru -RedirectStandardOutput $stdout -RedirectStandardError $stderr
 $handle=$p.Handle
 if (!$p.WaitForExit(15000)) { $p.Kill(); throw 'db-check did not exit within 15 seconds' }
 $p.Refresh()
 $out=[IO.File]::ReadAllText($stdout)
 $err=[IO.File]::ReadAllText($stderr)
 if ($Accept) {
  if ($p.ExitCode -ne 0) { throw "db-check failed (exit=$($p.ExitCode)): $err; stdout=$out" }
  $info=$out | ConvertFrom-Json
  if ($info.version -ne '3.53.4' -or $info.journal_mode -ne 'wal' -or $info.synchronous -ne 2 -or $info.foreign_keys -ne 1 -or $info.busy_timeout_ms -ne 5000 -or $info.max_open_connections -ne 1) { throw "runtime mismatch: $out" }
  if (!(Test-Path "$Root\data\uvp.db")) { throw 'database missing at explicit path' }
 } else {
  if ($p.ExitCode -eq 0 -or $err -notmatch $ExpectedError) { throw "expected rejection missing: $err" }
  if (Test-Path "$Root\data\uvp.db") { throw 'rejected configuration created a database' }
 }
 if (Test-Path "$Root\logs") {
  if (@(Get-ChildItem "$Root\logs" -File -Recurse).Count -gt 0) { throw 'operation mode created business logs' }
 }
 return [ordered]@{name=$Name;passed=$true;exit_code=$p.ExitCode;runtime=if($Accept){$info}else{$null};error=if(!$Accept){$err}else{$null}}
}
$root=Set-Fixture 'SQLite 中文 # space' "gormv2:`n  usedbtype: sqlite`n"
$results += Invoke-Check $root 'open' $true
$before=(Get-FileHash "$root\data\uvp.db" -Algorithm SHA256).Hash
$results += Invoke-Check $root 'reopen' $true
if ((Get-FileHash "$root\data\uvp.db" -Algorithm SHA256).Hash -ne $before) { throw 'repeat diagnostic changed database contents' }
$root=Set-Fixture 'unknown' "gormv2:`n  usedbtype: unknown`n"
$results += Invoke-Check $root 'unknown' $false 'unknown database dialect'
$root=Set-Fixture 'conflict' "gormv2:`n  usedbtype: sqlite`n  mysql:`n    isinitglobalgormmysql: 1`n    host: 192.0.2.1`n"
$results += Invoke-Check $root 'conflict' $false 'cannot be combined'
$results | ConvertTo-Json -Depth 5

param(
 [Parameter(Mandatory=$true)][string]$ServerExe,
 [Parameter(Mandatory=$true)][string]$WorkRoot
)
$ErrorActionPreference='Stop'
[Console]::OutputEncoding=New-Object System.Text.UTF8Encoding($false)
$OutputEncoding=[Console]::OutputEncoding
if(Test-Path $WorkRoot){throw 'Use a new isolated work directory on the local disk'}
$root=Join-Path $WorkRoot 'SQLite 迁移 中文 # space'
foreach($dir in @($root,"$root\config","$root\resource","$root\web","$root\data","$root\recordings")){
 New-Item -ItemType Directory -Path $dir -Force|Out-Null
}
[IO.File]::WriteAllText("$root\config\config.yml","gormv2:`n  usedbtype: sqlite`n",(New-Object Text.UTF8Encoding($false)))
$env:UVP_INSTALL_DIR=$root;$env:UVP_CONFIG_DIR="$root\config";$env:UVP_RESOURCE_DIR="$root\resource"
$env:UVP_WEB_DIR="$root\web";$env:UVP_DATA_DIR="$root\data";$env:UVP_RECORDINGS_DIR="$root\recordings"
$script:run=0
function Invoke-Mode([string]$Flag,[bool]$Success){
 $script:run++
 $stdout="$root\mode-$script:run.stdout";$stderr="$root\mode-$script:run.stderr"
 $p=Start-Process -FilePath $ServerExe -ArgumentList $Flag -WorkingDirectory 'C:\Windows\System32' -PassThru -RedirectStandardOutput $stdout -RedirectStandardError $stderr
 $handle=$p.Handle
 if(!$p.WaitForExit(150000)){$p.Kill();throw 'database operation timed out'}
 $p.Refresh()
 if(($p.ExitCode -eq 0) -ne $Success){throw "unexpected exit=$($p.ExitCode) flag=$Flag : $([IO.File]::ReadAllText($stderr))"}
 return [ordered]@{flag=$Flag;exit_code=$p.ExitCode;stdout=[IO.File]::ReadAllText($stdout);stderr=[IO.File]::ReadAllText($stderr)}
}
$results=@()
$before=Invoke-Mode '-migrate-up' $false
if($before.stderr -notmatch 'baseline') {throw 'empty database must fail for missing baseline'}
$results+=$before
$results+=Invoke-Mode '-bootstrap-db' $true
$firstUp=Invoke-Mode '-migrate-up' $true
if($firstUp.stderr -notmatch 'before=1 after=2'){throw 'expected the pinned media identity increment after baseline'}
$results+=$firstUp
$hash=(Get-FileHash "$root\data\uvp.db" -Algorithm SHA256).Hash
foreach($i in 1,2){
 $result=Invoke-Mode '-migrate-up' $true
 if($result.stderr -notmatch 'before=2 after=2') {throw 'repeated migration must preserve the baseline and pinned increment markers'}
 $results+=$result
 if((Get-FileHash "$root\data\uvp.db" -Algorithm SHA256).Hash -ne $hash){throw 'no-op migration changed database'}
}
$down=Invoke-Mode '-migrate-down=2026-09-08-test-sqlite.sql' $false
if($down.stderr -notmatch 'backup') {throw 'SQLite down must direct restoration of complete backup'}
$results+=$down
if((Get-FileHash "$root\data\uvp.db" -Algorithm SHA256).Hash -ne $hash){throw 'rejected down changed database'}
if((Test-Path "$root\logs") -and @(Get-ChildItem "$root\logs" -File -Recurse).Count -gt 0){throw 'database-only mode created business logs'}
[ordered]@{passed=$true;database_sha256=$hash;results=$results}|ConvertTo-Json -Depth 5

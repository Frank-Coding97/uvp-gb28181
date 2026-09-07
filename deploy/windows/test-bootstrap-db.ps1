param(
 [Parameter(Mandatory=$true)][string]$ServerExe,
 [Parameter(Mandatory=$true)][string]$WorkRoot
)
$ErrorActionPreference='Stop'
[Console]::OutputEncoding=New-Object System.Text.UTF8Encoding($false)
$OutputEncoding=[Console]::OutputEncoding
if(Test-Path $WorkRoot){throw 'Use a new isolated work directory'}
$root=Join-Path $WorkRoot 'SQLite 初始化 中文 # space'
foreach($dir in @($root,"$root\config","$root\resource","$root\web","$root\data","$root\recordings")){
 New-Item -ItemType Directory -Path $dir -Force|Out-Null
}
[IO.File]::WriteAllText("$root\config\config.yml","gormv2:`n  usedbtype: sqlite`n",(New-Object Text.UTF8Encoding($false)))
$env:UVP_INSTALL_DIR=$root;$env:UVP_CONFIG_DIR="$root\config";$env:UVP_RESOURCE_DIR="$root\resource"
$env:UVP_WEB_DIR="$root\web";$env:UVP_DATA_DIR="$root\data";$env:UVP_RECORDINGS_DIR="$root\recordings"
$results=@();$firstHash='';$baselineChecksum=''
foreach($i in 1,2){
 $stdout="$root\bootstrap-$i.stdout";$stderr="$root\bootstrap-$i.stderr"
 $p=Start-Process -FilePath $ServerExe -ArgumentList '-bootstrap-db' -WorkingDirectory 'C:\Windows\System32' -PassThru -RedirectStandardOutput $stdout -RedirectStandardError $stderr
 $handle=$p.Handle
 if(!$p.WaitForExit(120000)){$p.Kill();throw 'bootstrap-db timed out'}
 $p.Refresh()
 if($p.ExitCode -ne 0){throw "bootstrap-db exit=$($p.ExitCode): $([IO.File]::ReadAllText($stderr))"}
 $r=[IO.File]::ReadAllText($stdout)|ConvertFrom-Json
 if($r.version -ne 'sqlite-baseline-20260907-e07857cc' -or $r.checksum -notmatch '^[a-f0-9]{64}$' -or $r.created -ne ($i -eq 1)){throw 'incorrect initialization result'}
 $hash=(Get-FileHash "$root\data\uvp.db" -Algorithm SHA256).Hash
 if($i -eq 1){$firstHash=$hash;$baselineChecksum=$r.checksum}else{
  if($hash -ne $firstHash -or $r.checksum -ne $baselineChecksum){throw 'repeat initialization changed database'}
 }
 if(Test-Path "$root\logs"){
  if(@(Get-ChildItem "$root\logs" -File -Recurse).Count -gt 0){throw 'operation mode created business logs'}
 }
 $results+=[ordered]@{run=$i;passed=$true;exit_code=$p.ExitCode;result=$r;database_sha256=$hash}
}
$results|ConvertTo-Json -Depth 5

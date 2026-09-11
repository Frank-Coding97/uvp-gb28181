param(
 [Parameter(Mandatory=$true)][string]$ServerExe,
 [Parameter(Mandatory=$true)][string]$ProbeExe,
 [Parameter(Mandatory=$true)][string]$WorkRoot
)
$ErrorActionPreference='Stop'
if(Test-Path $WorkRoot){throw 'Use a fresh isolated test directory'}
$results=@()
foreach($kind in @('debug','secret','yaml','acl')){
 $root=Join-Path $WorkRoot $kind
 $env:UVP_INSTALL_DIR=$root;$env:UVP_CONFIG_DIR="$root\config";$env:UVP_RESOURCE_DIR="$root\resource"
 $env:UVP_WEB_DIR="$root\web";$env:UVP_DATA_DIR="$root\data";$env:UVP_RECORDINGS_DIR="$root\recordings"
 $metadata=& $ProbeExe
 if($LASTEXITCODE -ne 0){throw 'Secure configuration initialization failed'}
 $path="$root\config\config.yml"
 $raw=[IO.File]::ReadAllText($path)
 switch($kind){
  'debug' {$raw=$raw.Replace('appdebug: false','appdebug: true')}
  'secret' {$raw=[regex]::Replace($raw,'jwttokensignkey: [^\r\n]+','jwttokensignkey: CHANGE_ME')}
  'yaml' {$raw='token: [invalid'}
  'acl' {
   $acl=Get-Acl -LiteralPath $path
   $users=New-Object Security.Principal.SecurityIdentifier('S-1-5-32-545')
   $acl.AddAccessRule((New-Object Security.AccessControl.FileSystemAccessRule($users,'Read','Allow')))
   Set-Acl -LiteralPath $path -AclObject $acl
  }
 }
 if($kind -ne 'acl'){[IO.File]::WriteAllText($path,$raw,(New-Object Text.UTF8Encoding($false)))}
 $raw=$null
 $stdout="$root\business.stdout";$stderr="$root\business.stderr"
 $process=Start-Process -FilePath $ServerExe -WorkingDirectory 'C:\Windows\System32' -PassThru -RedirectStandardOutput $stdout -RedirectStandardError $stderr
 $handle=$process.Handle
 if(!$process.WaitForExit(15000)){$process.Kill();throw 'Invalid configuration unexpectedly started business services'}
 $process.Refresh()
 $errorText=[IO.File]::ReadAllText($stderr)
 if($process.ExitCode -eq 0 -or $errorText -notmatch 'standalone configuration invalid:'){throw 'Invalid business configuration was not explicitly rejected'}
 $expected=@{debug='server.appdebug must be false';secret='jwttokensignkey';yaml='invalid standalone YAML';acl='secure permissions rejected'}
 if($errorText -notmatch [regex]::Escape($expected[$kind])){throw 'Configuration was rejected for an unexpected reason'}
 if(Test-Path "$root\data\uvp.db"){throw 'Invalid configuration opened a database'}
 $results += [ordered]@{case=$kind;exit_code=$process.ExitCode;database_created=$false;passed=$true}
}
[ordered]@{passed=$true;results=$results}|ConvertTo-Json -Depth 4

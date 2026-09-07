param(
 [Parameter(Mandatory=$true)][string]$ProbeExe,
 [Parameter(Mandatory=$true)][string]$WorkRoot
)
# Test-only: credentials go to the native helper through stdin, never argv.
$ErrorActionPreference='Stop'
[Console]::OutputEncoding=New-Object System.Text.UTF8Encoding($false)
$OutputEncoding=[Console]::OutputEncoding
if(Test-Path $WorkRoot){throw 'Use a fresh isolated test directory'}
if(!(Test-Path -LiteralPath $ProbeExe -PathType Leaf)){throw 'Configuration probe executable is missing'}
$current=[Security.Principal.WindowsIdentity]::GetCurrent()
$principal=New-Object Security.Principal.WindowsPrincipal($current)
if(!$principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)){
 throw 'Cross-account acceptance needs an administrator to create temporary users; configuration runs under an ordinary effective token'
}
Add-Type -TypeDefinition @'
using System;
using System.ComponentModel;
using System.IO;
using System.Runtime.InteropServices;
using System.Security.Principal;
public static class UvpConfigAclCleanup {
 [DllImport("advapi32.dll", CharSet=CharSet.Unicode, SetLastError=true)]
 static extern bool LogonUser(string user,string domain,string password,int type,int provider,out IntPtr token);
 [DllImport("kernel32.dll", SetLastError=true)] static extern bool CloseHandle(IntPtr handle);
 public static void DeleteInstance(string user,string password,string path) {
  IntPtr token;
  if(!LogonUser(user,".",password,2,0,out token)) throw new Win32Exception(Marshal.GetLastWin32Error());
  try { using(WindowsImpersonationContext scope=WindowsIdentity.Impersonate(token)) {
   if(Directory.Exists(path)) Directory.Delete(path,true);
  }} finally {CloseHandle(token);}
 }
}
'@
function New-TestPassword {
 $bytes=New-Object byte[] 32
 $rng=[Security.Cryptography.RandomNumberGenerator]::Create()
 try {$rng.GetBytes($bytes)} finally {$rng.Dispose()}
 return 'Aa1!'+[Convert]::ToBase64String($bytes)
}
$suffix=[Guid]::NewGuid().ToString('N').Substring(0,8)
$userA='uvpt14a'+$suffix;$userB='uvpt14b'+$suffix
$passwordA=New-TestPassword;$passwordB=New-TestPassword
$created=@();$root=$null;$result=$null
try {
 foreach($entry in @(@($userA,$passwordA),@($userB,$passwordB))){
  $secure=ConvertTo-SecureString $entry[1] -AsPlainText -Force
  $newUser=New-LocalUser -Name $entry[0] -Password $secure -Description 'Temporary UVP configuration ACL test'
  $created += $newUser.Name
  Add-LocalGroupMember -Group (Get-LocalGroup -SID 'S-1-5-32-545') -Member $newUser
 }
 $sidA=(Get-LocalUser $userA).SID;$sidB=(Get-LocalUser $userB).SID
 New-Item -ItemType Directory -Path $WorkRoot | Out-Null
 $acl=New-Object Security.AccessControl.DirectorySecurity
 $acl.SetAccessRuleProtection($true,$false);$acl.SetOwner($current.User)
 $inherit=[Security.AccessControl.InheritanceFlags]'ContainerInherit,ObjectInherit'
 $propagation=[Security.AccessControl.PropagationFlags]::None
 $allow=[Security.AccessControl.AccessControlType]::Allow
 $system=New-Object Security.Principal.SecurityIdentifier('S-1-5-18')
 foreach($sid in @($current.User,$system,$sidA)){
  $acl.AddAccessRule((New-Object Security.AccessControl.FileSystemAccessRule($sid,'FullControl',$inherit,$propagation,$allow)))
 }
 $acl.AddAccessRule((New-Object Security.AccessControl.FileSystemAccessRule($sidB,'ReadAndExecute',$inherit,$propagation,$allow)))
 Set-Acl -Path $WorkRoot -AclObject $acl
 $localProbe=Join-Path $WorkRoot 'config-probe.exe'
 Copy-Item -LiteralPath $ProbeExe -Destination $localProbe
 $control=Join-Path $WorkRoot 'readable-control.txt'
 [IO.File]::WriteAllText($control,'positive control')
 $root=Join-Path $WorkRoot 'instance'
 $env:UVP_INSTALL_DIR=$root;$env:UVP_CONFIG_DIR="$root\config";$env:UVP_RESOURCE_DIR="$root\resource"
 $env:UVP_WEB_DIR="$root\web";$env:UVP_DATA_DIR="$root\data";$env:UVP_RECORDINGS_DIR="$root\recordings"
 $request=@{owner=@{user=$userA;password=$passwordA;sid=$sidA.Value};reader=@{user=$userB;password=$passwordB;sid=$sidB.Value};control_path=$control}|ConvertTo-Json -Depth 4 -Compress
 $start=New-Object Diagnostics.ProcessStartInfo
 $start.FileName=$localProbe;$start.Arguments='--acl-check';$start.WorkingDirectory=$WorkRoot
 $start.UseShellExecute=$false;$start.CreateNoWindow=$true
 $start.RedirectStandardInput=$true;$start.RedirectStandardOutput=$true;$start.RedirectStandardError=$true
 $process=New-Object Diagnostics.Process;$process.StartInfo=$start
 if(!$process.Start()){throw 'native ACL helper did not start'}
 $stdoutTask=$process.StandardOutput.ReadToEndAsync();$stderrTask=$process.StandardError.ReadToEndAsync()
 $process.StandardInput.WriteLine($request);$process.StandardInput.Close();$request=$null
 if(!$process.WaitForExit(30000)){$process.Kill();$process.WaitForExit();throw 'native ACL helper timed out'}
 $stdout=$stdoutTask.Result;$stderr=$stderrTask.Result
 if($stdout.Contains($passwordA) -or $stdout.Contains($passwordB) -or $stderr.Contains($passwordA) -or $stderr.Contains($passwordB)){throw 'ACL helper exposed test credentials'}
 [IO.File]::WriteAllText((Join-Path $WorkRoot 'probe.stdout'),$stdout)
 [IO.File]::WriteAllText((Join-Path $WorkRoot 'probe.stderr'),$stderr)
 if($process.ExitCode -ne 0){throw "native ACL helper failed (exit=$($process.ExitCode)): $stderr"}
 $result=$stdout | ConvertFrom-Json
 if(!$result.passed -or !$result.owner_is_standard -or !$result.reader_is_standard -or !$result.positive_control_read -or !$result.repeat_preserved -or $result.read_denied.Count -ne 4){throw 'native ACL acceptance assertions are incomplete'}
} finally {
 $cleanupErrors=@()
 if($root -and (Test-Path $root)){
  try {[UvpConfigAclCleanup]::DeleteInstance($userA,$passwordA,$root)} catch {$cleanupErrors += 'isolated instance cleanup failed'}
 }
 foreach($name in $created){
  try {
   $sid=(Get-LocalUser -Name $name).SID.Value
   Get-CimInstance Win32_UserProfile -Filter "SID='$sid'" | Remove-CimInstance
  } catch {$cleanupErrors += "temporary profile cleanup failed: $name"}
  try {Remove-LocalUser -Name $name} catch {$cleanupErrors += "temporary account cleanup failed: $name"}
 }
 $passwordA=$null;$passwordB=$null;$request=$null
 if($cleanupErrors.Count -gt 0){throw ($cleanupErrors -join '; ')}
}
if($result){$result|ConvertTo-Json -Depth 5}

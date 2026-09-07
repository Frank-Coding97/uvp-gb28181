param([Parameter(Mandatory=$true)][string]$WorkRoot)
$ErrorActionPreference='Stop'
$exe=Join-Path $WorkRoot 'UVP.exe'
$backend=Join-Path $WorkRoot 'releases\t15-smoke\backend\uvp-server.exe'
$release=Join-Path $WorkRoot 'releases\t15-smoke'
if(-not (Test-Path -LiteralPath $exe)){throw 'Prepared startup fixture is required'}
$listener=New-Object Net.Sockets.TcpListener([Net.IPAddress]::Loopback,8280)
$listener.Start()
try {
 $p=Start-Process -FilePath $exe -ArgumentList '-no-browser' -WorkingDirectory $WorkRoot -PassThru -RedirectStandardOutput (Join-Path $WorkRoot 'occupied.stdout') -RedirectStandardError (Join-Path $WorkRoot 'occupied.stderr')
 $handle=$p.Handle
 if(-not $p.WaitForExit(10000)){$p.Kill();$p.WaitForExit();throw 'Conflict did not fail promptly'}
 if($p.ExitCode -ne 1){throw 'Occupied port accepted'}
 $errorText=Get-Content -LiteralPath (Join-Path $WorkRoot 'occupied.stderr') -Raw
 if($errorText -notmatch '8280.*unavailable'){throw 'Missing actual conflict detail'}
 $probe=New-Object Net.Sockets.TcpClient
 $probe.Connect('127.0.0.1',8280)
 $probe.Dispose()
 $owned=@(Get-CimInstance Win32_Process|Where-Object {$_.ExecutablePath -and $_.ExecutablePath.StartsWith($release,[StringComparison]::OrdinalIgnoreCase)})
 if($owned.Count -ne 0){throw 'Conflict started component processes'}
} finally {$listener.Stop()}
$stdout=Join-Path $WorkRoot 'component-failure.stdout'
$stderr=Join-Path $WorkRoot 'component-failure.stderr'
$p=Start-Process -FilePath $exe -ArgumentList '-no-browser' -WorkingDirectory $WorkRoot -PassThru -RedirectStandardOutput $stdout -RedirectStandardError $stderr
$handle=$p.Handle
try {
 $deadline=[DateTime]::UtcNow.AddSeconds(45)
 $ready=$false
 while([DateTime]::UtcNow -lt $deadline -and -not $p.HasExited){
  if((Get-Content -LiteralPath $stdout -Raw -ErrorAction SilentlyContinue) -match 'MediaReady'){$ready=$true;break}
  Start-Sleep -Milliseconds 100
  $p.Refresh()
 }
 if(-not $ready){throw 'Prepared fixture did not restart after previous owner crash'}
 $rows=@(Get-CimInstance Win32_Process|Where-Object {$_.ExecutablePath -eq $backend})
 if($rows.Count -ne 1){throw 'Expected one owned backend'}
 $child=[Diagnostics.Process]::GetProcessById($rows[0].ProcessId)
 $childHandle=$child.Handle
 if($child.Path -ne $backend){throw 'Backend process identity changed'}
 $child.Kill()
 $child.WaitForExit()
 $child.Dispose()
 if(-not $p.WaitForExit(10000)){throw 'Component failure was not detected'}
 if($p.ExitCode -ne 1){throw 'Component failure was not reported'}
 $failure=Get-Content -LiteralPath $stderr -Raw
 if($failure -notmatch 'backend exited'){throw 'Component failure detail missing'}
 Start-Sleep -Milliseconds 200
 $left=@(Get-CimInstance Win32_Process|Where-Object {$_.ExecutablePath -and $_.ExecutablePath.StartsWith($release,[StringComparison]::OrdinalIgnoreCase)})
 if($left.Count -ne 0){throw 'Owned component processes remain'}
 @{port_conflict='passed';unrelated_listener='preserved';restart_after_owner_crash='passed';backend_exit_detection='passed';remaining_owned=$left.Count}|ConvertTo-Json
} finally {
 if(-not $p.HasExited){$p.Kill();$p.WaitForExit()}
 $p.Dispose()
}

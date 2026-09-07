# Test-only runner inside a disposable Sandbox. Both paths are explicit WSB mappings.
$ErrorActionPreference = 'Stop'
$inputRoot = 'C:\uvp-input'
$outputRoot = 'C:\uvp-output'
$completed = @{}
Set-Content -Encoding ASCII (Join-Path $outputRoot 'runner-ready.txt') 'ready'
while (-not (Test-Path (Join-Path $inputRoot 'runner-stop.txt'))) {
    foreach ($request in @(Get-ChildItem $inputRoot -Filter '*.request')) {
        $runId = $request.BaseName
        if ($runId -match '^[a-zA-Z0-9-]{1,64}$' -and -not $completed.ContainsKey($runId)) {
            $completed[$runId] = $true
            $prefix = Join-Path $outputRoot $runId
            if (Test-Path ($prefix + '.exit')) { throw 'Refusing to overwrite previous evidence' }
            $job = Join-Path $inputRoot ($runId + '.ps1')
            try {
                $p = Start-Process powershell.exe -ArgumentList @('-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass', '-File', $job) -Wait -PassThru -RedirectStandardOutput ($prefix + '.stdout') -RedirectStandardError ($prefix + '.stderr')
                Set-Content -Encoding ASCII ($prefix + '.exit') $p.ExitCode
            } catch {
                $_ | Out-String | Set-Content -Encoding UTF8 ($prefix + '.runner-error')
                Set-Content -Encoding ASCII ($prefix + '.exit') '1'
            }
        }
    }
    Start-Sleep -Seconds 2
}

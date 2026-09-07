[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$Source,

    [Parameter(Mandatory = $true)]
    [string]$Output,

    [Parameter(Mandatory = $true)]
    [string]$LockFile,

    [Parameter(Mandatory = $true)]
    [string]$VcpkgRoot,

    [string]$SourceRevision,

    [string]$BuildRoot,

    [int]$Parallel = 0
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version 2

function Resolve-FullPath {
    param([string]$Value)
    return [System.IO.Path]::GetFullPath($Value)
}

function Invoke-GitText {
    param([string[]]$Arguments)
    $gitArguments = @('-c', 'core.autocrlf=false', '-c', 'core.filemode=false') + $Arguments
    $lines = @(& git @gitArguments 2>&1)
    if ($LASTEXITCODE -ne 0) {
        throw "git failed ($LASTEXITCODE): $($gitArguments -join ' ')`n$($lines -join "`n")"
    }
    return ($lines -join "`n").Trim()
}

function Assert-CleanGitTree {
    param([string]$Path)
    $status = Invoke-GitText @('-C', $Path, 'status', '--porcelain=1', '--untracked-files=all', '--ignored')
    if ($status) {
        throw "ZLM source is not clean (including ignored files): $Path`n$status"
    }
}

function Get-CacheOnValue {
    param([string]$Cache, [string]$Name)
    return ($Cache -match ("(?m)^" + [regex]::Escape($Name) + ":BOOL=ON\r?$"))
}

function Get-RelativeFilePath {
    param([string]$Base, [string]$Path)
    return $Path.Substring($Base.Length + 1).Replace('\', '/')
}

function Test-SystemDll {
    param([string]$Name)
    $lower = $Name.ToLowerInvariant()
    return $lower -match '^(api-ms-win-|ext-ms-win-|kernel32\.dll$|kernelbase\.dll$|user32\.dll$|gdi32\.dll$|advapi32\.dll$|secur32\.dll$|crypt32\.dll$|bcrypt\.dll$|winhttp\.dll$|ws2_32\.dll$|iphlpapi\.dll$|shlwapi\.dll$|ole32\.dll$|oleaut32\.dll$|shell32\.dll$|comdlg32\.dll$|ntdll\.dll$|rpcrt4\.dll$|mswsock\.dll$|dnsapi\.dll$|imm32\.dll$|version\.dll$|psapi\.dll$|dbghelp\.dll$|normaliz\.dll$|wldap32\.dll$)'
}

$source = Resolve-FullPath $Source
$output = Resolve-FullPath $Output
$lockPath = Resolve-FullPath $LockFile
$vcpkg = Resolve-FullPath $VcpkgRoot

if (-not (Test-Path -LiteralPath (Join-Path $source '.git'))) {
    throw "Source must be a Git checkout: $source"
}
if (-not (Test-Path -LiteralPath $lockPath)) {
    throw "Source lock file does not exist: $lockPath"
}
if ($output.StartsWith($source + [System.IO.Path]::DirectorySeparatorChar, [System.StringComparison]::OrdinalIgnoreCase)) {
    throw 'Output must be outside the source checkout so the locked input remains reusable.'
}
if (-not (Test-Path -LiteralPath (Join-Path $vcpkg 'scripts\buildsystems\vcpkg.cmake'))) {
    throw "vcpkg toolchain not found under: $vcpkg"
}
$expectedVcpkgRevision = 'efcfaaf60d7ec57a159fc3110403d939bfb69729'
$vcpkgRevision = Invoke-GitText @('-C', $vcpkg, 'rev-parse', 'HEAD')
if ($vcpkgRevision -cne $expectedVcpkgRevision) {
    throw "vcpkg revision mismatch: expected $expectedVcpkgRevision, got $vcpkgRevision"
}
$vcpkgExecutable = Join-Path $vcpkg 'vcpkg.exe'
if (-not (Test-Path -LiteralPath $vcpkgExecutable)) {
    throw "vcpkg executable not found: $vcpkgExecutable"
}
$vcpkgList = @(& $vcpkgExecutable list 2>&1)
if ($LASTEXITCODE -ne 0) {
    throw "vcpkg list failed with exit code $LASTEXITCODE"
}
$expectedVcpkgPackages = [ordered]@{
    openssl = '3.5.1'
    libsrtp = '2.7.0#1'
    usrsctp = '0.9.5.0#4'
}
$vcpkgPackages = [ordered]@{}
foreach ($package in $expectedVcpkgPackages.Keys) {
    $packagePattern = '^' + [regex]::Escape($package) + ':x64-windows-static\s+(\S+)'
    $packageLine = $vcpkgList | Where-Object { $_ -match $packagePattern } | Select-Object -First 1
    if (-not $packageLine) {
        throw "Required vcpkg package is missing from x64-windows-static: $package"
    }
    $packageText = [string]$packageLine
    if ($packageText -notmatch $packagePattern) {
        throw "Could not parse vcpkg package version: $packageText"
    }
    $actualPackageVersion = $Matches[1]
    if ($actualPackageVersion -cne [string]$expectedVcpkgPackages[$package]) {
        throw "vcpkg package version mismatch for ${package}: expected $($expectedVcpkgPackages[$package]), got $actualPackageVersion"
    }
    $vcpkgPackages[$package] = $actualPackageVersion
}

$lock = Get-Content -LiteralPath $lockPath -Raw | ConvertFrom-Json
$lockedRevision = [string]$lock.components.zlm.revision
if ($lockedRevision -notmatch '^[0-9a-f]{40}$') {
    throw 'sources.lock.json has no valid 40-character ZLM revision.'
}
$actualRevision = Invoke-GitText @('-C', $source, 'rev-parse', 'HEAD')
$expectedRevision = $lockedRevision
if ($SourceRevision) {
    if ($SourceRevision -notmatch '^[0-9a-f]{40}$') {
        throw 'SourceRevision must be a 40-character lowercase Git revision.'
    }
    $expectedRevision = $SourceRevision
}
if ($actualRevision -cne $expectedRevision) {
    throw "ZLM revision mismatch: expected $expectedRevision, got $actualRevision"
}
if ($actualRevision -cne $lockedRevision) {
    & git -c core.autocrlf=false -c core.filemode=false -C $source merge-base --is-ancestor $lockedRevision $actualRevision 2>$null
    if ($LASTEXITCODE -ne 0) {
        throw "ZLM custom revision $actualRevision is not based on locked revision $lockedRevision"
    }
}
Assert-CleanGitTree $source

$expectedSubmodules = @($lock.components.zlm.submodules.PSObject.Properties | ForEach-Object { $_.Name })
$configuredSubmoduleText = Invoke-GitText @('-C', $source, 'config', '--file', (Join-Path $source '.gitmodules'), '--get-regexp', 'path')
$configuredSubmodules = @($configuredSubmoduleText -split '\r?\n' | Where-Object { $_ } | ForEach-Object { ($_ -split '\s+', 2)[1] })
$submoduleDiff = @(Compare-Object $expectedSubmodules $configuredSubmodules)
if ($submoduleDiff.Count -ne 0) {
    throw "ZLM submodule set differs from sources.lock.json: $($submoduleDiff | Out-String)"
}
foreach ($path in $expectedSubmodules) {
    $submodulePath = Join-Path $source ($path -replace '/', '\')
    $expected = [string]$lock.components.zlm.submodules.PSObject.Properties[$path].Value
    $actual = Invoke-GitText @('-C', $submodulePath, 'rev-parse', 'HEAD')
    if ($actual -cne $expected) {
        throw "ZLM submodule revision mismatch at ${path}: expected $expected, got $actual"
    }
    Assert-CleanGitTree $submodulePath
}

if (-not $BuildRoot) {
    $BuildRoot = Join-Path ([System.IO.Path]::GetDirectoryName($output)) 'build'
}
$buildRoot = Resolve-FullPath $BuildRoot
if ($buildRoot.StartsWith($source + [System.IO.Path]::DirectorySeparatorChar, [System.StringComparison]::OrdinalIgnoreCase)) {
    throw 'BuildRoot must be outside the source checkout so the locked input remains reusable.'
}
if (Test-Path -LiteralPath (Join-Path $buildRoot 'CMakeCache.txt')) {
    throw "Refusing to reuse a CMake cache: $buildRoot"
}
if (Test-Path -LiteralPath $output) {
    throw "Refusing to overwrite an existing package directory: $output"
}
New-Item -ItemType Directory -Path $buildRoot -Force | Out-Null

$vswhere = 'C:\Program Files (x86)\Microsoft Visual Studio\Installer\vswhere.exe'
$vsInstall = ''
if (Test-Path -LiteralPath $vswhere) {
    $vsInstall = ((& $vswhere -latest -products '*' -requires Microsoft.VisualStudio.Component.VC.Tools.x86.x64 -property installationPath 2>$null | Select-Object -First 1) | Out-String).Trim()
}
$cmakeCandidates = @()
if ($vsInstall) {
    $cmakeCandidates += Join-Path $vsInstall 'Common7\IDE\CommonExtensions\Microsoft\CMake\CMake\bin\cmake.exe'
}
$cmakeCommand = Get-Command cmake -ErrorAction SilentlyContinue
if ($cmakeCommand) {
    $cmakeCandidates += $cmakeCommand.Source
}
$cmakeCandidates += 'C:\Program Files\CMake\bin\cmake.exe'
$cmake = $cmakeCandidates | Where-Object { $_ -and (Test-Path -LiteralPath $_) } | Select-Object -First 1
if (-not $cmake) {
    throw 'CMake was not found in the selected Visual Studio installation or PATH.'
}

$toolchain = Join-Path $vcpkg 'scripts\buildsystems\vcpkg.cmake'
$configureArguments = @(
    '-S', $source,
    '-B', $buildRoot,
    '-G', 'Visual Studio 17 2022',
    '-A', 'x64',
    '-DCMAKE_BUILD_TYPE=Release',
    '-DCMAKE_MSVC_RUNTIME_LIBRARY:STRING=MultiThreaded$<$<CONFIG:Debug>:Debug>',
    "-DCMAKE_TOOLCHAIN_FILE=$toolchain",
    '-DVCPKG_TARGET_TRIPLET=x64-windows-static',
    '-DOPENSSL_USE_STATIC_LIBS=TRUE',
    '-DENABLE_SERVER=ON',
    '-DENABLE_API=ON',
    '-DENABLE_RTPPROXY=ON',
    '-DENABLE_MP4=ON',
    '-DENABLE_HLS=ON',
    '-DENABLE_OPENSSL=ON',
    '-DENABLE_WEBRTC=ON',
    '-DENABLE_SCTP=ON',
    '-DENABLE_MSVC_MT=ON',
    '-DENABLE_TESTS=OFF',
    '-DENABLE_FFMPEG=OFF',
    '-DENABLE_PLAYER=OFF',
    '-DENABLE_PYTHON=OFF',
    '-DENABLE_SRT=OFF',
    '-DENABLE_MYSQL=OFF',
    '-DDISABLE_REPORT=ON'
)

Write-Output "ZLM source revision: $actualRevision"
Write-Output "ZLM source: $source"
Write-Output "CMake: $cmake"
Write-Output "vcpkg: $vcpkg"
Write-Output 'Configuring required RTPProxy/MP4/HLS/WebRTC/OpenSSL features.'
$configureOutput = (& $cmake @configureArguments 2>&1 | Out-String)
$configureExit = $LASTEXITCODE
Write-Output $configureOutput
if ($configureExit -ne 0) {
    throw "CMake configure failed with exit code $configureExit"
}
foreach ($marker in @('ENABLE_RTPPROXY defined', 'ENABLE_MP4 defined', 'ENABLE_HLS defined', 'ENABLE_OPENSSL defined', 'ENABLE_WEBRTC defined')) {
    if ($configureOutput -notmatch [regex]::Escape($marker)) {
        throw "CMake did not confirm required feature: $marker"
    }
}

$cache = Get-Content -LiteralPath (Join-Path $buildRoot 'CMakeCache.txt') -Raw
foreach ($name in @('ENABLE_SERVER', 'ENABLE_RTPPROXY', 'ENABLE_MP4', 'ENABLE_HLS', 'ENABLE_OPENSSL', 'ENABLE_WEBRTC', 'ENABLE_SCTP')) {
    if (-not (Get-CacheOnValue $cache $name)) {
        throw "CMake cache does not retain required option $name=ON"
    }
}
$runtimeCacheLine = @($cache -split '\r?\n' | Where-Object { $_ -match '^CMAKE_MSVC_RUNTIME_LIBRARY:STRING=' } | Select-Object -First 1)
if (-not $runtimeCacheLine -or $runtimeCacheLine -notmatch 'MultiThreaded') {
    throw 'CMake cache does not retain CMAKE_MSVC_RUNTIME_LIBRARY=MultiThreaded.'
}
$mediaServerProject = Get-ChildItem -LiteralPath $buildRoot -Filter 'MediaServer.vcxproj' -Recurse -File | Select-Object -First 1
if (-not $mediaServerProject) {
    throw "Generated MediaServer.vcxproj was not found under: $buildRoot"
}
$mediaServerProjectText = Get-Content -LiteralPath $mediaServerProject.FullName -Raw
if ($mediaServerProjectText -match '<RuntimeLibrary>MultiThreadedDLL</RuntimeLibrary>') {
    throw "MediaServer.vcxproj still selects the dynamic MSVC runtime: $($mediaServerProject.FullName)"
}
if ($mediaServerProjectText -notmatch '<RuntimeLibrary>MultiThreaded</RuntimeLibrary>') {
    throw "MediaServer.vcxproj has no static Release MSVC runtime setting: $($mediaServerProject.FullName)"
}
Write-Output "MSVC runtime: MultiThreaded ($($mediaServerProject.FullName))"

Write-Output 'Building MediaServer (Release, x64).'
if ($Parallel -gt 0) {
    & $cmake --build $buildRoot --config Release --parallel $Parallel
} else {
    & $cmake --build $buildRoot --config Release
}
if ($LASTEXITCODE -ne 0) {
    throw "CMake build failed with exit code $LASTEXITCODE"
}

$builtRoot = Join-Path $source 'release\windows\Release'
$executable = Join-Path $builtRoot 'MediaServer.exe'
if (-not (Test-Path -LiteralPath $executable)) {
    $configurationExecutable = Join-Path $builtRoot 'Release\MediaServer.exe'
    if (Test-Path -LiteralPath $configurationExecutable) {
        $executable = $configurationExecutable
    }
}
foreach ($required in @('MediaServer.exe', 'config.ini', 'default.pem')) {
    $requiredPath = Join-Path $builtRoot $required
    if ($required -eq 'MediaServer.exe') {
        $requiredPath = $executable
    }
    if (-not (Test-Path -LiteralPath $requiredPath)) {
        throw "Required ZLM build resource is missing: $requiredPath"
    }
}
if (-not (Test-Path -LiteralPath (Join-Path $builtRoot 'www'))) {
    throw "Required ZLM web resource is missing: $(Join-Path $builtRoot 'www')"
}

$mediaRoot = Join-Path $output 'media'
$licenseRoot = Join-Path $output 'licenses\zlm'
New-Item -ItemType Directory -Path $mediaRoot -Force | Out-Null
New-Item -ItemType Directory -Path $licenseRoot -Force | Out-Null
Copy-Item -LiteralPath $executable -Destination (Join-Path $mediaRoot 'MediaServer.exe')
Copy-Item -LiteralPath (Join-Path $builtRoot 'config.ini') -Destination (Join-Path $mediaRoot 'config.ini')
Copy-Item -LiteralPath (Join-Path $builtRoot 'default.pem') -Destination (Join-Path $mediaRoot 'default.pem')
$webSource = Join-Path $builtRoot 'www'
$webDestination = Join-Path $mediaRoot 'www'
New-Item -ItemType Directory -Path $webDestination | Out-Null
foreach ($entry in Get-ChildItem -LiteralPath $webSource -Recurse -Force) {
    $relative = Get-RelativeFilePath $webSource $entry.FullName
    if ($relative -match '(^|/)\.git(/|$)') { continue }
    if ($entry.Attributes -band [IO.FileAttributes]::ReparsePoint) {
        throw "Runtime web assets must not contain links: $relative"
    }
    $destination = Join-Path $webDestination $relative
    if ($entry.PSIsContainer) {
        New-Item -ItemType Directory -Path $destination -Force | Out-Null
    } else {
        Copy-Item -LiteralPath $entry.FullName -Destination $destination
    }
}

$licenseFiles = @(
    @{ Name = 'ZLMediaKit-LICENSE'; Path = (Join-Path $source 'LICENSE') },
    @{ Name = 'ZLToolKit-LICENSE'; Path = (Join-Path $source '3rdpart\ZLToolKit\LICENSE') },
    @{ Name = 'jsoncpp-LICENSE'; Path = (Join-Path $source '3rdpart\jsoncpp\LICENSE') },
    @{ Name = 'media-server-LICENSE'; Path = (Join-Path $source '3rdpart\media-server\LICENSE') },
    @{ Name = 'pybind11-LICENSE'; Path = (Join-Path $source '3rdpart\pybind11\LICENSE') },
    @{ Name = 'webassist-LICENSE'; Path = (Join-Path $source 'www\webassist\LICENSE') },
    @{ Name = 'openssl-COPYRIGHT'; Path = (Join-Path $vcpkg 'installed\x64-windows-static\share\openssl\copyright') },
    @{ Name = 'libsrtp-COPYRIGHT'; Path = (Join-Path $vcpkg 'installed\x64-windows-static\share\libsrtp\copyright') },
    @{ Name = 'usrsctp-COPYRIGHT'; Path = (Join-Path $vcpkg 'installed\x64-windows-static\share\usrsctp\copyright') }
)
foreach ($license in $licenseFiles) {
    if (-not (Test-Path -LiteralPath $license.Path)) {
        throw "Required license file is missing: $($license.Path)"
    }
    Copy-Item -LiteralPath $license.Path -Destination (Join-Path $licenseRoot $license.Name)
}

$dumpbin = $null
if ($vsInstall -and (Test-Path -LiteralPath (Join-Path $vsInstall 'VC\Tools\MSVC'))) {
    $dumpbin = Get-ChildItem -LiteralPath (Join-Path $vsInstall 'VC\Tools\MSVC') -Filter dumpbin.exe -Recurse -File | Where-Object { $_.FullName -match '\\Hostx64\\x64\\dumpbin\.exe$' } | Sort-Object FullName -Descending | Select-Object -First 1
}
if (-not $dumpbin) {
    $dumpbinCommand = Get-Command dumpbin -ErrorAction SilentlyContinue
    if ($dumpbinCommand) { $dumpbin = $dumpbinCommand.Source }
}
if (-not $dumpbin) {
    throw 'dumpbin.exe is required to record the MediaServer DLL closure.'
}
if ($dumpbin -is [System.IO.FileInfo]) {
    $dumpbin = $dumpbin.FullName
}
$dependencyOutput = @(& $dumpbin '/DEPENDENTS' $executable 2>&1)
if ($LASTEXITCODE -ne 0) {
    throw "dumpbin failed with exit code $LASTEXITCODE"
}
$dependencies = @($dependencyOutput | ForEach-Object { if ($_ -match '^\s+([A-Za-z0-9][A-Za-z0-9._-]*\.dll)\s*$') { $Matches[1].ToLowerInvariant() } } | Sort-Object -Unique)
$systemDependencies = @($dependencies | Where-Object { Test-SystemDll $_ })
$externalDependencies = @($dependencies | Where-Object { -not (Test-SystemDll $_) })
foreach ($dll in $externalDependencies) {
    $present = Get-ChildItem -LiteralPath $mediaRoot -Filter $dll -Recurse -File -ErrorAction SilentlyContinue | Select-Object -First 1
    if (-not $present) {
        $candidate = Get-ChildItem -LiteralPath $vcpkg -Filter $dll -Recurse -File -ErrorAction SilentlyContinue | Select-Object -First 1
        if ($candidate) {
            Copy-Item -LiteralPath $candidate.FullName -Destination (Join-Path $mediaRoot $dll)
        } else {
            throw "MediaServer has an unbundled non-system DLL dependency: $dll"
        }
    }
}

$files = @(Get-ChildItem -LiteralPath $output -Recurse -Force -File | Sort-Object FullName | ForEach-Object {
    [ordered]@{
        path = Get-RelativeFilePath $output $_.FullName
        sha256 = (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
        bytes = $_.Length
    }
})
$submoduleMap = [ordered]@{}
foreach ($path in $expectedSubmodules) {
    $submoduleMap[$path] = [string]$lock.components.zlm.submodules.PSObject.Properties[$path].Value
}
$manifest = [ordered]@{
    schema = 1
    component = 'ZLMediaKit'
    target = 'windows-x64'
    source = [ordered]@{
        revision = $actualRevision
        baseRevision = $lockedRevision
        submodules = $submoduleMap
    }
    toolchain = [ordered]@{
        cmake = ((& $cmake --version | Select-Object -First 1).Trim())
        generator = 'Visual Studio 17 2022'
        architecture = 'x64'
        vcpkgTriplet = 'x64-windows-static'
        vcpkgRevision = $vcpkgRevision
        vcpkgPackages = $vcpkgPackages
    }
    features = [ordered]@{
        RTPProxy = $true
        MP4 = $true
        HLS = $true
        OpenSSL = $true
        WebRTC = $true
        SCTP = $true
        SRT = $false
        FFmpeg = $false
        MySQL = $false
        Tests = $false
    }
    executable = 'media/MediaServer.exe'
    runtimeDependencies = $dependencies
    systemDependencies = $systemDependencies
    bundledDependencies = @($externalDependencies)
    files = $files
}
$manifest | ConvertTo-Json -Depth 10 | Set-Content -LiteralPath (Join-Path $output 'zlm-manifest.json') -Encoding UTF8
Write-Output "ZLM package ready: $output"

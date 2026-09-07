param(
 [Parameter(Mandatory=$true)][string]$WorkRoot,
 [Parameter(Mandatory=$true)][string]$LauncherPath,
 [Parameter(Mandatory=$true)][string]$BackendPath,
 [Parameter(Mandatory=$true)][string]$WebZip,
 [Parameter(Mandatory=$true)][string]$ResourceZip,
 [Parameter(Mandatory=$true)][string]$RedisDirectory,
 [Parameter(Mandatory=$true)][string]$MediaDirectory,
 [Parameter(Mandatory=$true)][string]$SourceCommit
)
$ErrorActionPreference='Stop'
if(Test-Path -LiteralPath $WorkRoot){throw 'Isolated test directory already exists'}
$release=Join-Path $WorkRoot 'releases\t18-setup'
New-Item -ItemType Directory -Path (Join-Path $release 'backend'),(Join-Path $release 'web'),(Join-Path $release 'resource') -Force|Out-Null
Copy-Item -LiteralPath $LauncherPath -Destination (Join-Path $WorkRoot 'UVP.exe')
Copy-Item -LiteralPath $BackendPath -Destination (Join-Path $release 'backend\uvp-server.exe')
Copy-Item -LiteralPath $RedisDirectory -Destination (Join-Path $release 'redis') -Recurse
Copy-Item -LiteralPath $MediaDirectory -Destination (Join-Path $release 'media') -Recurse
Add-Type -AssemblyName System.IO.Compression.FileSystem
# ZIP inputs must have root-level web/resource files and UTF-8 entry names.
[IO.Compression.ZipFile]::ExtractToDirectory($WebZip,(Join-Path $release 'web'))
[IO.Compression.ZipFile]::ExtractToDirectory($ResourceZip,(Join-Path $release 'resource'))
if(-not (Test-Path -LiteralPath (Join-Path $release 'web\index.html'))){throw 'Web ZIP must contain index.html at its root'}
$files=@(Get-ChildItem -LiteralPath $release -File -Recurse|ForEach-Object {
 @{path=$_.FullName.Substring($release.Length+1).Replace('\','/');sha256=(Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLower()}
})
$manifest=@{format_version=1;version='t18-setup';source_commit=$SourceCommit;schema_min=1;schema_max=3;files=$files}|ConvertTo-Json -Depth 6
[IO.File]::WriteAllText((Join-Path $release 'manifest.json'),$manifest,(New-Object Text.UTF8Encoding($false)))
[IO.File]::WriteAllText((Join-Path $WorkRoot 'current.json'),'{"version":"t18-setup"}',(New-Object Text.UTF8Encoding($false)))
Write-Output 'Isolated T18 test fixture prepared'

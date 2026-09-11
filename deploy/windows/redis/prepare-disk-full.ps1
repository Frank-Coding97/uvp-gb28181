# Test-only: creates a new 64 MiB VHD. Never selects or formats an existing disk.
param(
    [Parameter(Mandatory=$true)][string]$VhdPath,
    [ValidatePattern('^[D-Z]$')][string]$DriveLetter = 'V'
)
$ErrorActionPreference = 'Stop'
if ($VhdPath -notmatch '^[A-Za-z]:\\' -or $VhdPath -match '["\r\n]' -or [IO.Path]::GetExtension($VhdPath) -ne '.vhd') { throw 'Expected a local .vhd path' }
if (Test-Path $VhdPath) { throw 'VHD already exists' }
if (Test-Path ($DriveLetter + ':\')) { throw 'Drive letter is already in use' }
if (-not (Test-Path (Split-Path $VhdPath))) { throw 'Test directory must already exist' }
$script = [IO.Path]::GetTempFileName()
try {
    @"
create vdisk file="$VhdPath" maximum=64 type=expandable
select vdisk file="$VhdPath"
attach vdisk
create partition primary
format fs=ntfs quick label=UVP_P0_TEST
assign letter=$DriveLetter
exit
"@ | Set-Content -Encoding ASCII $script
    & diskpart.exe /s $script
    if (-not (Test-Path ($DriveLetter + ':\'))) { throw 'Dedicated VHD was not mounted' }
    $volume = Get-Volume -DriveLetter $DriveLetter
    if ($volume.FileSystemLabel -ne 'UVP_P0_TEST' -or $volume.Size -gt 134217728) { throw 'Unexpected test volume' }
    $volume | Select-Object DriveLetter,FileSystemLabel,Size,SizeRemaining | ConvertTo-Json
} finally {
    Remove-Item -LiteralPath $script
}

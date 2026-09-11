param(
    [Parameter(Mandatory=$true)][string]$TestExe,
    [Parameter(Mandatory=$true)][string]$EvidenceRoot
)
$ErrorActionPreference = 'Stop'
$uvpUser = 'uvpt16_' + [Guid]::NewGuid().ToString('N').Substring(0, 10)
$uvpDir = Join-Path $EvidenceRoot ('pipe-user-' + [Guid]::NewGuid().ToString('N'))
$uvpCreated = $false
$uvpExit = 1
try {
    New-Item -ItemType Directory -Path $uvpDir | Out-Null
    $uvpSid = [Security.Principal.WindowsIdentity]::GetCurrent().User.Value
    $uvpAcl = New-Object Security.AccessControl.DirectorySecurity
    $uvpAcl.SetSecurityDescriptorSddlForm("D:P(A;OICI;FA;;;$uvpSid)(A;OICI;FA;;;SY)")
    Set-Acl -LiteralPath $uvpDir -AclObject $uvpAcl
    $uvpPassword = 'Aa1!' + [Guid]::NewGuid().ToString('N') + [Guid]::NewGuid().ToString('N')
    New-LocalUser -Name $uvpUser -Password (ConvertTo-SecureString $uvpPassword -AsPlainText -Force) -AccountExpires (Get-Date).AddDays(1) -Description 'UVP T16 temporary pipe ACL acceptance' | Out-Null
    $uvpCreated = $true
    $uvpCredentialFile = Join-Path $uvpDir 'credentials.json'
    $uvpCredential = @{domain='.'; user=$uvpUser; password=$uvpPassword} | ConvertTo-Json -Compress
    [IO.File]::WriteAllText($uvpCredentialFile, $uvpCredential, (New-Object Text.UTF8Encoding($false)))
    $uvpPassword = $null
    $uvpCredential = $null
    $env:UVP_CONTROLPIPE_OTHER_USER_CREDENTIAL_FILE = $uvpCredentialFile
    & $TestExe '-test.v' '-test.run=TestPipeRejectsDifferentWindowsUser' '-test.timeout=20s'
    $uvpExit = $LASTEXITCODE
} finally {
    Remove-Item Env:UVP_CONTROLPIPE_OTHER_USER_CREDENTIAL_FILE -ErrorAction SilentlyContinue
    if ($uvpCreated) { Remove-LocalUser -Name $uvpUser }
    if (Test-Path -LiteralPath $uvpDir) { Remove-Item -LiteralPath $uvpDir -Recurse -Force }
}
if (Get-LocalUser -Name $uvpUser -ErrorAction SilentlyContinue) { throw 'Temporary test user cleanup failed' }
if (Test-Path -LiteralPath $uvpDir) { throw 'Temporary credential cleanup failed' }
Write-Output 'Temporary user and credentials removed.'
exit $uvpExit

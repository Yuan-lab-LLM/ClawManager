[CmdletBinding(SupportsShouldProcess = $true)]
param(
    [Parameter(Mandatory = $true)]
    [string]$RootCertificate,

    [ValidateSet("CurrentUser", "LocalMachine")]
    [string]$StoreScope = "CurrentUser",

    [switch]$ConfigureFirefoxEnterpriseRoots
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$certificatePath = (Resolve-Path -LiteralPath $RootCertificate).Path
$certificate = [System.Security.Cryptography.X509Certificates.X509Certificate2]::new($certificatePath)
$basicConstraints = $certificate.Extensions | Where-Object {
    $_ -is [System.Security.Cryptography.X509Certificates.X509BasicConstraintsExtension]
}
if (-not $basicConstraints -or -not $basicConstraints.CertificateAuthority) {
    throw "The supplied certificate is not a CA certificate: $certificatePath"
}

$storePath = "Cert:\$StoreScope\Root"
if ($PSCmdlet.ShouldProcess($storePath, "Trust ClawManager internal Root CA $($certificate.Thumbprint)")) {
    Import-Certificate -FilePath $certificatePath -CertStoreLocation $storePath | Out-Null
}

if ($ConfigureFirefoxEnterpriseRoots) {
    $policyRoot = if ($StoreScope -eq "LocalMachine") {
        "HKLM:\SOFTWARE\Policies\Mozilla\Firefox\Certificates"
    } else {
        "HKCU:\SOFTWARE\Policies\Mozilla\Firefox\Certificates"
    }
    if ($PSCmdlet.ShouldProcess($policyRoot, "Enable Firefox enterprise root trust")) {
        New-Item -Path $policyRoot -Force | Out-Null
        New-ItemProperty -Path $policyRoot -Name ImportEnterpriseRoots -PropertyType DWord -Value 1 -Force | Out-Null
    }
}

$trusted = Get-ChildItem $storePath | Where-Object Thumbprint -EQ $certificate.Thumbprint
if (-not $trusted) {
    throw "Root CA import verification failed for $storePath"
}
Write-Host "ClawManager Root CA is trusted in $storePath. Thumbprint: $($certificate.Thumbprint)"

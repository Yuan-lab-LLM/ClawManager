[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$OutputDirectory,

    [string]$GatewayIP = "10.130.15.40",
    [string]$GatewayDNSName = "10-130-15-40.nip.io",
    [int]$RootCAValidityDays = 3650,
    [int]$ServerCertificateValidityDays = 1825,
    [SecureString]$RootCAPassword,
    [string]$OpenSSLPath
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

function Resolve-OpenSSL {
    param([string]$RequestedPath)

    if ($RequestedPath) {
        if (-not (Test-Path -LiteralPath $RequestedPath -PathType Leaf)) {
            throw "OpenSSL executable does not exist: $RequestedPath"
        }
        return (Resolve-Path -LiteralPath $RequestedPath).Path
    }

    $command = Get-Command openssl -ErrorAction SilentlyContinue
    if ($command) {
        return $command.Source
    }

    $knownPaths = @(
        "C:\Program Files\Git\usr\bin\openssl.exe",
        "C:\Program Files\Git\mingw64\bin\openssl.exe"
    )
    foreach ($candidate in $knownPaths) {
        if (Test-Path -LiteralPath $candidate -PathType Leaf) {
            return $candidate
        }
    }
    throw "OpenSSL was not found. Install Git for Windows or pass -OpenSSLPath."
}

function Invoke-OpenSSL {
    param([Parameter(Mandatory = $true)][string[]]$Arguments)

    & $script:ResolvedOpenSSL @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "OpenSSL failed with exit code ${LASTEXITCODE}: $($Arguments -join ' ')"
    }
}

if ($RootCAValidityDays -lt 3650) {
    throw "Root CA validity must be at least 3650 days for this production profile."
}
if ($ServerCertificateValidityDays -lt 365 -or $ServerCertificateValidityDays -gt $RootCAValidityDays) {
    throw "Server certificate validity must be between 365 days and the Root CA validity."
}

$script:ResolvedOpenSSL = Resolve-OpenSSL -RequestedPath $OpenSSLPath
$repoRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot "..\..\..\..\..")).Path.TrimEnd('\')
$absoluteOutput = [System.IO.Path]::GetFullPath($OutputDirectory).TrimEnd('\')
if ($absoluteOutput.StartsWith($repoRoot + '\', [System.StringComparison]::OrdinalIgnoreCase) -or
    $absoluteOutput.Equals($repoRoot, [System.StringComparison]::OrdinalIgnoreCase)) {
    throw "Refusing to write CA private material inside the Git repository: $absoluteOutput"
}

$managedFiles = @(
    "root-ca.key", "root-ca.crt", "root-ca.srl",
    "clawmanager.key", "clawmanager.csr", "clawmanager.crt",
    "clawmanager-fullchain.crt", "server-extensions.cnf", "certificate-manifest.txt"
)
if (Test-Path -LiteralPath $absoluteOutput) {
    foreach ($name in $managedFiles) {
        if (Test-Path -LiteralPath (Join-Path $absoluteOutput $name)) {
            throw "Output already contains managed CA material. Use a new empty directory: $absoluteOutput"
        }
    }
} else {
    New-Item -ItemType Directory -Path $absoluteOutput -Force | Out-Null
}

if (-not $RootCAPassword) {
    $RootCAPassword = Read-Host "Enter a strong password for the offline Root CA private key" -AsSecureString
}

$passwordPointer = [IntPtr]::Zero
try {
    $passwordPointer = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($RootCAPassword)
    $plainPassword = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($passwordPointer)
    if ([string]::IsNullOrWhiteSpace($plainPassword) -or $plainPassword.Length -lt 16) {
        throw "Root CA password must contain at least 16 characters."
    }
    $env:CLAWMANAGER_CA_PASSWORD = $plainPassword

    $rootKey = Join-Path $absoluteOutput "root-ca.key"
    $rootCertificate = Join-Path $absoluteOutput "root-ca.crt"
    $serverKey = Join-Path $absoluteOutput "clawmanager.key"
    $serverRequest = Join-Path $absoluteOutput "clawmanager.csr"
    $serverCertificate = Join-Path $absoluteOutput "clawmanager.crt"
    $fullChain = Join-Path $absoluteOutput "clawmanager-fullchain.crt"
    $extensionsFile = Join-Path $absoluteOutput "server-extensions.cnf"
    $manifestFile = Join-Path $absoluteOutput "certificate-manifest.txt"

    Invoke-OpenSSL -Arguments @("genpkey", "-algorithm", "RSA", "-aes-256-cbc", "-pass", "env:CLAWMANAGER_CA_PASSWORD", "-pkeyopt", "rsa_keygen_bits:4096", "-out", $rootKey)
    Invoke-OpenSSL -Arguments @("req", "-x509", "-new", "-sha256", "-days", "$RootCAValidityDays", "-key", $rootKey, "-passin", "env:CLAWMANAGER_CA_PASSWORD", "-out", $rootCertificate, "-subj", "/C=CN/O=Inspur/OU=ClawManager/CN=ClawManager Nine Node Internal Root CA", "-addext", "basicConstraints=critical,CA:TRUE,pathlen:0", "-addext", "keyUsage=critical,keyCertSign,cRLSign", "-addext", "subjectKeyIdentifier=hash")

    Invoke-OpenSSL -Arguments @("genpkey", "-algorithm", "RSA", "-pkeyopt", "rsa_keygen_bits:3072", "-out", $serverKey)
    Invoke-OpenSSL -Arguments @("req", "-new", "-sha256", "-key", $serverKey, "-out", $serverRequest, "-subj", "/C=CN/O=Inspur/OU=ClawManager/CN=$GatewayDNSName")

    @"
authorityKeyIdentifier=keyid,issuer
subjectKeyIdentifier=hash
basicConstraints=critical,CA:FALSE
keyUsage=critical,digitalSignature,keyEncipherment
extendedKeyUsage=serverAuth
subjectAltName=@alt_names

[alt_names]
DNS.1=$GatewayDNSName
DNS.2=*.$GatewayDNSName
IP.1=$GatewayIP
"@ | Set-Content -LiteralPath $extensionsFile -Encoding ascii

    Invoke-OpenSSL -Arguments @("x509", "-req", "-sha256", "-days", "$ServerCertificateValidityDays", "-in", $serverRequest, "-CA", $rootCertificate, "-CAkey", $rootKey, "-passin", "env:CLAWMANAGER_CA_PASSWORD", "-CAcreateserial", "-extfile", $extensionsFile, "-out", $serverCertificate)

    $leafPem = [System.IO.File]::ReadAllText($serverCertificate)
    $rootPem = [System.IO.File]::ReadAllText($rootCertificate)
    [System.IO.File]::WriteAllText($fullChain, $leafPem.TrimEnd() + "`n" + $rootPem.TrimEnd() + "`n")

    Invoke-OpenSSL -Arguments @("verify", "-CAfile", $rootCertificate, $serverCertificate)
    Invoke-OpenSSL -Arguments @("x509", "-checkend", "31536000", "-noout", "-in", $serverCertificate)

    $certificateText = & $script:ResolvedOpenSSL x509 -in $serverCertificate -noout -subject -issuer -dates -fingerprint -sha256 -ext subjectAltName
    if ($LASTEXITCODE -ne 0) {
        throw "Unable to inspect the generated server certificate."
    }
    $certificateText | Set-Content -LiteralPath $manifestFile -Encoding utf8

    Remove-Item -LiteralPath $serverRequest, $extensionsFile -Force
    Write-Host "Internal CA and server certificate generated successfully."
    Write-Host "Output: $absoluteOutput"
    Write-Host "Distribute root-ca.crt to client trust stores. Never distribute root-ca.key."
    Write-Host "Use clawmanager-fullchain.crt and clawmanager.key for the Kubernetes TLS Secret."
}
finally {
    Remove-Item Env:CLAWMANAGER_CA_PASSWORD -ErrorAction SilentlyContinue
    if ($passwordPointer -ne [IntPtr]::Zero) {
        [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($passwordPointer)
    }
    $plainPassword = $null
}

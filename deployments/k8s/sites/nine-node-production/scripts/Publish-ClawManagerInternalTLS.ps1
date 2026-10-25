[CmdletBinding(SupportsShouldProcess = $true)]
param(
    [Parameter(Mandatory = $true)]
    [string]$CertificateDirectory,

    [Parameter(Mandatory = $true)]
    [string]$KubeContext,

    [string]$Namespace = "clawmanager-system",
    [string]$Deployment = "clawmanager-app",
    [string]$GatewayIP = "10.130.15.40",
    [string]$GatewayDNSName = "10-130-15-40.nip.io",
    [int]$NodePort = 30443,
    [string]$OpenSSLPath
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

function Resolve-RequiredCommand {
    param([string]$Name, [string[]]$KnownPaths)
    $command = Get-Command $Name -ErrorAction SilentlyContinue
    if ($command) { return $command.Source }
    foreach ($candidate in $KnownPaths) {
        if ($candidate -and (Test-Path -LiteralPath $candidate -PathType Leaf)) {
            return $candidate
        }
    }
    throw "$Name was not found."
}

$kubectl = Resolve-RequiredCommand -Name kubectl -KnownPaths @(
    "C:\Program Files\Docker\Docker\resources\bin\kubectl.exe"
)
if ($OpenSSLPath) {
    $openssl = (Resolve-Path -LiteralPath $OpenSSLPath).Path
} else {
    $openssl = Resolve-RequiredCommand -Name openssl -KnownPaths @(
        "C:\Program Files\Git\usr\bin\openssl.exe",
        "C:\Program Files\Git\mingw64\bin\openssl.exe"
    )
}

$directory = (Resolve-Path -LiteralPath $CertificateDirectory).Path
$rootCertificate = Join-Path $directory "root-ca.crt"
$serverCertificate = Join-Path $directory "clawmanager-fullchain.crt"
$serverKey = Join-Path $directory "clawmanager.key"
foreach ($required in @($rootCertificate, $serverCertificate, $serverKey)) {
    if (-not (Test-Path -LiteralPath $required -PathType Leaf)) {
        throw "Required certificate material is missing: $required"
    }
}

& $openssl verify -CAfile $rootCertificate (Join-Path $directory "clawmanager.crt")
if ($LASTEXITCODE -ne 0) { throw "Certificate-chain verification failed." }
& $openssl x509 -checkend 31536000 -noout -in (Join-Path $directory "clawmanager.crt")
if ($LASTEXITCODE -ne 0) { throw "Server certificate expires in less than one year." }
$certificateText = & $openssl x509 -in (Join-Path $directory "clawmanager.crt") -noout -ext subjectAltName
if ($LASTEXITCODE -ne 0 -or $certificateText -notmatch [regex]::Escape("DNS:*.$GatewayDNSName") -or $certificateText -notmatch [regex]::Escape("IP Address:$GatewayIP")) {
    throw "Server certificate SAN does not cover *.$GatewayDNSName and $GatewayIP."
}

& $kubectl --context $KubeContext get deployment $Deployment --namespace $Namespace -o name | Out-Null
if ($LASTEXITCODE -ne 0) { throw "Target ClawManager deployment was not found in context $KubeContext." }

$openCodeTemplate = "https://opencode-{instance_id}.${GatewayDNSName}:${NodePort}/"
$deepSeekTemplate = "https://deepseek-harness-{instance_id}.${GatewayDNSName}:${NodePort}/"

if ($PSCmdlet.ShouldProcess("$KubeContext/$Namespace", "Publish clawmanager-tls and runtime public-origin templates")) {
    $secretArguments = @(
        "--context", $KubeContext,
        "create", "secret", "tls", "clawmanager-tls",
        "--namespace", $Namespace,
        "--cert", $serverCertificate,
        "--key", $serverKey,
        "--dry-run=client", "-o", "yaml"
    )
    $secretYaml = & $kubectl @secretArguments
    if ($LASTEXITCODE -ne 0) { throw "Unable to render clawmanager-tls Secret." }
    $secretYaml | & $kubectl --context $KubeContext apply -f -
    if ($LASTEXITCODE -ne 0) { throw "Unable to apply clawmanager-tls Secret." }

    $setEnvironmentArguments = @(
        "--context", $KubeContext,
        "set", "env", "deployment/$Deployment",
        "--namespace", $Namespace,
        "CLAWMANAGER_OPENCODE_PUBLIC_URL_TEMPLATE=$openCodeTemplate",
        "CLAWMANAGER_DEEPSEEK_HARNESS_PUBLIC_URL_TEMPLATE=$deepSeekTemplate"
    )
    & $kubectl @setEnvironmentArguments
    if ($LASTEXITCODE -ne 0) { throw "Unable to update runtime public-origin templates." }

    & $kubectl --context $KubeContext rollout status deployment/$Deployment --namespace $Namespace --timeout=10m
    if ($LASTEXITCODE -ne 0) { throw "ClawManager rollout did not complete successfully." }
}

Write-Host "Published TLS and runtime origins without changing Runtime or Workspace configuration."
Write-Host "OpenCode template: $openCodeTemplate"
Write-Host "DeepSeek Harness template: $deepSeekTemplate"

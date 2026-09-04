[CmdletBinding(SupportsShouldProcess = $true, ConfirmImpact = 'Medium')]
param(
    [string]$Namespace = 'clawmanager-system',
    [string]$SecretName = 'clawmanager-tls',
    [string]$GeneratorDeployment = 'clawmanager-app',
    [string]$NipIoSuffix = '127-0-0-1.nip.io',
    [string]$CommonName = 'localhost',
    [ValidateRange(1, 36500)]
    [int]$ValidityDays = 3650,
    [string]$KubeContext = 'docker-desktop',
    [string]$Kubeconfig = "$env:USERPROFILE\.kube\config",
    [string]$OutputDirectory = "$env:TEMP\clawmanager-nipio-tls",
    [switch]$TrustCurrentUser,
    [switch]$SkipRolloutWait
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

if ($NipIoSuffix -notmatch '^[a-zA-Z0-9.-]+$' -or $NipIoSuffix.StartsWith('.') -or $NipIoSuffix.EndsWith('.')) {
    throw "Invalid nip.io suffix: $NipIoSuffix"
}
if ($Namespace -notmatch '^[a-z0-9]([-a-z0-9]*[a-z0-9])?$') {
    throw "Invalid Kubernetes namespace: $Namespace"
}
if ($SecretName -notmatch '^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$') {
    throw "Invalid Kubernetes Secret name: $SecretName"
}

$kubectlBaseArgs = @()
if ($Kubeconfig) {
    $kubectlBaseArgs += @('--kubeconfig', $Kubeconfig)
}
if ($KubeContext) {
    $kubectlBaseArgs += @('--context', $KubeContext)
}

function Invoke-Kubectl {
    param(
        [Parameter(Mandatory = $true)]
        [string[]]$Arguments,
        [switch]$AllowNotFound
    )

    $output = & kubectl @kubectlBaseArgs @Arguments 2>&1
    if ($LASTEXITCODE -ne 0) {
        $message = ($output | Out-String).Trim()
        if ($AllowNotFound -and $message -match 'NotFound') {
            return $null
        }
        throw "kubectl $($Arguments -join ' ') failed:`n$message"
    }
    return $output
}

if (-not (Get-Command kubectl -ErrorAction SilentlyContinue)) {
    throw 'kubectl was not found in PATH.'
}
if ($Kubeconfig -and -not (Test-Path -LiteralPath $Kubeconfig)) {
    throw "Kubeconfig does not exist: $Kubeconfig"
}

$dnsNames = @(
    'localhost',
    'clawmanager.local',
    $NipIoSuffix,
    "*.$NipIoSuffix"
) | Select-Object -Unique

$sanParts = @($dnsNames | ForEach-Object { "DNS:$_" }) + 'IP:127.0.0.1'
$subjectAltName = $sanParts -join ','
$remoteDirectory = "/tmp/clawmanager-tls-$([Guid]::NewGuid().ToString('N'))"
$deploymentRef = "deployment/$GeneratorDeployment"

Write-Host "Target Secret : $Namespace/$SecretName"
Write-Host "Certificate   : $subjectAltName"
Write-Host "Generator     : $Namespace/$deploymentRef"

Invoke-Kubectl -Arguments @('version', '--client=true') | Out-Null
Invoke-Kubectl -Arguments @('-n', $Namespace, 'get', $deploymentRef, '-o', 'name') | Out-Null

if (-not $PSCmdlet.ShouldProcess("$Namespace/$SecretName", 'Generate and install a new TLS certificate')) {
    return
}

$remoteScript = @'
set -eu
target="$1"
common_name="$2"
subject_alt_name="$3"
validity_days="$4"
umask 077
mkdir -p "$target"
openssl req -x509 -nodes -newkey rsa:2048 \
  -days "$validity_days" \
  -subj "/CN=$common_name" \
  -addext "subjectAltName=$subject_alt_name" \
  -addext "keyUsage=critical,digitalSignature,keyEncipherment" \
  -addext "extendedKeyUsage=serverAuth" \
  -keyout "$target/tls.key" \
  -out "$target/tls.crt"
openssl x509 -in "$target/tls.crt" -noout -subject -dates -ext subjectAltName
'@

try {
    Write-Host 'Generating certificate inside the ClawManager pod...'
    Invoke-Kubectl -Arguments @(
        '-n', $Namespace, 'exec', $deploymentRef, '--',
        'sh', '-c', $remoteScript, 'sh', $remoteDirectory, $CommonName, $subjectAltName, $ValidityDays.ToString()
    ) | ForEach-Object { Write-Host $_ }

    $certificateBase64 = (Invoke-Kubectl -Arguments @(
        '-n', $Namespace, 'exec', $deploymentRef, '--',
        'sh', '-c', 'base64 "$1/tls.crt" | tr -d "\n"', 'sh', $remoteDirectory
    ) | Out-String).Trim()
    $privateKeyBase64 = (Invoke-Kubectl -Arguments @(
        '-n', $Namespace, 'exec', $deploymentRef, '--',
        'sh', '-c', 'base64 "$1/tls.key" | tr -d "\n"', 'sh', $remoteDirectory
    ) | Out-String).Trim()

    if (-not $certificateBase64 -or -not $privateKeyBase64) {
        throw 'The generated certificate or private key was empty.'
    }

    New-Item -ItemType Directory -Path $OutputDirectory -Force | Out-Null
    $timestamp = Get-Date -Format 'yyyyMMdd-HHmmss'
    $backupPath = Join-Path $OutputDirectory "$SecretName-$timestamp.backup.yaml"
    $certificatePath = Join-Path $OutputDirectory 'tls.crt'

    $existingSecret = Invoke-Kubectl -Arguments @(
        '-n', $Namespace, 'get', 'secret', $SecretName, '-o', 'yaml'
    ) -AllowNotFound
    if ($null -ne $existingSecret) {
        $existingSecret | Set-Content -LiteralPath $backupPath -Encoding UTF8
        Write-Host "Existing Secret backup: $backupPath"
    }

    [IO.File]::WriteAllBytes($certificatePath, [Convert]::FromBase64String($certificateBase64))

    $secretManifest = [ordered]@{
        apiVersion = 'v1'
        kind = 'Secret'
        metadata = [ordered]@{
            name = $SecretName
            namespace = $Namespace
        }
        type = 'kubernetes.io/tls'
        data = [ordered]@{
            'tls.crt' = $certificateBase64
            'tls.key' = $privateKeyBase64
        }
    } | ConvertTo-Json -Depth 8

    $secretManifest | & kubectl @kubectlBaseArgs apply -f -
    if ($LASTEXITCODE -ne 0) {
        throw "Failed to apply Secret $Namespace/$SecretName."
    }

    $deploymentsJson = (Invoke-Kubectl -Arguments @(
        '-n', $Namespace, 'get', 'deployments', '-o', 'json'
    ) | Out-String) | ConvertFrom-Json

    $consumers = @($deploymentsJson.items | Where-Object {
        @($_.spec.template.spec.volumes | Where-Object {
            $secretProperty = $_.PSObject.Properties['secret']
            $null -ne $secretProperty -and $secretProperty.Value.secretName -eq $SecretName
        }).Count -gt 0
    } | ForEach-Object { $_.metadata.name })

    if ($consumers.Count -eq 0) {
        Write-Warning "No Deployment in namespace $Namespace directly mounts Secret $SecretName. Restart the TLS endpoint manually if it reads the Secret another way."
    } else {
        foreach ($deployment in $consumers) {
            Write-Host "Restarting deployment/$deployment..."
            Invoke-Kubectl -Arguments @('-n', $Namespace, 'rollout', 'restart', "deployment/$deployment") |
                ForEach-Object { Write-Host $_ }
        }
        if (-not $SkipRolloutWait) {
            foreach ($deployment in $consumers) {
                Invoke-Kubectl -Arguments @(
                    '-n', $Namespace, 'rollout', 'status', "deployment/$deployment", '--timeout=5m'
                ) | ForEach-Object { Write-Host $_ }
            }
        }
    }

    if ($TrustCurrentUser) {
        Write-Warning 'This trusts the self-signed certificate for the current Windows user.'
        Import-Certificate -FilePath $certificatePath -CertStoreLocation 'Cert:\CurrentUser\Root' | Out-Null
        Write-Host 'Certificate imported into Cert:\CurrentUser\Root.'
    }

    Write-Host ''
    Write-Host 'TLS deployment completed.' -ForegroundColor Green
    Write-Host "Certificate copy: $certificatePath"
    Write-Host "Test URL       : https://opencode-1.$NipIoSuffix`:8443/"
}
finally {
    try {
        Invoke-Kubectl -Arguments @(
            '-n', $Namespace, 'exec', $deploymentRef, '--',
            'sh', '-c', 'rm -rf -- "$1"', 'sh', $remoteDirectory
        ) | Out-Null
    } catch {
        Write-Warning "Could not remove temporary pod files at ${remoteDirectory}: $($_.Exception.Message)"
    }
}

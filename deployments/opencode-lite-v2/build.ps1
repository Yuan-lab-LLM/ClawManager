param(
    [string]$RuntimeSource = 'D:\AgentsRuntime',
    [string]$BaseImage = 'clawmanager:opencode-workspace-root-fix-20260825',
    [string]$Image = 'clawmanager-local/opencode-lite:2.0.27-20261012'
)

$ErrorActionPreference = 'Stop'
$projectRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\..'))
$contextPath = Join-Path $projectRoot '.tmp\opencode-lite-v2-build'
$agentSource = Join-Path $RuntimeSource 'clawmanager-agent'
if (-not (Test-Path -LiteralPath (Join-Path $agentSource 'go.mod'))) {
    throw "Runtime Agent source not found: $agentSource"
}
$healthSource = Get-Content -Raw -LiteralPath (Join-Path $agentSource 'internal\runtime\opencode\health.go')
if (-not $healthSource.Contains('OPENCODE_RUNTIME_MAJOR=2')) {
    throw 'Apply the OpenCode v2 Runtime Agent health changes before building.'
}
New-Item -ItemType Directory -Force $contextPath | Out-Null
$agentContext = Join-Path $contextPath 'clawmanager-agent'
New-Item -ItemType Directory -Force $agentContext | Out-Null
foreach ($entry in @('cmd', 'internal', 'go.mod')) {
    Copy-Item -LiteralPath (Join-Path $agentSource $entry) -Destination $agentContext -Recurse -Force
}
$archive = Join-Path $contextPath 'opencode-cli-linux-x64-2.0.27.tgz'
if (-not (Test-Path -LiteralPath $archive)) {
    & npm.cmd pack '@opencode/cli-linux-x64@2.0.27' '--registry=https://registry.npmjs.org' '--pack-destination' $contextPath
    if ($LASTEXITCODE -ne 0) { throw 'Official OpenCode package download failed.' }
}
$expectedHash = 'QPGrzBSSs5ATG3nM4XjS4tmWXUB22Di36H0vfMuhKwk6i3PR3Nb4sm+l00gvTdcyCRohYsKwqjvD67T0DPcQLQ=='
$stream = [IO.File]::OpenRead($archive)
$sha = [Security.Cryptography.SHA512]::Create()
try { $actualHash = [Convert]::ToBase64String($sha.ComputeHash($stream)) }
finally { $stream.Dispose(); $sha.Dispose() }
if ($actualHash -ne $expectedHash) { throw 'OpenCode package SHA-512 verification failed.' }
& tar -xzf $archive -C $contextPath
if ($LASTEXITCODE -ne 0) { throw 'OpenCode package extraction failed.' }
& docker build -f (Join-Path $PSScriptRoot 'Dockerfile') --build-arg "BASE_IMAGE=$BaseImage" -t $Image $contextPath
if ($LASTEXITCODE -ne 0) { throw 'OpenCode Lite v2 image build failed.' }
Write-Host "Built $Image (linux/amd64, OpenCode 2.0.27)"

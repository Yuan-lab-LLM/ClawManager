param(
    [string]$RuntimeSource = 'D:\AgentsRuntime',
    [string]$BaseImage = 'clawmanager-local/opencode-lite:2.0.27-20261012',
    [string]$Image = 'clawmanager-local/opencode-pro:2.0.27-20261016'
)
$ErrorActionPreference = 'Stop'
$projectRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\..'))
$contextPath = Join-Path $projectRoot '.tmp\opencode-pro-v2-build'
$binSource = Join-Path $RuntimeSource 'opencode\rootfs\usr\local\bin'
$binContext = Join-Path $contextPath 'rootfs\usr\local\bin'
New-Item -ItemType Directory -Force $binContext | Out-Null
foreach ($name in @('start-opencode-terminal', 'start-opencode-pro-web', 'opencode-pro-session.py')) {
    $contents = [IO.File]::ReadAllText((Join-Path $binSource $name)).Replace("`r`n", "`n")
    [IO.File]::WriteAllText((Join-Path $binContext $name), $contents, (New-Object Text.UTF8Encoding($false)))
}
& python -B -m unittest discover -s (Join-Path $RuntimeSource 'opencode\tests') -p test_session.py -v
if ($LASTEXITCODE -ne 0) { throw 'Pro session regression tests failed.' }
& docker build -f (Join-Path $PSScriptRoot 'Dockerfile') --build-arg "BASE_IMAGE=$BaseImage" -t $Image $contextPath
if ($LASTEXITCODE -ne 0) { throw 'OpenCode Pro v2 image build failed.' }
Write-Host "Built $Image from $RuntimeSource (OpenCode 2.0.27, linux/amd64)"

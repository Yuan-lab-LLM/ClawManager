[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$Image,
    [Parameter(Mandatory = $true)][string]$RuntimeRoot,
    [string]$NativeFixtureDirectory
)
$ErrorActionPreference = 'Stop'
$patchRoot = (Resolve-Path -LiteralPath (Join-Path $RuntimeRoot 'openclaw\patches')).Path
$scriptRoot = (Resolve-Path -LiteralPath $PSScriptRoot).Path
$machineFixture = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot 'fixtures\receipt-test-machine-id')).Path
$imageID = & docker image inspect $Image --format '{{.Id}}'
if ($LASTEXITCODE -ne 0 -or $imageID -notmatch '^sha256:[a-f0-9]{64}$') { throw 'A locally cached image is required; this script never pulls images.' }
Write-Host "Linux test image: $imageID"
$containerName = 'cm-receipt-test-' + [Guid]::NewGuid().ToString('N')
$dockerArgs = @('run', '--rm', '--pull=never', '--name', $containerName,
    '--network', 'none', '--read-only', '--cap-drop', 'ALL', '--security-opt', 'no-new-privileges',
    '--user', '1000:1000', '--cpus', '2', '--memory', '1g', '--pids-limit', '256',
    '--tmpfs', '/tmp:rw,nosuid,nodev,size=268435456,mode=1777',
    '--mount', "type=bind,source=$scriptRoot,target=/tests,readonly",
    '--mount', "type=bind,source=$patchRoot,target=/runtime,readonly",
    '--mount', "type=bind,source=$machineFixture,target=/etc/machine-id,readonly",
    '--env', 'OPENCLAW_RECEIPT_STORE_MODULE=/runtime/task_receipt_store.mjs')
if ($NativeFixtureDirectory) {
    $fixtureRoot = (Resolve-Path -LiteralPath $NativeFixtureDirectory).Path
    $nativeFiles = @('models-DIG525mR.mjs', 'chat-metadata-handler-n6SiH0iX.mjs',
        'chat-history-handler-Bl3TJIG1.mjs', 'embedded-agent-gW15apSG.mjs',
        'model-fallback-runner-COVxfi1J.mjs', 'model-IiOP36F5.mjs', 'compact-BCDNk0Nn.mjs',
        'server-shutdown-DqBhRLh6.mjs', 'server-close.runtime.js')
    foreach ($name in $nativeFiles) {
        $source = Join-Path $fixtureRoot $name
        if ($name -eq 'embedded-agent-gW15apSG.mjs' -and -not (Test-Path -LiteralPath $source)) {
            $source = Join-Path $fixtureRoot 'openclaw-embedded-agent.mjs'
        }
        $source = (Resolve-Path -LiteralPath $source).Path
        # Mount only these pinned fixture files, never the whole temporary folder.
        $dockerArgs += @('--mount', "type=bind,source=$source,target=/fixtures/$name,readonly")
    }
    $dockerArgs += @('--env', 'OPENCLAW_NATIVE_FIXTURE_DIR=/fixtures')
}
$dockerArgs += @('--workdir', '/tests', '--entrypoint', 'node', $imageID, '/tests/run-receipt-linux-tests.mjs')
if ($NativeFixtureDirectory) { $dockerArgs += '--include-runtime' }
& docker @dockerArgs
if ($LASTEXITCODE -ne 0) { throw "Linux tests failed (exit $LASTEXITCODE). Container name: $containerName" }

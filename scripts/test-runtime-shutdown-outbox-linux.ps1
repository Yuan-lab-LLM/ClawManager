[CmdletBinding()]
param(
    [string]$RuntimeRoot = 'D:\AgentsRuntime',
    [string]$Image = 'sha256:ad9dc0f6daf5ef911c5989db30d0ecb572070699f8b9dfed6bd4c64a23bdfa95'
)
$ErrorActionPreference = 'Stop'
foreach ($file in @('outbox.go', 'outbox_test.go', 'delivery_test.go', 'crash_test.go')) {
    $first = Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $RuntimeRoot "openclaw\internal\shutdownoutbox\$file")
    $second = Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $RuntimeRoot "clawmanager-agent\internal\shutdownoutbox\$file")
    if ($first.Hash -ne $second.Hash) { throw "Outbox copies differ: $file. Test both before proceeding." }
}
$imagePlatform = & docker image inspect $Image --format '{{.Os}}/{{.Architecture}}'
if ($LASTEXITCODE -ne 0 -or $imagePlatform -ne 'linux/amd64') { throw 'A cached linux/amd64 image is required; no images are pulled.' }
$artifactRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\.codex-temp'))
if (Test-Path -LiteralPath $artifactRoot) {
    if ((Get-Item -LiteralPath $artifactRoot -Force).Attributes -band [IO.FileAttributes]::ReparsePoint) { throw 'Artifact directory must not be a link.' }
} else { New-Item -ItemType Directory -Path $artifactRoot | Out-Null }
$artifactRoot = (Resolve-Path -LiteralPath $artifactRoot).Path
$artifact = Join-Path $artifactRoot ('shutdown-outbox-linux-' + [Guid]::NewGuid().ToString('N') + '.test')
$previousGOOS = $env:GOOS
$previousGOARCH = $env:GOARCH
$previousCGO = $env:CGO_ENABLED
try {
    $env:GOOS = 'linux'; $env:GOARCH = 'amd64'; $env:CGO_ENABLED = '0'
    Push-Location (Join-Path $RuntimeRoot 'openclaw')
    try {
        & go test -c -o $artifact ./internal/shutdownoutbox
        if ($LASTEXITCODE -ne 0) { throw 'Linux test compilation failed.' }
    } finally { Pop-Location }
    $containerName = 'cm-shutdown-outbox-test-' + [Guid]::NewGuid().ToString('N')
    & docker run --rm --pull=never --name $containerName --network none --read-only --cap-drop ALL --security-opt no-new-privileges --user '1000:1000' --cpus 2 --memory 512m --pids-limit 128 --tmpfs '/tmp:rw,nosuid,nodev,size=134217728,mode=1777' --mount "type=bind,source=$artifact,target=/tests/shutdownoutbox.test,readonly" --entrypoint /tests/shutdownoutbox.test $Image '-test.v' '-test.timeout=2m'
    if ($LASTEXITCODE -ne 0) { throw 'Linux outbox regression failed.' }
} finally {
    $env:GOOS = $previousGOOS; $env:GOARCH = $previousGOARCH; $env:CGO_ENABLED = $previousCGO
    # Only remove the exact generated binary, never a directory or a glob.
    if ((Split-Path -Parent $artifact) -eq $artifactRoot -and (Split-Path -Leaf $artifact) -match '^shutdown-outbox-linux-[a-f0-9]{32}\.test$' -and (Test-Path -LiteralPath $artifact)) {
        Remove-Item -LiteralPath $artifact -Force
    }
}

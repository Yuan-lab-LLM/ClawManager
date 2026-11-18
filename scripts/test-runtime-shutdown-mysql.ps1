[CmdletBinding()]
param([string]$Image = 'mysql:8.4.8')
$ErrorActionPreference = 'Stop'
$name = 'cm-shutdown-test-' + [Guid]::NewGuid().ToString('N')
$schema = 'clawmanager_test_shutdown_' + [Guid]::NewGuid().ToString('N')
$oldDSN = $env:CLAWMANAGER_TEST_MYSQL_DSN
$backendRoot = Join-Path $PSScriptRoot '..\backend'
$created = $false
try {
    & docker image inspect $Image --format '{{.Id}}'
    if ($LASTEXITCODE -ne 0) { throw 'A cached MySQL image is required; no images are pulled.' }
    & docker run --detach --rm --pull=never --name $name --label "clawmanager.shutdown-test=$name" --cpus 1 --memory 768m --pids-limit 256 --publish '127.0.0.1::3306' --tmpfs '/var/lib/mysql:rw,size=402653184' --env 'MYSQL_ALLOW_EMPTY_PASSWORD=yes' --env 'MYSQL_ROOT_HOST=%' --env "MYSQL_DATABASE=$schema" $Image --innodb-buffer-pool-size=64M --performance-schema=OFF
    if ($LASTEXITCODE -ne 0) { throw 'Cannot start isolated test MySQL.' }
    $created = $true
    $ready = $false
    for ($attempt = 0; $attempt -lt 60; $attempt++) {
        try {
            $readyResult = & docker exec $name mysql '--protocol=TCP' '--host=127.0.0.1' '--user=root' '--batch' '--skip-column-names' "--database=$schema" '--execute=SELECT 1' 2>$null
            if ($LASTEXITCODE -eq 0 -and $readyResult -eq '1') { $ready = $true; break }
        } catch { <# The final TCP server is not ready yet. #> }
        Start-Sleep -Seconds 1
    }
    if (-not $ready) { throw 'Isolated MySQL readiness timed out.' }
    $address = (& docker port $name '3306/tcp').Trim()
    if ($LASTEXITCODE -ne 0 -or $address -notmatch '^127\.0\.0\.1:\d+$') { throw 'Unexpected test port binding.' }
    $env:CLAWMANAGER_TEST_MYSQL_DSN = "root@tcp($address)/${schema}?parseTime=true"
    Push-Location $backendRoot
    try {
        & go test -vet=all ./internal/repository -run '^TestRuntimeShutdownAuditMySQL' -count=1 -v
        if ($LASTEXITCODE -ne 0) { throw 'Shutdown audit database tests failed.' }
    } finally { Pop-Location }
} finally {
    $env:CLAWMANAGER_TEST_MYSQL_DSN = $oldDSN
    if ($created) {
        $labelsJSON = & docker inspect $name --format '{{json .Config.Labels}}'
        $labels = if ($LASTEXITCODE -eq 0) { $labelsJSON | ConvertFrom-Json } else { $null }
        if ($labels.'clawmanager.shutdown-test' -eq $name -and $name -match '^cm-shutdown-test-[a-f0-9]{32}$') {
            & docker rm --force $name | Out-Null
            if ($LASTEXITCODE -ne 0) { Write-Warning "Failed to remove test container $name" }
        } else { Write-Warning "Test identity changed; container was not removed: $name" }
    }
}

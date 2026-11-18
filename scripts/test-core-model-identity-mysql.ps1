[CmdletBinding()]
param([string]$Image = 'mysql:8.4.8')
$ErrorActionPreference = 'Stop'
$containerName = 'cm-core-model-test-' + [Guid]::NewGuid().ToString('N')
$schemaName = 'clawmanager_test_identity_' + [Guid]::NewGuid().ToString('N')
$previousIdentityDSN = $env:CLAWMANAGER_IDENTITY_TEST_MYSQL_DSN
$previousRuntimeDSN = $env:CLAWMANAGER_TEST_MYSQL_DSN
$created = $false
try {
    & docker image inspect $Image --format '{{.Id}}'
    if ($LASTEXITCODE -ne 0) { throw 'A cached MySQL image is required; no image is pulled.' }
    & docker run --detach --rm --pull=never --name $containerName --label "clawmanager.core-model-test=$containerName" --cpus 1 --memory 768m --pids-limit 256 --publish '127.0.0.1::3306' --tmpfs '/var/lib/mysql:rw,size=402653184' --env 'MYSQL_ALLOW_EMPTY_PASSWORD=yes' --env 'MYSQL_ROOT_HOST=%' --env "MYSQL_DATABASE=$schemaName" $Image --innodb-buffer-pool-size=64M --performance-schema=OFF
    if ($LASTEXITCODE -ne 0) { throw 'Cannot start isolated core-model MySQL.' }
    $created = $true
    $ready = $false
    for ($attempt = 0; $attempt -lt 60; $attempt++) {
        try {
            $answer = & docker exec $containerName mysql '--protocol=TCP' '--host=127.0.0.1' '--user=root' '--batch' '--skip-column-names' "--database=$schemaName" '--execute=SELECT 1' 2>$null
            if ($LASTEXITCODE -eq 0 -and $answer -eq '1') { $ready = $true; break }
        } catch { <# Wait for the final TCP server, not an initialization process. #> }
        Start-Sleep -Seconds 1
    }
    if (-not $ready) { throw 'Core-model MySQL readiness timed out.' }
    $address = (& docker port $containerName '3306/tcp').Trim()
    if ($LASTEXITCODE -ne 0 -or $address -notmatch '^127\.0\.0\.1:\d+$') { throw 'Unexpected test port binding.' }
    $env:CLAWMANAGER_TEST_MYSQL_DSN = $null
    $env:CLAWMANAGER_IDENTITY_TEST_MYSQL_DSN = "root@tcp($address)/${schemaName}?parseTime=true"
    Push-Location (Join-Path $PSScriptRoot '..\backend')
    try {
        & go test -vet=all ./internal/repository -run '^TestRuntimeModelIdentityMySQL$' -count=1 -v
        if ($LASTEXITCODE -ne 0) { throw 'Core-model identity integration test failed.' }
    } finally { Pop-Location }
} finally {
    $env:CLAWMANAGER_IDENTITY_TEST_MYSQL_DSN = $previousIdentityDSN
    $env:CLAWMANAGER_TEST_MYSQL_DSN = $previousRuntimeDSN
    if ($created) {
        $raw = & docker inspect $containerName --format '{{json .Config.Labels}}'
        $labels = if ($LASTEXITCODE -eq 0) { $raw | ConvertFrom-Json } else { $null }
        if ($labels.'clawmanager.core-model-test' -eq $containerName -and $containerName -match '^cm-core-model-test-[a-f0-9]{32}$') {
            & docker rm --force $containerName | Out-Null
            if ($LASTEXITCODE -ne 0) { Write-Warning 'Temporary core-model test container cleanup failed; inspect the exact recorded container name.' }
        } else { Write-Warning 'Test container ownership could not be verified; nothing was removed.' }
    }
}

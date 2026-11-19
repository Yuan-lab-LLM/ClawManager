param(
    [string]$Image = '10.130.14.23:5000/laya-hsr:cpu-offline-20260928-v1',
    [string]$DockerContext = 'desktop-linux'
)
$ErrorActionPreference = 'Stop'
$testName = 'laya-offline-smoke-' + [Guid]::NewGuid().ToString('N').Substring(0, 12)
$keyBytes = New-Object byte[] 32
$rng = [Security.Cryptography.RandomNumberGenerator]::Create()
$rng.GetBytes($keyBytes)
$rng.Dispose()
$testKey = [Convert]::ToBase64String($keyBytes)
$created = $false
try {
    docker --context $DockerContext run -d --name $testName --network none --read-only --tmpfs /tmp:rw,nosuid,size=512m --cap-drop ALL --security-opt no-new-privileges --cpus 4 --memory 8g -e "LAYA_API_KEY=$testKey" $Image
    if ($LASTEXITCODE -ne 0) { throw 'Could not start offline test container' }
    $created = $true
    Write-Output "Offline container: $testName (no network, read-only rootfs)"
    $ready = $false
    $deadline = (Get-Date).AddMinutes(10)
    while ((Get-Date) -lt $deadline) {
        $running = docker --context $DockerContext inspect --format '{{.State.Running}}' $testName
        if ($running -ne 'true') { throw 'Offline service exited before readiness' }
        # Numeric loopback avoids Windows native-command quote conversion.
        docker --context $DockerContext exec $testName python -c 'import socket; s=socket.socket(); s.settimeout(3); exit(s.connect_ex((str(2130706433),8000)))'
        if ($LASTEXITCODE -eq 0) { $ready = $true; break }
        Start-Sleep -Seconds 3
    }
    if (-not $ready) { throw 'Offline service startup timed out' }
    docker --context $DockerContext exec $testName python /opt/laya/smoke_test.py
    if ($LASTEXITCODE -ne 0) { throw 'Offline inference smoke test failed' }
    docker --context $DockerContext stats --no-stream --format '{{.Name}} memory={{.MemUsage}} cpu={{.CPUPerc}}' $testName
} finally {
    if ($created) {
        docker --context $DockerContext logs --tail 60 $testName
        # Only remove this script's randomly named disposable test container.
        docker --context $DockerContext rm -f $testName
    }
    $testKey = $null
}

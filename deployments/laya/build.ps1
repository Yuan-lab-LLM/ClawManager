param(
    [string]$Image = '10.130.14.23:5000/laya-hsr:cpu-offline-20260928-v1',
    [string]$DockerContext = 'desktop-linux'
)
$ErrorActionPreference = 'Stop'
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '../..')).Path
$contextDir = Join-Path $repoRoot '.codex-temp/laya-offline-build'
$sourceDir = Join-Path $contextDir 'source'
$sourceRevision = '9d955671415fc19f069b9cc998928075c1f255ec'
$actualRevision = git -C $sourceDir rev-parse HEAD
if ($LASTEXITCODE -ne 0 -or $actualRevision -ne $sourceRevision) {
    throw "Expected Laya source revision $sourceRevision under $sourceDir"
}
$snapshot = Join-Path $repoRoot '.codex-temp/laya-model-cache/models--convaiinnovations--laya-multilingual/snapshots/e4e9ddf21a7b1903b7acffd8814ad4307bf63a67'
$modelDir = Join-Path $contextDir 'model'
$files = @('model.safetensors', 'rl_agent_config.json', 'encoder/config.json', 'tokenizer/tokenizer.json', 'tokenizer/tokenizer_config.json')
foreach ($relative in $files) {
    $inputPath = Join-Path $snapshot $relative
    $outputPath = Join-Path $modelDir $relative
    New-Item -ItemType Directory -Force -Path (Split-Path $outputPath -Parent) | Out-Null
    # File.Copy dereferences HF snapshot symlinks; no Windows links go into the image.
    [System.IO.File]::Copy($inputPath, $outputPath, $true)
    if ((Get-Item $outputPath).Length -eq 0) { throw "Empty model file: $relative" }
}
$weightHash = (Get-FileHash (Join-Path $modelDir 'model.safetensors') -Algorithm SHA256).Hash
if ($weightHash -ne '9d628fd971b700382ac6f65920a86f149777b2e748e0c955fb3b19695aa8f204') {
    throw 'Model weight SHA256 mismatch'
}
Copy-Item (Join-Path $PSScriptRoot 'offline_server.py') $contextDir -Force
Copy-Item (Join-Path $PSScriptRoot 'smoke_test.py') $contextDir -Force
Copy-Item (Join-Path $PSScriptRoot 'context.dockerignore') (Join-Path $contextDir '.dockerignore') -Force
docker --context $DockerContext build --platform linux/amd64 --progress plain --build-arg "LAYA_REVISION=$sourceRevision" -f (Join-Path $PSScriptRoot 'Dockerfile') -t $Image $contextDir
if ($LASTEXITCODE -ne 0) { throw 'Laya image build failed' }

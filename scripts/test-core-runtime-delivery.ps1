[CmdletBinding()]
param(
    [ValidateSet('all', 'backend', 'frontend', 'runtime')][string]$Component = 'all',
    [string]$RuntimeRoot = 'D:\AgentsRuntime',
    [string]$PythonExe = '',
    [switch]$SkipFrontendBuild,
    [switch]$BrowserTests
)

# Local regression only. No package installation, database/container creation,
# image build, Kubernetes mutation, protocol activation, commit or push.
$ErrorActionPreference = 'Stop'
$projectRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$results = New-Object System.Collections.Generic.List[object]
$savedDatabaseEnvironment = @{}
foreach ($name in @('CLAWMANAGER_TEST_MYSQL_DSN', 'CLAWMANAGER_IDENTITY_TEST_MYSQL_DSN')) {
    $savedDatabaseEnvironment[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
    [Environment]::SetEnvironmentVariable($name, $null, 'Process')
}
function Invoke-CoreCheck {
    param([string]$Name, [string]$Directory, [string]$Program, [string[]]$Arguments)
    $timer = [Diagnostics.Stopwatch]::StartNew()
    $code = 1
    Write-Host "Running core check: $Name"
    try {
        if (-not (Test-Path -LiteralPath $Directory -PathType Container)) { throw 'Required source directory is unavailable.' }
        $null = Get-Command $Program -ErrorAction Stop
        Push-Location -LiteralPath $Directory
        try {
            & $Program @Arguments
            $code = $LASTEXITCODE
            if ($null -eq $code) { $code = 1 }
        } finally { Pop-Location }
    } catch {
        # Do not print inherited credentials or arbitrary dependency exceptions.
        Write-Warning "Cannot complete $Name. Check the required local tool and dependencies."
    } finally {
        $timer.Stop()
        $results.Add([pscustomobject]@{Check = $Name; ExitCode = $code; Seconds = [math]::Round($timer.Elapsed.TotalSeconds, 1)})
    }
}
try {
    Write-Host 'Scope: model references, catalog sync, access control and preserved runtime configuration.'
    Write-Host 'MySQL integration is intentionally not run here; inherited test DSNs are cleared for this process and restored afterward.'
    if ($Component -in @('all', 'backend')) {
        Invoke-CoreCheck 'Gateway aliases and permissions' (Join-Path $projectRoot 'backend') 'go' @('test', '-vet=all', './internal/aigateway', '-run', 'ModelAccess|RuntimeModel|RuntimeCatalog|HistoricalRuntime|ListAvailableModels|ProviderPermission', '-count=1')
        Invoke-CoreCheck 'Platform model/config compatibility' (Join-Path $projectRoot 'backend') 'go' @('test', '-vet=all', './internal/models', './internal/repository', './internal/handlers', './internal/middleware', './internal/services', './cmd/server', '-run', 'Model|Catalog|ProviderRef|OpenCode|OpenClaw|Hermes|Harness|InstanceEnv|RuntimeProtocol|ManagedRuntime|BuildInstancePodEnv', '-count=1')
    }
    if ($Component -in @('all', 'frontend')) {
        Invoke-CoreCheck 'Default instance entry and session access' (Join-Path $projectRoot 'frontend') 'node' @('--test', 'scripts/opencode-default-entry.test.mjs', 'src/hooks/useInstanceDesktopAccess.test.mjs')
        if (-not $SkipFrontendBuild) {
            $npm = if ([Environment]::OSVersion.Platform -eq [PlatformID]::Win32NT) { 'npm.cmd' } else { 'npm' }
            Invoke-CoreCheck 'Frontend TypeScript and production build' (Join-Path $projectRoot 'frontend') $npm @('run', 'build')
        } else { Write-Warning 'Frontend build was explicitly skipped; this is incomplete build validation.' }
        if ($BrowserTests) {
            Invoke-CoreCheck 'OpenCode browser storage isolation' (Join-Path $projectRoot 'e2e') 'node' @('scripts/opencode-browser-storage.test.mjs')
            Invoke-CoreCheck 'OpenCode browser onboarding' (Join-Path $projectRoot 'e2e') 'node' @('scripts/opencode-onboarding.test.mjs')
        } else { Write-Warning 'Real browser checks were not requested. Use -BrowserTests with cached Playwright/Chromium, then perform live instance acceptance separately.' }
    }
    if ($Component -in @('all', 'runtime')) {
        $runtimePath = [IO.Path]::GetFullPath($RuntimeRoot)
        Invoke-CoreCheck 'Pro model references and restart preservation' (Join-Path $runtimePath 'openclaw') 'go' @('test', '-vet=all', './internal/managedmodelrefs', './internal/configmanager', './internal/bootstrap', '-count=1')
        Invoke-CoreCheck 'Lite model catalogs and runtime configuration' (Join-Path $runtimePath 'clawmanager-agent') 'go' @('test', '-vet=all', './internal/managedmodelrefs', './internal/openclawcompat', './internal/llmconfig', './internal/runtime/openclaw', './internal/runtime/opencode', './internal/runtime/hermes', './internal/runtime/deepseek', './cmd/clawmanager-agent', '-count=1')
        Invoke-CoreCheck 'OpenClaw historical references and picker' $runtimePath 'node' @('--test', 'openclaw/patches/test_managed_model_reference_102.mjs')
        Invoke-CoreCheck 'OpenCode catalog governance and startup' $runtimePath 'node' @('--test', 'opencode/model-governance/governance.test.mjs', 'opencode/tests/start-opencode-web.test.mjs')
        Invoke-CoreCheck 'Harness catalog and saved selections' $runtimePath 'node' @('--test', 'deepseek-harness/scripts/managed-model-reference.test.mjs', 'deepseek-harness/scripts/managed-config.test.mjs')
        if ([Environment]::OSVersion.Platform -ne [PlatformID]::Win32NT) {
            Invoke-CoreCheck 'Harness real private-session termination' $runtimePath 'node' @('--test', 'deepseek-harness/scripts/harness-pro-process.test.mjs')
        } else { Write-Warning 'Harness real private-session termination requires Linux; Windows checks are not process-group acceptance.' }
        $pythonProgram = $PythonExe
        $pythonPrefix = @()
        if ($pythonProgram -eq '') {
            if ([Environment]::OSVersion.Platform -eq [PlatformID]::Win32NT -and (Get-Command 'py' -ErrorAction SilentlyContinue)) {
                $pythonProgram = 'py'; $pythonPrefix = @('-3')
            } else { $pythonProgram = 'python3' }
        }
        Invoke-CoreCheck 'Hermes Python test dependencies' $runtimePath $pythonProgram ($pythonPrefix + @('-c', 'import sys, yaml; assert sys.version_info >= (3, 9); print(sys.version)'))
        Invoke-CoreCheck 'Hermes catalog reconciliation' $runtimePath $pythonProgram ($pythonPrefix + @('hermes/tests/test_model_catalog_merge.py'))
        Invoke-CoreCheck 'Hermes historical model references' $runtimePath $pythonProgram ($pythonPrefix + @('hermes/tests/test_managed_model_catalog.py'))
        Invoke-CoreCheck 'Hermes native tool patch integrity' $runtimePath $pythonProgram ($pythonPrefix + @('hermes/tests/test_lite_tool_executor.py'))
    }
} finally {
    foreach ($name in $savedDatabaseEnvironment.Keys) {
        [Environment]::SetEnvironmentVariable($name, $savedDatabaseEnvironment[$name], 'Process')
    }
    $results | Format-Table -AutoSize | Out-Host
}
if (@($results | Where-Object { $_.ExitCode -ne 0 }).Count -gt 0) { throw 'Core regression has failed checks. No deployment or source reset was performed.' }
Write-Host 'Selected local core checks completed. Native fixtures, disposable MySQL, live session/permission/upgrade acceptance and remote CI are separate gates; skipped tests are not acceptance.'

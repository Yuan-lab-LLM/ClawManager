# Some VPN clients overwrite Windows proxy bypass rules when reconnecting.
# Run after reconnecting if the local OpenCode dedicated origin stops opening.
# This does not disable the proxy, alter its server, or change container settings.
[CmdletBinding(SupportsShouldProcess = $true)]
param()

$ErrorActionPreference = 'Stop'
$settingsPath = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Internet Settings'
$localRuntimeBypass = '*.127-0-0-1.nip.io'
$before = Get-ItemProperty -LiteralPath $settingsPath
$entries = @([string]$before.ProxyOverride -split ';' | Where-Object {
    -not [string]::IsNullOrWhiteSpace($_)
})

if ($entries -notcontains $localRuntimeBypass) {
    if (-not $PSCmdlet.ShouldProcess($settingsPath, "Add proxy bypass: $localRuntimeBypass")) {
        return
    }
    Set-ItemProperty -LiteralPath $settingsPath -Name ProxyOverride -Value (($entries + $localRuntimeBypass) -join ';')
} elseif ($WhatIfPreference) {
    Write-Output 'The local runtime bypass rule already exists.'
    return
}

if (-not ('ClawManager.LocalProxyRefresh' -as [type])) {
    Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
namespace ClawManager {
    public static class LocalProxyRefresh {
        [DllImport("wininet.dll", SetLastError = true)]
        [return: MarshalAs(UnmanagedType.Bool)]
        public static extern bool InternetSetOption(IntPtr handle, int option, IntPtr buffer, int length);
    }
}
'@
}

$changed = [ClawManager.LocalProxyRefresh]::InternetSetOption([IntPtr]::Zero, 39, [IntPtr]::Zero, 0)
$refreshed = [ClawManager.LocalProxyRefresh]::InternetSetOption([IntPtr]::Zero, 37, [IntPtr]::Zero, 0)
$after = Get-ItemProperty -LiteralPath $settingsPath
if ($after.ProxyEnable -ne $before.ProxyEnable -or $after.ProxyServer -ne $before.ProxyServer) {
    throw 'The proxy server settings changed concurrently. Check your VPN client settings.'
}
if (@([string]$after.ProxyOverride -split ';') -notcontains $localRuntimeBypass) {
    throw 'The bypass rule was overwritten. Add it to the VPN client bypass settings.'
}
if (-not $changed -or -not $refreshed) {
    Write-Warning 'The rule was saved, but Windows could not notify all proxy consumers. Restart the browser manually if needed.'
}

[PSCustomObject]@{
    LocalRuntimeBypass = $localRuntimeBypass
    ProxyEnabled = $after.ProxyEnable
    ProxyServer = $after.ProxyServer
    NotificationSucceeded = ($changed -and $refreshed)
}

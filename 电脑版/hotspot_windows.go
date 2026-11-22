//go:build windows

package main

import (
	"encoding/base64"
	"strings"
	"time"
	"unicode/utf16"
)

const hotspotPS = `
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [Text.Encoding]::UTF8
$out = @{ ok = $false; state = 'Unknown'; ssid = ''; pass = ''; clients = 0; maxClients = 0; profile = ''; status = ''; err = '' }
try {
  Add-Type -AssemblyName System.Runtime.WindowsRuntime
  $asTaskGeneric = ([System.WindowsRuntimeSystemExtensions].GetMethods() | Where-Object { $_.Name -eq 'AsTask' -and $_.GetParameters().Count -eq 1 -and $_.GetParameters()[0].ParameterType.Name -eq 'IAsyncOperation` + "`" + `1' })[0]
  function Await($op, $t) { $task = $asTaskGeneric.MakeGenericMethod($t).Invoke($null, @($op)); $task.Wait(-1) | Out-Null; $task.Result }
  $cp = [Windows.Networking.Connectivity.NetworkInformation,Windows.Networking.Connectivity,ContentType=WindowsRuntime]::GetInternetConnectionProfile()
  if ($cp -eq $null) { $out.status = 'NoInternet'; throw 'no internet connection profile' }
  $out.profile = $cp.ProfileName
  $tm = [Windows.Networking.NetworkOperators.NetworkOperatorTetheringManager,Windows.Networking.NetworkOperators,ContentType=WindowsRuntime]::CreateFromConnectionProfile($cp)
  $rt = [Windows.Networking.NetworkOperators.NetworkOperatorTetheringOperationResult,Windows.Networking.NetworkOperators,ContentType=WindowsRuntime]
  if ('ACTION' -eq 'on' -and $tm.TetheringOperationalState -ne 'On') {
    $r = Await ($tm.StartTetheringAsync()) $rt
    $out.status = [string]$r.Status
    if ($r.Status -ne 'Success') { $out.err = [string]$r.AdditionalErrorMessage }
  } elseif ('ACTION' -eq 'off' -and $tm.TetheringOperationalState -ne 'Off') {
    $r = Await ($tm.StopTetheringAsync()) $rt
    $out.status = [string]$r.Status
    if ($r.Status -ne 'Success') { $out.err = [string]$r.AdditionalErrorMessage }
  }
  $cfg = $tm.GetCurrentAccessPointConfiguration()
  $out.ssid = [string]$cfg.Ssid; $out.pass = [string]$cfg.Passphrase
  $out.state = [string]$tm.TetheringOperationalState
  $out.clients = [int]$tm.ClientCount; $out.maxClients = [int]$tm.MaxClientCount
  $out.ok = ($out.status -eq '' -or $out.status -eq 'Success')
} catch { if ($out.err -eq '') { $out.err = $_.Exception.Message } }
$out | ConvertTo-Json -Compress
`

func encodePS(script string) string {
	u := utf16.Encode([]rune(script))
	b := make([]byte, len(u)*2)
	for i, c := range u {
		b[2*i] = byte(c)
		b[2*i+1] = byte(c >> 8)
	}
	return base64.StdEncoding.EncodeToString(b)
}

// hotspotDo: action = status | on | off.
func hotspotDo(action string) HotspotStatus {
	script := strings.ReplaceAll(hotspotPS, "'ACTION'", "'"+action+"'")
	out := runHidden(40*time.Second, "powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-EncodedCommand", encodePS(script))
	st := parseHotspot(out)
	if st.On || action != "status" || st.OK {
		hotspotOn.Store(st.On)
	}
	return st
}

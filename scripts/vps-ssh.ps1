param(
  [Parameter(Mandatory = $true, Position = 0)]
  [ValidateSet('ssh', 'scp')]
  [string]$Client,

  [Parameter(Mandatory = $true, Position = 1, ValueFromRemainingArguments = $true)]
  [string[]]$ClientArguments
)

$ErrorActionPreference = 'Stop'
$target = 'root@beinirest.ru'
$sshOptions = @(
  '-o', 'HostKeyAlias=72.56.232.231',
  '-o', 'BatchMode=yes',
  '-o', 'ConnectTimeout=8',
  '-o', 'ConnectionAttempts=1'
)

& ssh @sshOptions $target 'true' 2>$null
$routeOptions = $sshOptions
if ($LASTEXITCODE -ne 0) {
  $previousPythonPath = $env:PYTHONPATH
  $env:PYTHONPATH = if ($previousPythonPath) { "$PSScriptRoot;$previousPythonPath" } else { $PSScriptRoot }
  $routeOptions = $sshOptions + @('-o', 'ProxyCommand=python -m vps_socks5 %h %p')
  & ssh @routeOptions $target 'true' 2>$null
  if ($LASTEXITCODE -ne 0) {
    Write-Error 'SSH недоступен напрямую и через SOCKS 127.0.0.1:10808. Проверьте сеть, VPN и VPS.'
    exit 1
  }
  Write-Host 'SSH route: domain-based SOCKS 127.0.0.1:10808'
} else {
  Write-Host 'SSH route: direct'
}

if ($Client -eq 'scp') {
  & scp @routeOptions @ClientArguments
} else {
  & ssh @routeOptions @ClientArguments
}
exit $LASTEXITCODE

# Same runner as Linux and CI. Works from any current directory.
$ErrorActionPreference = 'Stop'
$runner = Join-Path $PSScriptRoot 'check.py'
& python $runner
exit $LASTEXITCODE

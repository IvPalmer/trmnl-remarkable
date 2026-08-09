$ErrorActionPreference = 'Stop'
$command = Get-Command signtool.exe -ErrorAction SilentlyContinue
if ($command) { return $command.Source }
$kits = Join-Path ${env:ProgramFiles(x86)} 'Windows Kits\10\bin'
$signTool = Get-ChildItem -Path (Join-Path $kits '*\x64\signtool.exe') -ErrorAction SilentlyContinue |
    Sort-Object FullName -Descending | Select-Object -First 1 -ExpandProperty FullName
if (-not $signTool) { throw 'signtool.exe was not found in PATH or the Windows 10 SDK.' }
return $signTool

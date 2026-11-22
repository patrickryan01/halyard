Get-Process halyard -ErrorAction SilentlyContinue | Stop-Process -Force
Start-Sleep 2
$p = Start-Process 'C:\Program Files\Halyard\uninstall.exe' -ArgumentList '/S _?=C:\Program Files\Halyard' -Wait -PassThru
"uninstall exit: $($p.ExitCode)"
Remove-Item 'C:\Program Files\Halyard\uninstall.exe' -ErrorAction SilentlyContinue
Remove-Item 'C:\Program Files\Halyard' -ErrorAction SilentlyContinue
"exe present: $(Test-Path 'C:\Program Files\Halyard\halyard.exe')"
"mailto key present: $(Test-Path 'HKLM:\Software\Classes\Halyard.Url.mailto')"
"uninstall entry present: $(Test-Path 'HKLM:\Software\Microsoft\Windows\CurrentVersion\Uninstall\Halyard')"
"start menu present: $(Test-Path 'C:\ProgramData\Microsoft\Windows\Start Menu\Programs\Halyard')"
"user mail kept: $(Test-Path (Join-Path $env:APPDATA 'halyard\mail.db'))"

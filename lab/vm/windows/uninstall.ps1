New-Item -ItemType Directory -Force C:\Users\Public\hp | Out-Null
Invoke-WebRequest -UseBasicParsing 'http://10.0.2.2:8800/files/admin-uninstall.ps1' -OutFile C:\Users\Public\hp\admin-uninstall.ps1
Start-Process powershell -Verb RunAs -Wait -ArgumentList '-NoProfile','-ExecutionPolicy','Bypass','-File','C:\Users\Public\hp\admin-uninstall.ps1'
Get-Content C:\Users\Public\hp\admin-uninstall.out
# the person's mail lives in their own profile, which is not elevated
"user mail kept: $(Test-Path (Join-Path $env:APPDATA 'halyard\mail.db'))"

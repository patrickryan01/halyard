New-Item -ItemType Directory -Force C:\Users\Public\hp | Out-Null
Get-Process halyard -ErrorAction SilentlyContinue | Stop-Process -Force; Start-Sleep 2
# Fetches the installer, the certificate and the admin script, then runs the admin part elevated (the host answers the UAC prompt).
Invoke-WebRequest -UseBasicParsing 'http://10.0.2.2:8800/files/halyard-1.0.0-setup.exe' -OutFile C:\Users\Public\hp\halyard-1.0.0-setup.exe
Invoke-WebRequest -UseBasicParsing 'http://10.0.2.2:8800/files/fireball-codesign.cer' -OutFile C:\Users\Public\hp\fireball-codesign.cer
Invoke-WebRequest -UseBasicParsing 'http://10.0.2.2:8800/files/admin-install.ps1' -OutFile C:\Users\Public\hp\admin-install.ps1
Start-Process powershell -Verb RunAs -Wait -ArgumentList '-NoProfile','-ExecutionPolicy','Bypass','-File','C:\Users\Public\hp\admin-install.ps1'
Get-Content C:\Users\Public\hp\admin-install.out

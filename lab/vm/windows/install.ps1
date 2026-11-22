$ErrorActionPreference = 'Continue'
$f = Get-ChildItem C:\halyard-*-setup.exe -ErrorAction SilentlyContinue | Select-Object -First 1
if (-not $f) { Invoke-WebRequest -UseBasicParsing 'http://10.0.2.2:8800/files/halyard-1.0.0-setup.exe' -OutFile C:\halyard-1.0.0-setup.exe; $f = Get-Item C:\halyard-1.0.0-setup.exe }
"installer: $($f.Name) $($f.Length) bytes"
$sig = Get-AuthenticodeSignature $f.FullName
"signature before trusting the certificate: $($sig.Status), signer $($sig.SignerCertificate.Subject)"
# the house certificate is self-signed: trust it (Trusted Root and Trusted Publishers), as an enrolled machine would
Invoke-WebRequest -UseBasicParsing 'http://10.0.2.2:8800/files/fireball-codesign.cer' -OutFile C:\fireball-codesign.cer
Import-Certificate -FilePath C:\fireball-codesign.cer -CertStoreLocation Cert:\LocalMachine\Root | Out-Null
Import-Certificate -FilePath C:\fireball-codesign.cer -CertStoreLocation Cert:\LocalMachine\TrustedPublisher | Out-Null
$sig = Get-AuthenticodeSignature $f.FullName
"signature after trusting it: $($sig.Status); timestamp: $($sig.TimeStamperCertificate.Subject)"
$p = Start-Process $f.FullName -ArgumentList '/S' -Wait -PassThru
"installer exit: $($p.ExitCode)"
"installed exe: $(Test-Path 'C:\Program Files\Halyard\halyard.exe')"
"webview2 runtime: $((Get-ItemProperty 'HKLM:\SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}' -ErrorAction SilentlyContinue).pv)"
"mailto command: $((Get-ItemProperty 'HKLM:\Software\Classes\Halyard.Url.mailto\shell\open\command' -ErrorAction SilentlyContinue).'(default)')"
"registered application: $((Get-ItemProperty 'HKLM:\Software\RegisteredApplications' -ErrorAction SilentlyContinue).Halyard)"
"start menu entry: $(Test-Path 'C:\ProgramData\Microsoft\Windows\Start Menu\Programs\Halyard\Halyard.lnk')"
"uninstall entry version: $((Get-ItemProperty 'HKLM:\Software\Microsoft\Windows\CurrentVersion\Uninstall\Halyard' -ErrorAction SilentlyContinue).DisplayVersion)"
foreach ($n in 'halyard.exe','halyardd.exe') { $x = Get-AuthenticodeSignature "C:\Program Files\Halyard\$n"; "installed $n signature: $($x.Status)" }

# Elevated part of the install. Output goes to C:\Users\Public\hp\admin-install.out.
$o = 'C:\Users\Public\hp\admin-install.out'; Remove-Item $o -ErrorAction SilentlyContinue
function O($m) { $m | Add-Content $o }
$f = Get-Item C:\Users\Public\hp\halyard-1.0.0-setup.exe
$sig = Get-AuthenticodeSignature $f.FullName
O "installer: $($f.Name) $($f.Length) bytes"
O "signature before trusting the certificate: $($sig.Status), signer $($sig.SignerCertificate.Subject)"
# the house certificate is self-signed: trust it as Trusted Root and Trusted Publisher, as an enrolled machine would
Import-Certificate -FilePath C:\Users\Public\hp\fireball-codesign.cer -CertStoreLocation Cert:\LocalMachine\Root | Out-Null
Import-Certificate -FilePath C:\Users\Public\hp\fireball-codesign.cer -CertStoreLocation Cert:\LocalMachine\TrustedPublisher | Out-Null
$sig = Get-AuthenticodeSignature $f.FullName
O "signature after trusting it: $($sig.Status); timestamp signer: $($sig.TimeStamperCertificate.Subject)"
$p = Start-Process $f.FullName -ArgumentList '/S' -Wait -PassThru
O "installer exit: $($p.ExitCode)"
O "installed exe: $(Test-Path 'C:\Program Files\Halyard\halyard.exe')"
foreach ($n in 'halyard.exe','halyardd.exe') { $x = Get-AuthenticodeSignature "C:\Program Files\Halyard\$n"; O "installed $n signature: $($x.Status)" }
O "webview2 runtime: $((Get-ItemProperty 'HKLM:\SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}' -ErrorAction SilentlyContinue).pv)"
O "mailto command: $((Get-ItemProperty 'HKLM:\Software\Classes\Halyard.Url.mailto\shell\open\command' -ErrorAction SilentlyContinue).'(default)')"
O "registered application: $((Get-ItemProperty 'HKLM:\Software\RegisteredApplications' -ErrorAction SilentlyContinue).Halyard)"
O "start menu entry: $(Test-Path 'C:\ProgramData\Microsoft\Windows\Start Menu\Programs\Halyard\Halyard.lnk')"
O "uninstall entry version: $((Get-ItemProperty 'HKLM:\Software\Microsoft\Windows\CurrentVersion\Uninstall\Halyard' -ErrorAction SilentlyContinue).DisplayVersion)"
O 'done'

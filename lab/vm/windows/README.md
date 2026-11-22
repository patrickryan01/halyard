# Windows proof

Runs a Windows 11 Enterprise evaluation VM harness (`winvm.py`, a guest helper that polls the host on 10.0.2.2:8800
and runs scripts in the signed-in session). The disk is a private copy under `a private path`; the original is never touched.
`proof.sh` drives the steps below; each `*.ps1` runs in the guest, and screenshots are taken from the host with `winvm.py shot`.

1. `install.ps1`   fetches `halyard-<v>-setup.exe`, installs it silently, reports the WebView2 runtime, the mailto handler, the Start menu entry, the uninstall entry
2. `launch.ps1`    starts the app (first run screen)
3. `account.ps1`   adds the lab IMAP account through the app's MCP endpoint, as another program would (the lab is on the host's loopback, 10.0.2.2 in the guest)
4. `settings.ps1 dark|light`   switches the theme through MCP
5. the host sends a new message (`mail.py`); the app raises a desktop notification; screenshot
6. `mailto.ps1`    opens a mailto: link through the registered handler; the composer appears in the running app
7. `uninstall.ps1` silent uninstall; reports what is left (the person's mail stays)

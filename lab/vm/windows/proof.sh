#!/bin/bash
# Host side of the Windows proof. Run on the Beast with the lab up (lab/stalwart.sh up) and the installer at $W/files.
#   proof.sh INSTALLER_EXE OUTDIR
set -euo pipefail
W=${PHONEDESK:-$HOME/work/MAIL/win}; export PHONEDESK=$W
INST=${1:?installer}; OUT=${2:?outdir}; HERE=$(cd "$(dirname "$0")" && pwd); mkdir -p "$OUT"
V="python3 $W/winvm.py"
export WINVM_PORT=8800; ss -ltn | grep -q ":8800 " && { echo "port 8800 is busy: the guest agent in this disk always calls 10.0.2.2:8800, so only one crew can run it at a time"; exit 1; }
$V put "$INST"; $V put "${CER:?CER=path to fireball-codesign.cer}"
($V serve >/dev/null 2>&1 &) ; sleep 1
$V start
for i in $(seq 1 90); do [ "$(curl -s http://127.0.0.1:$WINVM_PORT/admin/seen | cut -d. -f1)" -gt $(( $(date +%s) - 30 )) ] 2>/dev/null && break; sleep 5; done
# the installer and uninstaller need admin; the host answers the UAC prompt with alt-y while the job runs
uac() { (for i in $(seq 1 40); do sleep 4; $V key alt-y >/dev/null 2>&1; done) & KP=$!; "$@"; kill $KP 2>/dev/null || true; wait $KP 2>/dev/null || true; }
for f in admin-install.ps1 admin-uninstall.ps1; do cp "$HERE/$f" "$W/files/"; done
echo "== install"; uac $V run "$HERE/install.ps1" 600 | tee "$OUT/windows-install.txt"
echo "== launch"; $V run "$HERE/launch.ps1" 120 | tee -a "$OUT/windows-install.txt"; sleep 5; $V shot "$OUT/win-1-first-run.png"
echo "== account"; $V run "$HERE/account.ps1" 120 | tee -a "$OUT/windows-install.txt"; sleep 40; $V shot "$OUT/win-2-inbox.png"
for t in dark light; do sed "s/@THEME@/$t/" "$HERE/settings.ps1.tmpl" > "$W/settings-$t.ps1"; $V run "$W/settings-$t.ps1" 60 >/dev/null; sleep 5; $V shot "$OUT/win-3-$t-theme.png"; done
echo "== notification"; python3 "$HERE/mail.py"; for n in a b c d e f; do sleep 3; $V shot "$OUT/win-4$n-notification.png"; done
echo "== mailto"; $V run "$HERE/mailto.ps1" 60 | tee -a "$OUT/windows-install.txt"; sleep 4; $V shot "$OUT/win-5-mailto.png"
echo "== uninstall"; uac $V run "$HERE/uninstall.ps1" 120 | tee -a "$OUT/windows-install.txt"; sleep 3; $V shot "$OUT/win-6-uninstalled.png"
$V stop; pkill -f "winvm.py serve" || true
echo "windows proof done: $OUT"

#!/bin/bash
# Second pass of the Windows proof, after the installer fixes: install, launch, account, live theme change, notification center, mailto handler, uninstall.
set -uo pipefail
W=$HOME/work/MAIL/win; export PHONEDESK=$W WINVM_PORT=8800; OUT=$W/out2; HERE=$W/lab/windows; mkdir -p $OUT; cd $W
V="python3 winvm.py"
uac() { (for i in $(seq 1 40); do sleep 4; $V key alt-y >/dev/null 2>&1; done) & KP=$!; "$@"; kill $KP 2>/dev/null || true; wait $KP 2>/dev/null || true; }
echo "== install"; uac $V run $HERE/install.ps1 600 | tee $OUT/windows-install.txt
echo "== launch"; $V run $HERE/launch.ps1 120 | tee -a $OUT/windows-install.txt; sleep 5; $V shot $OUT/win-1-first-run.png
echo "== account"; $V run $HERE/account.ps1 120 | tee -a $OUT/windows-install.txt; sleep 40; $V shot $OUT/win-2-inbox.png
sed "s/@THEME@/dark/" $HERE/settings.ps1.tmpl > $W/settings-dark.ps1; $V run $W/settings-dark.ps1 60 >/dev/null; sleep 4; $V shot $OUT/win-3-dark-theme-live.png
echo "== notification"; python3 $HERE/mail.py; for n in a b c; do sleep 2; $V shot $OUT/win-4$n-notification.png; done; sleep 6; $V key meta_l-n; sleep 3; $V shot $OUT/win-4d-notification-center.png; $V key esc
echo "== mailto"; $V run $HERE/mailto.ps1 60 | tee -a $OUT/windows-install.txt; sleep 3; $V shot $OUT/win-5-mailto.png
echo "== uninstall"; uac $V run $HERE/uninstall.ps1 120 | tee -a $OUT/windows-install.txt; sleep 3; $V shot $OUT/win-6-uninstalled.png
echo "steps done"

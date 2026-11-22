#!/bin/bash
# Third pass: the notification, with the log of what the app did.
set -uo pipefail
W=$HOME/work/MAIL/win; export PHONEDESK=$W WINVM_PORT=8800; OUT=$W/out3; HERE=$W/lab/windows; mkdir -p $OUT; cd $W
V="python3 winvm.py"
uac() { (for i in $(seq 1 40); do sleep 4; $V key alt-y >/dev/null 2>&1; done) & KP=$!; "$@"; kill $KP 2>/dev/null || true; wait $KP 2>/dev/null || true; }
echo "== install"; uac $V run $HERE/install.ps1 600 | tee $OUT/windows-install.txt | tail -4
echo "== launch"; $V run $HERE/launch.ps1 120; sleep 20; $V shot $OUT/win-1-reopened.png
echo "== notification"; python3 $HERE/mail.py; for n in a b c d e f; do sleep 2; $V shot $OUT/win-4$n-notification.png; done
sleep 4; $V key meta_l-n; sleep 3; $V shot $OUT/win-4g-notification-center.png; $V key esc
echo "== app log"; $V run $HERE/showlog.ps1 60 | tee $OUT/halyard-log.txt
echo "steps done"

#!/bin/sh
# Runs every check behind the maintainer feature list on a box with Go, Node, Playwright's Chromium and Podman (the Beast), then
# has tools/completeness.py read the results. The Microsoft sign in needs the test user's password on standard input.
# Usage: PLAYWRIGHT=... CHROME=... tools/completeness.sh OUTDIR [PKGDIR] < password
set -u
OUT=${1:?output directory}; PKG=${2:-}
mkdir -p "$OUT"; cd "$(dirname "$0")/.."
PW=$(cat); export HALYARD_NO_KEYRING=1
go build -tags demo -o "$OUT/halyardd-demo" ./cmd/halyardd && go build -o "$OUT/halyardd" ./cmd/halyardd || exit 1
lab/stalwart.sh up > "$OUT/lab.up" 2>&1 || { cat "$OUT/lab.up"; exit 1; }
go test ./... -count=1 -v > "$OUT/go.log" 2>&1
MAIL_LAB_HOST=127.0.0.1 go test ./internal/e2e -run 'TestFeat|TestIMAPLab|TestJMAPLab|TestMCPIMAPLab|TestMCPJMAPLab' -count=1 -p 1 -v > "$OUT/lab.log" 2>&1
node ui/test/smoke.mjs "$OUT/halyardd-demo" > "$OUT/smoke.log" 2>&1
node ui/test/features.mjs "$OUT/halyardd" > "$OUT/ui.log" 2>&1
printf %s "$PW" | HALYARD_NO_BROWSER=1 node ui/test/live_add.mjs "$OUT/halyardd" > "$OUT/live.log" 2>&1
node ui/test/welcome_keep.mjs "$OUT/halyardd" > "$OUT/welcome.log" 2>&1
node ui/test/multi_sync.mjs "$OUT/halyardd" > "$OUT/multi-sync.log" 2>&1
node tools/i18n-check.mjs "$OUT/halyardd-demo" > "$OUT/i18n.log" 2>&1
lab/stalwart.sh down >/dev/null 2>&1
python3 tools/completeness.py --doc "${COMPLETENESS_DOC:?path of the completeness list}" --go "$OUT/go.log" --lab "$OUT/lab.log" --ui "$OUT/ui.log" --live "$OUT/live.log" --smoke "$OUT/smoke.log" --i18n "$OUT/i18n.log" ${PKG:+--pkgdir "$PKG"} | tee "$OUT/completeness.txt"
for f in welcome:"welcome keep ok" multi-sync:"multi sync ok"; do n=${f%%:*}; m=${f#*:}; grep -q "$m" "$OUT/$n.log" && echo "$n: ok" || { echo "$n: NOT OK (see $OUT/$n.log)"; echo NOT COMPLETE; exit 1; }; done | tee -a "$OUT/completeness.txt"

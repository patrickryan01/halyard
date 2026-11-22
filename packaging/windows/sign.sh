#!/bin/sh
# Authenticode-signs a Windows binary or installer with the house code-signing certificate and timestamps it.
#   packaging/windows/sign.sh FILE
# The certificate comes from SIGN_CERT_PFX (a path) or SIGN_CERT_PFX_BASE64 (the file, base64), with SIGN_CERT_PASSWORD.
# Needs osslsigncode. Exits 3 when there is no certificate, so a caller can refuse an unsigned release.
set -eu
F=${1:?file}
PFX=${SIGN_CERT_PFX:-}
if [ -z "$PFX" ] && [ -n "${SIGN_CERT_PFX_BASE64:-}" ]; then
  PFX=$(mktemp); trap 'rm -f "$PFX"' EXIT
  printf '%s' "$SIGN_CERT_PFX_BASE64" | base64 -d > "$PFX"
fi
[ -n "$PFX" ] || { echo "no signing certificate (SIGN_CERT_PFX or SIGN_CERT_PFX_BASE64)" >&2; exit 3; }
TS=${SIGN_TIMESTAMP_URL:-http://timestamp.digicert.com}
osslsigncode sign -pkcs12 "$PFX" -pass "${SIGN_CERT_PASSWORD:-}" -n "Halyard" -i https://fireballz.ai -h sha256 -ts "$TS" -in "$F" -out "$F.signed"
mv "$F.signed" "$F"
echo "signed $F"

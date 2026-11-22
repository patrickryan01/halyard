#!/bin/sh
# Verifies the Authenticode signature of a Windows file against the trusted signing certificate, and fails if it is not signed.
#   packaging/windows/verify-signed.sh FILE [TRUST_PEM]
# TRUST_PEM defaults to packaging/windows/trust/halyard-code-signing.pem: the public part of the house certificate (a self-signed
# certificate is its own root). When the certificate changes, that file changes with it.
set -eu
F=${1:?file}
T=${2:-${SIGN_TRUST_PEM:-}}
[ -n "$T" ] || T=$(dirname "$0")/trust/halyard-code-signing.pem
[ -f "$T" ] || { echo "no trust file at $T" >&2; exit 4; }
OUT=$(osslsigncode verify -in "$F" -CAfile "$T" 2>&1 || true)
echo "$OUT" | tail -12
echo "$OUT" | grep -q "Signature verification: ok" || { echo "NOT SIGNED OR NOT TRUSTED: $F" >&2; exit 1; }
echo "signature ok: $F"

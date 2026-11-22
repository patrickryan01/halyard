#!/bin/sh
# Podman lab: one Stalwart server (IMAP with IDLE, SMTP submission, JMAP) with two users,
# alice@lab.test and bob@lab.test, passwords labpass-alice and labpass-bob. Self-signed TLS.
# Ports on the host's loopback: 18993 imaps, 18465 smtps, 18587 submission, 18443 https/jmap, 18080 http.
# Usage: lab/stalwart.sh up | down
set -eu
NAME=mail-lab-stalwart
IMG=docker.io/stalwartlabs/stalwart:v1
ADMIN=admin:labpass123
J() { curl -sk -u "$ADMIN" -H 'Content-Type: application/json' -d "$1" "$2"; }
U='"using":["urn:ietf:params:jmap:core","urn:stalwart:jmap"]'
case "${1:-up}" in
down) podman rm -f "$NAME" >/dev/null 2>&1 || true; podman volume rm -f mailab-etc mailab-data >/dev/null 2>&1 || true ;;
up)
  podman rm -f "$NAME" >/dev/null 2>&1 || true; podman volume rm -f mailab-etc mailab-data >/dev/null 2>&1 || true
  podman run -d --name "$NAME" -e STALWART_RECOVERY_ADMIN="$ADMIN" -p 127.0.0.1:18080:8080 -p 127.0.0.1:18993:993 -p 127.0.0.1:18465:465 \
    -p 127.0.0.1:18587:587 -p 127.0.0.1:18443:443 -v mailab-etc:/etc/stalwart -v mailab-data:/var/lib/stalwart "$IMG" >/dev/null
  i=0; until curl -s -o /dev/null -u "$ADMIN" http://127.0.0.1:18080/jmap/session; do i=$((i+1)); [ $i -gt 30 ] && { echo "stalwart did not start"; exit 1; }; sleep 1; done
  J "{$U,\"methodCalls\":[[\"x:Bootstrap/set\",{\"accountId\":\"d333333\",\"update\":{\"singleton\":{\"defaultHostname\":\"mail.lab.test\",\"defaultDomain\":\"lab.test\",\"requestTlsCertificate\":false,\"generat\\u0065DkimKeys\":false,\"tracer\":{\"@type\":\"Stdout\",\"ansi\":false,\"multiline\":false,\"enable\":true,\"level\":\"info\",\"lossy\":false,\"events\":{},\"eventsPolicy\":\"exclude\"}}}},\"s\"]]}" http://127.0.0.1:18080/jmap/ >/dev/null
  podman restart "$NAME" >/dev/null
  i=0; until curl -sk -o /dev/null -u "$ADMIN" https://127.0.0.1:18443/jmap/session; do i=$((i+1)); [ $i -gt 40 ] && { echo "stalwart did not restart"; exit 1; }; sleep 1; done
  for n in alice bob; do
    J "{$U,\"methodCalls\":[[\"x:Account/set\",{\"accountId\":\"d333333\",\"create\":{\"u\":{\"@type\":\"User\",\"name\":\"$n\",\"domainId\":\"b\",\"credentials\":{\"0\":{\"@type\":\"Password\",\"secret\":\"labpass-$n\"}}}}},\"c\"]]}" https://127.0.0.1:18443/jmap/ | grep -q created || { echo "could not create $n"; exit 1; }
  done
  echo "lab ready" ;;
esac

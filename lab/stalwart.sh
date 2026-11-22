#!/bin/sh
# Podman lab: one Stalwart server (IMAP with IDLE, SMTP submission, JMAP) with three users,
# alice@lab.test and bob@lab.test (passwords labpass-alice, labpass-bob) and gmailstyle@lab.test (a 16 letter password, like a Google app password). Self-signed TLS.
# Ports on the host's loopback: 18993 imaps, 18465 smtps, 18587 submission, 18995 pop3s, 14190 managesieve, 18443 https/jmap, 18080 http.
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
    -p 127.0.0.1:18587:587 -p 127.0.0.1:18443:443 -p 127.0.0.1:18995:995 -p 127.0.0.1:14190:4190 -v mailab-etc:/etc/stalwart -v mailab-data:/var/lib/stalwart "$IMG" >/dev/null
  i=0; until curl -s -o /dev/null -u "$ADMIN" http://127.0.0.1:18080/jmap/session; do i=$((i+1)); [ $i -gt 30 ] && { echo "stalwart did not start"; exit 1; }; sleep 1; done
  J "{$U,\"methodCalls\":[[\"x:Bootstrap/set\",{\"accountId\":\"d333333\",\"update\":{\"singleton\":{\"defaultHostname\":\"mail.lab.test\",\"defaultDomain\":\"lab.test\",\"requestTlsCertificate\":false,\"generat\\u0065DkimKeys\":false,\"tracer\":{\"@type\":\"Stdout\",\"ansi\":false,\"multiline\":false,\"enable\":true,\"level\":\"info\",\"lossy\":false,\"events\":{},\"eventsPolicy\":\"exclude\"}}}},\"s\"]]}" http://127.0.0.1:18080/jmap/ >/dev/null
  podman restart "$NAME" >/dev/null
  i=0; until curl -sk -o /dev/null -u "$ADMIN" https://127.0.0.1:18443/jmap/session; do i=$((i+1)); [ $i -gt 40 ] && { echo "stalwart did not restart"; exit 1; }; sleep 1; done
  for np in alice:labpass-alice bob:labpass-bob gmailstyle:qwfpgjluyarstdhn; do
    n=${np%%:*}; pw=${np#*:}
    J "{$U,\"methodCalls\":[[\"x:Account/set\",{\"accountId\":\"d333333\",\"create\":{\"u\":{\"@type\":\"User\",\"name\":\"$n\",\"domainId\":\"b\",\"credentials\":{\"0\":{\"@type\":\"Password\",\"secret\":\"$pw\"}}}}},\"c\"]]}" https://127.0.0.1:18443/jmap/ | grep -q created || { echo "could not create $n"; exit 1; }
  done
  # Stalwart defers (SMTP 452) a client that passes an inbound throttle, and ships two: five connections a second per
  # client address, and 25 messages an hour per sender domain and recipient (https://stalw.art/docs/mta/inbound/rate-limit).
  # A test lab sends far more than that to a handful of people, so every MtaInboundThrottle is destroyed, and the script
  # fails unless the list is empty afterwards.
  for try in 1 2 3; do
    ids=$(J "{$U,\"methodCalls\":[[\"x:MtaInboundThrottle/get\",{\"accountId\":\"d333333\",\"ids\":null},\"a\"]]}" https://127.0.0.1:18443/jmap/ | grep -o '"id":"[a-z0-9]*"' | sed 's/"id":"\(.*\)"/"\1"/' | paste -sd, -)
    [ -z "$ids" ] && break
    J "{$U,\"methodCalls\":[[\"x:MtaInboundThrottle/set\",{\"accountId\":\"d333333\",\"destroy\":[$ids]},\"d\"]]}" https://127.0.0.1:18443/jmap/ >/dev/null
    sleep 1
  done
  [ -z "$ids" ] || { echo "could not remove the inbound throttles: $ids"; exit 1; }
  # the running server keeps the throttles it loaded until its settings are reloaded (its log shows the destroyed
  # MtaInboundThrottle still firing), and a restart brings the defaults back, so ask it to reload
  J "{$U,\"methodCalls\":[[\"x:Action/set\",{\"accountId\":\"d333333\",\"create\":{\"a\":{\"@type\":\"ReloadSettings\"}}},\"r\"]]}" https://127.0.0.1:18443/jmap/ | grep -q '"created"' || { echo "could not reload the settings"; exit 1; }
  echo "lab ready" ;;
esac

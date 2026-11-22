#!/bin/sh
# Prints the -ldflags for a release build. The sign-in client registrations are not in the source:
# they come from the environment of the machine that builds the release.
#   HALYARD_VERSION, HALYARD_GOOGLE_CLIENT_ID, HALYARD_GOOGLE_CLIENT_SECRET, HALYARD_GOOGLE_PORTS, HALYARD_MS_CLIENT_ID
M=github.com/patrickryan01/halyard
# The Microsoft client is the "Halyard" app registration in the Fireball tenant (secrets/access/ms-halyard-app.sh). A client ID is public.
HALYARD_MS_CLIENT_ID=${HALYARD_MS_CLIENT_ID:-5bc58f81-5155-4a3e-aedc-d6692de040c7}
F="-s -w -X $M/internal/brand.Version=${HALYARD_VERSION:-dev}"
[ -n "${HALYARD_GOOGLE_CLIENT_ID:-}" ] && F="$F -X $M/internal/providers/google.ClientID=$HALYARD_GOOGLE_CLIENT_ID"
[ -n "${HALYARD_GOOGLE_CLIENT_SECRET:-}" ] && F="$F -X $M/internal/providers/google.ClientSecret=$HALYARD_GOOGLE_CLIENT_SECRET"
[ -n "${HALYARD_GOOGLE_PORTS:-}" ] && F="$F -X $M/internal/providers/google.Ports=$HALYARD_GOOGLE_PORTS"
[ -n "${HALYARD_MS_CLIENT_ID:-}" ] && F="$F -X $M/internal/providers/microsoft.ClientID=$HALYARD_MS_CLIENT_ID"
echo "$F"

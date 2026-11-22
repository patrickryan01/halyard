#!/bin/sh
# Prints the -ldflags for a release build. The sign-in client registrations are not in the source:
# they come from the environment of the machine that builds the release.
#   HALYARD_VERSION, HALYARD_GOOGLE_CLIENT_ID, HALYARD_GOOGLE_CLIENT_SECRET, HALYARD_MS_CLIENT_ID
M=github.com/patrickryan01/halyard
F="-s -w -X $M/internal/brand.Version=${HALYARD_VERSION:-dev}"
[ -n "${HALYARD_GOOGLE_CLIENT_ID:-}" ] && F="$F -X $M/internal/providers/google.ClientID=$HALYARD_GOOGLE_CLIENT_ID"
[ -n "${HALYARD_GOOGLE_CLIENT_SECRET:-}" ] && F="$F -X $M/internal/providers/google.ClientSecret=$HALYARD_GOOGLE_CLIENT_SECRET"
[ -n "${HALYARD_MS_CLIENT_ID:-}" ] && F="$F -X $M/internal/providers/microsoft.ClientID=$HALYARD_MS_CLIENT_ID"
echo "$F"

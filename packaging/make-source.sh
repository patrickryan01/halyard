#!/bin/sh
# Writes halyard-VERSION.tar.gz (the tree at HEAD, with vendored modules not needed) into DIR.
# Usage: packaging/make-source.sh VERSION DIR
set -eu
V=${1:?version}; D=${2:?dir}
mkdir -p "$D"
git archive --format=tar.gz --prefix="halyard-$V/" -o "$D/halyard-$V.tar.gz" HEAD
echo "$D/halyard-$V.tar.gz"

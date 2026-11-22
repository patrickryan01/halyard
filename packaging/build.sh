#!/bin/sh
# Builds a package in a container, on the machine that has Podman (the Beast), one at a time.
#   packaging/build.sh VERSION arch|rpm|windows OUTDIR
# Run from the repo root. The version tarball is made from HEAD. Sign-in client IDs come from the
# environment (see packaging/common/build-flags.sh).
set -eu
V=${1:?version}; T=${2:?target}; OUT=${3:?outdir}
mkdir -p "$OUT/src" "$OUT/pkg"
[ -f "$OUT/src/halyard-$V.tar.gz" ] || packaging/make-source.sh "$V" "$OUT/src" >/dev/null
ENVS="-e HALYARD_VERSION=$V -e HALYARD_GOOGLE_CLIENT_ID=${HALYARD_GOOGLE_CLIENT_ID:-} -e HALYARD_GOOGLE_CLIENT_SECRET=${HALYARD_GOOGLE_CLIENT_SECRET:-} -e HALYARD_MS_CLIENT_ID=${HALYARD_MS_CLIENT_ID:-}"
case "$T" in
arch)
  podman run --rm $ENVS -v "$OUT:/out:Z" docker.io/library/archlinux:latest sh -ec '
    pacman -Syu --noconfirm --needed base-devel go gtk3 webkit2gtk-4.1 libsecret xdg-utils git >/dev/null
    useradd -m builder; mkdir -p /home/builder/b
    tar -xzf /out/src/halyard-'"$V"'.tar.gz -C /home/builder/b; cp /out/src/halyard-'"$V"'.tar.gz /home/builder/b/
    cp /home/builder/b/halyard-'"$V"'/packaging/arch/PKGBUILD /home/builder/b/
    chown -R builder /home/builder/b
    su builder -c "cd /home/builder/b && HALYARD_VERSION='"$V"' makepkg --noconfirm --nocheck -f"
    cp /home/builder/b/*.pkg.tar.zst /out/pkg/' ;;
rpm)
  podman run --rm $ENVS -v "$OUT:/out:Z" registry.opensuse.org/opensuse/tumbleweed:latest sh -ec '
    zypper -n install --no-recommends rpm-build go gcc pkgconf-pkg-config "pkgconfig(gtk+-3.0)" "pkgconfig(webkit2gtk-4.1)" tar gzip >/dev/null
    mkdir -p /root/rpmbuild/SOURCES /root/rpmbuild/SPECS
    cp /out/src/halyard-'"$V"'.tar.gz /root/rpmbuild/SOURCES/
    tar -xzOf /out/src/halyard-'"$V"'.tar.gz halyard-'"$V"'/packaging/rpm/halyard.spec > /root/rpmbuild/SPECS/halyard.spec
    rpmbuild -bb --define "_topdir /root/rpmbuild" --define "hver '"$V"'" /root/rpmbuild/SPECS/halyard.spec
    cp /root/rpmbuild/RPMS/*/halyard-*.rpm /out/pkg/' ;;
windows)
  podman run --rm $ENVS -v "$OUT:/out:Z" docker.io/library/golang:1.27-bookworm sh -ec '
    apt-get update -qq >/dev/null && apt-get install -y -qq nsis >/dev/null
    mkdir -p /src && tar -xzf /out/src/halyard-'"$V"'.tar.gz -C /src && cd /src/halyard-'"$V"'
    LDF=$(sh packaging/common/build-flags.sh)
    mkdir -p /bin-win
    GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -tags "desktop,production" -ldflags "$LDF -H windowsgui" -o /bin-win/halyard.exe ./cmd/halyard
    GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "$LDF" -o /bin-win/halyardd.exe ./cmd/halyardd
    cd packaging/windows && makensis -DVERSION='"$V"' -DBINDIR=/bin-win halyard.nsi
    cp halyard-'"$V"'-setup.exe /out/pkg/' ;;
*) echo "unknown target $T"; exit 2 ;;
esac
ls -la "$OUT/pkg"

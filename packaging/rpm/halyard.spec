# Halyard for EmberRTOS Desktop (openSUSE Tumbleweed). Source0 is written by packaging/make-source.sh.
%global debug_package %{nil}
Name:           halyard
Version:        %{?hver}%{!?hver:1.0.0}
Release:        0
Summary:        Modular mail client for Google, Microsoft 365 and self-hosted mail
License:        SUSE-NonFree
Group:          Productivity/Networking/Email/Clients
URL:            https://github.com/patrickryan01/halyard
Source0:        halyard-%{version}.tar.gz
BuildRequires:  go
BuildRequires:  gcc
BuildRequires:  pkgconfig
BuildRequires:  pkgconfig(gtk+-3.0)
BuildRequires:  pkgconfig(webkit2gtk-4.1)
Requires:       libwebkit2gtk-4_1-0
Requires:       libgtk-3-0
Requires:       xdg-utils

%description
Halyard is a mail client written in Go with a web interface in a native window. Google, Microsoft 365
and self-hosted (IMAP, SMTP and JMAP) accounts sit behind one plugin interface. It has conversation
threads, full text search, labels and folders, snooze, undo send, a rich composer, a unified inbox,
an offline cache, and a read and write MCP server on this computer for other programs.

%prep
%setup -q

%build
export CGO_ENABLED=1
LDF=$(HALYARD_VERSION=%{version} sh packaging/common/build-flags.sh)
go build -trimpath -tags "desktop,production,webkit2_41" -ldflags "$LDF" -o halyard ./cmd/halyard
CGO_ENABLED=0 go build -trimpath -ldflags "$LDF" -o halyardd ./cmd/halyardd
CGO_ENABLED=0 go build -trimpath -ldflags "$LDF" -o halyardctl ./cmd/halyardctl

%check
go test ./internal/store ./internal/mime ./internal/discover ./internal/ical ./internal/rules ./internal/secure ./internal/sieve ./internal/dav ./internal/cli

%install
install -Dm755 halyard %{buildroot}%{_bindir}/halyard
install -Dm755 halyardd %{buildroot}%{_bindir}/halyardd
%{_bindir}/halyardctl
install -Dm755 halyardctl %{buildroot}%{_bindir}/halyardctl
install -Dm644 packaging/common/halyard.desktop %{buildroot}%{_datadir}/applications/halyard.desktop
install -Dm644 packaging/common/halyard.svg %{buildroot}%{_datadir}/icons/hicolor/scalable/apps/halyard.svg
install -Dm644 packaging/common/halyard-512.png %{buildroot}%{_datadir}/icons/hicolor/512x512/apps/halyard.png
install -Dm644 packaging/common/halyard-256.png %{buildroot}%{_datadir}/icons/hicolor/256x256/apps/halyard.png

%files
%license LICENSE
%{_bindir}/halyard
%{_bindir}/halyardd
%{_datadir}/applications/halyard.desktop
%{_datadir}/icons/hicolor/*/apps/halyard.*

%changelog

# Release checklist: Halyard

Halyard is one desktop application, built three ways (Arch, EmberRTOS Desktop which is openSUSE Tumbleweed based, and Windows), released once and whole. No feature ships behind a flag, and nothing is left for a later release.

## 1. Before the tag

1. `main` is the commit you mean to release and the tree is clean.
2. The version is the same in `release-notes/<version>.md`, the package files (`packaging/arch/PKGBUILD` takes `HALYARD_VERSION`, `packaging/rpm/halyard.spec` takes `hver`, `packaging/windows/halyard.nsi` takes `-DVERSION`), and the build command. `internal/brand.Version` is set by `-ldflags`, never edited.
3. `release-notes/<version>.md` has a `## Not proven` section listing everything the proof below did not cover.
4. Finished features ship on: read `internal/app/app.go` and the settings defaults in `internal/api/api.go` for any switch that is off.
5. The sign-in client registrations (`HALYARD_GOOGLE_CLIENT_ID`, `HALYARD_GOOGLE_CLIENT_SECRET`, `HALYARD_MS_CLIENT_ID`) are set for the build when they exist. Without a Google client the add account screen walks through an app password, which works with nothing registered.
6. `docs/COMPLETENESS.md` is complete: every row that is not dropped has a passing check.

## 2. Gates on the code

1. `go vet ./... && go test ./...` pass.
2. The lab: `lab/stalwart.sh up`, then `MAIL_LAB_HOST=127.0.0.1 go test ./internal/e2e -run 'TestFeat|TestIMAPLab|TestJMAPLab|TestMCPIMAPLab|TestMCPJMAPLab' -p 1` pass.
3. Live providers pass with a real mailbox each, when the credentials are at hand: `TestGoogleLive`, `TestMCPGoogleLive` and `TestGmailOverIMAPLive` with `HALYARD_LIVE_GOOGLE_TOKEN` and `HALYARD_LIVE_GOOGLE_USER`, and `TestMCPMicrosoftLive` with `HALYARD_LIVE_MS_TOKEN` and `HALYARD_LIVE_MS_USER`. A run that cannot do this says so under `## Not proven`.
4. The browser tests pass: `node ui/test/smoke.mjs <demo build of halyardd>` (`go build -tags demo ./cmd/halyardd`), `node ui/test/features.mjs <halyardd>`, `node ui/test/live_add.mjs <halyardd>` and `node tools/i18n-check.mjs <demo build>`.
5. `tools/completeness.sh <outdir>` runs all of the above and the list check, and prints COMPLETE.

## 3. Build the three packages (`packaging/build.sh <version> arch|rpm|windows <dir>`, one at a time)

| Target | Build | Gate |
|---|---|---|
| Arch | `makepkg` in an `archlinux` container from the `git archive` tarball | `halyard-<v>-1-x86_64.pkg.tar.zst` exists; `pacman -Qip` shows version and deps; the file list has `usr/bin/halyard`, `usr/bin/halyardd`, `usr/bin/halyardctl`, the desktop file and icons |
| EmberRTOS Desktop (openSUSE Tumbleweed) | `rpmbuild` in a `tumbleweed` container | `halyard-<v>-0.x86_64.rpm` exists; `rpm -qpl` lists the same files; `rpm -qpR` shows the library deps |
| Windows | `GOOS=windows` Go build, `makensis`, then `packaging/windows/sign.sh` on `halyard.exe`, `halyardd.exe`, `halyardctl.exe` and the installer with the release signing certificate (`SIGN_CERT_PFX_BASE64`, `SIGN_CERT_PASSWORD`) | `halyard-<v>-setup.exe` exists; `packaging/windows/verify-signed.sh` prints `signature ok` for every file against `packaging/windows/trust/halyard-code-signing.pem`; the build and the tag workflow both refuse to produce an unsigned release (`HALYARD_REQUIRE_SIGNING=1`) |

## 4. Proof in a VM (one VM at a time)

Install the package in a fresh VM for each platform and prove, with a screenshot of each:

1. Arch: install the `.pkg.tar.zst`, launch `halyard` from the app menu, the first run screen appears, an IMAP account is added against the lab and mail loads.
2. EmberRTOS Desktop (Tumbleweed): the same with the `.rpm`.
3. Windows 11: run the installer, the app starts from the Start menu, the first run screen appears, an IMAP account is added and mail loads; `mailto:` is registered under Default apps; uninstall removes the program and leaves the person's mail alone.

## 5. Release notes say what was not proven

Anything above that did not run is listed under `## Not proven` in `release-notes/<version>.md`, such as a sign in that waits on a client registration, or a screenshot nobody reviewed.

## 6. Publish

Only the maintainer pushes the version tag. The tag workflow builds the packages and refuses an unsigned Windows installer. After it finishes, install each package once from the release page and read the logs.

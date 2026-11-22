# Release checklist: Halyard

Written 2026-11-22 from the closest repos of the same kind: `Embernet-ai/emberarch/RELEASE_CHECKLIST.md` (a Go tooling repo that ships Arch packages and is proven in VMs on The Beast) and `Embernet-ai/hearth/RELEASE_CHECKLIST.md` (a Go product with live provider proof). Halyard is one desktop application, built three ways, released once and whole.

Rules for every step: builds, the lab and the VMs run on The Beast, one heavy job at a time, with the lead's go. No Docker, Compose or LXC: Podman only. A check that is not on this list is a proposal to Patrick, not a private test. Only the lead pushes tags; an agent reports "ready to tag" with the sha and the proof below. One release carries everything: no feature behind a flag, no "later". A later bug goes in the next patch, never a re-cut tag.

## 1. Before the tag

1. `main` is the commit you mean to release and the tree is clean.
2. The version is the same in `RELEASE-NOTES` (`release-notes/<version>.md`), the package files (`packaging/arch/PKGBUILD` takes `HALYARD_VERSION`, `packaging/rpm/halyard.spec` takes `hver`, `packaging/windows/halyard.nsi` takes `-DVERSION`), and the build command. `internal/brand.Version` is set by `-ldflags`, never edited.
3. `release-notes/<version>.md` has a `## Not proven` section listing everything the proof below did not cover.
4. Finished features ship on: read `internal/app/app.go` and the settings defaults in `internal/api/api.go` for any switch that is off, and turn each finished one on. The MCP server is on by default.
5. The sign-in client registrations (`HALYARD_GOOGLE_CLIENT_ID`, `HALYARD_GOOGLE_CLIENT_SECRET`, `HALYARD_MS_CLIENT_ID`) are set for the build. Without them the buttons say the build has none registered; that is a release blocker, not a note.

## 2. Gates on the code (on The Beast)

1. `go vet ./... && go test ./...` pass.
2. The lab: `lab/stalwart.sh up`, then `MAIL_LAB_HOST=127.0.0.1 ./e2e.test -test.run Lab` passes IMAP, JMAP, and the MCP runs for both.
3. Live providers pass with a real mailbox each: `TestGoogleLive` and `TestMCPGoogleLive` with `HEARTH_LIVE_GOOGLE_TOKEN` (`secrets/access/google-dwd-mint.py`, account@fireballz.ai), and `TestMCPMicrosoftLive` with `HEARTH_LIVE_MS_TOKEN` (`secrets/access/ms-liveproof-token.py delegated`).
4. The UI smoke test passes: `CHROME=... node ui/test/smoke.mjs <demo build of halyardd>`.

## 3. Build the three packages (`packaging/build.sh <version> arch|rpm|windows <dir>`, one at a time)

| Target | Build | Gate |
|---|---|---|
| Arch | `makepkg` in an `archlinux` container from the `git archive` tarball | `halyard-<v>-1-x86_64.pkg.tar.zst` exists; `pacman -Qip` shows version and deps; the file list has `usr/bin/halyard`, `usr/bin/halyardd`, the desktop file and icons |
| EmberRTOS Desktop (openSUSE Tumbleweed) | `rpmbuild` in a `tumbleweed` container | `halyard-<v>-0.x86_64.rpm` exists; `rpm -qpl` lists the same files; `rpm -qpR` shows the library deps |
| Windows | `GOOS=windows` Go build plus `makensis` | `halyard-<v>-setup.exe` exists and is a PE file; **it is unsigned** unless `HALYARD_SIGN_PFX` was set, and the release notes say which |

## 4. Proof in a VM on The Beast (one VM at a time, capped)

Install the package in a fresh VM for each platform and prove, with a screenshot of each:

1. EmberArch (Arch): install the `.pkg.tar.zst`, launch `halyard` from the app menu, the first-run screen appears, an IMAP account is added against the lab (served to the VM) and mail loads.
2. EmberRTOS Desktop (Tumbleweed): the same with the `.rpm`.
3. Windows 11: run the installer, the app starts from the Start menu, the first-run screen appears, an IMAP account is added and mail loads; `mailto:` is registered under Default apps; uninstall removes the program and leaves the person's mail alone.

## 5. Release notes say what was not proven

Anything above that did not run is listed under `## Not proven` in `release-notes/<version>.md`: the Windows code signature if unsigned, a real Microsoft Entra application registration if the build used a test one, and any provider behavior only a mailbox of that kind can show.

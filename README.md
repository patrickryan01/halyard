# Halyard

A mail client written in Go, for Linux and Windows. It is as easy to use as Gmail on the web, and it works with Google, Microsoft, and any server you run yourself.

## What it does

* **Accounts:** type an email address and the provider is found for you. Gmail and Google Workspace (Gmail API, or IMAP with an app password), Microsoft 365 and Outlook.com (Graph, one click sign in), IMAP and SMTP, POP3, and JMAP. Several accounts at once, one combined inbox.
* **Reading:** conversations, full text search with operators (`from:` `to:` `subject:` `filename:` `label:` `is:` `has:` `before:` `after:` `larger:` `smaller:` and more), saved searches, snooze, labels and folders, calendar invitations you can answer, unsubscribe from mailing lists, read receipts, print, view source, save as .eml or mbox.
* **Writing:** rich text, attachments by drop and paste, drafts kept on the server, undo send, schedule for later, templates, several identities and signatures, spell check, sign and encrypt with OpenPGP or S/MIME.
* **Organizing:** filters that run on new mail, server side Sieve filters (ManageSieve), vacation replies kept on the server, folders you can rename and delete, mark all read, empty trash.
* **Contacts and calendar:** an address book and calendars from any CardDAV and CalDAV server.
* **Moving in:** import mbox files and Thunderbird profiles.
* **Everywhere:** a command palette and keyboard shortcuts you can change, light and dark themes, an icon in the notification area, desktop notifications, an offline cache that replays your changes, English and thirteen other languages.
* **Automation:** the `halyard` command line (send, search, list, read and the rest) and a built in MCP server that reads and writes every account. See `docs/MCP.md`.

`docs/COMPLETENESS.md` lists every feature a full mail client has, with the automated check behind each one.

## Build and test

    go build ./... && go test ./...
    lab/stalwart.sh up          # a local mail server for the live tests; needs Podman
    MAIL_LAB_HOST=127.0.0.1 go test ./internal/e2e -run 'Lab|TestFeat'
    node ui/test/smoke.mjs <demo build of halyardd>    # go build -tags demo ./cmd/halyardd

`tools/completeness.sh` runs all of it and checks the list in `docs/COMPLETENESS.md`.

## Layout

`cmd/halyard` the window and the command line, `cmd/halyardd` the headless daemon, `cmd/halyardctl` the command line alone, `internal/` the core (`plugin` and `providers` for each kind of account, `store`, `engine`, `api`, `mcpserver`, `secure`, `sieve`, `dav`, `ical`, `rules`, `oauth`, `discover`), `ui/web` the front end and its translations, `packaging` the three builds, `lab` the local mail server, `design` the art.

## How the window is built

The window is Wails v2: one library gives a window, a single instance lock and the platform webview (WebKitGTK on Linux, WebView2 on Windows). The front end reaches the Go core through the asset handler inside the same process, so no network port needs protecting. The same front end runs in a plain browser against `cmd/halyardd`, which is how it is tested.

## Release

Builds: `packaging/build.sh <version> arch|rpm|windows <dir>`. See `RELEASE_CHECKLIST.md`.

## License

See `LICENSE`.

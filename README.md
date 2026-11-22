# Halyard

A mail client written in Go. The core (accounts, sync engine, local store with full text search, the plugin interface) is Go; the interface is a web front end in a native window.

* **Providers are plugins** behind one Go interface (`internal/plugin`):
  * Google: the Gmail API with OAuth, labels, threads and history sync, People for contacts.
  * Microsoft 365 and Outlook.com: Graph with OAuth, folders and delta sync, contacts.
  * Self-hosted: IMAP with IDLE and SMTP, and JMAP where the server has it (Stalwart, Cyrus, Fastmail).
* **Easy like Gmail:** conversation threads, fast search with operators, labels or folders, archive, snooze, undo send, a rich composer with attachments and drag and drop, signatures, a unified inbox across accounts, Gmail's keyboard shortcuts, desktop notifications, an offline cache, contacts autocomplete, light and dark themes, and account setup from one field.
* **A built in MCP server**, on by default, read and write across every account. See Settings for the endpoint and a client config, or run `halyardd --mcp-stdio`.

## Why Wails

The native window is Wails v2: one library gives a window, a single-instance lock and the platform webview (WebKitGTK on Linux, WebView2 on Windows). The UI reaches the Go core through the asset handler inside the same process, so there is no network port to protect. The same UI runs in a plain browser against `cmd/halyardd`, which is how it is tested.

## Layout

`cmd/halyard` the window, `cmd/halyardd` the headless daemon and MCP stdio, `internal/{plugin,providers,store,engine,api,mcpserver,oauth,discover}`, `ui/web` the front end (`tokens.css` drives both themes), `packaging` the three builds, `lab` the Podman IMAP and JMAP server, `design` the art and the render scripts.

## Build and test

    go build ./... && go test ./...
    lab/stalwart.sh up          # on a machine with Podman
    MAIL_LAB_HOST=127.0.0.1 go test ./internal/e2e -run Lab

Release builds: `packaging/build.sh <version> arch|rpm|windows <dir>`. See `RELEASE_CHECKLIST.md`.

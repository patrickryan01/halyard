# The Halyard MCP server

On by default, read and write, and it signs in as the person using the app, so every account the app has is reachable through it. Nothing in it is read only.

## Connect

* **HTTP (the running app):** `http://127.0.0.1:8765/mcp` with `Authorization: Bearer <token>`. The app and the headless daemon write the exact URL and token to `mcp.json` in the data directory, and Settings shows a ready client config. Only loopback is served. If port 8765 is taken, the next free port is used and `mcp.json` has it.
* **stdio (a client launches it):** `halyardd --mcp-stdio`. It opens its own core on the same data directory, so use it when the app is not running.
* Move the HTTP address with `HALYARD_MCP_ADDR`.


## Tools (37)

| Area | Tools |
|---|---|
| Accounts | `list_accounts`, `add_account_password` (IMAP or JMAP), `start_oauth_signin`, `finish_oauth_signin` (Google, Microsoft), `update_account`, `remove_account`, `sync_now` |
| Folders | `list_folders`, `create_folder` |
| Reading | `list_threads`, `search_threads`, `get_thread`, `get_message`, `get_attachment` (base64 or save to a path) |
| Sending | `send_message`, `reply_message`, `forward_message`, `cancel_send`, `list_outbox`, `retry_send` |
| Drafts | `create_draft`, `update_draft`, `list_drafts`, `get_draft`, `send_draft`, `delete_draft` |
| Actions | `thread_action` (archive, trash, untrash, spam, read, unread, star, unstar, delete), `move_threads`, `label_threads`, `unlabel_threads`, `snooze_threads` |
| Contacts | `search_contacts`, `create_contact`, `update_contact`, `delete_contact` (Google and Microsoft write to their own address book; other accounts use the local one) |
| Settings | `get_settings`, `update_settings` |

Sends go through the outbox and leave after the user's undo window unless `immediate` is set. Calendars are not part of the client, so there are no calendar tools.

## Tested

`internal/e2e/mcp_test.go` drives all of it with the official MCP client over HTTP with the bearer token, against the Podman IMAP and JMAP lab, a real Gmail mailbox, and a real Microsoft 365 mailbox.

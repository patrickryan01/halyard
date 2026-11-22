# The Halyard MCP server

On by default, read and write, and it signs in as the person using the app, so every account the app has is reachable through it. Nothing in it is read only.

## Connect

* **HTTP (the running app):** `http://127.0.0.1:8765/mcp` with `Authorization: Bearer <token>`. The app and the headless daemon write the exact URL and token to `mcp.json` in the data directory, and Settings shows a ready client config. Only loopback is served. If port 8765 is taken, the next free port is used and `mcp.json` has it.
* **stdio (a client launches it):** `halyardd --mcp-stdio`. It opens its own core on the same data directory, so use it when the app is not running.
* Move the HTTP address with `HALYARD_MCP_ADDR`.

A client config for an MCP client that takes an HTTP server:

    { "mcpServers": { "halyard": { "type": "http", "url": "http://127.0.0.1:8765/mcp", "headers": { "Authorization": "Bearer <token from mcp.json>" } } } }

The command line (`halyard` or `halyardctl`) is a client of this same server, so anything it can do a script can do through MCP and the other way round.

## Tools (87)

| Area | Tools |
|---|---|
| Accounts | `list_accounts`, `add_account_password` (IMAP, POP3 or JMAP), `start_oauth_signin`, `finish_oauth_signin` (Google, Microsoft), `update_account`, `remove_account`, `sync_now` |
| Folders | `list_folders`, `create_folder`, `rename_folder`, `delete_folder`, `mark_folder_read`, `empty_folder` |
| Reading | `list_threads`, `search_threads` (operators listed in the tool), `get_thread`, `get_message`, `get_message_source`, `get_attachment` |
| Sending | `send_message` (also scheduled, as another identity, with a read receipt, signed or encrypted), `reply_message`, `forward_message`, `cancel_send`, `list_outbox`, `retry_send` |
| Drafts | `create_draft`, `update_draft`, `list_drafts`, `get_draft`, `send_draft`, `delete_draft` |
| Actions | `thread_action`, `move_threads`, `label_threads`, `unlabel_threads`, `snooze_threads` |
| Filters | `list_rules`, `save_rule`, `delete_rule`, `move_rule`, `run_rules`; server side: `list_sieve_scripts`, `get_sieve_script`, `put_sieve_script`, `check_sieve_script`, `activate_sieve_script`, `delete_sieve_script` |
| Writing aids | `list_templates`, `save_template`, `delete_template`, `list_saved_searches`, `save_search`, `delete_saved_search`, `list_identities`, `refresh_identities`, `save_identity`, `delete_identity` |
| Automatic reply | `get_vacation`, `set_vacation` |
| Invitations and lists | `get_invite`, `respond_invite`, `add_invite_to_calendar`, `unsubscribe`, `send_read_receipt` |
| Calendar and contacts servers | `set_calendar_server`, `list_calendars`, `list_events`, `save_event`, `delete_event`, `list_address_book`; contacts: `search_contacts`, `create_contact`, `update_contact`, `delete_contact` |
| Keys | `list_keys`, `make_key`, `import_key`, `remove_key`, `export_key`, `open_secure_message`, `keep_signer` |
| Import and export | `import_mbox`, `import_thunderbird`, `export_message`, `export_threads` |
| Settings | `get_settings`, `update_settings` |

Sends go through the outbox and leave after the undo window unless `immediate` is set or a time is given.

## Tested

`internal/e2e/mcp_test.go` drives the tools with the official MCP client over HTTP with the bearer token, against a local IMAP and JMAP server, and, with test credentials, a Gmail mailbox and a Microsoft 365 mailbox.

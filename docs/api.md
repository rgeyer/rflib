# HTTP API

`rflib serve` offers what the command line and the web interface do as a JSON API, for scripts and the web interface itself.

```sh
./rflib serve                                    # http://127.0.0.1:8080/api/v1
RFLIB_API_TOKEN=s3cret ./rflib serve --addr :8080
curl -H "Authorization: Bearer s3cret" 'localhost:8080/api/v1/libraries/default/channels?q=band:2m%20is:repeater'
```

- **The description:** `GET /api/v1/openapi.yaml` is the OpenAPI document, which lists every endpoint and shape. Its source is `internal/api/openapi.yaml`, and the tests hold every route and response to it.
- **Authentication:** with `$RFLIB_API_TOKEN` set, every request but `/health` and the document needs `Authorization: Bearer TOKEN`. Serving on anything but a loopback address requires a token. On a server with accounts, `POST /auth/signin` sets a session cookie instead, `GET /auth` says who's signed in, and a change sent from a browser must come from the server's own pages (see [Accounts](accounts.md)).
- **Resources:** everything lives under `/api/v1/libraries/{lib}/`: `channels` (with `bulk-update`, `bulk-status`, `relink`, and `copy`; channels are retired, never deleted, and `hide_retired` lists them as the Channels page does; a shared channel's ID gives your view of it, and saving it saves your notes, tags, and short names), `radios` (memories, channels, overrides, sources, sync, `draft`, `revisions`, and export, which saves a revision when the radio changed; changes to a radio's list go to its draft, as the spec's introduction describes), `searches`, `imports/{format}`, `exports/{format}`, `shared` (convert, changes, ack, and relink), and `feed`. `organizations`, `systems`, and `plans` (each with `history`), which live in the shared list, and `models`, `profiles`, `seeds` (with `update`), and `tags` sit beside `libraries`.
- **Values:** frequencies are exact integer Hz, and tones use the library's syntax (`ctcss:100.000`, `dcs:023`). Queries are the same as the CLI's, in a `q` parameter or a `query` field. Channel IDs can be shortened to a unique prefix.
- **Changes:** `PATCH` changes only the fields given, and `null` clears `tx_hz` and `location`. Changes to the `shared` library, organizations, systems, and plans need a `reason`.
- **Errors** are `{"error": {"code", "message"}}`: 400 for a request to fix, 401 without a valid token or session, 403 for everyday changes to the shared library and for what your role doesn't allow, 404, 409 for a name in use, 429 after too many failed sign-ins, and 500 for anything else (the detail goes to the server's log).
- **The feed:** `GET /libraries/shared/feed?since=CURSOR` returns shared channels changed since a previous response's `cursor`. It's how a local copy will keep up with a hosted shared list.

---

[Documentation index](README.md)

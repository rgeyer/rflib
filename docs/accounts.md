# Accounts and hosting

By default, `rflib serve` serves one library, yours, to whoever has the token. To host rflib for other people, start it with accounts: each person signs in and has a library of their own.

```sh
./rflib user add N0CALL --admin                     # the first account; it takes the library you have now
./rflib serve --accounts --signup invite --addr :8080 --behind-tls
./rflib user invite                                # a code that lets one person register
```

- **Accounts:** you sign in with a callsign or a username. rflib checks a callsign's form, but not that you hold it. Each account gets its own library, named after it, holding its channels, notes, radios, and saved searches. No one else sees it, except an admin.
- **Roles:** a **member** uses their own library and reads the shared list, with its organizations, systems, and plans. An **admin** also maintains the shared list (corrections, built-in lists, organizations, systems, and plans) and the accounts. The shared list's history records who made each change.
- **Sign-up:** `--signup` says who may register from the sign-in page: `open` (anyone who can reach the server), `invite` (with a code from `rflib user invite`, good for one sign-up for two weeks), or `closed` (the default: only accounts from `rflib user add`).
- **Administration** works on the database directly, on the server: `rflib user list`, `add`, `role` (`--admin`, `--member`, `--disable`, or `--enable`), `passwd` (for someone who forgot their password, since rflib sends no email; it signs them out everywhere), `remove` (which deletes the account's library), and `invite`. The last admin can't be demoted or removed.
- **HTTPS:** signing in sends a password, and every request after it a session cookie, so off this machine rflib needs HTTPS. rflib doesn't serve HTTPS itself. Put it behind a proxy that does, such as Traefik or Caddy, and pass `--behind-tls`: the session cookie is then sent only over HTTPS, and rflib takes each client's address from the proxy's `X-Forwarded-For` to slow repeated failed sign-ins. Where HTTPS isn't possible, such as on an AREDN mesh network, `--plain-http` serves sign-in over plain HTTP anyway. Anyone on the path can then read passwords and take sessions, so use a password you use nowhere else.
- **Sessions** last 30 days from when they were last used. Changing your password signs out your other sessions.
- **The CLI** still works on the database directly, as the admin, on the admin's library. Scripts against a server with accounts, and personal API tokens, come later. Until then, a server with accounts doesn't use `$RFLIB_API_TOKEN`.

---

[Documentation index](README.md)

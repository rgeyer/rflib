# Your library

## Library location

Every command takes `--db PATH`. When it is omitted, `rflib` uses `$RFLIB_DB`, then `$XDG_DATA_HOME/rflib/library.db` (`~/.local/share/rflib/library.db` by default, and `%LOCALAPPDATA%\rflib\library.db` on Windows). The examples in this documentation set `RFLIB_DB` once instead of repeating `--db`, and run `./rflib` from a Unix shell. On Windows, run `rflib.exe`, and set the variable with `$env:RFLIB_DB = "radio-library.db"` in PowerShell or `set RFLIB_DB=radio-library.db` in the Command Prompt.

The CLI only creates a database when you explicitly initialize one. After upgrading `rflib`, commands may report that the library schema is out of date; re-run `init` to upgrade it in place. Back up the database file first, since an upgrade can convert data.

```sh
export RFLIB_DB=./radio-library.db
./rflib init                 # a new database's shared list starts with the built-in lists
```

Back up the database by copying it while neither the CLI nor `rflib serve` is running, or with `rflib export json` (see [Export](import-export.md#export)).

## Your library and the shared list

A database holds **your library**, your own channels, radios, and saved searches, and the **shared list**, channels shared with everyone (see [Shared channels](shared-channels.md)). Commands work on your library; the `rflib shared` commands work on the shared list. For a separate collection, such as a scratch space to try an import or someone else's radios, use a separate database file with `--db` or `$RFLIB_DB`. On a server with accounts, each person has a library of their own, and everyone shares the one shared list.

---

[Documentation index](README.md)

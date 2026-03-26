# exocortex

A note-taking/todo/information-storage system written in Go. The primary interface is **exo**, a terminal UI built with [bubbletea](https://github.com/charmbracelet/bubbletea).

## Concepts

exocortex tries to be as friction-free as possible when it comes to capturing information without worrying about organization upfront.

On startup, exo reopens the last tag you were on, or the `inbox` tag if none exists yet.

**Tags** are the top-level organizational unit. They are created explicitly via the new tag command, or on-the-fly by referencing them in row text using `[[double brackets]]`. Tags are automatically deleted when they have no rows and no rows reference them.

**Rows** are bullets under a tag. When a row references another tag (e.g. `[[todo]] take out the trash`), exocortex links that row bidirectionally — viewing the `todo` tag will show a reference back to the originating tag with the full row text visible and editable.

## Installation

```
go install github.com/neutralinsomniac/exocortex/cmd/exo@latest
```

Or with Nix:

```
nix run github:neutralinsomniac/exocortex
```

## Usage

Run `exo` from anywhere. All data is stored in a single SQLite database at `$XDG_DATA_HOME/exocortex/exocortex.db` (defaulting to `~/.local/share/exocortex/exocortex.db`).

Press `?` inside the app to open the help screen.

### Keybindings

#### Tags

| Key | Action |
|-----|--------|
| `i` | Go to inbox tag |
| `n` | New tag |
| `r` | Rename current tag |
| `t` | Tag selector (type to filter) |
| `/` | Fuzzy search all rows |
| `\` | Toggle show/hide done rows |
| `ctrl-t` | Go back in tag history |
| `1`–`9` | Jump to numbered tag reference |

#### Rows

| Key | Action |
|-----|--------|
| `g` / `G` | Jump to first / last row |
| `j` / `k` | Move cursor down / up |
| `enter` | Follow tag link in selected row |
| `o` / `O` | Add row below / above cursor |
| `e` | Edit selected row |
| `N` | Add/edit note on selected row |
| `d` | Cut selected row (or all marked) |
| `D` | Mark done / un-done (or all marked) |
| `y` | Yank (copy) selected row |
| `p` / `P` | Paste below / above cursor |
| `J` / `K` | Move row down / up within its priority group |
| `space` | Toggle row in/out of multi-selection |
| `;` | Clear multi-selection |
| `!` `@` `#` `$` `%` | Set priority 1–5 (repeat to clear) |
| `)` | Clear priority |
| `u` / `U` | Undo / redo |
| `S` | Sync with server |
| `?` | Help |
| `q` | Quit |

## Sync

`exo-server` is a companion binary that lets you keep a hosted exocortex database and sync to it from multiple machines. Sync is bidirectional with last-write-wins conflict resolution per row.

### Running the server

```
go install github.com/neutralinsomniac/exocortex/cmd/exo-server@latest
```

```
exo-server -db /path/to/exocortex.db -token <secret> -cert /path/to/cert.pem -key /path/to/key.pem
```

| Flag | Default | Description |
|------|---------|-------------|
| `-db` | (required) | Path to the SQLite database file |
| `-token` | (required) | Shared secret used to authenticate clients |
| `-cert` | | TLS certificate file (PEM). Must be paired with `-key` |
| `-key` | | TLS private key file (PEM). Must be paired with `-cert` |
| `-addr` | `:8765` | Address to listen on |

Omitting `-cert` and `-key` runs the server in plaintext mode, which is only suitable for local testing.

### Configuring the client

Set the server URL and token in the exocortex settings table using any SQLite client:

```sh
sqlite3 ~/.local/share/exocortex/exocortex.db \
  "INSERT OR REPLACE INTO settings VALUES ('sync_url', 'https://yourserver:8765');
   INSERT OR REPLACE INTO settings VALUES ('sync_token', 'yoursecret');"
```

Once configured, press `S` inside `exo` to sync. Status is shown in the bottom bar.

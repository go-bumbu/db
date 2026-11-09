# AGENTS.md

This file provides guidance to AI coding agents (e.g. Claude Code) when working with code in this repository.

## Commands

```bash
make test              # run fast tests with coverage (no Docker)
make test-all          # run all tests incl. Postgres in Docker (TESTDBS_ALL=1, integration/ module)
make lint              # run golangci-lint (must be installed externally)
make benchmark         # run benchmarks
make license-check     # check dependency licenses (needs go-licence-detector)
make verify            # run tests + license-check + lint + benchmarks + coverage check
go test ./...          # run all tests
go test ./gormdb/ -run TestOpen  # run a single test
CGO_ENABLED=1 go test ./gormdb/sqlitecgo/  # the mattn driver needs CGO
```

## Architecture

This is a Go library (`github.com/go-bumbu/db`) providing the GORM database foundation for bumbu apps. It is not an application: it is imported by other projects and never imports an app.

### Packages

- **gormdb/** — Core. `Cfg` (`SqliteCfg`, `PostgresCfg`), `Cfg.Validate`, `PostgresCfg.DSN`, `Open(cfg, Drivers)` and `Close`. Defines the `Drivers`, `SqliteDriver` and `PostgresDriver` interfaces. Imports no database driver.
- **gormdb/sqlite/** — Pure-Go SQLite driver (`glebarez/sqlite`, modernc). No CGO. Pragmas in glebarez's `_pragma=name(value)` format.
- **gormdb/sqlitecgo/** — CGO SQLite driver (`mattn/go-sqlite3`). Needs `CGO_ENABLED=1`. Pragmas in mattn's `_busy_timeout=N` / `_foreign_keys=1` format. Use it for SQLite extensions such as sqlite-vec.
- **gormdb/postgres/** — Postgres driver (`gorm.io/driver/postgres`, pgx).
- **gormdb/internal/sqlitetest/** — Test suite shared by both SQLite driver packages (routing, read-only pool, WAL, error translation).
- **gormdb/gormdbtest/** — `Sqlite(t, drv)` opens a temp-file SQLite database closed on cleanup, so apps test on the same writer/reader setup they run on instead of `:memory:`.
- **integration/** — Separate module (`replace` to `../`) with the Docker-backed Postgres tests, so testcontainers and Docker stay out of the library's `go.mod`. Not reached by `go test ./...` at the root.
- **gormdb/internal/sqliteerr/** — Maps SQLite extended result codes to gorm sentinels; both SQLite drivers call it from their `Translate` after reading the code from their own error type.

### Key Design Decisions

- **Pluggable drivers, no global registration**: the core imports no database driver; the app passes the drivers it supports in `gormdb.Drivers{Sqlite: ..., Postgres: ...}` and `Cfg.Driver` selects among them. A binary links only the databases it uses, and CGO is needed only when it imports `sqlitecgo`. A config naming a driver that was not passed makes `Open` return an error naming it. The config never chooses between glebarez and mattn; the app does.
- **Pragmas in the DSN, not `Exec`**: most pragmas are per connection, and an `Exec` hits one pooled connection only. The two drivers use different formats (glebarez `_pragma=busy_timeout(5000)`, mattn `_busy_timeout=5000`), produced by `SqliteDriver.WriterParams` / `ReaderParams` (Open copies the map before adding `mode=ro`). `foreign_keys` is on so SQLite enforces foreign keys like Postgres. A file database and its readers are addressed by `file:` URIs with an escaped path, so `?`, `#` or `%` in a directory name do not break the DSN.
- **Writer/reader split for SQLite** (`gormdb/sqlite.go`): one write connection (`SetMaxOpenConns(1)`, WAL, checked at open) plus a read-only pool (`mode=ro`, `SqliteCfg.Readers`, default 10) registered with `dbresolver`. Writers queue in Go instead of hitting `SQLITE_BUSY` (within the process; another process on the same file can still exceed `busy_timeout`); WAL lets reads run while a write is in progress. See the benchmark at https://github.com/andresbott/gorm-sqlite-concurrency. In-memory databases fall back to one connection. `Close` closes both pools: they are held by a small GORM plugin (`sqlitePools`, in `db.Plugins`), so there is no global state.
- **Routing contract** (documented on `Open`): routing goes by SQL text, not by method: queries GORM builds and raw SQL starting with `SELECT` (from `Raw` and `Exec` alike) go to the read pool; `Create`, `Update`, `Delete`, `PRAGMA`, `WITH ...`, other raw SQL, a `SELECT` of 10 characters or fewer, `SELECT ... FOR UPDATE` and `Clauses(dbresolver.Write)` go to the writer; inside `db.Transaction` and `db.Connection` everything stays on the connection they hold (the `sqlitePools` plugin wraps dbresolver's callbacks to skip a pinned `*sql.Conn`, which dbresolver would otherwise swap back to a pool and deadlock on). The wrapper relies on dbresolver's callback name and on GORM sorting `Before("*")` callbacks last; after bumping GORM or dbresolver, the suite's `connection stays pinned` and `quiet open` tests are the guard. Reads wait for a free reader, so more concurrent reads than `Readers` queue in Go. SQL that depends on the connection (`SELECT last_insert_rowid()`, `changes()`, temp tables) needs a transaction or `db.Connection`, since a bare SELECT lands on a read-only connection (`dbresolver.Write` reaches the writer, but another goroutine may write in between). `db.DB()` is the writer pool: its limits must not be changed.
- **Deadlock caveat**: inside `db.Transaction` or `db.Connection`, use only `tx`. The single writer connection is held by the callback, so any statement on `db` that reaches the writer (Exec, WITH, PRAGMA, short SELECTs, `dbresolver.Write`) waits forever. For `:memory:` (one connection for everything) any statement on `db` inside the callback hangs, reads included, and so does a write while query rows are open, although the same code works on a file database; apps should test on a temp file (`gormdbtest.Sqlite`).
- **`_txlock=immediate`**: a transaction takes the write lock at `BEGIN`, so a read-then-write transaction waits (honouring `busy_timeout`) instead of failing with `SQLITE_BUSY` on upgrade. Set for the writer on both SQLite drivers.
- **glebarez vs mattn**: glebarez (modernc) is the no-CGO option, but per the benchmark above it writes roughly 100x slower than mattn in WAL mode (about 197 vs 20,000 writes/s). It is not "the default"; choose `sqlitecgo` when write throughput or extensions matter and CGO is acceptable.
- **Error translation**: `Open` always sets `TranslateError: true`. The SQLite drivers wrap the upstream dialector's `Translate` to also map constraint errors (unique, primary key, foreign key, CHECK) to gorm sentinels while keeping the original driver error. Callers check with `errors.Is(err, gorm.ErrDuplicatedKey)` / `errors.Is(err, gorm.ErrForeignKeyViolated)`, never by string matching. There is no separate `dberr` package.
- **Postgres DSN**: built as a URL, so user, password and database name are escaped; port defaults to 5432; `sslmode` defaults to `require`; an empty user is left out of the URL (a password then goes in the `password` query parameter); query spaces are `%20`, never `+`, which libpq keeps literally; `connect_timeout` defaults to 10s so `Open` fails fast; `Params` adds parameters but cannot repeat a field's; pool settings are applied after open. `Validate` rejects a host with a port, a port out of range, an unknown `sslmode` and negative durations. `PostgresCfg` redacts `Password` in `String` and `LogValue`.
- **SQLite path**: `SqliteCfg.Path` is a filesystem path or `:memory:`; `Validate` rejects a `file:` URI (both drivers would silently create a file with that name). `fileDSN` cleans the path so a leading `//` is not read as a URI authority, and keeps a path that cleans to `:memory:` (e.g. `./:memory:`) a file. `BusyTimeout` must not be negative; it is kept between 1ms and 2^31-1 ms, since SQLite turns waiting off for 0 and for a value it cannot store. A failed `Open` removes the directories it created.
- **Constructor convention**: configuration goes through one `Cfg` struct, like the other bumbu libraries.
- **WAL caveats**: WAL does not work on network filesystems, and copying a database file needs its `-wal` file (or close the database first). `Close` closes the readers, waits (up to 5s) for reads still running, then closes the writer, so the writer can checkpoint and remove the `-wal` file; concurrent and repeated calls are serialised; reads still open after the wait (rows never closed) make `Close` return an error; always release through `gormdb.Close`, not `db.DB().Close()`.

## Linting

Uses golangci-lint v2 with: nolintlint, gocyclo (max 20), nestif (max 5), gosec, dupl. All `//nolint` directives require an explanation and specific linter name.

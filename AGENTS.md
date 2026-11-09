# AGENTS.md

This file provides guidance to AI coding agents (e.g. Claude Code) when working with code in this repository.

## Commands

```bash
make test              # run fast tests with coverage (no Docker)
make test-all          # run all tests incl. Postgres in Docker (TESTDBS_ALL=1)
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

### Key Design Decisions

- **Pluggable drivers, no global registration**: the core imports no database driver; the app passes the drivers it supports in `gormdb.Drivers{Sqlite: ..., Postgres: ...}` and `Cfg.Driver` selects among them. A binary links only the databases it uses, and CGO is needed only when it imports `sqlitecgo`. A config naming a driver that was not passed makes `Open` return an error naming it. The config never chooses between glebarez and mattn; the app does.
- **Pragmas in the DSN, not `Exec`**: most pragmas are per connection, and an `Exec` hits one pooled connection only. The two drivers use different formats (glebarez `_pragma=busy_timeout(5000)`, mattn `_busy_timeout=5000`), produced by `SqliteDriver.Params`. `foreign_keys` is on so SQLite enforces foreign keys like Postgres. A file database and its readers are addressed by `file:` URIs with an escaped path, so `?`, `#` or `%` in a directory name do not break the DSN.
- **Writer/reader split for SQLite** (`gormdb/sqlite.go`): one write connection (`SetMaxOpenConns(1)`, WAL, checked at open) plus a read-only pool (`mode=ro`, `SqliteCfg.Readers`, default 10) registered with `dbresolver`. Writers queue in Go instead of hitting `SQLITE_BUSY` (within the process; another process on the same file can still exceed `busy_timeout`); WAL lets reads run while a write is in progress. See the benchmark at https://github.com/andresbott/gorm-sqlite-concurrency. In-memory databases fall back to one connection. `Close` closes both pools (the readers are tracked in a registry keyed by the writer pool).
- **Routing contract** (documented on `Open`): queries GORM builds and raw SQL starting with `SELECT` go to the read pool; `Exec`, `Create`, `Update`, `PRAGMA`, `WITH ...`, other raw SQL, a `SELECT` of 10 characters or fewer, `SELECT ... FOR UPDATE` and `Clauses(dbresolver.Write)` go to the writer; inside `db.Transaction` everything stays on the writer connection. Reads wait for a free reader, so more concurrent reads than `Readers` queue in Go. SQL that depends on the connection (`SELECT last_insert_rowid()`, `changes()`, temp tables) needs `db.Clauses(dbresolver.Write)` or a transaction, since a bare SELECT lands on a read-only connection.
- **Deadlock caveat**: inside `db.Transaction`, use only `tx`. The single writer connection is held by the transaction, so any statement on `db` that reaches the writer (Exec, WITH, PRAGMA, short SELECTs, `dbresolver.Write`) waits forever. For `:memory:` (one connection for everything) any statement on `db` inside a transaction hangs, reads included, although the same code works on a file database.
- **`_txlock=immediate`**: a transaction takes the write lock at `BEGIN`, so a read-then-write transaction waits (honouring `busy_timeout`) instead of failing with `SQLITE_BUSY` on upgrade. Set for the writer on both SQLite drivers.
- **glebarez vs mattn**: glebarez (modernc) is the no-CGO option, but per the benchmark above it writes roughly 100x slower than mattn in WAL mode (about 197 vs 20,000 writes/s). It is not "the default"; choose `sqlitecgo` when write throughput or extensions matter and CGO is acceptable.
- **Error translation**: `Open` always sets `TranslateError: true`. The SQLite drivers wrap the upstream dialector's `Translate` to also map constraint errors (unique, primary key, foreign key, CHECK) to gorm sentinels while keeping the original driver error. Callers check with `errors.Is(err, gorm.ErrDuplicatedKey)` / `errors.Is(err, gorm.ErrForeignKeyViolated)`, never by string matching. There is no separate `dberr` package.
- **Postgres DSN**: built as a URL, so user, password and database name are escaped; port defaults to 5432; `sslmode` defaults to `require`; an empty user is left out of the URL.
- **SQLite path**: `SqliteCfg.Path` is a filesystem path or `:memory:`; `Validate` rejects a `file:` URI (both drivers would silently create a file with that name). `fileDSN` cleans the path so a leading `//` is not read as a URI authority.
- **Constructor convention**: configuration goes through one `Cfg` struct, like the other bumbu libraries.
- **WAL caveats**: WAL does not work on network filesystems, and copying a database file needs its `-wal` file (or close the database first). `Close` closes the readers before the writer so the writer can checkpoint and remove the `-wal` file; always release through `gormdb.Close`, not `db.DB().Close()`.

## Linting

Uses golangci-lint v2 with: nolintlint, gocyclo (max 20), nestif (max 5), gosec, dupl. All `//nolint` directives require an explanation and specific linter name.

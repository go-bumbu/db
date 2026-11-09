# db

GORM database helpers for Go apps: open SQLite or Postgres from one config struct, with the same API and the same errors on both.

[![CI](https://github.com/go-bumbu/db/actions/workflows/test.yml/badge.svg)](https://github.com/go-bumbu/db/actions/workflows/test.yml)
[![License: LGPL-3.0](https://img.shields.io/badge/license-LGPL--3.0-blue.svg)](LICENSE)

## Features

- Pick SQLite or Postgres in your config file, with no code change
- Use SQLite from many goroutines without "database is locked": writes queue on one connection while reads run in parallel
- Catch duplicate keys and foreign-key violations the same way on every database
- Build without CGO, or use the CGO SQLite driver when you need extensions such as sqlite-vec
- Compile only the database drivers your app uses

## Add to your project

```sh
go get github.com/go-bumbu/db@latest
```

The CGO SQLite driver (`gormdb/sqlitecgo`) needs a C compiler. The pure-Go one (`gormdb/sqlite`) builds anywhere but writes about 100x slower ([benchmark](https://github.com/andresbott/gorm-sqlite-concurrency)).

```go
import (
	"github.com/go-bumbu/db/gormdb"
	"github.com/go-bumbu/db/gormdb/sqlite"
	"gorm.io/gorm"
)

func openDB() (*gorm.DB, error) {
	// describe the database; usually filled from your config file
	cfg := gormdb.Cfg{
		Driver: gormdb.DriverSqlite,
		Sqlite: gormdb.SqliteCfg{Path: "data/app.db"},
	}
	// open it with the drivers this app supports
	return gormdb.Open(cfg, gormdb.Drivers{Sqlite: sqlite.Driver{}})
}
```

Release it with `gormdb.Close(db)` on shutdown.

### SQLite's extra rules

The same code runs on SQLite and Postgres as long as it keeps to these, since the SQLite writer is a single connection:

- Inside `db.Transaction` or `db.Connection`, use only `tx`; a statement on `db` waits forever.
- SQL that depends on the connection (`last_insert_rowid()`, `changes()`, temp tables) needs a transaction or `db.Connection`.
- `db.DB()` is the writer pool: do not tune it, and release through `gormdb.Close`.
- `WITH ...` queries go to the writer, even read-only ones. Force a statement with `db.Clauses(gormdb.Read)` or `db.Clauses(gormdb.Write)`.

Test on a temporary file with the same setup, not on `:memory:`:

```go
db := gormdbtest.Sqlite(t, sqlite.Driver{})
```

### Detect duplicates

```go
// constraint errors come back as GORM's sentinels, on every driver
if err := db.Create(&user).Error; errors.Is(err, gorm.ErrDuplicatedKey) {
	return ErrEmailTaken
}
```

### Drivers in your build

- `gormdb/sqlite` (glebarez) registers the `database/sql` driver name `sqlite`, the same name as `modernc.org/sqlite`. A binary that links both panics at start-up.
- Your binary links only the driver packages you import, but your module graph and `go.sum` list all three (glebarez, mattn and pgx), as they share one module.

API reference and the routing rules for reads and writes: [pkg.go.dev/github.com/go-bumbu/db/gormdb](https://pkg.go.dev/github.com/go-bumbu/db/gormdb).

## Development

See [DEVELOPMENT.md](DEVELOPMENT.md).

Licensed under the [GNU LGPL v3](LICENSE).

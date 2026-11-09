package gormdb

import (
	"database/sql"
	"net/url"
	"time"

	"gorm.io/gorm"
)

// Drivers holds the database drivers an app compiles in. Only the one
// matching Cfg.Driver is used; the other may be nil.
type Drivers struct {
	Sqlite   SqliteDriver
	Postgres PostgresDriver
}

// SqliteDriver adapts a SQLite driver package to gormdb. The two adapters
// shipped with gormdb, gormdb/sqlite and gormdb/sqlitecgo, are the supported
// implementations: they pass the behaviour suite in gormdb's internal
// sqlitetest package, which is not importable from outside the module.
//
// Each driver decides how its DSN sets the pragmas; Open verifies on the
// writer, after it opened it, that journal_mode is WAL (file databases) and
// foreign_keys is on, and fails naming this contract if not.
type SqliteDriver interface {
	// OpenDB returns a connection pool for dsn, a file: URI or
	// ":memory:?...", whose query holds the parameters below. Like sql.Open
	// it must not connect yet.
	OpenDB(dsn string) (*sql.DB, error)
	// WriterParams returns the writer's DSN query parameters in the driver's
	// own format: busy timeout, foreign keys, WAL and an immediate
	// transaction lock. ReaderParams returns a reader's: busy timeout and
	// foreign keys only. Each call returns a map the caller may keep.
	WriterParams(busyTimeout time.Duration) url.Values
	ReaderParams(busyTimeout time.Duration) url.Values
	// PragmaParams returns the DSN query parameters that make every
	// connection opened by OpenDB run the extra pragmas (SqliteCfg.Pragmas,
	// already validated). Open adds them to the writer's and the readers'
	// parameters. Each call returns a map the caller may keep.
	PragmaParams(pragmas map[string]string) url.Values
	// Dialector wraps an open connection pool. Translate must keep the
	// original error.
	Dialector(conn gorm.ConnPool) gorm.Dialector
}

// PostgresDriver adapts a Postgres driver package to gormdb. As with SQLite,
// the driver opens the pool and Open owns it: Open applies the pool settings,
// hands the pool to Dialector, and closes it if opening fails.
//
// An app that must build the pool itself (credentials that rotate, a tracing
// wrapper) embeds the shipped gormdb/postgres Driver and replaces OpenDB.
type PostgresDriver interface {
	// OpenDB returns a connection pool for cfg, already validated. The
	// shipped driver connects with cfg.DSN(); another may read the fields
	// and build the pool its own way. Like sql.Open it must not connect yet.
	// Open sets MaxOpenConns, MaxIdleConns and ConnMaxLifetime on the result.
	OpenDB(cfg PostgresCfg) (*sql.DB, error)
	// Dialector wraps an open connection pool. Translate must keep the
	// original error.
	Dialector(conn gorm.ConnPool) gorm.Dialector
}

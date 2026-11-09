package gormdb

import (
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

// SqliteDriver adapts a SQLite driver package to gormdb.
type SqliteDriver interface {
	// SQLDriverName is the database/sql driver name: "sqlite" (glebarez),
	// "sqlite3" (mattn).
	SQLDriverName() string
	// Params returns the DSN query parameters in the driver's own format.
	// The writer gets busy timeout, foreign keys, WAL and an immediate
	// transaction lock; a reader gets busy timeout and foreign keys only.
	Params(busyTimeout time.Duration, writer bool) url.Values
	// Dialector wraps an open connection pool. Translate must keep the
	// original error.
	Dialector(conn gorm.ConnPool) gorm.Dialector
}

// PostgresDriver adapts a Postgres driver package to gormdb.
type PostgresDriver interface {
	Dialector(dsn string) gorm.Dialector
}

// Package sqlitecgo adapts the CGO mattn/go-sqlite3 driver to gormdb. It
// requires CGO_ENABLED=1. Importing it registers the database/sql driver
// "sqlite3".
package sqlitecgo

import (
	"net/url"
	"strconv"
	"time"

	"github.com/go-bumbu/db/gormdb"
	_ "github.com/mattn/go-sqlite3" // registers the "sqlite3" database/sql driver
	gormsqlite "gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// Driver implements gormdb.SqliteDriver with mattn/go-sqlite3.
type Driver struct{}

var _ gormdb.SqliteDriver = Driver{}

// SQLDriverName returns the database/sql driver name mattn registers.
func (Driver) SQLDriverName() string { return "sqlite3" }

// WriterParams returns the writer's DSN query parameters in mattn's format.
func (d Driver) WriterParams(busyTimeout time.Duration) url.Values {
	q := d.ReaderParams(busyTimeout)
	q.Set("_journal_mode", "WAL")
	q.Set("_txlock", "immediate")
	return q
}

// ReaderParams returns a reader's DSN query parameters in mattn's format.
func (Driver) ReaderParams(busyTimeout time.Duration) url.Values {
	return url.Values{
		"_busy_timeout": {strconv.FormatInt(busyTimeout.Milliseconds(), 10)},
		"_foreign_keys": {"1"},
	}
}

// Dialector wraps an open connection pool.
func (Driver) Dialector(conn gorm.ConnPool) gorm.Dialector {
	return dialector{Dialector: gormsqlite.Dialector{Conn: conn}}
}

// dialector embeds the upstream gorm sqlite dialector by value and shadows its
// Translate (see translate.go).
type dialector struct {
	gormsqlite.Dialector
}

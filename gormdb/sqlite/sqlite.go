// Package sqlite adapts the pure-Go glebarez SQLite driver to gormdb, so the
// binary builds without CGO. Importing it registers the database/sql driver
// "sqlite".
package sqlite

import (
	"fmt"
	"net/url"
	"time"

	glebarez "github.com/glebarez/sqlite"
	"github.com/go-bumbu/db/gormdb"
	"gorm.io/gorm"
)

// Driver implements gormdb.SqliteDriver with glebarez.
type Driver struct{}

var _ gormdb.SqliteDriver = Driver{}

// SQLDriverName returns the database/sql driver name glebarez registers.
func (Driver) SQLDriverName() string { return "sqlite" }

// WriterParams returns the writer's DSN query parameters in glebarez's format.
func (Driver) WriterParams(busyTimeout time.Duration) url.Values {
	return url.Values{
		"_pragma": {busyTimeoutPragma(busyTimeout), "journal_mode(WAL)", "foreign_keys(1)"},
		"_txlock": {"immediate"},
	}
}

// ReaderParams returns a reader's DSN query parameters in glebarez's format.
func (Driver) ReaderParams(busyTimeout time.Duration) url.Values {
	return url.Values{"_pragma": {busyTimeoutPragma(busyTimeout), "foreign_keys(1)"}}
}

func busyTimeoutPragma(d time.Duration) string {
	return fmt.Sprintf("busy_timeout(%d)", d.Milliseconds())
}

// Dialector wraps an open connection pool.
func (Driver) Dialector(conn gorm.ConnPool) gorm.Dialector {
	return dialector{Dialector: glebarez.Dialector{Conn: conn}}
}

// dialector embeds glebarez's by value and shadows its Translate (see translate.go).
type dialector struct {
	glebarez.Dialector
}

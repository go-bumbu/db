// Package sqlite adapts the pure-Go glebarez SQLite driver to gormdb, so the
// binary builds without CGO. Importing it registers the database/sql driver
// "sqlite", the name modernc.org/sqlite registers too: a binary that links
// both panics at init.
package sqlite

import (
	"database/sql"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"time"

	glebarez "github.com/glebarez/sqlite"
	"github.com/go-bumbu/db/gormdb"
	"gorm.io/gorm"
)

// Driver implements gormdb.SqliteDriver with glebarez.
type Driver struct{}

var _ gormdb.SqliteDriver = Driver{}

// OpenDB returns a connection pool for dsn with the database/sql driver
// glebarez registers, "sqlite".
func (Driver) OpenDB(dsn string) (*sql.DB, error) { return sql.Open("sqlite", dsn) }

// PragmaParams returns the pragmas as glebarez's _pragma=name(value)
// parameters, sorted by name.
func (Driver) PragmaParams(pragmas map[string]string) url.Values {
	q := url.Values{}
	for _, name := range slices.Sorted(maps.Keys(pragmas)) {
		q.Add("_pragma", name+"("+pragmas[name]+")")
	}
	return q
}

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

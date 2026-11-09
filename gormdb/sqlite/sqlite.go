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

// Params returns the DSN query parameters in glebarez's format.
func (Driver) Params(busyTimeout time.Duration, writer bool) url.Values {
	pragmas := []string{fmt.Sprintf("busy_timeout(%d)", busyTimeout.Milliseconds())}
	if writer {
		pragmas = append(pragmas, "journal_mode(WAL)")
	}
	pragmas = append(pragmas, "foreign_keys(1)")
	q := url.Values{"_pragma": pragmas}
	if writer {
		q.Set("_txlock", "immediate")
	}
	return q
}

// Dialector wraps an open connection pool.
func (Driver) Dialector(conn gorm.ConnPool) gorm.Dialector {
	return dialector{Dialector: glebarez.Dialector{Conn: conn}}
}

// dialector embeds glebarez's by value and shadows its Translate (see translate.go).
type dialector struct {
	glebarez.Dialector
}

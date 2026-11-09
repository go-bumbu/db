// Package sqlitecgo adapts the CGO mattn/go-sqlite3 driver to gormdb. It
// requires CGO_ENABLED=1. Importing it registers the database/sql driver
// "sqlite3".
package sqlitecgo

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-bumbu/db/gormdb"
	"github.com/mattn/go-sqlite3"
	gormsqlite "gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// pragmaParam carries SqliteCfg.Pragmas in the DSN. mattn only knows a fixed
// set of pragma parameters (_synchronous, _cache_size, ...), none for an
// arbitrary one, so OpenDB takes these out of the DSN and runs them from a
// connect hook on every new connection.
const pragmaParam = "_gormdb_pragma"

// Driver implements gormdb.SqliteDriver with mattn/go-sqlite3.
type Driver struct{}

var _ gormdb.SqliteDriver = Driver{}

// OpenDB returns a mattn connection pool for dsn. The pragmas PragmaParams
// put in it run, as "PRAGMA name(value)", on every connection after mattn's
// own parameters, so they win over mattn's defaults (synchronous=NORMAL).
func (Driver) OpenDB(dsn string) (*sql.DB, error) {
	base, query, _ := strings.Cut(dsn, "?") // the path is escaped: the first '?' starts the query
	q, err := url.ParseQuery(query)
	if err != nil {
		return nil, fmt.Errorf("sqlitecgo: parse the DSN parameters: %w", err)
	}
	pragmas := q[pragmaParam]
	q.Del(pragmaParam)
	drv := &sqlite3.SQLiteDriver{}
	if len(pragmas) > 0 {
		drv.ConnectHook = func(conn *sqlite3.SQLiteConn) error {
			for _, p := range pragmas {
				// p is name(value), both validated by gormdb as plain identifiers or integers
				if _, err := conn.Exec("PRAGMA "+p, nil); err != nil {
					return fmt.Errorf("sqlitecgo: PRAGMA %s: %w", p, err)
				}
			}
			return nil
		}
	}
	return sql.OpenDB(connector{driver: drv, dsn: base + "?" + q.Encode()}), nil
}

// connector opens connections to one DSN with a driver of its own, so the
// connect hook needs no globally registered driver name.
type connector struct {
	driver *sqlite3.SQLiteDriver
	dsn    string
}

func (c connector) Connect(context.Context) (driver.Conn, error) { return c.driver.Open(c.dsn) }
func (c connector) Driver() driver.Driver                        { return c.driver }

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

// PragmaParams returns the pragmas as name(value) values of a parameter of
// gormdb's own, sorted by name; OpenDB runs them (see pragmaParam).
func (Driver) PragmaParams(pragmas map[string]string) url.Values {
	q := url.Values{}
	for _, name := range slices.Sorted(maps.Keys(pragmas)) {
		q.Add(pragmaParam, name+"("+pragmas[name]+")")
	}
	return q
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

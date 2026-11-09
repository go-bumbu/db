// Package gormdbtest opens databases for tests. A test should run against a
// SQLite file, not ":memory:": an in-memory database has a single connection
// and no read pool, so it routes, and deadlocks, differently from the file
// database the app runs on.
package gormdbtest

import (
	"path/filepath"
	"testing"

	"github.com/go-bumbu/db/gormdb"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Sqlite opens a SQLite database file in a temporary directory with drv, the
// same writer/reader setup the app gets from gormdb.Open, and closes it when
// the test ends.
//
// cfg is optional: it passes the app's options, such as Logger,
// NamingStrategy, NowFunc or the Sqlite section's Readers and Pragmas. Its
// Driver and Sqlite.Path are overridden, and its Postgres section is ignored.
// A nil Logger becomes logger.Discard, so GORM prints nothing to stdout; set
// one to see GORM's output.
func Sqlite(t testing.TB, drv gormdb.SqliteDriver, cfg ...gormdb.Cfg) *gorm.DB {
	t.Helper()
	var c gormdb.Cfg
	if len(cfg) > 0 {
		c = cfg[0]
	}
	c.Driver = gormdb.DriverSqlite
	c.Sqlite.Path = filepath.Join(t.TempDir(), "test.db")
	if c.Logger == nil {
		c.Logger = logger.Discard
	}
	db, err := gormdb.Open(c, gormdb.Drivers{Sqlite: drv})
	if err != nil {
		t.Fatalf("gormdbtest: open sqlite: %v", err)
	}
	t.Cleanup(func() {
		if err := gormdb.Close(db); err != nil {
			t.Errorf("gormdbtest: close sqlite: %v", err)
		}
	})
	return db
}

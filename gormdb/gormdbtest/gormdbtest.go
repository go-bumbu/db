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
)

// Sqlite opens a SQLite database file in a temporary directory with drv, the
// same writer/reader setup the app gets from gormdb.Open, and closes it when
// the test ends. cfg is optional; its Path is ignored.
func Sqlite(t testing.TB, drv gormdb.SqliteDriver, cfg ...gormdb.SqliteCfg) *gorm.DB {
	t.Helper()
	var c gormdb.SqliteCfg
	if len(cfg) > 0 {
		c = cfg[0]
	}
	c.Path = filepath.Join(t.TempDir(), "test.db")
	db, err := gormdb.Open(gormdb.Cfg{Driver: gormdb.DriverSqlite, Sqlite: c}, gormdb.Drivers{Sqlite: drv})
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

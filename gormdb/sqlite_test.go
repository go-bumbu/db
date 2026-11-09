package gormdb_test

import (
	"testing"

	"github.com/go-bumbu/db/gormdb/internal/sqlitetest"
	"github.com/go-bumbu/db/gormdb/sqlite"
)

// TestSqliteGlebarez puts the core's SQLite code under the shared suite with
// the glebarez driver.
func TestSqliteGlebarez(t *testing.T) { sqlitetest.Run(t, sqlite.Driver{}) }

package gormdb_test

import (
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-bumbu/db/gormdb"
	"github.com/go-bumbu/db/gormdb/internal/sqlitetest"
	"github.com/go-bumbu/db/gormdb/sqlite"
	"gorm.io/plugin/dbresolver"
)

// TestSqliteGlebarez puts the core's SQLite code under the shared suite with
// the glebarez driver.
func TestSqliteGlebarez(t *testing.T) { sqlitetest.Run(t, sqlite.Driver{}) }

// Rows never closed: Close gives up after the drain timeout and says so,
// instead of returning nil with the WAL not checkpointed.
func TestCloseReportsReadsStillOpen(t *testing.T) {
	defer gormdb.SetReaderDrainTimeout(100 * time.Millisecond)()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := gormdb.Open(gormdb.Cfg{Driver: gormdb.DriverSqlite, Sqlite: gormdb.SqliteCfg{Path: path}}, gormdb.Drivers{Sqlite: sqlite.Driver{}})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := db.Exec("CREATE TABLE t (n INTEGER)").Error; err != nil {
		t.Fatal(err)
	}
	rows, err := db.Clauses(dbresolver.Read).Raw("SELECT n FROM t").Rows()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	err = gormdb.Close(db)
	if err == nil || !strings.Contains(err.Error(), "reader connections still open") {
		t.Errorf("Close: got %v, want an error about open reader connections", err)
	}
}

// failingDriver is glebarez with a database/sql driver name nobody registered,
// so Open fails after it created the directory.
type failingDriver struct{ sqlite.Driver }

func (failingDriver) SQLDriverName() string { return "gormdb-test-unregistered" }

func TestFailedOpenRemovesCreatedDirectories(t *testing.T) {
	base := t.TempDir()
	cfg := gormdb.Cfg{Driver: gormdb.DriverSqlite, Sqlite: gormdb.SqliteCfg{Path: filepath.Join(base, "a", "b", "test.db")}}
	if _, err := gormdb.Open(cfg, gormdb.Drivers{Sqlite: failingDriver{}}); err == nil {
		t.Fatal("Open with an unregistered driver succeeded")
	}
	if _, err := os.Stat(filepath.Join(base, "a")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the directory Open created survived the failure: stat error %v", err)
	}
	// a directory that existed before is left alone
	if _, err := os.Stat(base); err != nil {
		t.Errorf("the existing directory was removed: %v", err)
	}
}

// sharedParamsDriver hands out the same reader map on every call, which the
// SqliteDriver contract allows Open to keep but not to change.
type sharedParamsDriver struct{ sqlite.Driver }

var sharedReaderParams = sqlite.Driver{}.ReaderParams(time.Second)

func (sharedParamsDriver) ReaderParams(time.Duration) url.Values { return sharedReaderParams }

func TestOpenDoesNotChangeDriverParams(t *testing.T) {
	db, err := gormdb.Open(gormdb.Cfg{Driver: gormdb.DriverSqlite, Sqlite: gormdb.SqliteCfg{Path: filepath.Join(t.TempDir(), "test.db")}}, gormdb.Drivers{Sqlite: sharedParamsDriver{}})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := gormdb.Close(db); err != nil {
		t.Errorf("Close: %v", err)
	}
	if got := sharedReaderParams.Get("mode"); got != "" {
		t.Errorf("Open set mode=%q in the driver's own map", got)
	}
}

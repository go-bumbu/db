package sqlitetest

import (
	"bytes"
	"errors"
	stdlog "log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-bumbu/db/gormdb"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// within fails the test when fn does not return in time: a deadlock must fail,
// not hang the test binary.
func within(t *testing.T, d time.Duration, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("%s did not return within %s", what, d)
	}
}

// db.Connection pins one connection; everything on tx must stay on it, so it
// neither waits for the writer it holds nor reads from a read-only connection
// that cannot see its temp tables.
func connectionStaysPinned(t *testing.T, drv gormdb.SqliteDriver) {
	db := openWithCounter(t, drv, 0)
	var n, rowid int
	var err error
	within(t, 5*time.Second, "db.Connection", func() {
		err = db.Connection(func(tx *gorm.DB) error {
			for _, stmt := range []string{"CREATE TEMP TABLE pinned (n INTEGER)", "INSERT INTO pinned VALUES (7)"} {
				if err := tx.Exec(stmt).Error; err != nil {
					return err
				}
			}
			if err := tx.Raw("SELECT last_insert_rowid()").Scan(&rowid).Error; err != nil {
				return err
			}
			// a GORM-built query, which would go to the read pool
			return tx.Table("pinned").Select("n").Scan(&n).Error
		})
	})
	if err != nil {
		t.Fatalf("db.Connection: %v", err)
	}
	if n != 7 || rowid != 1 {
		t.Errorf("n = %d, last_insert_rowid = %d, want 7 and 1", n, rowid)
	}
	// outside db.Connection, reads still go to the read pool
	if err := db.Clauses(gormdb.Read).Exec("INSERT INTO counter VALUES (1)").Error; err == nil {
		t.Error("a write forced onto the read pool succeeded after db.Connection")
	}
}

// A BusyTimeout under a millisecond must not become busy_timeout 0, which
// turns waiting off.
func subMillisecondBusyTimeout(t *testing.T, drv gormdb.SqliteDriver) {
	db := open(t, drv, gormdb.SqliteCfg{Path: filepath.Join(t.TempDir(), "test.db"), BusyTimeout: 500 * time.Microsecond})
	for name, q := range map[string]*gorm.DB{"writer": db, "reader": db.Clauses(gormdb.Read)} {
		var got string
		if err := q.Raw("PRAGMA busy_timeout").Scan(&got).Error; err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got != "1" {
			t.Errorf("%s busy_timeout: got %q, want 1", name, got)
		}
	}
}

// "./:memory:" is a file named ":memory:", not an in-memory database.
func memoryNamedFile(t *testing.T, drv gormdb.SqliteDriver) {
	t.Chdir(t.TempDir())
	db := open(t, drv, gormdb.SqliteCfg{Path: "./:memory:"})
	if err := db.Exec("CREATE TABLE counter (n INTEGER)").Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := os.Stat(":memory:"); err != nil {
		t.Errorf("the file :memory: was not created: %v", err)
	}
}

// Close waits for a read still running before it closes the writer, also
// when several goroutines close the database at once, so the writer can
// checkpoint and remove the -wal file.
func closeWaitsForReads(t *testing.T, drv gormdb.SqliteDriver) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := gormdb.Open(gormdb.Cfg{Driver: gormdb.DriverSqlite, Sqlite: gormdb.SqliteCfg{Path: path}}, gormdb.Drivers{Sqlite: drv})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	for _, stmt := range []string{"CREATE TABLE counter (n INTEGER)", "INSERT INTO counter VALUES (1)"} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	rows, err := db.Clauses(gormdb.Read).Raw("SELECT n FROM counter").Rows()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = rows.Close()
	}()

	var wg sync.WaitGroup
	errs := make([]error, 3)
	within(t, 5*time.Second, "Close", func() {
		for i := range errs {
			wg.Go(func() { errs[i] = gormdb.Close(db) })
		}
		wg.Wait()
	})
	for i, err := range errs {
		if err != nil {
			t.Errorf("Close %d: %v", i, err)
		}
	}
	if _, err := os.Stat(path + "-wal"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the -wal file survived Close: stat error %v", err)
	}
}

// A BusyTimeout SQLite cannot store (over 2^31-1 ms) must not become 0
// either: it is capped.
func hugeBusyTimeout(t *testing.T, drv gormdb.SqliteDriver) {
	db := open(t, drv, gormdb.SqliteCfg{Path: filepath.Join(t.TempDir(), "test.db"), BusyTimeout: 25 * 24 * time.Hour})
	var got string
	if err := db.Raw("PRAGMA busy_timeout").Scan(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got != "2147483647" {
		t.Errorf("busy_timeout: got %q, want 2147483647", got)
	}
}

// Setting up the read pool must not log library internals through the app's
// logger.
func quietOpen(t *testing.T, drv gormdb.SqliteDriver) {
	var buf bytes.Buffer
	log := logger.New(stdlog.New(&buf, "", 0), logger.Config{LogLevel: logger.Info})
	cfg := gormdb.Cfg{Driver: gormdb.DriverSqlite, Sqlite: gormdb.SqliteCfg{Path: filepath.Join(t.TempDir(), "test.db")}, Logger: log}
	db, err := gormdb.Open(cfg, gormdb.Drivers{Sqlite: drv})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := gormdb.Close(db); err != nil {
		t.Errorf("Close: %v", err)
	}
	if strings.Contains(buf.String(), "callback") {
		t.Errorf("Open logged callback set-up:\n%s", buf.String())
	}
}

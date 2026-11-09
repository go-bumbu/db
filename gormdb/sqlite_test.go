package gormdb_test

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-bumbu/db/gormdb"
	"github.com/go-bumbu/db/gormdb/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
	"gorm.io/plugin/dbresolver"
)

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
	rows, err := db.Clauses(gormdb.Read).Raw("SELECT n FROM t").Rows()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	err = gormdb.Close(db)
	if err == nil || !strings.Contains(err.Error(), "reader connections still open") {
		t.Errorf("Close: got %v, want an error about open reader connections", err)
	}
}

// failingDriver is glebarez failing to open a pool, so Open fails after it
// created the directory.
type failingDriver struct{ sqlite.Driver }

func (failingDriver) OpenDB(string) (*sql.DB, error) { return nil, errors.New("no pool") }

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
	if got := len(sharedReaderParams["_pragma"]); got != 2 {
		t.Errorf("Open changed the driver's own map: %d _pragma values, want 2", got)
	}
}

// noForeignKeysDriver is glebarez with foreign keys left off on the writer,
// breaking the SqliteDriver contract.
type noForeignKeysDriver struct{ sqlite.Driver }

func (d noForeignKeysDriver) WriterParams(timeout time.Duration) url.Values {
	q := d.Driver.WriterParams(timeout)
	q["_pragma"] = slices.DeleteFunc(q["_pragma"], func(p string) bool { return strings.HasPrefix(p, "foreign_keys") })
	return q
}

func TestOpenChecksForeignKeys(t *testing.T) {
	for _, path := range []string{filepath.Join(t.TempDir(), "test.db"), ":memory:"} {
		db, err := gormdb.Open(gormdb.Cfg{Driver: gormdb.DriverSqlite, Sqlite: gormdb.SqliteCfg{Path: path}}, gormdb.Drivers{Sqlite: noForeignKeysDriver{}})
		if err == nil {
			_ = gormdb.Close(db)
			t.Fatalf("%s: Open accepted a writer with foreign keys off", path)
		}
		if !strings.Contains(err.Error(), "foreign_keys is off") || !strings.Contains(err.Error(), "SqliteDriver") {
			t.Errorf("%s: got %v, want an error about foreign_keys naming the driver contract", path, err)
		}
	}
}

// The check must fail where the wrapper is not in effect: here dbresolver is
// registered without it, as a GORM version where the replacement lost to the
// original would leave it.
func TestCheckPinningDetectsMissingWrapper(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	open := func(dsn string) *sql.DB {
		t.Helper()
		pool, err := sqlite.Driver{}.OpenDB(dsn)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = pool.Close() })
		return pool
	}
	writer := open("file:" + path)
	writer.SetMaxOpenConns(1)
	db, err := gorm.Open(sqlite.Driver{}.Dialector(writer), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TABLE t (n INTEGER)").Error; err != nil {
		t.Fatal(err)
	}
	readers := open("file:" + path + "?mode=ro")
	if err := db.Use(dbresolver.Register(dbresolver.Config{Replicas: []gorm.Dialector{sqlite.Driver{}.Dialector(readers)}})); err != nil {
		t.Fatal(err)
	}
	err = gormdb.CheckPinning(db)
	if err == nil {
		t.Fatal("the check passed without the wrapper")
	}
	// The modules are named on every build; their versions only where the
	// binary carries build info for them (test binaries do from Go 1.27).
	wants := []string{"create, query, update, delete, row, raw", "not supported", "gorm.io/gorm ", "gorm.io/plugin/dbresolver "}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, m := range info.Deps {
			if (m.Path == "gorm.io/gorm" || m.Path == "gorm.io/plugin/dbresolver") && m.Replace == nil {
				wants = append(wants, m.Path+" "+m.Version)
			}
		}
	}
	for _, want := range wants {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestCheckPinningPassesAfterOpen(t *testing.T) {
	db, err := gormdb.Open(gormdb.Cfg{Driver: gormdb.DriverSqlite, Sqlite: gormdb.SqliteCfg{Path: filepath.Join(t.TempDir(), "test.db")}}, gormdb.Drivers{Sqlite: sqlite.Driver{}})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = gormdb.Close(db) }()
	if err := gormdb.CheckPinning(db); err != nil {
		t.Errorf("CheckPinning after Open: %v", err)
	}
}

// Cfg's GORM options reach gorm.Config.
func TestOpenPassesGormOptions(t *testing.T) {
	now := func() time.Time { return time.Unix(0, 0) }
	cfg := gormdb.Cfg{
		Driver:                 gormdb.DriverSqlite,
		Sqlite:                 gormdb.SqliteCfg{Path: ":memory:"},
		NamingStrategy:         schema.NamingStrategy{SingularTable: true},
		NowFunc:                now,
		SkipDefaultTransaction: true,
	}
	db, err := gormdb.Open(cfg, gormdb.Drivers{Sqlite: sqlite.Driver{}})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = gormdb.Close(db) }()
	if !db.SkipDefaultTransaction {
		t.Error("SkipDefaultTransaction not passed")
	}
	if db.NowFunc == nil || !db.NowFunc().Equal(now()) {
		t.Error("NowFunc not passed")
	}
	if got := db.NamingStrategy.TableName("Item"); got != "item" {
		t.Errorf("NamingStrategy not passed: table name %q, want item", got)
	}
}

func TestReaderDBAndPing(t *testing.T) {
	ctx := context.Background()
	file, err := gormdb.Open(gormdb.Cfg{Driver: gormdb.DriverSqlite, Sqlite: gormdb.SqliteCfg{Path: filepath.Join(t.TempDir(), "test.db"), Readers: 3}}, gormdb.Drivers{Sqlite: sqlite.Driver{}})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	readers, ok := gormdb.ReaderDB(file)
	if !ok {
		t.Fatal("ReaderDB: no read pool on a file database")
	}
	if got := readers.Stats().MaxOpenConnections; got != 3 {
		t.Errorf("reader pool MaxOpenConnections = %d, want 3", got)
	}
	if err := gormdb.Ping(ctx, file); err != nil {
		t.Errorf("Ping: %v", err)
	}
	if got := readers.Stats().OpenConnections; got != 1 {
		t.Errorf("Ping opened %d reader connections, want 1", got)
	}
	if err := gormdb.Close(file); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := gormdb.Ping(ctx, file); err == nil {
		t.Error("Ping after Close succeeded")
	}

	memory, err := gormdb.Open(gormdb.Cfg{Driver: gormdb.DriverSqlite, Sqlite: gormdb.SqliteCfg{Path: ":memory:"}}, gormdb.Drivers{Sqlite: sqlite.Driver{}})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = gormdb.Close(memory) }()
	if _, ok := gormdb.ReaderDB(memory); ok {
		t.Error("ReaderDB: a read pool on :memory:")
	}
	if err := gormdb.Ping(ctx, memory); err != nil {
		t.Errorf("Ping :memory:: %v", err)
	}
	if _, ok := gormdb.ReaderDB(nil); ok {
		t.Error("ReaderDB(nil) reported a pool")
	}
}

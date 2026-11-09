package gormdbtest_test

import (
	"bytes"
	"context"
	stdlog "log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-bumbu/db/gormdb"
	"github.com/go-bumbu/db/gormdb/gormdbtest"
	"github.com/go-bumbu/db/gormdb/sqlite"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

func TestSqlite(t *testing.T) {
	db := gormdbtest.Sqlite(t, sqlite.Driver{}, gormdb.Cfg{Sqlite: gormdb.SqliteCfg{Readers: 2}})
	if err := db.Exec("CREATE TABLE t (n INTEGER)").Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	// a file database: the read pool exists and is read-only
	if err := db.Clauses(gormdb.Read).Exec("INSERT INTO t VALUES (1)").Error; err == nil {
		t.Error("a write on the read pool succeeded; want a read-only file database")
	}
	readers, ok := gormdb.ReaderDB(db)
	if !ok {
		t.Fatal("no read pool")
	}
	if got := readers.Stats().MaxOpenConnections; got != 2 {
		t.Errorf("readers = %d, want the 2 passed in cfg", got)
	}
}

// The caller's Cfg options reach gorm.Config; Driver and Path are overridden.
func TestSqliteCfgOptions(t *testing.T) {
	var buf bytes.Buffer
	log := logger.New(stdlog.New(&buf, "", 0), logger.Config{LogLevel: logger.Info})
	now := time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)
	db := gormdbtest.Sqlite(t, sqlite.Driver{}, gormdb.Cfg{
		Driver:         gormdb.DriverPostgres,
		Sqlite:         gormdb.SqliteCfg{Path: "ignored.db"},
		Logger:         log,
		NamingStrategy: schema.NamingStrategy{TablePrefix: "app_"},
		NowFunc:        func() time.Time { return now },
	})
	type item struct {
		ID        uint
		CreatedAt time.Time
	}
	if err := db.AutoMigrate(&item{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	if err := db.Create(&item{}).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	var got item
	if err := db.Table("app_items").First(&got).Error; err != nil {
		t.Fatalf("read app_items: %v", err)
	}
	if !got.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt = %s, want NowFunc's %s", got.CreatedAt, now)
	}
	if !strings.Contains(buf.String(), "app_items") {
		t.Errorf("the caller's logger saw no statement:\n%s", buf.String())
	}
	if _, err := os.Stat("ignored.db"); !os.IsNotExist(err) {
		t.Errorf("Sqlite used the caller's Path: stat error %v", err)
	}
	if err := gormdb.Ping(context.Background(), db); err != nil {
		t.Errorf("Ping: %v", err)
	}
}

// Without a logger, Sqlite prints nothing: GORM's default logger writes to
// stdout (the os.Stdout of package init, so swapping os.Stdout cannot catch it).
func TestSqliteQuietByDefault(t *testing.T) {
	db := gormdbtest.Sqlite(t, sqlite.Driver{})
	if db.Logger != logger.Discard {
		t.Errorf("logger = %#v, want logger.Discard", db.Logger)
	}
}

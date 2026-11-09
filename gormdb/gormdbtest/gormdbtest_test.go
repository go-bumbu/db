package gormdbtest_test

import (
	"testing"

	"github.com/go-bumbu/db/gormdb"
	"github.com/go-bumbu/db/gormdb/gormdbtest"
	"github.com/go-bumbu/db/gormdb/sqlite"
	"gorm.io/plugin/dbresolver"
)

func TestSqlite(t *testing.T) {
	db := gormdbtest.Sqlite(t, sqlite.Driver{}, gormdb.SqliteCfg{Readers: 2})
	if err := db.Exec("CREATE TABLE t (n INTEGER)").Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	// a file database: the read pool exists and is read-only
	if err := db.Clauses(dbresolver.Read).Exec("INSERT INTO t VALUES (1)").Error; err == nil {
		t.Error("a write on the read pool succeeded; want a read-only file database")
	}
}

package gormdb_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-bumbu/db/gormdb"
)

func TestOpenMissingDriver(t *testing.T) {
	_, err := gormdb.Open(gormdb.Cfg{Driver: gormdb.DriverPostgres, Postgres: gormdb.PostgresCfg{Host: "h", DbName: "d"}}, gormdb.Drivers{})
	if err == nil || !strings.Contains(err.Error(), "no postgres driver was passed to Open") {
		t.Errorf("postgres: got error %v", err)
	}
	_, err = gormdb.Open(gormdb.Cfg{Driver: gormdb.DriverSqlite, Sqlite: gormdb.SqliteCfg{Path: filepath.Join(t.TempDir(), "x.db")}}, gormdb.Drivers{})
	if err == nil || !strings.Contains(err.Error(), "no sqlite driver was passed to Open") {
		t.Errorf("sqlite: got error %v", err)
	}
}

func TestOpenInvalidConfig(t *testing.T) {
	_, err := gormdb.Open(gormdb.Cfg{Driver: "mysql"}, gormdb.Drivers{})
	if err == nil || !strings.Contains(err.Error(), "unsupported driver") {
		t.Errorf("got error %v", err)
	}
}

// The core must never pull in a database driver: apps compile only the ones they import.
func TestCoreImportsNoDriver(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "github.com/go-bumbu/db/gormdb").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	banned := []string{
		"github.com/glebarez/", "modernc.org/sqlite", "github.com/mattn/go-sqlite3",
		"gorm.io/driver/", "github.com/jackc/pgx",
	}
	for _, line := range strings.Split(string(out), "\n") {
		for _, b := range banned {
			if strings.HasPrefix(line, b) {
				t.Errorf("core imports driver package %s", line)
			}
		}
	}
}

// Package integration_test runs gormdb against real databases in Docker
// (TESTDBS_ALL=1). It is a module of its own so the Docker and
// testcontainers dependencies stay out of the library's go.mod.
package integration_test

import (
	"errors"
	"os"
	"testing"

	"github.com/go-bumbu/db/gormdb"
	"github.com/go-bumbu/db/gormdb/postgres"
	"github.com/go-bumbu/testdbs"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func TestMain(m *testing.M) {
	testdbs.InitDBS()
	code := m.Run()
	_ = testdbs.Clean()
	os.Exit(code)
}

type parent struct {
	ID   uint   `gorm:"primaryKey"`
	Name string `gorm:"uniqueIndex;not null"`
}

type child struct {
	ID       uint `gorm:"primaryKey"`
	ParentID uint `gorm:"not null"`
	Parent   parent
}

func pgErrCode(t *testing.T, err error) string {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("error %v does not wrap *pgconn.PgError", err)
	}
	return pgErr.Code
}

// postgresProvider returns the Postgres started by testdbs, and skips the test
// when there is none.
func postgresProvider(t *testing.T) testdbs.ConnParamsProvider {
	t.Helper()
	var provider testdbs.ConnParamsProvider
	for _, target := range testdbs.DBs() {
		if target.DbType() != testdbs.DBTypePostgres {
			continue
		}
		if p, ok := target.(testdbs.ConnParamsProvider); ok {
			provider = p
		}
	}
	if provider == nil {
		t.Skip("Postgres runs with TESTDBS_ALL=1 (Docker)")
	}
	return provider
}

func TestOpenRealPostgres(t *testing.T) {
	p := postgresProvider(t).ConnParamsDbName(t.Name())
	cfg := gormdb.Cfg{Driver: gormdb.DriverPostgres, Postgres: gormdb.PostgresCfg{
		Host: p.Host, Port: p.Port, User: p.User, Password: p.Password, DbName: p.DbName, SslMode: p.SslMode,
		MaxOpenConns: 7, Params: map[string]string{"application_name": "gormdb test"},
	},
		Logger: gormlogger.Discard,
	}
	db, err := gormdb.Open(cfg, gormdb.Drivers{Postgres: postgres.Driver{}})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// Closed on every exit, so a t.Fatalf below does not leave the pool open
	// while the container is torn down. The test closes the database itself
	// at the end; the cleanup then has nothing left to do.
	closed := false
	t.Cleanup(func() {
		if closed {
			return
		}
		if err := gormdb.Close(db); err != nil {
			t.Errorf("Close: %v", err)
		}
	})

	var one int
	if err := db.Raw("SELECT 1").Scan(&one).Error; err != nil || one != 1 {
		t.Fatalf("SELECT 1 = %d, err %v", one, err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if got := sqlDB.Stats().MaxOpenConnections; got != 7 {
		t.Errorf("MaxOpenConnections = %d, want 7", got)
	}
	var app string
	if err := db.Raw("SELECT current_setting('application_name')").Scan(&app).Error; err != nil || app != "gormdb test" {
		t.Errorf("application_name = %q, err %v; want %q", app, err, "gormdb test")
	}

	if err := db.AutoMigrate(&parent{}, &child{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}

	if err := db.Create(&parent{Name: "a"}).Error; err != nil {
		t.Fatalf("create parent: %v", err)
	}
	err = db.Create(&parent{Name: "a"}).Error
	if !errors.Is(err, gorm.ErrDuplicatedKey) {
		t.Fatalf("duplicate: want ErrDuplicatedKey, got %v", err)
	}
	if code := pgErrCode(t, err); code != "23505" {
		t.Errorf("duplicate PgError code = %s, want 23505", code)
	}

	err = db.Exec("INSERT INTO children (parent_id) VALUES (9999)").Error
	if !errors.Is(err, gorm.ErrForeignKeyViolated) {
		t.Fatalf("fk: want ErrForeignKeyViolated, got %v", err)
	}
	if code := pgErrCode(t, err); code != "23503" {
		t.Errorf("fk PgError code = %s, want 23503", code)
	}

	closed = true
	if err := gormdb.Close(db); err != nil {
		t.Errorf("Close: %v", err)
	}
}

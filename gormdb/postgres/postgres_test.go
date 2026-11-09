package postgres_test

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

// TestDialector needs no Docker; it keeps package coverage above 70% in
// `make coverage`.
func TestDialector(t *testing.T) {
	if got := (postgres.Driver{}).Dialector("postgres://x/y").Name(); got != "postgres" {
		t.Errorf("Dialector name = %q, want postgres", got)
	}
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

func TestOpenRealPostgres(t *testing.T) {
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

	p := provider.ConnParamsDbName(t.Name())
	cfg := gormdb.Cfg{Driver: gormdb.DriverPostgres, Postgres: gormdb.PostgresCfg{
		Host: p.Host, Port: p.Port, User: p.User, Password: p.Password, DbName: p.DbName, SslMode: p.SslMode,
	},
		Logger: gormlogger.Discard,
	}
	db, err := gormdb.Open(cfg, gormdb.Drivers{Postgres: postgres.Driver{}})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	var one int
	if err := db.Raw("SELECT 1").Scan(&one).Error; err != nil || one != 1 {
		t.Fatalf("SELECT 1 = %d, err %v", one, err)
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

	if err := gormdb.Close(db); err != nil {
		t.Errorf("Close: %v", err)
	}
}

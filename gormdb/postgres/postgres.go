// Package postgres adapts the gorm.io/driver/postgres driver (pgx) to gormdb.
package postgres

import (
	"database/sql"

	"github.com/go-bumbu/db/gormdb"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Driver implements gormdb.PostgresDriver. The upstream dialector already
// translates constraint errors to gorm's sentinels while keeping the
// *pgconn.PgError, so no wrapper is needed.
//
// To build the pool differently (credentials that rotate, a tracing wrapper),
// embed Driver in a type of your own and replace OpenDB.
type Driver struct{}

var _ gormdb.PostgresDriver = Driver{}

// OpenDB returns a pgx connection pool for cfg.DSN(). It does not connect.
func (Driver) OpenDB(cfg gormdb.PostgresCfg) (*sql.DB, error) {
	return sql.Open("pgx", cfg.DSN())
}

// Dialector returns the gorm dialector over an open connection pool.
func (Driver) Dialector(conn gorm.ConnPool) gorm.Dialector {
	return gormpostgres.New(gormpostgres.Config{Conn: conn})
}

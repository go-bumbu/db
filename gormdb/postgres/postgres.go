// Package postgres adapts the gorm.io/driver/postgres driver (pgx) to gormdb.
package postgres

import (
	"github.com/go-bumbu/db/gormdb"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Driver implements gormdb.PostgresDriver. The upstream dialector already
// translates constraint errors to gorm's sentinels while keeping the
// *pgconn.PgError, so no wrapper is needed.
type Driver struct{}

var _ gormdb.PostgresDriver = Driver{}

// Dialector returns the gorm dialector for the given DSN.
func (Driver) Dialector(dsn string) gorm.Dialector { return gormpostgres.Open(dsn) }

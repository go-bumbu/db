package sqlite

import (
	"errors"

	gosqlite "github.com/glebarez/go-sqlite"
	"github.com/go-bumbu/db/gormdb/internal/sqliteerr"
)

// Translate maps SQLite constraint errors to gorm's sentinel errors. Unlike
// the upstream glebarez dialector, which returns the bare sentinel, it wraps
// the driver error too, so callers can still read which constraint failed.
// Any other error is returned unchanged.
func (dialector) Translate(err error) error {
	var sqliteErr *gosqlite.Error
	if !errors.As(err, &sqliteErr) {
		return err
	}
	return sqliteerr.Translate(err, sqliteErr.Code())
}

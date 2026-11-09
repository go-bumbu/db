package sqlitecgo

import (
	"errors"

	"github.com/go-bumbu/db/gormdb/internal/sqliteerr"
	"github.com/mattn/go-sqlite3"
)

// Translate maps SQLite constraint errors to gorm's sentinel errors. Unlike
// the upstream gorm sqlite dialector, which returns the bare sentinel, it
// wraps the driver error too, so callers can still read which constraint
// failed. Any other error is returned unchanged.
func (dialector) Translate(err error) error {
	var se sqlite3.Error
	if !errors.As(err, &se) {
		return err
	}
	return sqliteerr.Translate(err, int(se.ExtendedCode))
}

package sqlite

import (
	"errors"
	"fmt"

	gosqlite "github.com/glebarez/go-sqlite"
	"gorm.io/gorm"
	sqlitelib "modernc.org/sqlite/lib"
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
	var sentinel error
	// already translated: do not wrap the sentinel twice
	for _, s := range []error{gorm.ErrDuplicatedKey, gorm.ErrForeignKeyViolated, gorm.ErrCheckConstraintViolated} {
		if errors.Is(err, s) {
			return err
		}
	}
	switch sqliteErr.Code() {
	case sqlitelib.SQLITE_CONSTRAINT_UNIQUE, sqlitelib.SQLITE_CONSTRAINT_PRIMARYKEY:
		sentinel = gorm.ErrDuplicatedKey
	case sqlitelib.SQLITE_CONSTRAINT_FOREIGNKEY:
		sentinel = gorm.ErrForeignKeyViolated
	case sqlitelib.SQLITE_CONSTRAINT_CHECK:
		sentinel = gorm.ErrCheckConstraintViolated
	default:
		return err
	}
	return fmt.Errorf("%w: %w", sentinel, err)
}

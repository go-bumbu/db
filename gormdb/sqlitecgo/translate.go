package sqlitecgo

import (
	"errors"
	"fmt"

	"github.com/mattn/go-sqlite3"
	"gorm.io/gorm"
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
	var sentinel error
	// already translated: do not wrap the sentinel twice
	for _, s := range []error{gorm.ErrDuplicatedKey, gorm.ErrForeignKeyViolated, gorm.ErrCheckConstraintViolated} {
		if errors.Is(err, s) {
			return err
		}
	}
	switch se.ExtendedCode {
	case sqlite3.ErrConstraintUnique, sqlite3.ErrConstraintPrimaryKey:
		sentinel = gorm.ErrDuplicatedKey
	case sqlite3.ErrConstraintForeignKey:
		sentinel = gorm.ErrForeignKeyViolated
	case sqlite3.ErrConstraintCheck:
		sentinel = gorm.ErrCheckConstraintViolated
	default:
		return err
	}
	return fmt.Errorf("%w: %w", sentinel, err)
}

// Package sqliteerr maps SQLite constraint errors to gorm's sentinel errors.
// It is shared by the SQLite driver adapters, which only differ in how they
// read the extended result code from their driver's error type.
package sqliteerr

import (
	"errors"
	"fmt"

	"gorm.io/gorm"
)

// SQLite extended result codes (https://www.sqlite.org/rescode.html).
const (
	constraintCheck      = 275  // SQLITE_CONSTRAINT_CHECK
	constraintForeignKey = 787  // SQLITE_CONSTRAINT_FOREIGNKEY
	constraintPrimaryKey = 1555 // SQLITE_CONSTRAINT_PRIMARYKEY
	constraintUnique     = 2067 // SQLITE_CONSTRAINT_UNIQUE
)

// Translate wraps err, a SQLite error with the given extended result code, in
// the matching gorm sentinel, keeping err itself so callers can still read
// which constraint failed. An error that is already translated, or whose code
// has no sentinel, is returned unchanged.
func Translate(err error, extendedCode int) error {
	var sentinel error
	switch extendedCode {
	case constraintUnique, constraintPrimaryKey:
		sentinel = gorm.ErrDuplicatedKey
	case constraintForeignKey:
		sentinel = gorm.ErrForeignKeyViolated
	case constraintCheck:
		sentinel = gorm.ErrCheckConstraintViolated
	default:
		return err
	}
	if errors.Is(err, sentinel) {
		return err // already translated: do not wrap the sentinel twice
	}
	return fmt.Errorf("%w: %w", sentinel, err)
}

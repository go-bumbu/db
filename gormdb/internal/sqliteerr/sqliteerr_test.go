package sqliteerr

import (
	"errors"
	"testing"

	"gorm.io/gorm"
)

func TestTranslate(t *testing.T) {
	driverErr := errors.New("driver error")
	tests := []struct {
		name string
		code int
		want error // nil: returned unchanged
	}{
		{name: "unique", code: constraintUnique, want: gorm.ErrDuplicatedKey},
		{name: "primary key", code: constraintPrimaryKey, want: gorm.ErrDuplicatedKey},
		{name: "foreign key", code: constraintForeignKey, want: gorm.ErrForeignKeyViolated},
		{name: "check", code: constraintCheck, want: gorm.ErrCheckConstraintViolated},
		{name: "busy is not a constraint", code: 5},              // SQLITE_BUSY
		{name: "not null has no sentinel", code: 1299},           // SQLITE_CONSTRAINT_NOTNULL
		{name: "base constraint code has no sentinel", code: 19}, // SQLITE_CONSTRAINT
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Translate(driverErr, tc.code)
			if tc.want == nil {
				if got != driverErr {
					t.Errorf("got %v, want the driver error unchanged", got)
				}
				return
			}
			if !errors.Is(got, tc.want) {
				t.Errorf("got %v, want it to wrap %v", got, tc.want)
			}
			if !errors.Is(got, driverErr) {
				t.Errorf("got %v, want it to keep the driver error", got)
			}
		})
	}
}

// Translating twice must not wrap the sentinel twice.
func TestTranslateIsIdempotent(t *testing.T) {
	once := Translate(errors.New("driver error"), constraintUnique)
	if twice := Translate(once, constraintUnique); twice != once {
		t.Errorf("second Translate changed the error: %v", twice)
	}
}

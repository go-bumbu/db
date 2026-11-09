package gormdb

import (
	"time"

	"gorm.io/gorm"
)

// SetReaderDrainTimeout shortens Close's wait for running reads in a test and
// returns a function that restores it.
func SetReaderDrainTimeout(d time.Duration) (restore func()) {
	old := readerDrainTimeout
	readerDrainTimeout = d
	return func() { readerDrainTimeout = old }
}

// CheckPinning runs Open's check that the pinned-connection wrapper is in
// effect on db, with a connection of db's own pool.
func CheckPinning(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return checkPinning(db, sqlDB)
}

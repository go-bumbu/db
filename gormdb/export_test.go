package gormdb

import "time"

// SetReaderDrainTimeout shortens Close's wait for running reads in a test and
// returns a function that restores it.
func SetReaderDrainTimeout(d time.Duration) (restore func()) {
	old := readerDrainTimeout
	readerDrainTimeout = d
	return func() { readerDrainTimeout = old }
}

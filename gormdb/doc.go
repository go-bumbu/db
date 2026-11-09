// Package gormdb opens a GORM connection to the database selected by
// configuration, so the code above it stays driver-agnostic: business modules
// take a *gorm.DB and never know whether SQLite or Postgres is behind it.
//
// The core package imports no database driver. The application picks the
// drivers it wants (see the driver subpackages) and passes them to Open, so
// the binary only links the databases it actually uses.
package gormdb

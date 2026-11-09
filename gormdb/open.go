package gormdb

import (
	"database/sql"
	"errors"
	"fmt"

	"gorm.io/gorm"
)

// Open validates cfg and opens the configured database with the matching
// driver from drivers; the driver for the other database may be nil.
//
// GORM's TranslateError is on, so callers check
// errors.Is(err, gorm.ErrDuplicatedKey) and
// errors.Is(err, gorm.ErrForeignKeyViolated) regardless of the database.
// Release the database with Close.
//
// SQLite runs a single write connection plus a pool of read-only ones (see
// SqliteCfg.Readers), in WAL mode. Writers queue in Go instead of exhausting
// SQLite's busy timeout; a read does not wait for a write, only for a free
// reader (more concurrent reads than Readers queue in Go). Statements are
// routed by GORM's dbresolver: queries GORM builds (Table, Select, Find, ...)
// and raw SQL that begins with SELECT go to the read pool; everything else
// (Exec, Create, Update, PRAGMA, WITH ..., other raw SQL, a SELECT of 10
// characters or fewer, SELECT ... FOR UPDATE) goes to the writer.
// db.Clauses(dbresolver.Read) forces a statement onto the read pool,
// db.Clauses(dbresolver.Write) onto the writer. SQL that depends on the
// connection (SELECT last_insert_rowid(), changes(), temp tables) must run
// with db.Clauses(dbresolver.Write), or in a transaction, because a bare
// SELECT lands on another, read-only connection.
//
// Inside db.Transaction, use only tx: the single writer connection is held by
// the transaction, so a statement on db that reaches the writer waits for it
// forever (a deadlock). For ":memory:", which uses one connection for
// everything and has no read pool, any statement on db inside a transaction
// deadlocks, reads included, though the same code works on a file database.
//
// WAL does not work on network filesystems: keep the database on a local
// disk. Copying a database file also needs its -wal file, or the latest
// writes are lost; close the database first for a self-contained file.
func Open(cfg Cfg, drivers Drivers) (*gorm.DB, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	gcfg := &gorm.Config{Logger: cfg.Logger, TranslateError: true}

	switch cfg.Driver {
	case DriverPostgres:
		if drivers.Postgres == nil {
			return nil, missingDriver(cfg.Driver)
		}
		db, err := gorm.Open(drivers.Postgres.Dialector(cfg.Postgres.DSN()), gcfg)
		if err != nil {
			return nil, fmt.Errorf("open postgres: %w", err)
		}
		return db, nil
	default: // DriverSqlite, Validate rejected anything else
		if drivers.Sqlite == nil {
			return nil, missingDriver(cfg.Driver)
		}
		return openSqlite(cfg.Sqlite, drivers.Sqlite, gcfg)
	}
}

func missingDriver(driver string) error {
	return fmt.Errorf("gormdb: config selects %q but no %s driver was passed to Open", driver, driver)
}

// Close releases the database's connections, the read-only pool included. It
// is safe to call more than once. Always release through Close:
// db.DB().Close() leaks the reader pool and its registry entry.
func Close(db *gorm.DB) error {
	if db == nil {
		return errors.New("gormdb: Close of a nil database")
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	// The readers close first: while one is open, closing the writer skips the
	// WAL checkpoint and leaves the data in the -wal file. LoadAndDelete decides
	// who closes the readers, so concurrent or repeated calls close them once.
	if readers, ok := readerPools.LoadAndDelete(sqlDB); ok {
		err = readers.(*sql.DB).Close()
	}
	return errors.Join(err, sqlDB.Close())
}

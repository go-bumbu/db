package gormdb

import (
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
// with db.Clauses(dbresolver.Write), in a transaction, or in db.Connection,
// because a bare SELECT lands on another, read-only connection. Inside
// db.Transaction and db.Connection every statement on tx stays on the
// connection they hold, the writer, reads included.
//
// Inside db.Transaction or db.Connection, use only tx: the single writer
// connection is held by the callback, so a statement on db that reaches the
// writer waits for it forever (a deadlock). For ":memory:", which uses one
// connection for everything and has no read pool, any statement on db inside
// the callback deadlocks, reads included, though the same code works on a
// file database.
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
// is safe to call more than once, and from several goroutines. Always release
// through Close: db.DB().Close() leaves the reader pool open.
//
// For a SQLite file database Close closes the readers first and waits for
// reads still running (up to 5s, in case rows are never closed) before it
// closes the writer, so the writer can checkpoint and remove the -wal file.
func Close(db *gorm.DB) error {
	if db == nil {
		return errors.New("gormdb: Close of a nil database")
	}
	if pools, ok := db.Plugins[sqlitePoolsName].(*sqlitePools); ok {
		return pools.close()
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

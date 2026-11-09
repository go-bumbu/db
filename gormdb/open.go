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
// reader (more concurrent reads than Readers queue in Go). db.DB() returns
// the writer pool: do not change its limits (SetMaxOpenConns above 1 brings
// SQLITE_BUSY back), and statements sent through it all go to the writer.
//
// Statements are routed by GORM's dbresolver, by their SQL text rather than
// the method that sends them: queries GORM builds (Table, Select, Find, ...)
// and raw SQL that begins with SELECT, from Raw and Exec alike, go to the read
// pool; everything else (Create, Update, Delete, PRAGMA, WITH ..., other raw
// SQL, a SELECT of 10 characters or fewer, SELECT ... FOR UPDATE) goes to the
// writer. db.Clauses(dbresolver.Read) forces a statement onto the read pool,
// db.Clauses(dbresolver.Write) onto the writer. SQL that depends on the
// connection (SELECT last_insert_rowid(), changes(), temp tables) must run in
// a transaction or in db.Connection, because a bare SELECT lands on another,
// read-only connection; db.Clauses(dbresolver.Write) reaches the writer too,
// but another goroutine may write in between. For a new row's ID prefer
// Create, which reads it back, or INSERT ... RETURNING. Inside db.Transaction
// and db.Connection every statement on tx stays on the connection they hold,
// the writer, reads included.
//
// Inside db.Transaction or db.Connection, use only tx: the single writer
// connection is held by the callback, so a statement on db that reaches the
// writer waits for it forever (a deadlock).
//
// ":memory:" uses one connection for everything and has no read pool, so it
// behaves differently from a file database: routing is never exercised, any
// statement on db inside a transaction deadlocks (reads included), and so
// does a write while rows from a query are still open; connection-dependent
// SQL works where it would fail on a file. Test against a file in a
// temporary directory (gormdbtest.Sqlite) rather than ":memory:".
//
// WAL does not work on network filesystems: keep the database on a local
// disk. Copying a database file also needs its -wal file, or the latest
// writes are lost; close the database first for a self-contained file.
//
// Postgres gets PostgresCfg's pool settings, and a connect timeout so Open
// fails fast against an unreachable host.
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
		return openPostgres(cfg.Postgres, drivers.Postgres, gcfg)
	default: // DriverSqlite, Validate rejected anything else
		if drivers.Sqlite == nil {
			return nil, missingDriver(cfg.Driver)
		}
		return openSqlite(cfg.Sqlite, drivers.Sqlite, gcfg)
	}
}

// openPostgres opens the database and applies the pool settings.
func openPostgres(cfg PostgresCfg, drv PostgresDriver, gcfg *gorm.Config) (*gorm.DB, error) {
	db, err := gorm.Open(drv.Dialector(cfg.DSN()), gcfg)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	if cfg.MaxOpenConns > 0 {
		sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns > 0 {
		sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	}
	if cfg.ConnMaxLifetime > 0 {
		sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	}
	return db, nil
}

func missingDriver(driver string) error {
	return fmt.Errorf("gormdb: config selects %q but no %s driver was passed to Open", driver, driver)
}

// Close releases the database's connections, the read-only pool included. It
// is safe to call more than once, and from several goroutines. Always release
// through Close: db.DB().Close() leaves the reader pool open.
//
// For a SQLite file database Close closes the readers first and waits for
// reads still running before it closes the writer, so the writer can
// checkpoint and remove the -wal file. It waits up to 5s: a read still
// running then (rows never closed) is reported as an error, and the -wal
// file is left next to the database, which still holds every write.
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

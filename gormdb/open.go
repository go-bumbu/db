package gormdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

// Read and Write force a statement onto the read pool or the writer of a
// SQLite file database: db.Clauses(gormdb.Read).Raw(...) or
// db.Clauses(gormdb.Write).Find(...). They are dbresolver's clauses, so apps
// need not import it; on Postgres and ":memory:" they change nothing.
const (
	Read  = dbresolver.Read
	Write = dbresolver.Write
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
// Routing, the contract: a plain SELECT goes to the read pool, everything
// else to the writer. That holds for the queries GORM builds (Table, Select,
// Find, ...) and for raw SQL alike, from Raw or Exec: routing goes by the SQL
// text, not by the method that sends it. db.Clauses(gormdb.Read) forces a
// statement onto the read pool, db.Clauses(gormdb.Write) onto the writer.
// Inside db.Transaction and db.Connection every statement on tx stays on the
// connection they hold, the writer, reads included.
//
// The routing is GORM's dbresolver; how it tells a plain SELECT apart is its
// behaviour at the version gormdb is tested with, not a gormdb promise: raw
// SQL is a read when it begins with SELECT, is longer than 10 characters and
// does not end in FOR UPDATE, and a built query with a FOR clause (FOR
// UPDATE) goes to the writer. WITH ..., PRAGMA and other raw SQL go to the
// writer, the single connection, also when they only read. Force a
// statement with gormdb.Read or gormdb.Write where the guess matters.
//
// SQL that depends on the connection (SELECT last_insert_rowid(), changes(),
// temp tables) must run in a transaction or in db.Connection, because a bare
// SELECT lands on another, read-only connection; db.Clauses(gormdb.Write)
// reaches the writer too, but another goroutine may write in between. For a
// new row's ID prefer Create, which reads it back, or INSERT ... RETURNING.
//
// Inside db.Transaction or db.Connection, use only tx: the single writer
// connection is held by the callback, so a statement on db that reaches the
// writer waits for it forever (a deadlock).
//
// On a SQLite file database the dbresolver plugin slot (gorm:db_resolver) is
// taken by the read pool, so an app's own db.Use(dbresolver.Register(...))
// returns gorm.ErrRegistered. Open checks that its handling of db.Connection
// is in effect with the linked GORM and dbresolver versions, and fails with
// an error naming them if not.
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
	gcfg := &gorm.Config{
		Logger:                 cfg.Logger,
		TranslateError:         true,
		NamingStrategy:         cfg.NamingStrategy,
		NowFunc:                cfg.NowFunc,
		SkipDefaultTransaction: cfg.SkipDefaultTransaction,
	}

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

// openPostgres opens the pool through the driver, applies the pool settings
// and hands the pool to GORM. It owns the pool until Open succeeds, so a
// failed open closes it here, whatever the GORM version does.
func openPostgres(cfg PostgresCfg, drv PostgresDriver, gcfg *gorm.Config) (*gorm.DB, error) {
	pool, err := drv.OpenDB(cfg)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	if pool == nil {
		return nil, errors.New("open postgres: the driver's OpenDB returned no pool")
	}
	if cfg.MaxOpenConns > 0 {
		pool.SetMaxOpenConns(cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns > 0 {
		pool.SetMaxIdleConns(cfg.MaxIdleConns)
	}
	if cfg.ConnMaxLifetime > 0 {
		pool.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	}
	db, err := gorm.Open(drv.Dialector(pool), gcfg)
	if err != nil {
		_ = pool.Close() // a second Close of a sql.DB is harmless
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	return db, nil
}

func missingDriver(driver string) error {
	return fmt.Errorf("gormdb: config selects %q but no %s driver was passed to Open", driver, driver)
}

// ReaderDB returns the read-only pool of a SQLite file database opened by
// Open, for its DBStats; ok is false for Postgres and ":memory:", which have
// none. Do not change its limits or close it: Close releases it.
func ReaderDB(db *gorm.DB) (readers *sql.DB, ok bool) {
	if db == nil {
		return nil, false
	}
	pools, ok := db.Plugins[sqlitePoolsName].(*sqlitePools)
	if !ok {
		return nil, false
	}
	return pools.readers, true
}

// Ping checks that the database is reachable: the writer pool (db.DB()) and,
// for a SQLite file database, the read pool, one connection of each. The
// SQLite writer is a single connection, so Ping waits, within ctx, for a
// transaction in progress.
func Ping(ctx context.Context, db *gorm.DB) error {
	if db == nil {
		return errors.New("gormdb: Ping of a nil database")
	}
	writer, err := db.DB()
	if err != nil {
		return err
	}
	if err := writer.PingContext(ctx); err != nil {
		return fmt.Errorf("gormdb: ping the writer: %w", err)
	}
	if readers, ok := ReaderDB(db); ok {
		if err := readers.PingContext(ctx); err != nil {
			return fmt.Errorf("gormdb: ping the readers: %w", err)
		}
	}
	return nil
}

// Close releases the database's connections, the read-only pool included. It
// is safe to call more than once, and from several goroutines. Always release
// through Close: db.DB() is the writer pool only, so db.DB().Close() leaves
// the reader pool open; its limits must not be changed either (see Open).
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

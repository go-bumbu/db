package gormdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	sqlitePoolsName = "gormdb:sqlite_pools"
	resolverName    = "gorm:db_resolver"
)

// readerDrainTimeout bounds how long Close waits for reads still running, so
// rows a caller never closes cannot hang it. A variable so tests can shorten it.
var readerDrainTimeout = 5 * time.Second

// sqlitePools is a GORM plugin on a SQLite file database. It holds the writer
// and read-only pools, so Close finds and releases them in order without any
// global state, and it keeps a connection pinned with db.Connection on that
// connection.
type sqlitePools struct {
	writer, readers *sql.DB
	mu              sync.Mutex // serialises close
}

// Name implements gorm.Plugin.
func (*sqlitePools) Name() string { return sqlitePoolsName }

// Initialize implements gorm.Plugin. dbresolver only leaves a statement where
// it is when it runs in a transaction (*sql.Tx); the *sql.Conn that
// db.Connection pins would be swapped back to a pool. On the writer pool that
// waits forever for the very connection the callback holds, and a read would
// not see the pinned connection's temp tables. Its callbacks are wrapped to
// skip a pinned connection, so everything inside db.Connection stays on it.
//
// The wrapping relies on dbresolver's callback name and on GORM running the
// replacement in place of the original, so Initialize then checks it is in
// effect (checkPinning): a GORM or dbresolver version where it silently stops
// working fails Open instead of hanging inside db.Connection later.
//
// Replacing a callback logs an Info line through the app's logger; it is
// silenced while the plugin is set up, as it says nothing about the app.
func (p *sqlitePools) Initialize(db *gorm.DB) error {
	saved := db.Logger
	db.Logger = logger.Discard
	defer func() { db.Logger = saved }()
	if err := wrapResolver(db); err != nil {
		return err
	}
	return checkPinning(db, p.writer)
}

// wrapResolver replaces dbresolver's callback on every processor with one
// that skips a statement pinned to a *sql.Conn.
func wrapResolver(db *gorm.DB) error {
	cb := db.Callback()
	wrap := func(get func(string) func(*gorm.DB), replace func(string, func(*gorm.DB)) error) error {
		resolve := get(resolverName)
		if resolve == nil {
			return fmt.Errorf("callback %s is not registered", resolverName)
		}
		return replace(resolverName, func(tx *gorm.DB) {
			if _, pinned := tx.Statement.ConnPool.(*sql.Conn); pinned {
				return
			}
			resolve(tx)
		})
	}
	// The replacement is declared Before("*") like dbresolver's own callback:
	// GORM sorts "*" callbacks after the others and then takes the last
	// callback of a name, so a plain Replace would lose to the original.
	return errors.Join(
		wrap(cb.Create().Get, cb.Create().Before("*").Replace),
		wrap(cb.Query().Get, cb.Query().Before("*").Replace),
		wrap(cb.Update().Get, cb.Update().Before("*").Replace),
		wrap(cb.Delete().Get, cb.Delete().Before("*").Replace),
		wrap(cb.Row().Get, cb.Row().Before("*").Replace),
		wrap(cb.Raw().Get, cb.Raw().Before("*").Replace),
	)
}

// executor is a GORM callback processor (db.Callback().Create(), ...).
type executor interface {
	Execute(*gorm.DB) *gorm.DB
}

// errPinCheck marks checkPinning's statements: GORM's own callbacks skip a
// statement that carries an error, while the resolver callbacks do not look
// at it, so only they do anything.
var errPinCheck = errors.New("gormdb: pinned-connection check")

// checkPinning runs each processor's callbacks, as compiled, on a statement
// pinned to a connection of pool, the writer, and reports the processors that
// moved it off that connection: on those the wrapper is not the resolver
// callback in effect. Nothing reaches the database: the statements carry
// errPinCheck and run in dry-run mode, without GORM's default transaction.
// A control first checks that a query not pinned moves to the read pool, so
// a GORM that stopped running callbacks on such a statement fails the check
// instead of passing it.
func checkPinning(db *gorm.DB, pool *sql.DB) error {
	conn, err := pool.Conn(context.Background())
	if err != nil {
		return fmt.Errorf("gormdb: check the pinned-connection wrapper: %w", err)
	}
	defer func() { _ = conn.Close() }()

	run := func(proc executor, connPool gorm.ConnPool) gorm.ConnPool {
		tx := db.Session(&gorm.Session{DryRun: true, SkipDefaultTransaction: true, NewDB: true, Logger: logger.Discard}).
			Table("gormdb_pin_check") // a new statement, so db's own is not touched
		tx.Statement.ConnPool = connPool
		tx.Error = errPinCheck
		return proc.Execute(tx).Statement.ConnPool
	}
	unsupported := func(problem string) error {
		return fmt.Errorf("gormdb: %s; this GORM or dbresolver version is not supported (in use: %s; gormdb is tested with the versions in its go.mod)",
			problem, depVersions())
	}

	cb := db.Callback()
	if run(cb.Query(), pool) == gorm.ConnPool(pool) {
		return unsupported("dbresolver did not route a query to the read pool while Open checked its callbacks")
	}
	var moved []string
	for _, p := range []struct {
		name string
		proc executor
	}{
		{"create", cb.Create()}, {"query", cb.Query()}, {"update", cb.Update()},
		{"delete", cb.Delete()}, {"row", cb.Row()}, {"raw", cb.Raw()},
	} {
		if run(p.proc, conn) != gorm.ConnPool(conn) {
			moved = append(moved, p.name)
		}
	}
	if len(moved) > 0 {
		return unsupported("the wrapper that keeps db.Connection on its connection is not in effect for the " +
			strings.Join(moved, ", ") + " callbacks")
	}
	return nil
}

// depVersions returns the GORM and dbresolver versions linked in the binary.
func depVersions() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	var vs []string
	for _, m := range info.Deps {
		if m.Path != "gorm.io/gorm" && m.Path != "gorm.io/plugin/dbresolver" {
			continue
		}
		if m.Replace != nil {
			m = m.Replace
		}
		vs = append(vs, m.Path+" "+m.Version)
	}
	if len(vs) == 0 {
		return "unknown"
	}
	return strings.Join(vs, ", ")
}

// close closes the readers, waits for reads still running, then closes the
// writer: while a reader connection is open, closing the writer skips the WAL
// checkpoint and leaves the data in the -wal file. A read still running after
// readerDrainTimeout (rows never closed) is reported as an error, with the
// writer closed anyway and the -wal file left behind; the data is safe as
// long as that file stays next to the database. Concurrent and repeated calls
// run one after the other; sql.DB.Close is idempotent.
func (p *sqlitePools) close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	err := p.readers.Close()
	// sql.DB.Close does not wait for connections running a query: they close
	// when the query ends.
	deadline := time.Now().Add(readerDrainTimeout)
	for p.readers.Stats().OpenConnections > 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if n := p.readers.Stats().OpenConnections; n > 0 {
		err = errors.Join(err, fmt.Errorf("gormdb: %d reader connections still open after %s (rows not closed?); the WAL was not checkpointed", n, readerDrainTimeout))
	}
	return errors.Join(err, p.writer.Close())
}

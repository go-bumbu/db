package gormdb

import (
	"database/sql"
	"errors"
	"fmt"
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
// Replacing a callback logs an Info line through the app's logger; it is
// silenced while the plugin is set up, as it says nothing about the app.
func (*sqlitePools) Initialize(db *gorm.DB) error {
	saved := db.Logger
	db.Logger = logger.Discard
	defer func() { db.Logger = saved }()
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

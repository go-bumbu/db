package gormdb

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

// readerDSN returns the DSN of a read-only connection to the database file at
// path. It is always a file: URI, as only a URI can say mode=ro, so path is
// escaped: characters such as '?', '#' and '%' mean something in a URI.
// params are the driver's reader parameters; they are copied, not changed,
// as a driver may hand out a shared map.
func readerDSN(path string, params url.Values) string {
	q := mergeParams(params)
	q.Set("mode", "ro")
	return fileDSN(path, q)
}

// mergeParams returns a new map holding the values of every set, in order. The
// sets are not changed: a driver may hand out a shared map.
func mergeParams(sets ...url.Values) url.Values {
	q := url.Values{}
	for _, set := range sets {
		for k, v := range set {
			q[k] = append(q[k], v...)
		}
	}
	return q
}

// fileDSN builds a file: URI with an escaped path.
func fileDSN(path string, params url.Values) string {
	// Clean: a leading "//" would otherwise read as a URI authority
	p := filepath.Clean(path)
	if p == ":memory:" {
		// "./:memory:" is a file named ":memory:"; once cleaned, SQLite would
		// read it as an in-memory database
		p = "./" + p
	}
	return "file:" + (&url.URL{Path: p}).EscapedPath() + "?" + params.Encode()
}

// isMemory reports whether path names an in-memory database.
func isMemory(path string) bool { return path == ":memory:" }

// busyTimeout returns the busy timeout to use for cfg: the default for zero,
// otherwise kept between 1ms and maxBusyTimeout, since SQLite counts in whole
// milliseconds and both 0 and a value it cannot store turn waiting off.
func busyTimeout(cfg SqliteCfg) time.Duration {
	if cfg.BusyTimeout == 0 {
		return defaultBusyTimeout
	}
	return min(max(cfg.BusyTimeout, time.Millisecond), maxBusyTimeout)
}

// mkdirAll creates dir, an absolute path, and its missing parents like
// os.MkdirAll. The function it returns removes the directories it created, so
// a failed Open leaves nothing behind; they hold only that Open's files.
func mkdirAll(dir string) (undo func(), err error) {
	undo = func() {}
	top := "" // the topmost directory that does not exist yet
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(d); err == nil {
			break
		}
		top = d
		if d == filepath.Dir(d) {
			break
		}
	}
	if top != "" {
		undo = func() { _ = os.RemoveAll(top) }
	}
	return undo, os.MkdirAll(dir, 0o750)
}

// openSqlite opens the writer connection (one pooled connection, WAL and
// immediate transactions) and, for a file database, a pool of read-only
// connections that dbresolver routes reads to.
func openSqlite(cfg SqliteCfg, drv SqliteDriver, gcfg *gorm.Config) (db *gorm.DB, err error) {
	timeout := busyTimeout(cfg)
	memory := isMemory(cfg.Path)

	// The pragmas go in the DSN rather than in db.Exec: most pragmas are
	// per-connection state, and an Exec runs on a single pooled connection,
	// leaving the others at their defaults.
	//   - busy_timeout: wait for a lock instead of failing with SQLITE_BUSY at
	//     once under write contention.
	//   - journal_mode=WAL: readers do not block the writer and vice versa.
	//   - foreign_keys: SQLite ignores foreign keys unless asked to; Postgres
	//     always enforces them, so both drivers behave the same.
	//   - _txlock=immediate: a transaction takes the write lock when it begins,
	//     so a read-then-write transaction waits for the lock (honouring
	//     busy_timeout) instead of failing with SQLITE_BUSY when it upgrades.
	//   - SqliteCfg.Pragmas: the app's own, on the readers too. mattn has no
	//     DSN parameter for an arbitrary pragma; its adapter runs them from a
	//     connect hook, which is why the driver opens the pool (OpenDB).
	//
	// A file database is addressed by a file: URI with an escaped path, like
	// the readers', so '?' or '#' in a folder name do not cut the path short.
	pragmas := drv.PragmaParams(cfg.Pragmas)
	params := mergeParams(drv.WriterParams(timeout), pragmas)
	path := cfg.Path
	var dsn string
	if memory {
		dsn = ":memory:?" + params.Encode()
	} else {
		// Resolved once, here: connections open lazily, so a relative path
		// would be resolved against the working directory of the moment, and
		// a reader or a reconnecting writer would reach another file after an
		// os.Chdir.
		if path, err = filepath.Abs(path); err != nil {
			return nil, fmt.Errorf("sqlite: resolve path %s: %w", cfg.Path, err)
		}
		var undo func()
		undo, err = mkdirAll(filepath.Dir(path)) // err is the named result, seen by the defer
		defer func() {
			if err != nil {
				undo()
			}
		}()
		if err != nil {
			return nil, fmt.Errorf("create sqlite directory: %w", err)
		}
		dsn = fileDSN(path, params)
	}

	writer, err := drv.OpenDB(dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", cfg.Path, err)
	}
	// one write connection: writers queue in Go instead of exhausting
	// SQLite's busy timeout
	writer.SetMaxOpenConns(1)

	db, err = gorm.Open(drv.Dialector(writer), gcfg)
	if err != nil {
		_ = writer.Close()
		return nil, fmt.Errorf("open sqlite %s: %w", cfg.Path, err)
	}
	if err = checkWriter(writer, memory); err != nil {
		_ = writer.Close()
		return nil, err
	}
	if !memory {
		readerParams := mergeParams(drv.ReaderParams(timeout), pragmas)
		if err = addReaders(db, writer, path, cfg.Readers, drv, readerParams); err != nil {
			_ = writer.Close()
			return nil, err
		}
	}
	return db, nil
}

// checkWriter verifies what the SqliteDriver contract promises on the writer:
// WAL for a file database, and foreign keys on.
func checkWriter(writer *sql.DB, memory bool) error {
	if !memory {
		var mode string
		if err := writer.QueryRow("PRAGMA journal_mode=WAL").Scan(&mode); err != nil {
			return fmt.Errorf("sqlite: turn on WAL: %w", err)
		}
		if !strings.EqualFold(mode, "wal") {
			return fmt.Errorf("sqlite: turn on WAL: the journal mode stayed %q", mode)
		}
	}
	var fk int
	if err := writer.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil {
		return fmt.Errorf("sqlite: read foreign_keys: %w", err)
	}
	if fk != 1 {
		return errors.New("sqlite: foreign_keys is off on the writer: the SqliteDriver's WriterParams must turn it on (see gormdb.SqliteDriver)")
	}
	return nil
}

// addReaders opens the read-only pool on the database file at path, an
// absolute path, and routes reads to it.
func addReaders(db *gorm.DB, writer *sql.DB, path string, n int, drv SqliteDriver, params url.Values) error {
	readers, err := drv.OpenDB(readerDSN(path, params))
	if err != nil {
		return fmt.Errorf("open sqlite readers %s: %w", path, err)
	}
	if n == 0 {
		n = defaultSqliteReaders
	}
	readers.SetMaxOpenConns(n)
	readers.SetMaxIdleConns(n)
	// The pool is made here and handed over because the plugin's own
	// SetMaxOpenConns reaches its source as well: it would lift the writer's
	// limit.
	err = db.Use(dbresolver.Register(dbresolver.Config{
		Replicas: []gorm.Dialector{drv.Dialector(readers)},
	}))
	if err == nil {
		// registered after dbresolver, so its callbacks can wrap dbresolver's
		err = db.Use(&sqlitePools{writer: writer, readers: readers})
	}
	if err != nil {
		_ = readers.Close()
		return fmt.Errorf("sqlite: set up the read pool: %w", err)
	}
	return nil
}

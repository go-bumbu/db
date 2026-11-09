package gormdb

import (
	"database/sql"
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
// params are the driver's reader parameters; readerDSN adds mode=ro to them.
func readerDSN(path string, params url.Values) string {
	params.Set("mode", "ro")
	return fileDSN(path, params)
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
// and at least a millisecond, since SQLite counts in whole milliseconds and 0
// turns waiting off.
func busyTimeout(cfg SqliteCfg) time.Duration {
	switch {
	case cfg.BusyTimeout == 0:
		return defaultBusyTimeout
	case cfg.BusyTimeout < time.Millisecond:
		return time.Millisecond
	default:
		return cfg.BusyTimeout
	}
}

// openSqlite opens the writer connection (one pooled connection, WAL and
// immediate transactions) and, for a file database, a pool of read-only
// connections that dbresolver routes reads to.
func openSqlite(cfg SqliteCfg, drv SqliteDriver, gcfg *gorm.Config) (*gorm.DB, error) {
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
	//
	// A file database is addressed by a file: URI with an escaped path, like
	// the readers', so '?' or '#' in a folder name do not cut the path short.
	params := drv.Params(timeout, true)
	var dsn string
	if memory {
		dsn = ":memory:?" + params.Encode()
	} else {
		if err := os.MkdirAll(filepath.Dir(cfg.Path), 0o750); err != nil {
			return nil, fmt.Errorf("create sqlite directory: %w", err)
		}
		dsn = fileDSN(cfg.Path, params)
	}

	writer, err := sql.Open(drv.SQLDriverName(), dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", cfg.Path, err)
	}
	// one write connection: writers queue in Go instead of exhausting
	// SQLite's busy timeout
	writer.SetMaxOpenConns(1)

	db, err := gorm.Open(drv.Dialector(writer), gcfg)
	if err != nil {
		_ = writer.Close()
		return nil, fmt.Errorf("open sqlite %s: %w", cfg.Path, err)
	}

	if !memory {
		var mode string
		if err := writer.QueryRow("PRAGMA journal_mode=WAL").Scan(&mode); err != nil {
			_ = writer.Close()
			return nil, fmt.Errorf("sqlite: turn on WAL: %w", err)
		}
		if !strings.EqualFold(mode, "wal") {
			_ = writer.Close()
			return nil, fmt.Errorf("sqlite: turn on WAL: the journal mode stayed %q", mode)
		}
		if err := addReaders(db, writer, cfg, drv, timeout); err != nil {
			_ = writer.Close()
			return nil, err
		}
	}
	return db, nil
}

// addReaders opens the read-only pool and routes reads to it.
func addReaders(db *gorm.DB, writer *sql.DB, cfg SqliteCfg, drv SqliteDriver, timeout time.Duration) error {
	readers, err := sql.Open(drv.SQLDriverName(), readerDSN(cfg.Path, drv.Params(timeout, false)))
	if err != nil {
		return fmt.Errorf("open sqlite readers %s: %w", cfg.Path, err)
	}
	n := cfg.Readers
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

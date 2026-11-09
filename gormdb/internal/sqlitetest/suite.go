// Package sqlitetest is the behaviour suite every SQLite driver adapter must
// pass; each driver package runs it from its own tests.
package sqlitetest

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-bumbu/db/gormdb"
	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

// Run runs every behaviour subtest against drv.
func Run(t *testing.T, drv gormdb.SqliteDriver) {
	t.Helper()
	t.Run("creates missing directory", func(t *testing.T) { createsMissingDirectory(t, drv) })
	t.Run("writer pragmas", func(t *testing.T) { writerPragmas(t, drv) })
	t.Run("unwritable dir", func(t *testing.T) { unwritableDir(t, drv) })
	t.Run("one write connection", func(t *testing.T) { oneWriteConnection(t, drv) })
	t.Run("writer outlasts busy timeout", func(t *testing.T) { writerOutlastsBusyTimeout(t, drv) })
	t.Run("immediate transactions", func(t *testing.T) { immediateTransactions(t, drv) })
	t.Run("in-memory", func(t *testing.T) { inMemory(t, drv) })
	t.Run("txlock across handles", func(t *testing.T) { txlockAcrossHandles(t, drv) })
	t.Run("reader pragmas", func(t *testing.T) { readerPragmas(t, drv) })
	t.Run("reader cannot write", func(t *testing.T) { readerCannotWrite(t, drv) })
	t.Run("reads do not wait for a write", func(t *testing.T) { readsDoNotWaitForAWrite(t, drv) })
	t.Run("overlapping reads and writes", func(t *testing.T) { overlappingReadsAndWrites(t, drv) })
	t.Run("awkward folder", func(t *testing.T) { awkwardFolder(t, drv) })
	// not parallel: t.Chdir changes the process working directory
	t.Run("relative path", func(t *testing.T) { relativePath(t, drv) })
	t.Run("close closes readers", func(t *testing.T) { closeClosesReaders(t, drv) })
	t.Run("close checkpoints the WAL", func(t *testing.T) { closeCheckpointsWAL(t, drv) })
	t.Run("file URI rejected", func(t *testing.T) { fileURIRejected(t, drv) })
	t.Run("leading double slash", func(t *testing.T) { leadingDoubleSlash(t, drv) })
	t.Run("close twice", func(t *testing.T) { closeTwice(t, drv) })
	t.Run("close waits for reads", func(t *testing.T) { closeWaitsForReads(t, drv) })
	t.Run("connection stays pinned", func(t *testing.T) { connectionStaysPinned(t, drv) })
	t.Run("sub-millisecond busy timeout", func(t *testing.T) { subMillisecondBusyTimeout(t, drv) })
	t.Run("huge busy timeout", func(t *testing.T) { hugeBusyTimeout(t, drv) })
	t.Run("quiet open", func(t *testing.T) { quietOpen(t, drv) })
	// not parallel: t.Chdir changes the process working directory
	t.Run("file named :memory:", func(t *testing.T) { memoryNamedFile(t, drv) })
	t.Run("in-memory close", func(t *testing.T) { inMemoryClose(t, drv) })
	t.Run("duplicate keeps original", func(t *testing.T) { duplicateKeepsOriginal(t, drv) })
	t.Run("primary key duplicate", func(t *testing.T) { primaryKeyDuplicate(t, drv) })
	t.Run("foreign key keeps original", func(t *testing.T) { foreignKeyKeepsOriginal(t, drv) })
	t.Run("check keeps original", func(t *testing.T) { checkKeepsOriginal(t, drv) })
	t.Run("translate is idempotent", func(t *testing.T) { translateIdempotent(t, drv) })
	t.Run("other errors untouched", func(t *testing.T) { otherErrorsUntouched(t, drv) })
	t.Run("automigrate still works", func(t *testing.T) { autoMigrateStillWorks(t, drv) })
}

// open opens a SQLite database through gormdb.Open and closes it on cleanup.
func open(t *testing.T, drv gormdb.SqliteDriver, cfg gormdb.SqliteCfg) *gorm.DB {
	t.Helper()
	db, err := gormdb.Open(gormdb.Cfg{Driver: gormdb.DriverSqlite, Sqlite: cfg}, gormdb.Drivers{Sqlite: drv})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := gormdb.Close(db); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return db
}

// openWithCounter opens a database with one table of one row.
func openWithCounter(t *testing.T, drv gormdb.SqliteDriver, busy time.Duration) *gorm.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db := open(t, drv, gormdb.SqliteCfg{Path: path, BusyTimeout: busy})
	for _, stmt := range []string{"CREATE TABLE counter (n INTEGER)", "INSERT INTO counter VALUES (0)"} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	return db
}

// holdWriteTx opens a write transaction and returns once it holds the write
// connection. Calling the function it returns commits it and waits for that.
func holdWriteTx(t *testing.T, db *gorm.DB, stmt string) func() {
	t.Helper()
	held, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec(stmt).Error; err != nil {
				return err
			}
			close(held)
			<-release
			return nil
		})
		if err != nil {
			t.Errorf("held transaction: %v", err)
		}
	}()
	select {
	case <-held:
	case <-time.After(5 * time.Second):
		t.Fatal("the transaction never got the write connection")
	}
	var once sync.Once
	commit := func() {
		once.Do(func() { close(release) })
		<-done
	}
	// a failed test must not leave the connection held, or Close would hang
	t.Cleanup(commit)
	return commit
}

func createsMissingDirectory(t *testing.T, drv gormdb.SqliteDriver) {
	// a nested, missing directory: Open must create it
	path := filepath.Join(t.TempDir(), "data", "sqlite", "test.db")
	open(t, drv, gormdb.SqliteCfg{Path: path})
	if _, err := os.Stat(path); err != nil {
		t.Errorf("database file not created: %v", err)
	}
}

func writerPragmas(t *testing.T, drv gormdb.SqliteDriver) {
	path := filepath.Join(t.TempDir(), "test.db")
	db := open(t, drv, gormdb.SqliteCfg{Path: path, BusyTimeout: 2 * time.Second})

	// the pragmas are set from the DSN, so every pooled connection has them
	pragmas := map[string]string{
		"busy_timeout": "2000",
		"journal_mode": "wal",
		"foreign_keys": "1",
	}
	for pragma, want := range pragmas {
		var got string
		if err := db.Raw("PRAGMA " + pragma).Scan(&got).Error; err != nil {
			t.Fatalf("read %s: %v", pragma, err)
		}
		if got != want {
			t.Errorf("%s: got %q, want %q", pragma, got, want)
		}
	}
}

func unwritableDir(t *testing.T, drv gormdb.SqliteDriver) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	// the parent "directory" is a regular file
	cfg := gormdb.Cfg{Driver: gormdb.DriverSqlite, Sqlite: gormdb.SqliteCfg{Path: filepath.Join(file, "test.db")}}
	if _, err := gormdb.Open(cfg, gormdb.Drivers{Sqlite: drv}); err == nil {
		t.Fatal("expected an error when the directory cannot be created")
	}
}

// One write connection is what makes writers wait in Go's queue instead of
// running out of SQLite's busy timeout.
func oneWriteConnection(t *testing.T, drv gormdb.SqliteDriver) {
	db := open(t, drv, gormdb.SqliteCfg{Path: filepath.Join(t.TempDir(), "test.db")})
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("DB(): %v", err)
	}
	if got := sqlDB.Stats().MaxOpenConnections; got != 1 {
		t.Errorf("write connections = %d, want 1", got)
	}
}

// A write that waits longer than the busy timeout for another write takes its
// turn instead of failing.
func writerOutlastsBusyTimeout(t *testing.T, drv gormdb.SqliteDriver) {
	db := openWithCounter(t, drv, 50*time.Millisecond)
	commit := holdWriteTx(t, db, "UPDATE counter SET n = n + 1")

	wrote := make(chan error, 1)
	go func() { wrote <- db.Exec("UPDATE counter SET n = n + 1").Error }()
	select {
	case err := <-wrote:
		t.Fatalf("the write did not wait for the transaction: %v", err)
	case <-time.After(300 * time.Millisecond): // six times the busy timeout
	}
	commit()
	if err := <-wrote; err != nil {
		t.Fatalf("write after waiting: %v", err)
	}
	var n int
	if err := db.Raw("SELECT n FROM counter").Scan(&n).Error; err != nil {
		t.Fatalf("read: %v", err)
	}
	if n != 2 {
		t.Errorf("n = %d, want 2", n)
	}
}

// Read-then-write transactions from several goroutines must not lose updates
// nor fail with SQLITE_BUSY.
func immediateTransactions(t *testing.T, drv gormdb.SqliteDriver) {
	db := openWithCounter(t, drv, 0)
	const workers, rounds = 2, 25

	var wg sync.WaitGroup
	errs := make(chan error, workers*rounds)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range rounds {
				errs <- db.Transaction(func(tx *gorm.DB) error {
					var n int
					if err := tx.Raw("SELECT n FROM counter").Scan(&n).Error; err != nil {
						return err
					}
					return tx.Exec("UPDATE counter SET n = ?", n+1).Error
				})
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("transaction: %v", err)
		}
	}

	var n int
	if err := db.Raw("SELECT n FROM counter").Scan(&n).Error; err != nil {
		t.Fatalf("read: %v", err)
	}
	if n != workers*rounds {
		t.Errorf("n = %d, want %d", n, workers*rounds)
	}
}

func inMemory(t *testing.T, drv gormdb.SqliteDriver) {
	db := open(t, drv, gormdb.SqliteCfg{Path: ":memory:"})
	if err := db.Exec("CREATE TABLE t (v TEXT)").Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := db.Exec("INSERT INTO t VALUES ('x')").Error; err != nil {
		t.Fatalf("insert: %v", err)
	}
	var got string
	if err := db.Raw("SELECT v FROM t").Scan(&got).Error; err != nil {
		t.Fatalf("read: %v", err)
	}
	if got != "x" {
		t.Errorf("got %q, want x", got)
	}
}

// A second handle on the same file stands in for another process: it has its
// own writer, so only SQLite's lock, not Go's queue, orders the two. With
// deferred transactions the second one would fail on upgrading its read lock
// instead of waiting.
func txlockAcrossHandles(t *testing.T, drv gormdb.SqliteDriver) {
	path := filepath.Join(t.TempDir(), "test.db")
	a := open(t, drv, gormdb.SqliteCfg{Path: path, BusyTimeout: 5 * time.Second})
	b := open(t, drv, gormdb.SqliteCfg{Path: path, BusyTimeout: 5 * time.Second})
	for _, stmt := range []string{"CREATE TABLE counter (n INTEGER)", "INSERT INTO counter VALUES (0)"} {
		if err := a.Exec(stmt).Error; err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	increment := func(tx *gorm.DB) error {
		var n int
		if err := tx.Raw("SELECT n FROM counter").Scan(&n).Error; err != nil {
			return err
		}
		return tx.Exec("UPDATE counter SET n = ?", n+1).Error
	}

	// A reads, writes, and keeps the lock
	held, release, aDone := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		aDone <- a.Transaction(func(tx *gorm.DB) error {
			if err := increment(tx); err != nil {
				return err
			}
			close(held)
			<-release
			return nil
		})
	}()
	select {
	case <-held:
	case err := <-aDone:
		t.Fatalf("transaction on the first handle ended early: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("the first handle never got the write lock")
	}

	bDone := make(chan error, 1)
	go func() { bDone <- b.Transaction(increment) }()
	select {
	case err := <-bDone:
		t.Fatalf("the second handle did not wait for the lock: %v", err)
	case <-time.After(300 * time.Millisecond):
	}

	close(release)
	if err := <-aDone; err != nil {
		t.Fatalf("first handle: %v", err)
	}
	select {
	case err := <-bDone:
		if err != nil {
			t.Fatalf("second handle: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the second handle never finished")
	}

	var n int
	if err := a.Raw("SELECT n FROM counter").Scan(&n).Error; err != nil {
		t.Fatalf("read: %v", err)
	}
	if n != 2 {
		t.Errorf("n = %d, want 2", n)
	}
}

func readerPragmas(t *testing.T, drv gormdb.SqliteDriver) {
	path := filepath.Join(t.TempDir(), "test.db")
	db := open(t, drv, gormdb.SqliteCfg{Path: path, BusyTimeout: 2 * time.Second})

	pragmas := map[string]string{
		"busy_timeout": "2000",
		"foreign_keys": "1",
	}
	for pragma, want := range pragmas {
		var got string
		if err := db.Clauses(dbresolver.Read).Raw("PRAGMA " + pragma).Scan(&got).Error; err != nil {
			t.Fatalf("read %s: %v", pragma, err)
		}
		if got != want {
			t.Errorf("%s: got %q, want %q", pragma, got, want)
		}
	}
}

func readerCannotWrite(t *testing.T, drv gormdb.SqliteDriver) {
	db := openWithCounter(t, drv, 0)
	err := db.Clauses(dbresolver.Read).Exec("INSERT INTO counter VALUES (1)").Error
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "readonly") {
		t.Fatalf("write on the read pool: got %v, want a readonly error", err)
	}
	var rows int
	if err := db.Raw("SELECT COUNT(*) FROM counter").Scan(&rows).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if rows != 1 {
		t.Errorf("rows = %d, want 1", rows)
	}
}

// Reads go to their own pool, so a transaction holding the write connection
// stops none. They see what was committed, and see it at once.
func readsDoNotWaitForAWrite(t *testing.T, drv gormdb.SqliteDriver) {
	db := openWithCounter(t, drv, 5*time.Second)
	commit := holdWriteTx(t, db, "UPDATE counter SET n = 7")

	read := func() int {
		t.Helper()
		got := make(chan int, 1)
		go func() {
			var n int
			if err := db.Table("counter").Select("n").Scan(&n).Error; err != nil {
				t.Errorf("read: %v", err)
			}
			got <- n
		}()
		select {
		case n := <-got:
			return n
		case <-time.After(3 * time.Second):
			t.Fatal("the read waited for the write")
			return 0
		}
	}
	if n := read(); n != 0 {
		t.Errorf("before the commit n = %d, want 0", n)
	}
	commit()
	if n := read(); n != 7 {
		t.Errorf("after the commit n = %d, want 7", n)
	}
}

// Nothing fails, whatever overlaps. The read in a transaction sees its own
// write, as it stays on the transaction's connection.
func overlappingReadsAndWrites(t *testing.T, drv gormdb.SqliteDriver) {
	db := openWithCounter(t, drv, 5*time.Second)
	const workers, rounds = 8, 25
	var wg sync.WaitGroup
	for range workers {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for range rounds {
				err := db.Transaction(func(tx *gorm.DB) error {
					var before, after int
					if err := tx.Raw("SELECT n FROM counter").Scan(&before).Error; err != nil {
						return err
					}
					if err := tx.Exec("UPDATE counter SET n = ?", before+1).Error; err != nil {
						return err
					}
					if err := tx.Table("counter").Select("n").Scan(&after).Error; err != nil {
						return err
					}
					if after != before+1 {
						t.Errorf("the transaction read %d after writing %d", after, before+1)
					}
					return nil
				})
				if err != nil {
					t.Errorf("transaction: %v", err)
				}
			}
		}()
		go func() {
			defer wg.Done()
			for range rounds {
				var n int
				if err := db.Table("counter").Select("n").Scan(&n).Error; err != nil {
					t.Errorf("read: %v", err)
				}
			}
		}()
	}
	wg.Wait()
	var n int
	if err := db.Raw("SELECT n FROM counter").Scan(&n).Error; err != nil {
		t.Fatalf("read: %v", err)
	}
	if n != workers*rounds {
		t.Errorf("n = %d, want %d", n, workers*rounds)
	}
}

// The read pool reaches the file by a URI, in which these characters mean
// something; the path must reach it as it is.
func awkwardFolder(t *testing.T, drv gormdb.SqliteDriver) {
	path := filepath.Join(t.TempDir(), "my data #1 100% ?x", "test.db")
	db := open(t, drv, gormdb.SqliteCfg{Path: path})
	for _, stmt := range []string{"CREATE TABLE counter (n INTEGER)", "INSERT INTO counter VALUES (0)"} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	commit := holdWriteTx(t, db, "UPDATE counter SET n = 1") // so the read below can only be the read pool's
	defer commit()
	type result struct {
		n   int
		err error
	}
	got := make(chan result, 1)
	go func() {
		var n int
		err := db.Table("counter").Select("n").Scan(&n).Error
		got <- result{n, err}
	}()
	select {
	case r := <-got:
		if r.err != nil {
			t.Fatalf("read through the read pool: %v", r.err)
		}
		if r.n != 0 {
			t.Errorf("n = %d, want 0", r.n)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the read waited for the write")
	}
}

func relativePath(t *testing.T, drv gormdb.SqliteDriver) {
	t.Chdir(t.TempDir())
	db := open(t, drv, gormdb.SqliteCfg{Path: "data/app.db"})
	for _, stmt := range []string{"CREATE TABLE counter (n INTEGER)", "INSERT INTO counter VALUES (42)"} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	var n int
	if err := db.Clauses(dbresolver.Read).Raw("SELECT n FROM counter").Scan(&n).Error; err != nil {
		t.Fatalf("read: %v", err)
	}
	if n != 42 {
		t.Errorf("n = %d, want 42", n)
	}
}

func closeClosesReaders(t *testing.T, drv gormdb.SqliteDriver) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := gormdb.Open(gormdb.Cfg{Driver: gormdb.DriverSqlite, Sqlite: gormdb.SqliteCfg{Path: path}}, gormdb.Drivers{Sqlite: drv})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := gormdb.Close(db); err != nil {
		t.Fatalf("Close: %v", err)
	}
	var n int
	err = db.Clauses(dbresolver.Read).Raw("SELECT 1").Scan(&n).Error
	if err == nil || !strings.Contains(err.Error(), "database is closed") {
		t.Errorf("read after Close: got %v, want a closed-database error", err)
	}
}

// Closing the readers before the writer lets the writer checkpoint, so the
// database file alone holds everything.
func closeCheckpointsWAL(t *testing.T, drv gormdb.SqliteDriver) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.db")
	db, err := gormdb.Open(gormdb.Cfg{Driver: gormdb.DriverSqlite, Sqlite: gormdb.SqliteCfg{Path: path}}, gormdb.Drivers{Sqlite: drv})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	for _, stmt := range []string{"CREATE TABLE counter (n INTEGER)", "INSERT INTO counter VALUES (42)"} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	// make sure a reader connection is open when Close runs
	var n int
	if err := db.Table("counter").Select("n").Scan(&n).Error; err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := gormdb.Close(db); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(path + "-wal"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the -wal file survived Close: stat error %v", err)
	}

	data, err := os.ReadFile(path) //nolint:gosec // G304: a path inside the test's own temp dir
	if err != nil {
		t.Fatal(err)
	}
	copyPath := filepath.Join(t.TempDir(), "copy.db")
	if err := os.WriteFile(copyPath, data, 0o600); err != nil { //nolint:gosec // G703: a path inside the test's own temp dir
		t.Fatal(err)
	}
	cp := open(t, drv, gormdb.SqliteCfg{Path: copyPath})
	var got int
	if err := cp.Raw("SELECT n FROM counter").Scan(&got).Error; err != nil {
		t.Fatalf("read the copy of the .db file alone: %v", err)
	}
	if got != 42 {
		t.Errorf("copy: n = %d, want 42", got)
	}
}

// Only a filesystem path or ":memory:" is accepted; a URI must not silently
// become a file with a strange name.
func fileURIRejected(t *testing.T, drv gormdb.SqliteDriver) {
	t.Chdir(t.TempDir())
	cfg := gormdb.Cfg{Driver: gormdb.DriverSqlite, Sqlite: gormdb.SqliteCfg{Path: "file::memory:?cache=shared"}}
	db, err := gormdb.Open(cfg, gormdb.Drivers{Sqlite: drv})
	if err == nil {
		_ = gormdb.Close(db)
		t.Fatal("Open accepted a file: URI")
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("Open left files in the working directory: %v", entries)
	}
}

// A leading "//" must not be read as a URI authority.
func leadingDoubleSlash(t *testing.T, drv gormdb.SqliteDriver) {
	path := "/" + "/" + strings.TrimPrefix(filepath.Join(t.TempDir(), "test.db"), "/")
	db := open(t, drv, gormdb.SqliteCfg{Path: path})
	for _, stmt := range []string{"CREATE TABLE counter (n INTEGER)", "INSERT INTO counter VALUES (5)"} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	var n int
	if err := db.Clauses(dbresolver.Read).Raw("SELECT n FROM counter").Scan(&n).Error; err != nil {
		t.Fatalf("read: %v", err)
	}
	if n != 5 {
		t.Errorf("n = %d, want 5", n)
	}
}

func closeTwice(t *testing.T, drv gormdb.SqliteDriver) {
	db, err := gormdb.Open(gormdb.Cfg{Driver: gormdb.DriverSqlite, Sqlite: gormdb.SqliteCfg{Path: filepath.Join(t.TempDir(), "test.db")}}, gormdb.Drivers{Sqlite: drv})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := gormdb.Close(db); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := gormdb.Close(db); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func inMemoryClose(t *testing.T, drv gormdb.SqliteDriver) {
	db, err := gormdb.Open(gormdb.Cfg{Driver: gormdb.DriverSqlite, Sqlite: gormdb.SqliteCfg{Path: ":memory:"}}, gormdb.Drivers{Sqlite: drv})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := gormdb.Close(db); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// openWithParents opens a fresh database with a parents and a children table.
func openWithParents(t *testing.T, drv gormdb.SqliteDriver) *gorm.DB {
	t.Helper()
	db := open(t, drv, gormdb.SqliteCfg{Path: filepath.Join(t.TempDir(), "test.db")})
	for _, stmt := range []string{
		"CREATE TABLE parents (id INTEGER PRIMARY KEY, name TEXT UNIQUE NOT NULL, age INTEGER CHECK (age >= 0))",
		"CREATE TABLE children (id INTEGER PRIMARY KEY, parent_id INTEGER NOT NULL REFERENCES parents(id))",
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	return db
}

// requireTranslated checks that err maps to want and still carries the
// driver's own message.
func requireTranslated(t *testing.T, err, want error, text string) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("got %v, want it to match %v", err, want)
	}
	if !strings.Contains(err.Error(), text) {
		t.Errorf("error %q lost the original text %q", err, text)
	}
}

func duplicateKeepsOriginal(t *testing.T, drv gormdb.SqliteDriver) {
	db := openWithParents(t, drv)
	if err := db.Exec("INSERT INTO parents (name) VALUES ('a')").Error; err != nil {
		t.Fatalf("first insert: %v", err)
	}
	err := db.Exec("INSERT INTO parents (name) VALUES ('a')").Error
	requireTranslated(t, err, gorm.ErrDuplicatedKey, "UNIQUE constraint failed")
}

func primaryKeyDuplicate(t *testing.T, drv gormdb.SqliteDriver) {
	db := openWithParents(t, drv)
	if err := db.Exec("INSERT INTO parents (id, name) VALUES (1, 'a')").Error; err != nil {
		t.Fatalf("first insert: %v", err)
	}
	err := db.Exec("INSERT INTO parents (id, name) VALUES (1, 'b')").Error
	if !errors.Is(err, gorm.ErrDuplicatedKey) {
		t.Fatalf("got %v, want it to match %v", err, gorm.ErrDuplicatedKey)
	}
}

func foreignKeyKeepsOriginal(t *testing.T, drv gormdb.SqliteDriver) {
	db := openWithParents(t, drv)
	err := db.Exec("INSERT INTO children (parent_id) VALUES (999)").Error
	requireTranslated(t, err, gorm.ErrForeignKeyViolated, "FOREIGN KEY constraint failed")
}

func checkKeepsOriginal(t *testing.T, drv gormdb.SqliteDriver) {
	db := openWithParents(t, drv)
	err := db.Exec("INSERT INTO parents (name, age) VALUES ('a', -1)").Error
	requireTranslated(t, err, gorm.ErrCheckConstraintViolated, "CHECK constraint failed")
}

// Translating an already translated error must not wrap the sentinel again.
func translateIdempotent(t *testing.T, drv gormdb.SqliteDriver) {
	db := openWithParents(t, drv)
	if err := db.Exec("INSERT INTO parents (name) VALUES ('a')").Error; err != nil {
		t.Fatalf("first insert: %v", err)
	}
	err := db.Exec("INSERT INTO parents (name) VALUES ('a')").Error
	tr, ok := db.Dialector.(gorm.ErrorTranslator)
	if !ok {
		t.Fatal("the dialector does not translate errors")
	}
	if again := tr.Translate(err); again.Error() != err.Error() {
		t.Errorf("translating twice changed the error: %q -> %q", err, again)
	}
}

func otherErrorsUntouched(t *testing.T, drv gormdb.SqliteDriver) {
	db := openWithParents(t, drv)
	var n int
	err := db.Raw("SELECT COUNT(*) FROM missing").Scan(&n).Error
	if err == nil {
		t.Fatal("expected an error for a missing table")
	}
	for _, s := range []error{gorm.ErrDuplicatedKey, gorm.ErrForeignKeyViolated, gorm.ErrCheckConstraintViolated} {
		if errors.Is(err, s) {
			t.Errorf("error %v wrongly matches %v", err, s)
		}
	}
}

// The wrapper must keep the upstream Migrator, or AutoMigrate breaks.
func autoMigrateStillWorks(t *testing.T, drv gormdb.SqliteDriver) {
	db := open(t, drv, gormdb.SqliteCfg{Path: filepath.Join(t.TempDir(), "test.db")})
	type account struct {
		ID    uint
		Email string `gorm:"uniqueIndex"`
	}
	for range 2 {
		if err := db.AutoMigrate(&account{}); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}
	}
}

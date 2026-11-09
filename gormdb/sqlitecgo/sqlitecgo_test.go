package sqlitecgo_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-bumbu/db/gormdb/internal/sqlitetest"
	"github.com/go-bumbu/db/gormdb/sqlitecgo"
)

func TestSuite(t *testing.T) { sqlitetest.Run(t, sqlitecgo.Driver{}) }

func TestParams(t *testing.T) {
	w := sqlitecgo.Driver{}.WriterParams(3 * time.Second)
	for k, want := range map[string]string{
		"_busy_timeout": "3000",
		"_foreign_keys": "1",
		"_journal_mode": "WAL",
		"_txlock":       "immediate",
	} {
		if got := w.Get(k); got != want {
			t.Errorf("writer %s = %q, want %q", k, got, want)
		}
	}

	r := sqlitecgo.Driver{}.ReaderParams(3 * time.Second)
	for k, want := range map[string]string{
		"_busy_timeout": "3000",
		"_foreign_keys": "1",
		"_journal_mode": "",
		"_txlock":       "",
	} {
		if got := r.Get(k); got != want {
			t.Errorf("reader %s = %q, want %q", k, got, want)
		}
	}

	p := sqlitecgo.Driver{}.PragmaParams(map[string]string{"temp_store": "2", "synchronous": "NORMAL"})
	wantP := []string{"synchronous(NORMAL)", "temp_store(2)"} // sorted, so the DSN is stable
	if got := p["_gormdb_pragma"]; !slices.Equal(got, wantP) || len(p) != 1 {
		t.Errorf("pragma params = %v, want _gormdb_pragma %v", p, wantP)
	}
}

// A pragma SQLite rejects fails the connection, it is not dropped. (SQLite
// ignores an unknown pragma name; a syntax error is what it rejects.)
func TestOpenDBFailingPragma(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := sqlitecgo.Driver{}.OpenDB("file:" + path + "?_gormdb_pragma=synchronous(")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err := db.Ping(); err == nil || !strings.Contains(err.Error(), "PRAGMA") {
		t.Errorf("ping: got %v, want the failing PRAGMA", err)
	}
}

// Without pragmas, the DSN reaches mattn unchanged in meaning.
func TestOpenDBWithoutPragmas(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := sqlitecgo.Driver{}.OpenDB("file:" + path + "?_busy_timeout=1234")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var got int
	if err := db.QueryRow("PRAGMA busy_timeout").Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != 1234 {
		t.Errorf("busy_timeout = %d, want 1234", got)
	}
}

package sqlite_test

import (
	"slices"
	"testing"
	"time"

	"github.com/go-bumbu/db/gormdb/internal/sqlitetest"
	"github.com/go-bumbu/db/gormdb/sqlite"
)

func TestSuite(t *testing.T) { sqlitetest.Run(t, sqlite.Driver{}) }

func TestParams(t *testing.T) {
	w := sqlite.Driver{}.Params(3*time.Second, true)
	wantW := []string{"busy_timeout(3000)", "journal_mode(WAL)", "foreign_keys(1)"}
	if got := w["_pragma"]; !slices.Equal(got, wantW) {
		t.Errorf("writer pragmas = %v, want %v", got, wantW)
	}
	if got := w.Get("_txlock"); got != "immediate" {
		t.Errorf("writer _txlock = %q, want immediate", got)
	}

	r := sqlite.Driver{}.Params(3*time.Second, false)
	wantR := []string{"busy_timeout(3000)", "foreign_keys(1)"}
	if got := r["_pragma"]; !slices.Equal(got, wantR) {
		t.Errorf("reader pragmas = %v, want %v", got, wantR)
	}
	if got := r.Get("_txlock"); got != "" {
		t.Errorf("reader _txlock = %q, want empty", got)
	}
}

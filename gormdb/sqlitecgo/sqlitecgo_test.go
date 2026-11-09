package sqlitecgo_test

import (
	"testing"
	"time"

	"github.com/go-bumbu/db/gormdb/internal/sqlitetest"
	"github.com/go-bumbu/db/gormdb/sqlitecgo"
)

func TestSuite(t *testing.T) { sqlitetest.Run(t, sqlitecgo.Driver{}) }

func TestParams(t *testing.T) {
	w := sqlitecgo.Driver{}.Params(3*time.Second, true)
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

	r := sqlitecgo.Driver{}.Params(3*time.Second, false)
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
}

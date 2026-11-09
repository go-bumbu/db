package postgres_test

import (
	"testing"
	"time"

	"github.com/go-bumbu/db/gormdb"
	"github.com/go-bumbu/db/gormdb/postgres"
	"github.com/jackc/pgx/v5/pgconn"
	gormlogger "gorm.io/gorm/logger"
)

// The Docker-backed tests against a real Postgres live in the integration
// module; these need no database.

func TestDialector(t *testing.T) {
	if got := (postgres.Driver{}).Dialector("postgres://x/y").Name(); got != "postgres" {
		t.Errorf("Dialector name = %q, want postgres", got)
	}
}

// The DSN must mean to pgx what the config says: compare what pgx parses,
// not the string.
func TestDSNRoundTripsThroughPgx(t *testing.T) {
	const tricky = "a b+c&d=e%f@g:h/i?j#k"
	tests := []struct {
		name string
		cfg  gormdb.PostgresCfg
	}{
		{name: "with user", cfg: gormdb.PostgresCfg{Host: "db", User: tricky, Password: tricky, DbName: tricky}},
		{name: "password without user", cfg: gormdb.PostgresCfg{Host: "db", Password: tricky, DbName: "app"}},
		{name: "unix socket", cfg: gormdb.PostgresCfg{Host: "/var/run/postgresql", User: "u", DbName: "app"}},
		{name: "ipv6 with zone", cfg: gormdb.PostgresCfg{Host: "fe80::1%eth0", User: "u", DbName: "app"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.cfg.SslMode = "disable"
			tc.cfg.Port = 6543
			tc.cfg.ConnectTimeout = 3 * time.Second
			tc.cfg.Params = map[string]string{"application_name": tricky}
			if err := (gormdb.Cfg{Driver: gormdb.DriverPostgres, Postgres: tc.cfg}).Validate(); err != nil {
				t.Fatalf("Validate: %v", err)
			}
			pc, err := pgconn.ParseConfig(tc.cfg.DSN())
			if err != nil {
				t.Fatalf("pgx cannot parse %q: %v", tc.cfg.DSN(), err)
			}
			if pc.Host != tc.cfg.Host || pc.Port != 6543 || pc.Database != tc.cfg.DbName || pc.Password != tc.cfg.Password {
				t.Errorf("pgx read host=%q port=%d db=%q password=%q, want %q %d %q %q",
					pc.Host, pc.Port, pc.Database, pc.Password, tc.cfg.Host, 6543, tc.cfg.DbName, tc.cfg.Password)
			}
			if tc.cfg.User != "" && pc.User != tc.cfg.User {
				t.Errorf("user = %q, want %q", pc.User, tc.cfg.User)
			}
			if pc.ConnectTimeout != 3*time.Second {
				t.Errorf("connect timeout = %s, want 3s", pc.ConnectTimeout)
			}
			if got := pc.RuntimeParams["application_name"]; got != tricky {
				t.Errorf("application_name = %q, want %q", got, tricky)
			}
		})
	}
}

// Open must give up on an unreachable host after ConnectTimeout instead of
// waiting for the OS TCP timeout (about 2 minutes on Linux).
func TestOpenUnreachableHostTimesOut(t *testing.T) {
	cfg := gormdb.Cfg{Driver: gormdb.DriverPostgres, Logger: gormlogger.Discard, Postgres: gormdb.PostgresCfg{
		Host: "10.255.255.1", DbName: "app", SslMode: "disable", ConnectTimeout: time.Second,
	}}
	start := time.Now()
	db, err := gormdb.Open(cfg, gormdb.Drivers{Postgres: postgres.Driver{}})
	if err == nil {
		_ = gormdb.Close(db)
		t.Fatal("Open of an unreachable host succeeded")
	}
	if took := time.Since(start); took > 10*time.Second {
		t.Errorf("Open took %s, want about the 1s connect timeout", took)
	}
}

package gormdb

import (
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Cfg
		wantErr string
	}{
		{name: "sqlite", cfg: Cfg{Driver: DriverSqlite, Sqlite: SqliteCfg{Path: "x.db"}}},
		{name: "postgres", cfg: Cfg{Driver: DriverPostgres, Postgres: PostgresCfg{Host: "h", DbName: "d"}}},
		{name: "unknown driver", cfg: Cfg{Driver: "mysql"}, wantErr: "unsupported driver"},
		{name: "empty driver", cfg: Cfg{}, wantErr: "unsupported driver"},
		{name: "sqlite without path", cfg: Cfg{Driver: DriverSqlite}, wantErr: "path"},
		{name: "sqlite file URI", cfg: Cfg{Driver: DriverSqlite, Sqlite: SqliteCfg{Path: "file::memory:?cache=shared"}}, wantErr: ":memory:"},
		{name: "sqlite memory ok", cfg: Cfg{Driver: DriverSqlite, Sqlite: SqliteCfg{Path: ":memory:"}}},
		{name: "sqlite negative readers", cfg: Cfg{Driver: DriverSqlite, Sqlite: SqliteCfg{Path: "x.db", Readers: -1}}, wantErr: "readers"},
		{name: "sqlite zero readers ok", cfg: Cfg{Driver: DriverSqlite, Sqlite: SqliteCfg{Path: "x.db", Readers: 0}}},
		{name: "postgres without host", cfg: Cfg{Driver: DriverPostgres, Postgres: PostgresCfg{DbName: "d"}}, wantErr: "host"},
		{name: "postgres without db", cfg: Cfg{Driver: DriverPostgres, Postgres: PostgresCfg{Host: "h"}}, wantErr: "database name"},
		{name: "sqlite negative busy timeout", cfg: Cfg{Driver: DriverSqlite, Sqlite: SqliteCfg{Path: "x.db", BusyTimeout: -time.Second}}, wantErr: "busy timeout"},
		{name: "postgres host with port", cfg: Cfg{Driver: DriverPostgres, Postgres: PostgresCfg{Host: "db:5433", DbName: "d"}}, wantErr: "port"},
		{name: "postgres bracketed ipv6", cfg: Cfg{Driver: DriverPostgres, Postgres: PostgresCfg{Host: "[::1]", DbName: "d"}}, wantErr: "port"},
		{name: "postgres ipv6 ok", cfg: Cfg{Driver: DriverPostgres, Postgres: PostgresCfg{Host: "::1", DbName: "d"}}},
		{name: "postgres port out of range", cfg: Cfg{Driver: DriverPostgres, Postgres: PostgresCfg{Host: "h", Port: 70000, DbName: "d"}}, wantErr: "port"},
		{name: "postgres negative port", cfg: Cfg{Driver: DriverPostgres, Postgres: PostgresCfg{Host: "h", Port: -1, DbName: "d"}}, wantErr: "port"},
		{name: "postgres unknown sslmode", cfg: Cfg{Driver: DriverPostgres, Postgres: PostgresCfg{Host: "h", DbName: "d", SslMode: "disabled"}}, wantErr: "sslmode"},
		{name: "postgres negative connect timeout", cfg: Cfg{Driver: DriverPostgres, Postgres: PostgresCfg{Host: "h", DbName: "d", ConnectTimeout: -time.Second}}, wantErr: "connect timeout"},
		{name: "postgres negative pool", cfg: Cfg{Driver: DriverPostgres, Postgres: PostgresCfg{Host: "h", DbName: "d", MaxOpenConns: -1}}, wantErr: "pool"},
		{name: "postgres reserved param", cfg: Cfg{Driver: DriverPostgres, Postgres: PostgresCfg{Host: "h", DbName: "d", Params: map[string]string{"SSLMode": "disable"}}}, wantErr: "own field"},
		{name: "postgres params ok", cfg: Cfg{Driver: DriverPostgres, Postgres: PostgresCfg{Host: "h", DbName: "d", SslMode: "verify-full", Params: map[string]string{"application_name": "app"}}}},
		{name: "postgres unix socket ok", cfg: Cfg{Driver: DriverPostgres, Postgres: PostgresCfg{Host: "/var/run/postgresql", DbName: "d"}}},
		{name: "sqlite readers with memory", cfg: Cfg{Driver: DriverSqlite, Sqlite: SqliteCfg{Path: ":memory:", Readers: 2}}, wantErr: "readers"},
		{name: "postgres ipv6 with zone ok", cfg: Cfg{Driver: DriverPostgres, Postgres: PostgresCfg{Host: "fe80::1%eth0", DbName: "d"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("got %v, want an error containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestPostgresDSN(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		got := PostgresCfg{Host: "db", User: "app", Password: "pw", DbName: "app"}.DSN()
		want := "postgres://app:pw@db:5432/app?connect_timeout=10&sslmode=require" //nolint:gosec // G101: fake test credentials
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("special characters are escaped", func(t *testing.T) {
		cfg := PostgresCfg{Host: "db", Port: 6543, User: "app", Password: "p@ss:w/rd?#", DbName: "app", SslMode: "disable"} //nolint:gosec // G101: fake test credentials
		u, err := url.Parse(cfg.DSN())
		if err != nil {
			t.Fatalf("DSN is not a valid URL: %v", err)
		}
		if pw, _ := u.User.Password(); pw != cfg.Password {
			t.Errorf("password: got %q, want %q", pw, cfg.Password)
		}
		if u.Port() != "6543" {
			t.Errorf("port: got %q, want 6543", u.Port())
		}
		if got := u.Query().Get("sslmode"); got != "disable" {
			t.Errorf("sslmode: got %q, want disable", got)
		}
	})

	t.Run("ipv6 host without user", func(t *testing.T) {
		got := PostgresCfg{Host: "::1", DbName: "app"}.DSN()
		want := "postgres://[::1]:5432/app?connect_timeout=10&sslmode=require"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("every special character in user, password and db name round-trips", func(t *testing.T) {
		cfg := PostgresCfg{Host: "db", User: "u@:/?#%", Password: "p@:/?#%", DbName: "d@:/?#%"}
		u, err := url.Parse(cfg.DSN())
		if err != nil {
			t.Fatalf("DSN is not a valid URL: %v", err)
		}
		if got := u.User.Username(); got != cfg.User {
			t.Errorf("user: got %q, want %q", got, cfg.User)
		}
		if pw, _ := u.User.Password(); pw != cfg.Password {
			t.Errorf("password: got %q, want %q", pw, cfg.Password)
		}
		if got := strings.TrimPrefix(u.Path, "/"); got != cfg.DbName {
			t.Errorf("db name: got %q, want %q", got, cfg.DbName)
		}
		if u.Port() != "5432" {
			t.Errorf("port: got %q, want 5432", u.Port())
		}
		if got := u.Query().Get("sslmode"); got != "require" {
			t.Errorf("sslmode: got %q, want require", got)
		}
	})

	t.Run("empty user leaves user out", func(t *testing.T) {
		got := PostgresCfg{Host: "db", DbName: "app"}.DSN()
		want := "postgres://db:5432/app?connect_timeout=10&sslmode=require"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("password without user is kept", func(t *testing.T) {
		got := PostgresCfg{Host: "db", Password: "p@ss&w", DbName: "app"}.DSN()
		want := "postgres://db:5432/app?connect_timeout=10&password=p%40ss%26w&sslmode=require"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("connect timeout is rounded up to seconds", func(t *testing.T) {
		u, err := url.Parse(PostgresCfg{Host: "db", DbName: "app", ConnectTimeout: 1500 * time.Millisecond}.DSN())
		if err != nil {
			t.Fatal(err)
		}
		if got := u.Query().Get("connect_timeout"); got != "2" {
			t.Errorf("connect_timeout: got %q, want 2", got)
		}
	})

	t.Run("params are added, fields win", func(t *testing.T) {
		u, err := url.Parse(PostgresCfg{Host: "db", DbName: "app", Params: map[string]string{"application_name": "my app"}}.DSN())
		if err != nil {
			t.Fatal(err)
		}
		if got := u.Query().Get("application_name"); got != "my app" {
			t.Errorf("application_name: got %q, want %q", got, "my app")
		}
		if strings.Contains(u.RawQuery, "+") {
			t.Errorf("query %q encodes a space as '+', which libpq reads as a '+'", u.RawQuery)
		}
	})
}

func TestPostgresCfgRedactsPassword(t *testing.T) {
	cfg := PostgresCfg{Host: "db", User: "app", Password: "s3cret", DbName: "app"}
	var buf strings.Builder
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("config", "postgres", cfg)
	for name, out := range map[string]string{
		"%v":        fmt.Sprintf("%v", cfg),
		"%+v":       fmt.Sprintf("%+v", cfg),
		"%+v (Cfg)": fmt.Sprintf("%+v", Cfg{Postgres: cfg}),
		"slog JSON": buf.String(),
	} {
		if strings.Contains(out, "s3cret") || !strings.Contains(out, "REDACTED") {
			t.Errorf("%s: password not redacted: %s", name, out)
		}
	}
	if cfg.Password != "s3cret" {
		t.Error("redacting changed the config itself")
	}
}

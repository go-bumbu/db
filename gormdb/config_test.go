package gormdb

import (
	"net/url"
	"strings"
	"testing"
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
		want := "postgres://app:pw@db:5432/app?sslmode=require" //nolint:gosec // G101: fake test credentials
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
		want := "postgres://[::1]:5432/app?sslmode=require"
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
		want := "postgres://db:5432/app?sslmode=require"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}

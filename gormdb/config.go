package gormdb

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	gormlogger "gorm.io/gorm/logger"
)

// Supported drivers.
const (
	DriverSqlite   = "sqlite"
	DriverPostgres = "postgres"
)

const (
	defaultBusyTimeout   = 5 * time.Second
	defaultSqliteReaders = 10
	defaultPostgresPort  = 5432
	defaultSslMode       = "require"
)

// Cfg selects and configures the database. Only the section matching Driver
// is used.
type Cfg struct {
	// Driver is DriverSqlite or DriverPostgres.
	Driver   string
	Sqlite   SqliteCfg
	Postgres PostgresCfg
	// Logger receives GORM's log output; nil keeps GORM's default logger.
	Logger gormlogger.Interface
}

// SqliteCfg configures a SQLite database file.
type SqliteCfg struct {
	// Path of the database file, or ":memory:" for an in-memory database (one
	// connection, no read pool). It is a filesystem path, never a URI: a path
	// starting with "file:" is rejected. Its directory is created when missing; keep
	// the file in a directory of its own, SQLite writes -wal and -shm files
	// next to it.
	Path string
	// BusyTimeout is how long a connection waits for a lock held by another
	// one before failing with SQLITE_BUSY; zero means 5s. SQLite counts in
	// whole milliseconds: it is rounded down, to at least 1ms. It must not be
	// negative.
	BusyTimeout time.Duration
	// Readers is the size of the read-only connection pool; zero means 10.
	Readers int
}

// PostgresCfg configures a Postgres connection.
type PostgresCfg struct {
	// Host is a host name or IP address, without a port (see Port) and, for
	// IPv6, without brackets.
	Host string
	Port int // zero means 5432
	// User may be empty: libpq's defaults (PGUSER, the OS user) then apply,
	// and Password, if set, is still sent.
	User     string
	Password string
	DbName   string
	// SslMode is passed as libpq's sslmode (disable, require, verify-full, ...);
	// empty means "require".
	SslMode string
}

// DSN returns the connection URL. User, password and database name are
// escaped, so any character is safe in them.
func (c PostgresCfg) DSN() string {
	port := c.Port
	if port == 0 {
		port = defaultPostgresPort
	}
	sslMode := c.SslMode
	if sslMode == "" {
		sslMode = defaultSslMode
	}
	q := url.Values{"sslmode": {sslMode}}
	u := url.URL{
		Scheme: "postgres",
		Host:   net.JoinHostPort(c.Host, strconv.Itoa(port)),
		Path:   "/" + c.DbName,
	}
	switch {
	case c.User != "":
		u.User = url.UserPassword(c.User, c.Password)
	case c.Password != "":
		// without a user, libpq's defaults (PGUSER, the OS user) apply; the
		// password goes in the query, as userinfo needs a user name
		q.Set("password", c.Password)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// Validate reports a configuration Open cannot work with. Call it while
// loading the config to fail before anything starts.
func (c Cfg) Validate() error {
	switch c.Driver {
	case DriverSqlite:
		if c.Sqlite.Path == "" {
			return errors.New("sqlite: path must not be empty")
		}
		if strings.HasPrefix(c.Sqlite.Path, "file:") {
			return errors.New(`sqlite: path is a filesystem path or ":memory:", not a file: URI`)
		}
		if c.Sqlite.Readers < 0 {
			return errors.New("sqlite: readers must not be negative")
		}
		if c.Sqlite.BusyTimeout < 0 {
			return errors.New("sqlite: busy timeout must not be negative")
		}
	case DriverPostgres:
		if c.Postgres.Host == "" {
			return errors.New("postgres: host must not be empty")
		}
		// a ':' is only valid in an IPv6 address; "db:5433" is a host with a port
		// (netip, unlike net.ParseIP, accepts a zone: fe80::1%eth0)
		if _, err := netip.ParseAddr(c.Postgres.Host); strings.Contains(c.Postgres.Host, ":") && err != nil {
			return fmt.Errorf("postgres: host %q must not include a port (set Port) or IPv6 brackets", c.Postgres.Host)
		}
		if c.Postgres.DbName == "" {
			return errors.New("postgres: database name must not be empty")
		}
	default:
		return fmt.Errorf("unsupported driver %q, want %q or %q", c.Driver, DriverSqlite, DriverPostgres)
	}
	return nil
}

package gormdb

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"net/url"
	"slices"
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
	// SQLite stores busy_timeout as a 32-bit count of milliseconds and drops
	// a larger value, which turns waiting off
	maxBusyTimeout        = (1<<31 - 1) * time.Millisecond
	defaultPostgresPort   = 5432
	defaultSslMode        = "require"
	defaultConnectTimeout = 10 * time.Second
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
	// whole milliseconds: it is rounded down, to at least 1ms and at most
	// 2^31-1 ms (about 24 days). It must not be negative.
	BusyTimeout time.Duration
	// Readers is the size of the read-only connection pool; zero means 10.
	// ":memory:" has no read pool, so it must stay zero there.
	Readers int
}

// PostgresCfg configures a Postgres connection. Printing it with fmt, or
// logging it with slog, shows Password as "REDACTED".
type PostgresCfg struct {
	// Host is a host name, an IP address or the directory of a Unix socket,
	// without a port (see Port) and, for IPv6, without brackets.
	Host string
	Port int // zero means 5432
	// User may be empty: libpq's defaults (PGUSER, the OS user) then apply,
	// and Password, if set, is still sent.
	User     string
	Password string
	DbName   string
	// SslMode is libpq's sslmode: disable, allow, prefer, require, verify-ca
	// or verify-full; empty means "require".
	SslMode string
	// ConnectTimeout bounds how long opening a connection may take, so Open
	// fails fast against an unreachable host; zero means 10s. It is sent in
	// whole seconds, rounded up.
	ConnectTimeout time.Duration
	// MaxOpenConns, MaxIdleConns and ConnMaxLifetime tune the connection pool
	// (see database/sql); zero keeps database/sql's default: no limit on open
	// connections, 2 idle ones, no maximum lifetime.
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	// Params are extra connection parameters, such as application_name,
	// search_path or default_query_exec_mode. They must not repeat one of the
	// fields above (host, port, user, password, dbname, sslmode,
	// connect_timeout).
	Params map[string]string
}

// reservedParams are the connection parameters PostgresCfg's fields set.
var reservedParams = []string{"host", "port", "user", "password", "dbname", "sslmode", "connect_timeout"}

// sslModes are the values libpq accepts for sslmode.
var sslModes = []string{"disable", "allow", "prefer", "require", "verify-ca", "verify-full"}

// DSN returns the connection URL. User, password, database name and Params
// are escaped, so any character is safe in them.
func (c PostgresCfg) DSN() string {
	port := c.Port
	if port == 0 {
		port = defaultPostgresPort
	}
	sslMode := c.SslMode
	if sslMode == "" {
		sslMode = defaultSslMode
	}
	timeout := c.ConnectTimeout
	if timeout == 0 {
		timeout = defaultConnectTimeout
	}
	q := url.Values{}
	for k, v := range c.Params {
		q.Set(k, v)
	}
	q.Set("sslmode", sslMode)
	q.Set("connect_timeout", strconv.FormatInt(int64((timeout+time.Second-1)/time.Second), 10))
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
	// Encode writes a space as '+', which libpq and pgx keep as a '+'; a
	// literal '+' is already %2B, so every '+' left is a space
	u.RawQuery = strings.ReplaceAll(q.Encode(), "+", "%20")
	return u.String()
}

// String shows the configuration with Password redacted.
func (c PostgresCfg) String() string {
	type plain PostgresCfg // without the String method
	return fmt.Sprintf("%+v", plain(c.redacted()))
}

// LogValue shows the configuration in slog with Password redacted.
func (c PostgresCfg) LogValue() slog.Value {
	type plain PostgresCfg // without the LogValue method
	return slog.AnyValue(plain(c.redacted()))
}

func (c PostgresCfg) redacted() PostgresCfg {
	if c.Password != "" {
		c.Password = "REDACTED"
	}
	return c
}

// Validate reports a configuration Open cannot work with. Call it while
// loading the config to fail before anything starts.
func (c Cfg) Validate() error {
	switch c.Driver {
	case DriverSqlite:
		return c.Sqlite.validate()
	case DriverPostgres:
		return c.Postgres.validate()
	default:
		return fmt.Errorf("unsupported driver %q, want %q or %q", c.Driver, DriverSqlite, DriverPostgres)
	}
}

func (c SqliteCfg) validate() error {
	switch {
	case c.Path == "":
		return errors.New("sqlite: path must not be empty")
	case strings.HasPrefix(c.Path, "file:"):
		return errors.New(`sqlite: path is a filesystem path or ":memory:", not a file: URI`)
	case c.Readers < 0:
		return errors.New("sqlite: readers must not be negative")
	case c.Readers > 0 && isMemory(c.Path):
		return errors.New(`sqlite: readers must be zero for ":memory:", which has no read pool`)
	case c.BusyTimeout < 0:
		return errors.New("sqlite: busy timeout must not be negative")
	}
	return nil
}

func (c PostgresCfg) validate() error {
	// a ':' is only valid in an IPv6 address; "db:5433" is a host with a port
	// (netip, unlike net.ParseIP, accepts a zone: fe80::1%eth0)
	_, ipErr := netip.ParseAddr(c.Host)
	switch {
	case c.Host == "":
		return errors.New("postgres: host must not be empty")
	case strings.Contains(c.Host, ":") && ipErr != nil:
		return fmt.Errorf("postgres: host %q must not include a port (set Port) or IPv6 brackets", c.Host)
	case c.Port < 0 || c.Port > 65535:
		return fmt.Errorf("postgres: port %d is out of range", c.Port)
	case c.DbName == "":
		return errors.New("postgres: database name must not be empty")
	case c.SslMode != "" && !slices.Contains(sslModes, c.SslMode):
		return fmt.Errorf("postgres: sslmode %q is not one of %s", c.SslMode, strings.Join(sslModes, ", "))
	case c.ConnectTimeout < 0:
		return errors.New("postgres: connect timeout must not be negative")
	case c.MaxOpenConns < 0 || c.MaxIdleConns < 0 || c.ConnMaxLifetime < 0:
		return errors.New("postgres: pool settings must not be negative")
	}
	for k := range c.Params {
		if slices.Contains(reservedParams, strings.ToLower(k)) {
			return fmt.Errorf("postgres: param %q is set by its own field, not in Params", k)
		}
	}
	return nil
}

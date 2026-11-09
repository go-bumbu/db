# Developing db

## Overview

All code lives under `gormdb/`. The core package (`gormdb/`) holds the config, `Open`/`Close` and the SQLite writer/reader model, and imports no database driver. Each driver is a subpackage: `gormdb/sqlite` (glebarez, pure Go), `gormdb/sqlitecgo` (mattn, CGO) and `gormdb/postgres` (pgx). Both SQLite drivers run the shared behaviour suite in `gormdb/internal/sqlitetest`. `gormdb/gormdbtest` opens a temp-file SQLite database for apps' tests.

The tests against a real Postgres in Docker live in `integration/`, a module of its own (with a `replace` to the parent), so testcontainers and Docker never reach the library's `go.mod` or the apps that import it. `make test-all` and `make lint` include it; `go test ./...` at the root does not.

Design decisions and the reasons behind them are in [AGENTS.md](AGENTS.md).

## Prerequisites

- Go 1.26 or newer (see `go.mod`)
- A C compiler (gcc) for `gormdb/sqlitecgo`, with `CGO_ENABLED=1`
- Docker, for the Postgres tests in `make test-all` (the `integration/` module)
- [golangci-lint](https://github.com/golangci/golangci-lint) v2 for `make lint`
- [go-licence-detector](https://github.com/elastic/go-licence-detector) for `make license-check`

## Common tasks

```sh
make help            # list the targets
make test            # fast tests; no Docker, Postgres tests are skipped
make test-all        # all tests, including Postgres in Docker (TESTDBS_ALL=1)
make lint            # golangci-lint
make coverage        # fail if a package is below 70% coverage
make verify          # the full check: test, license-check, lint, benchmark, coverage
```

Run `make verify` before opening a PR. CI runs the tests, the coverage check, lint and the license check on every push to `main` and on pull requests.

## Release

Releases are git tags. From a clean `main`:

```sh
make tag version=v0.1.0
```

It runs `make verify`, then creates the annotated tag and pushes it to `origin`. Go modules are served from the tag; there is nothing else to publish.

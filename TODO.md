<!-- todo:guide — managed by todo; this block is rewritten on save. Docs: https://github.com/candy-tools/todo
This file is a todo list managed by "todo", a terminal TODO app:
https://github.com/candy-tools/todo

todo watches this file and reloads it automatically when it changes on disk, so
you — human or agent — can edit it directly in any editor. Keep to this format
so todo can parse what you write:

  # Heading           Headings ("#" to "######") are categories; they nest by
                      heading level.
  - [ ] Open task     A "- [ ]" line is an open task; "- [x]" marks it done.
  - [/] In progress   "- [/]" flags a task in progress, "- [>]" defers it.
  - [x] Done task     Tasks must live under a category heading.
    - [ ] Subtask     Indent by two spaces to nest a subtask under a task.
    Description text  An indented, non-checkbox line is the task's description.

Notes for editors:
- Text above the first heading (this block included) is preserved on save.
- todo rewrites the file into the canonical form above on every change, so any
  other free-form markdown placed between items is not kept.

Prefer the CLI over editing by hand: it writes this file in the exact format
above. `todo add`, `todo done|progress|defer|reopen`, `todo edit`, `todo rm`
and `todo list --json` cover the common actions — run `todo --help`.
-->

# Context

- [ ] Read this first: why this module exists
  Goal: one shared GORM database foundation for bumbu apps (repo description: "GORM database helpers: open SQLite or Postgres from config and handle both drivers the same way").
  Today the same capability exists three times and has drifted:
  - reference-app: /home/bott/datos/edit/programacion/bumbu/reference-app/libs/gormdb/gormdb.go (sqlite+postgres, structured Cfg, Validate, pure-Go glebarez driver, pragmas in DSN incl. foreign_keys, one pool of 10). This is the starting point.
  - aether: /home/bott/datos/edit/programacion/andresbott/aether/server/app/cmd/db.go (sqlite only, glebarez, busy_timeout in DSN, WAL via Exec, no foreign_keys).
  - codex: /home/bott/datos/edit/programacion/andresbott/codex/app/database/database.go (sqlite+postgres from a raw DSN, CGO gorm.io/driver/sqlite, single writer + read-only pool via gorm.io/plugin/dbresolver, no validation).
  Rules: this is a library. It must never import any app. Constructors take a single Cfg struct (same convention as /home/bott/datos/edit/programacion/bumbu/http). Keep dependencies minimal and the go directive as low as the deps allow.
  Do not push, tag, or open PRs. Andrés does that. Commit only when asked.

# Decisions (confirm with Andrés before coding)

- [x] SQLite driver: pure-Go glebarez (recommended) or CGO mattn
  Outcome: drivers are pluggable, the app passes gormdb.Drivers. gormdb/sqlite (glebarez, pure Go, no CGO) and gormdb/sqlitecgo (mattn, CGO, extensions such as sqlite-vec). Per the benchmark glebarez writes about 100x slower than mattn in WAL mode.
  reference-app and aether use github.com/glebarez/sqlite; codex uses gorm.io/driver/sqlite (mattn).
- [x] SQLite connection model: single writer + read pool (recommended) or one shared pool
  Outcome: single writer + read pool is the only model (codex's model). Caveat documented on gormdb.Open: inside a transaction use only tx; a statement on db that reaches the writer deadlocks (and for :memory: any statement does).
- [x] Go version floor
  Outcome: the go directive is 1.26.0 (Andrés's call; the dependencies also require it).

# Scaffold the module

- [x] go.mod
  go mod init github.com/go-bumbu/db (the remote is already git@github.com:go-bumbu/db.git). Packages live in subdirectories (gormdb/). No code at the root.
- [x] Copy tooling from the http module
  Copy from /home/bott/datos/edit/programacion/bumbu/http and adapt: Makefile (test, lint, benchmark, license-check, coverage, verify, tag), .golangci.yaml, allowedLicenses.json, .github/workflows, LICENSE.
  In the Makefile coverage target, change the package list to this module's packages (./gormdb/...). Keep COVERAGE_THRESHOLD at 70.
  Run go-licence-detector against the new deps (gorm, glebarez/modernc, pgx) and extend allowedLicenses.json only if needed.
- [x] AGENTS.md and CLAUDE.md
  Follow the shape of /home/bott/datos/edit/programacion/bumbu/http/AGENTS.md: Commands, Architecture (one bullet per package), Key Design Decisions, Linting. CLAUDE.md contains just the line @AGENTS.md.
  Put the design decisions in AGENTS.md: pragmas in the DSN (not Exec, because pragmas are per connection), foreign_keys on for SQLite so it behaves like Postgres, the writer/reader split and its deadlock caveat, sslmode defaults to require, the DSN escapes user/password/db name.
- [x] README.md
  Keep it short: name + one-sentence description, what it does, go get github.com/go-bumbu/db, a happy-path example (gormdb.Open for sqlite, defer gormdb.Close), one-line license footer. Use the /gitauto:generate-readme skill if it is available.

# Package gormdb

- [x] Port libs/gormdb from reference-app
  Copy /home/bott/datos/edit/programacion/bumbu/reference-app/libs/gormdb/gormdb.go and gormdb_test.go into gormdb/ unchanged, then make go test ./... pass. Public API to keep: Cfg, SqliteCfg, PostgresCfg, DriverSqlite, DriverPostgres, Cfg.Validate, PostgresCfg.DSN, Open, Close.
  Keep the package doc comment, and also the comment on sqliteDSN that explains why pragmas go in the DSN.
- [x] SQLite: single writer + read-only pool
  Done: one write connection (WAL, immediate txlock) plus a read-only pool routed by dbresolver; Close releases both; in-memory falls back to one connection. Routing contract is documented on gormdb.Open.
- [x] Postgres tests against a real database
  Only DSN and Validate are covered today. Use github.com/go-bumbu/testdbs (/home/bott/datos/edit/programacion/bumbu/testdbs, starts DBs in Docker) to Open a real Postgres, run a query and Close it. Gate it as a slow test, the same way testdbs gates its slow DBs, so make test stays fast without Docker.
- [x] DSN tests
  Test PostgresCfg.DSN: special characters in user, password and db name (@ : / ? # %) are escaped; port defaults to 5432; sslmode defaults to require; an empty user leaves the user out of the URL.
- [x] Document errors.Is with gorm sentinels (dberr dropped)
  Open sets TranslateError and the SQLite drivers wrap Translate, so callers use errors.Is(err, gorm.ErrDuplicatedKey) / gorm.ErrForeignKeyViolated. Documented in README.md and AGENTS.md.

# Release and adoption

- [ ] make verify passes; Andrés tags v0.1.0
  Run make verify (test, license-check, lint, benchmark, coverage). Report the result and stop: Andrés creates the tag with make tag version=v0.1.0. Never tag or push yourself.
- [ ] Move reference-app to the module
  In /home/bott/datos/edit/programacion/bumbu/reference-app: import github.com/go-bumbu/db/gormdb, delete libs/gormdb, update app/cmd/server.go and app/router/openapi_test.go.
  gormdb.Open now takes gormdb.Drivers: the app imports gormdb/sqlite and gormdb/postgres and passes gormdb.Drivers{Sqlite: sqlite.Driver{}, Postgres: postgres.Driver{}}.
  Update the docs in the same change: docs/agents/architecture.md (the libs diagram lists libs/gormdb), docs/reference/modules-and-database.md (it says "copy libs/gormdb as is"; change it to "import github.com/go-bumbu/db/gormdb"), docs/reference/config-and-cli.md, and .go-deps-view.config if it lists libs/gormdb.
- [ ] Move aether to the module
  Replace openDB in /home/bott/datos/edit/programacion/andresbott/aether/server/app/cmd/db.go with gormdb.Open; aether keeps its own dbPath. Pass aether's GORM logger through Cfg.Logger.
  Watch out: aether gains foreign_keys(1). Before switching, run PRAGMA foreign_key_check on a real aether database to make sure existing data does not break any foreign key.
- [ ] Move codex to the module
  Replace /home/bott/datos/edit/programacion/andresbott/codex/app/database with gormdb.
  dberr was dropped and gormdb.Open turns TranslateError on, so codex's existing errors.Is(err, gorm.ErrDuplicatedKey) checks start working as they are; the string-matching fallback in wiki/pages.go can go.
  codex config takes a raw DSN today: mapping it to the structured Cfg changes codex's config format, so agree on the migration with Andrés first.
  SQLite Path must be a filesystem path or ":memory:": codex tests that use file::memory: URIs must change.
  Pass codex's logger (newLogger, which ignores ErrRecordNotFound) through Cfg.Logger.
  Driver: codex can keep mattn via gormdb/sqlitecgo (no benchmark needed), or switch to glebarez via gormdb/sqlite (then re-run codex's sqlite concurrency benchmark before and after).

# Later (wait for a real need)

- [>] Versioned migrations
  All apps use AutoMigrate, which can only add things. codex already calls Migrator() by hand about 11 times. Evaluate github.com/go-gormigrate/gormigrate before writing a runner of our own.
- [>] Cursor (keyset) pagination helper
  codex uses cursors heavily (/home/bott/datos/edit/programacion/andresbott/codex/internal/model/media/filelist.go, liveindexes.go); aether pages with Offset. Extract only the generic part (opaque cursor encoding + keyset WHERE clause), and only if it is not tied to media sorting.
- [>] SQLite backup and housekeeping
  A consistent backup (VACUUM INTO), a WAL checkpoint, an integrity check. Copying the db file without its -wal file loses recent writes. None of the apps does this yet.

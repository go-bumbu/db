// Package gormdb opens a GORM connection to the database selected by
// configuration. Business modules take a *gorm.DB and get the same API and
// the same error sentinels (gorm.ErrDuplicatedKey, gorm.ErrForeignKeyViolated,
// ...) on SQLite and on Postgres.
//
// SQLite adds rules of its own, as its writer is a single connection (see
// Open):
//   - inside db.Transaction or db.Connection, use only tx: a statement on db
//     waits for the connection the callback holds, forever;
//   - SQL that depends on the connection (last_insert_rowid(), changes(),
//     temp tables) needs a transaction or db.Connection;
//   - db.DB() is the writer pool and must not be tuned;
//   - WITH ... queries, even read-only ones, go to the single writer.
//
// Code that keeps to these rules runs on both databases; test it on
// gormdbtest.Sqlite, a temporary file with the same writer/reader setup.
//
// The core package imports no database driver. The application picks the
// drivers it wants (see the driver subpackages) and passes them to Open, so
// the binary only links the databases it actually uses.
package gormdb

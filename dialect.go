package gomigration

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// sqlDialect captures the syntactic and operational differences between the
// SQL backends supported by baseDriver: how bind parameters and identifiers
// are written, and how (if at all) a cross-process advisory lock is taken.
type sqlDialect struct {
	placeholder func(position int) string
	quoteIdent  func(name string) string
	// lock acquires a session-scoped advisory lock on the given connection,
	// returning a function that releases it. GET_LOCK/pg_advisory_lock are
	// tied to the specific connection/session that took them, so the caller
	// must keep conn checked out (not returned to the pool) until after
	// unlock runs. A nil lock means the backend has no such mechanism.
	lock func(ctx context.Context, conn *sql.Conn, migrationTableName string) (unlock func(context.Context) error, err error)
}

func placeholderQuestion(int) string { return "?" }
func placeholderDollar(position int) string {
	return fmt.Sprintf("$%d", position)
}

// quoteIdentWith returns a quoting function that wraps each dot-separated
// segment of an identifier individually, so schema-qualified names such as
// "myschema.mytable" are quoted safely rather than treated as one token.
func quoteIdentWith(quote string) func(string) string {
	return func(name string) string {
		parts := strings.Split(name, ".")
		for i, p := range parts {
			parts[i] = quote + p + quote
		}
		return strings.Join(parts, ".")
	}
}

var (
	quoteBacktick    = quoteIdentWith("`")
	quoteDoubleQuote = quoteIdentWith(`"`)
)

// migrationLockTimeoutSeconds bounds how long MySQL's GET_LOCK will wait for
// another process to release the migration lock.
const migrationLockTimeoutSeconds = 10

// lockNameFor derives a session-lock name for the given migration table,
// truncated to fit MySQL's 64-character GET_LOCK identifier limit.
func lockNameFor(migrationTableName string) string {
	name := "gomigration:" + migrationTableName
	if len(name) > 64 {
		name = name[:64]
	}
	return name
}

func mysqlAdvisoryLock(ctx context.Context, conn *sql.Conn, migrationTableName string) (func(context.Context) error, error) {
	lockName := lockNameFor(migrationTableName)

	var result sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, ?)", lockName, migrationLockTimeoutSeconds).Scan(&result); err != nil {
		return nil, fmt.Errorf("failed to acquire migration lock: %w", err)
	}
	if !result.Valid || result.Int64 != 1 {
		return nil, fmt.Errorf("failed to acquire migration lock %q: timed out waiting for another process", lockName)
	}

	return func(ctx context.Context) error {
		var result sql.NullInt64
		if err := conn.QueryRowContext(ctx, "SELECT RELEASE_LOCK(?)", lockName).Scan(&result); err != nil {
			return err
		}
		if !result.Valid || result.Int64 != 1 {
			return fmt.Errorf("failed to release migration lock %q: lock was not held by this session", lockName)
		}
		return nil
	}, nil
}

func postgresAdvisoryLock(ctx context.Context, conn *sql.Conn, migrationTableName string) (func(context.Context) error, error) {
	lockName := lockNameFor(migrationTableName)

	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock(hashtext($1))", lockName); err != nil {
		return nil, fmt.Errorf("failed to acquire migration lock: %w", err)
	}

	return func(ctx context.Context) error {
		_, err := conn.ExecContext(ctx, "SELECT pg_advisory_unlock(hashtext($1))", lockName)
		return err
	}, nil
}

var mysqlDialect = sqlDialect{
	placeholder: placeholderQuestion,
	quoteIdent:  quoteBacktick,
	lock:        mysqlAdvisoryLock,
}

var postgresDialect = sqlDialect{
	placeholder: placeholderDollar,
	quoteIdent:  quoteDoubleQuote,
	lock:        postgresAdvisoryLock,
}

// SQLite is typically single-writer/embedded; the OS-level file lock it
// already takes on write is sufficient, so no advisory lock is configured.
var sqliteDialect = sqlDialect{
	placeholder: placeholderQuestion,
	quoteIdent:  quoteDoubleQuote,
	lock:        nil,
}

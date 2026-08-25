package gomigration

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

// SqliteDriver is a driver for sqlite
type SqliteDriver struct {
	baseDriver
}

// NewSqliteDriver creates a new SqliteDriver
func NewSqliteDriver(
	database string,
) (*SqliteDriver, error) {
	// Open database
	db, err := sql.Open("sqlite3", database)
	if err != nil {
		return nil, err
	}

	// Ping database
	if err := db.Ping(); err != nil {
		return nil, err
	}

	// Return the driver with a default table name
	return &SqliteDriver{
		baseDriver: baseDriver{
			db:                 db,
			migrationTableName: "migrations",
			dialect:            sqliteDialect,
		},
	}, nil
}

// CleanDatabase drops all tables from the current database, except the
// migrations tracking table itself.
func (d *SqliteDriver) CleanDatabase(ctx context.Context) error {
	// Disable FK checks temporarily
	_, err := d.db.ExecContext(ctx, `PRAGMA foreign_keys = OFF;`)
	if err != nil {
		return fmt.Errorf("failed to disable FK checks: %w", err)
	}

	// Get all user-defined table names (excluding sqlite internal tables)
	rows, err := d.db.QueryContext(ctx, `
		SELECT name
		FROM sqlite_master
		WHERE type = 'table'
		AND name NOT LIKE 'sqlite_%';
	`)
	if err != nil {
		return fmt.Errorf("failed to query tables: %w", err)
	}
	defer rows.Close()

	var tableNames []string
	migrationsTableExists := false
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			return fmt.Errorf("failed to scan table name: %w", err)
		}
		if table == d.migrationTableName {
			migrationsTableExists = true
			continue
		}
		tableNames = append(tableNames, fmt.Sprintf(`"%s"`, table))
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("failed to read table names: %w", err)
	}

	// Drop all tables (SQLite doesn't support dropping multiple tables in one statement)
	for _, tableName := range tableNames {
		dropSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s;", tableName)
		if _, err := d.db.ExecContext(ctx, dropSQL); err != nil {
			return fmt.Errorf("failed to drop table %s: %w", tableName, err)
		}
	}

	// The migrations table itself is kept rather than dropped, but its
	// history rows are cleared so Migrate() doesn't see them as already
	// applied against a database that was just wiped.
	if migrationsTableExists {
		if err := d.clearMigrationHistory(ctx); err != nil {
			return fmt.Errorf("failed to clear migration history: %w", err)
		}
	}

	// Re-enable FK checks
	_, err = d.db.ExecContext(ctx, `PRAGMA foreign_keys = ON;`)
	if err != nil {
		return fmt.Errorf("failed to re-enable FK checks: %w", err)
	}

	return nil
}

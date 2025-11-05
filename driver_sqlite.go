package gomigration

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

// SqliteDriver is a driver for sqlite
type SqliteDriver struct {
	db                 *sql.DB
	migrationTableName string
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
	return &(SqliteDriver{db, "migrations"}), nil
}

// Close closes the database connection
func (d *SqliteDriver) Close() error {
	if d.db != nil {
		if err := d.db.Close(); err != nil {
			return err
		}
	}

	return nil
}

// SetMigrationTableName sets the migration table name of the migration tracking table
func (d *SqliteDriver) SetMigrationTableName(name string) {
	if name == "" {
		name = "migrations"
	}
	d.migrationTableName = name
}

// CreateMigrationTable creates the migration tracking table
func (d *SqliteDriver) CreateMigrationsTable(ctx context.Context) error {
	query := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			name VARCHAR(255) PRIMARY KEY NOT NULL,
			executed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
	`, d.migrationTableName)

	_, err := d.db.ExecContext(ctx, query)
	return err
}

// GetExecutedMigrations returns a list of previously executed migrations
func (d *SqliteDriver) GetExecutedMigrations(ctx context.Context, reverse bool) ([]ExecutedMigration, error) {
	order := "ASC"
	if reverse {
		order = "DESC"
	}

	query := fmt.Sprintf(`SELECT name, executed_at FROM %s ORDER BY name %s`, d.migrationTableName, order)
	rows, err := d.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var migrations []ExecutedMigration
	for rows.Next() {
		var name string
		var executedAt time.Time
		if err := rows.Scan(&name, &executedAt); err != nil {
			return nil, err
		}
		migrations = append(migrations, ExecutedMigration{Name: name, ExecutedAt: executedAt})
	}

	return migrations, rows.Err()
}

// CleanDatabase drops all table from the current database.
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
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			return fmt.Errorf("failed to scan table name: %w", err)
		}
		tableNames = append(tableNames, fmt.Sprintf(`"%s"`, table))
	}

	// No tables to drop
	if len(tableNames) == 0 {
		// Re-enable FK checks before returning
		_, _ = d.db.ExecContext(ctx, `PRAGMA foreign_keys = ON;`)
		return nil
	}

	// Drop all tables (SQLite doesn't support dropping multiple tables in one statement)
	for _, tableName := range tableNames {
		dropSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s;", tableName)
		_, err = d.db.ExecContext(ctx, dropSQL)
		if err != nil {
			return fmt.Errorf("failed to drop table %s: %w", tableName, err)
		}
	}

	// Re-enable FK checks
	_, err = d.db.ExecContext(ctx, `PRAGMA foreign_keys = ON;`)
	if err != nil {
		return fmt.Errorf("failed to re-enable FK checks: %w", err)
	}

	return nil
}

// ApplyMigrations applies a batch of "up" migrations with optional callbacks.
func (d *SqliteDriver) ApplyMigrations(
	ctx context.Context,
	migrations []Migration,
	onRunning func(migration *Migration),
	onSuccess func(migration *Migration),
	onFailed func(migration *Migration, err error),
) error {
	for i := range migrations {
		mig := migrations[i]

		if onRunning != nil {
			onRunning(&mig)
		}

		// Execute the migration SQL
		if err := d.executeMigrationSQL(ctx, mig.UpScript()); err != nil {
			if onFailed != nil {
				onFailed(&mig, err)
			}
			return fmt.Errorf("failed to apply migration %s: %w", mig.Name(), err)
		}

		// Record the migration
		if err := d.insertExecutedMigration(ctx, mig.Name(), time.Now()); err != nil {
			if onFailed != nil {
				onFailed(&mig, err)
			}
			return fmt.Errorf("failed to record migration %s: %w", mig.Name(), err)
		}

		if onSuccess != nil {
			onSuccess(&mig)
		}
	}
	return nil
}

// UnapplyMigrations rolls back a batch of "down" migrations with optional callbacks.
func (d *SqliteDriver) UnapplyMigrations(
	ctx context.Context,
	migrations []Migration,
	onRunning func(migration *Migration),
	onSuccess func(migration *Migration),
	onFailed func(migration *Migration, err error),
) error {
	for i := range migrations {
		mig := migrations[i]

		if onRunning != nil {
			onRunning(&mig)
		}

		// Execute the down migration SQL
		if err := d.executeMigrationSQL(ctx, mig.DownScript()); err != nil {
			if onFailed != nil {
				onFailed(&mig, err)
			}
			return fmt.Errorf("failed to unapply migration %s: %w", mig.Name(), err)
		}

		// Remove migration record from tracking table
		if err := d.removeExecutedMigration(ctx, mig.Name()); err != nil {
			if onFailed != nil {
				onFailed(&mig, err)
			}
			return fmt.Errorf("failed to remove migration record %s: %w", mig.Name(), err)
		}

		if onSuccess != nil {
			onSuccess(&mig)
		}
	}
	return nil
}

// executeMigrationSQL runs a raw SQL migration script.
func (d *SqliteDriver) executeMigrationSQL(ctx context.Context, sql string) error {
	if sql == "" {
		return nil
	}
	_, err := d.db.ExecContext(ctx, sql)
	return err
}

// insertExecutedMigration logs a migration into the migration tracking table.
func (d *SqliteDriver) insertExecutedMigration(ctx context.Context, name string, executedAt time.Time) error {
	query := fmt.Sprintf(`INSERT INTO %s (name, executed_at) VALUES (?, ?)`, d.migrationTableName)
	_, err := d.db.ExecContext(ctx, query, name, executedAt)
	return err
}

// removeExecutedMigration deletes a migration record from the migration table.
func (d *SqliteDriver) removeExecutedMigration(ctx context.Context, name string) error {
	query := fmt.Sprintf(`DELETE FROM %s WHERE name = ?`, d.migrationTableName)
	_, err := d.db.ExecContext(ctx, query, name)
	return err
}

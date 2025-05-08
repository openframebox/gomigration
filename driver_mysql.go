package gomigration

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql" // Import MySQL driver for database/sql
)

// MySqlDriver implements the Driver interface for MySQL.
type MySqlDriver struct {
	db                 *sql.DB
	migrationTableName string
}

// NewMySqlDriver initializes a new MySqlDriver with the given DB config.
func NewMySqlDriver(
	host string,
	port string,
	user string,
	password string,
	database string,
	charset string,
) (*MySqlDriver, error) {
	if charset == "" {
		charset = "utf8mb4"
	}

	// Build DSN string for MySQL connection
	dsn := fmt.Sprintf(
		"%s:%s@tcp(%s:%s)/%s?charset=%s&parseTime=True&loc=Local",
		user, password, host, port, database, charset,
	)

	// Open a new DB connection
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}

	// Test the DB connection
	if err := db.Ping(); err != nil {
		return nil, err
	}

	// Return the driver with a default table name
	return &MySqlDriver{
		db:                 db,
		migrationTableName: "migrations",
	}, nil
}

// Close closes the database connection.
func (m *MySqlDriver) Close() error {
	if m.db != nil {
		if err := m.db.Close(); err != nil {
			return err
		}
	}
	return nil
}

// SetMigrationTableName sets the name of the migration tracking table.
func (m *MySqlDriver) SetMigrationTableName(name string) {
	if name == "" {
		name = "migrations"
	}
	m.migrationTableName = name
}

// CreateMigrationsTable creates the migration table if it doesn't exist.
func (m *MySqlDriver) CreateMigrationsTable(ctx context.Context) error {
	query := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			name VARCHAR(255) PRIMARY KEY NOT NULL,
			executed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`, m.migrationTableName)
	_, err := m.db.ExecContext(ctx, query)
	return err
}

// GetExecutedMigrations returns a list of previously executed migrations, optionally in reverse order.
func (m *MySqlDriver) GetExecutedMigrations(ctx context.Context, reverse bool) ([]ExecutedMigration, error) {
	order := "ASC"
	if reverse {
		order = "DESC"
	}

	query := fmt.Sprintf(`SELECT name, executed_at FROM %s ORDER BY name %s`, m.migrationTableName, order)
	rows, err := m.db.QueryContext(ctx, query)
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

// CleanDatabase drops all tables from the current database.
func (m *MySqlDriver) CleanDatabase(ctx context.Context) error {
	// Disable FK checks temporarily
	_, err := m.db.ExecContext(ctx, `SET FOREIGN_KEY_CHECKS = 0;`)
	if err != nil {
		return fmt.Errorf("failed to disable FK checks: %w", err)
	}

	// Get all user-defined table names
	rows, err := m.db.QueryContext(ctx, `
		SELECT table_name 
		FROM information_schema.tables 
		WHERE table_schema = DATABASE();
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
		tableNames = append(tableNames, fmt.Sprintf("`%s`", table))
	}

	// No tables to drop
	if len(tableNames) == 0 {
		return nil
	}

	// Drop all tables in one statement
	dropSQL := fmt.Sprintf("DROP TABLE %s;", strings.Join(tableNames, ", "))
	_, err = m.db.ExecContext(ctx, dropSQL)
	if err != nil {
		return fmt.Errorf("failed to drop tables: %w", err)
	}

	// Re-enable FK checks
	_, err = m.db.ExecContext(ctx, `SET FOREIGN_KEY_CHECKS = 1;`)
	if err != nil {
		return fmt.Errorf("failed to re-enable FK checks: %w", err)
	}

	return nil
}

// ApplyMigrations applies a batch of "up" migrations with optional callbacks.
func (m *MySqlDriver) ApplyMigrations(
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
		if err := m.executeMigrationSQL(ctx, mig.UpScript()); err != nil {
			if onFailed != nil {
				onFailed(&mig, err)
			}
			return fmt.Errorf("failed to apply migration %s: %w", mig.Name(), err)
		}

		// Record the migration
		if err := m.insertExecutedMigration(ctx, mig.Name(), time.Now()); err != nil {
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
func (m *MySqlDriver) UnapplyMigrations(
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
		if err := m.executeMigrationSQL(ctx, mig.DownScript()); err != nil {
			if onFailed != nil {
				onFailed(&mig, err)
			}
			return fmt.Errorf("failed to unapply migration %s: %w", mig.Name(), err)
		}

		// Remove migration record from tracking table
		if err := m.removeExecutedMigration(ctx, mig.Name()); err != nil {
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
func (m *MySqlDriver) executeMigrationSQL(ctx context.Context, sql string) error {
	if sql == "" {
		return nil
	}
	_, err := m.db.ExecContext(ctx, sql)
	return err
}

// insertExecutedMigration logs a migration into the migration tracking table.
func (m *MySqlDriver) insertExecutedMigration(ctx context.Context, name string, executedAt time.Time) error {
	query := fmt.Sprintf(`INSERT INTO %s (name, executed_at) VALUES (?, ?)`, m.migrationTableName)
	_, err := m.db.ExecContext(ctx, query, name, executedAt)
	return err
}

// removeExecutedMigration deletes a migration record from the migration table.
func (m *MySqlDriver) removeExecutedMigration(ctx context.Context, name string) error {
	query := fmt.Sprintf(`DELETE FROM %s WHERE name = ?`, m.migrationTableName)
	_, err := m.db.ExecContext(ctx, query, name)
	return err
}

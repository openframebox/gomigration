package gomigration

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/go-sql-driver/mysql" // Import MySQL driver for database/sql
)

// cleanDatabaseBatchSize caps how many tables are dropped per statement so
// CleanDatabase doesn't build an unbounded DROP TABLE for large schemas.
const cleanDatabaseBatchSize = 50

// MySqlDriver implements the Driver interface for MySQL.
type MySqlDriver struct {
	baseDriver
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
		baseDriver: baseDriver{
			db:                 db,
			migrationTableName: "migrations",
			dialect:            mysqlDialect,
		},
	}, nil
}

// CleanDatabase drops all tables from the current database, except the
// migrations tracking table itself.
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
	migrationsTableExists := false
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			return fmt.Errorf("failed to scan table name: %w", err)
		}
		if table == m.migrationTableName {
			migrationsTableExists = true
			continue
		}
		tableNames = append(tableNames, fmt.Sprintf("`%s`", table))
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("failed to read table names: %w", err)
	}

	// Drop tables in batches so the statement doesn't grow unbounded
	for _, batch := range chunkStrings(tableNames, cleanDatabaseBatchSize) {
		dropSQL := fmt.Sprintf("DROP TABLE %s;", strings.Join(batch, ", "))
		if _, err := m.db.ExecContext(ctx, dropSQL); err != nil {
			return fmt.Errorf("failed to drop tables: %w", err)
		}
	}

	// The migrations table itself is kept rather than dropped, but its
	// history rows are cleared so Migrate() doesn't see them as already
	// applied against a database that was just wiped.
	if migrationsTableExists {
		if err := m.clearMigrationHistory(ctx); err != nil {
			return fmt.Errorf("failed to clear migration history: %w", err)
		}
	}

	// Re-enable FK checks
	_, err = m.db.ExecContext(ctx, `SET FOREIGN_KEY_CHECKS = 1;`)
	if err != nil {
		return fmt.Errorf("failed to re-enable FK checks: %w", err)
	}

	return nil
}

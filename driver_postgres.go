// Package gomigration provides a PostgreSQL migration driver for managing and applying SQL migrations.
package gomigration

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"

	_ "github.com/lib/pq"
)

// PostgresDriver manages database connections and migration operations for PostgreSQL.
type PostgresDriver struct {
	baseDriver
}

// NewPostgresDriver creates and returns a new instance of PostgresDriver.
// It opens a connection to the given PostgreSQL database using the provided credentials and schema.
func NewPostgresDriver(
	host string,
	port string,
	user string,
	password string,
	database string,
	schema string,
) (*PostgresDriver, error) {
	dsn := "host=%s port=%s user=%s password=%s dbname=%s sslmode=disable search_path=%s"
	dsn = fmt.Sprintf(dsn, host, port, user, password, database, schema)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		return nil, err
	}

	return &PostgresDriver{
		baseDriver: baseDriver{
			db:                 db,
			migrationTableName: "migrations",
			dialect:            postgresDialect,
		},
	}, nil
}

// CleanDatabase drops all tables in the "public" schema, except the
// migrations tracking table itself.
func (p *PostgresDriver) CleanDatabase(ctx context.Context) error {
	rows, err := p.db.QueryContext(ctx, `
		SELECT tablename
		FROM pg_tables
		WHERE schemaname = 'public';
	`)
	if err != nil {
		return fmt.Errorf("query table names: %w", err)
	}
	defer rows.Close()

	var tables []string
	migrationsTableExists := false
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			return fmt.Errorf("scan table name: %w", err)
		}
		if table == p.migrationTableName {
			migrationsTableExists = true
			continue
		}
		tables = append(tables, fmt.Sprintf(`"%s"`, table)) // safely quote identifiers
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read table names: %w", err)
	}

	if len(tables) == 0 {
		log.Println("no tables to drop")
	} else {
		// Drop tables in batches so the statement doesn't grow unbounded
		for _, batch := range chunkStrings(tables, cleanDatabaseBatchSize) {
			query := fmt.Sprintf(`DROP TABLE IF EXISTS %s CASCADE;`, strings.Join(batch, ", "))
			if _, err := p.db.ExecContext(ctx, query); err != nil {
				return fmt.Errorf("drop tables: %w", err)
			}
		}
		log.Println("all public tables dropped")
	}

	// The migrations table itself is kept rather than dropped, but its
	// history rows are cleared so Migrate() doesn't see them as already
	// applied against a database that was just wiped.
	if migrationsTableExists {
		if err := p.clearMigrationHistory(ctx); err != nil {
			return fmt.Errorf("clear migration history: %w", err)
		}
	}

	return nil
}

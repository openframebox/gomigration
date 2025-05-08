// Package gomigration provides a PostgreSQL migration driver for managing and applying SQL migrations.
package gomigration

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

// PostgresDriver manages database connections and migration operations for PostgreSQL.
type PostgresDriver struct {
	db                 *sql.DB
	migrationTableName string
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
		db:                 db,
		migrationTableName: "migrations",
	}, nil
}

// Close closes the database connection.
func (p *PostgresDriver) Close() error {
	if p.db != nil {
		return p.db.Close()
	}
	return nil
}

// SetMigrationTableName sets the name of the table used to track executed migrations.
// If the provided name is empty, the default "migrations" is used.
func (p *PostgresDriver) SetMigrationTableName(name string) {
	if name == "" {
		name = "migrations"
	}
	p.migrationTableName = name
}

// CreateMigrationsTable creates the migration tracking table if it does not exist.
func (p *PostgresDriver) CreateMigrationsTable(ctx context.Context) error {
	query := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			name VARCHAR(255) PRIMARY KEY NOT NULL,
			executed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
	`, p.migrationTableName)
	_, err := p.db.ExecContext(ctx, query)
	return err
}

// GetExecutedMigrations returns a list of executed migrations from the tracking table.
// If reverse is true, the list is ordered descending by name.
func (p *PostgresDriver) GetExecutedMigrations(ctx context.Context, reverse bool) ([]ExecutedMigration, error) {
	order := "ASC"
	if reverse {
		order = "DESC"
	}
	query := fmt.Sprintf(`SELECT name, executed_at FROM %s ORDER BY name %s;`, p.migrationTableName, order)

	rows, err := p.db.QueryContext(ctx, query)
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
		migrations = append(migrations, ExecutedMigration{
			Name:       name,
			ExecutedAt: executedAt,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return migrations, nil
}

// CleanDatabase drops all tables in the "public" schema.
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
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			return fmt.Errorf("scan table name: %w", err)
		}
		tables = append(tables, fmt.Sprintf(`"%s"`, table)) // safely quote identifiers
	}

	if len(tables) == 0 {
		log.Println("no tables to drop")
		return nil
	}

	query := fmt.Sprintf(`DROP TABLE IF EXISTS %s CASCADE;`, strings.Join(tables, ", "))
	if _, err := p.db.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("drop tables: %w", err)
	}

	log.Println("all public tables dropped")
	return nil
}

// ApplyMigrations runs the "up" SQL scripts for the given migrations.
// Optional callbacks can be provided to track the progress of each migration.
func (p *PostgresDriver) ApplyMigrations(
	ctx context.Context,
	migrations []Migration,
	onRunning func(migration *Migration),
	onSuccess func(migration *Migration),
	onFailed func(migration *Migration, err error),
) error {
	for i := range migrations {
		m := migrations[i]

		if onRunning != nil {
			onRunning(&m)
		}

		if err := p.executeMigrationSQL(ctx, m.UpScript()); err != nil {
			if onFailed != nil {
				onFailed(&m, err)
			}
			return fmt.Errorf("failed to apply migration %s: %w", m.Name(), err)
		}

		if err := p.insertExecutedMigration(ctx, m.Name(), time.Now()); err != nil {
			if onFailed != nil {
				onFailed(&m, err)
			}
			return fmt.Errorf("failed to record migration %s: %w", m.Name(), err)
		}

		if onSuccess != nil {
			onSuccess(&m)
		}
	}

	return nil
}

// UnapplyMigrations runs the "down" SQL scripts for the given migrations in reverse order.
// Optional callbacks can be provided to track the progress of each migration.
func (p *PostgresDriver) UnapplyMigrations(
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

		if err := p.executeMigrationSQL(ctx, mig.DownScript()); err != nil {
			if onFailed != nil {
				onFailed(&mig, err)
			}
			return fmt.Errorf("failed to unapply migration %s: %w", mig.Name(), err)
		}

		if err := p.removeExecutedMigration(ctx, mig.Name()); err != nil {
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

// executeMigrationSQL runs a given SQL script as part of a migration.
func (p *PostgresDriver) executeMigrationSQL(ctx context.Context, sql string) error {
	if sql == "" {
		return nil
	}

	_, err := p.db.ExecContext(ctx, sql)
	return err
}

// insertExecutedMigration records the given migration name and execution time in the tracking table.
func (p *PostgresDriver) insertExecutedMigration(ctx context.Context, name string, executedAt time.Time) error {
	query := fmt.Sprintf(`INSERT INTO %s (name, executed_at) VALUES ($1, $2)`, p.migrationTableName)
	_, err := p.db.ExecContext(ctx, query, name, executedAt)
	return err
}

// removeExecutedMigration deletes the record of the given migration from the tracking table.
func (p *PostgresDriver) removeExecutedMigration(ctx context.Context, name string) error {
	query := fmt.Sprintf(`DELETE FROM %s WHERE name = $1`, p.migrationTableName)
	_, err := p.db.ExecContext(ctx, query, name)
	return err
}

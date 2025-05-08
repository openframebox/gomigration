package gomigration

import (
	"context"
)

// Driver defines the contract for a migration driver implementation.
type Driver interface {
	// SetMigrationTableName sets the name of the table that stores executed migration records.
	SetMigrationTableName(name string)

	// CreateMigrationsTable creates the migration history table if it does not already exist.
	CreateMigrationsTable(ctx context.Context) error

	// GetExecutedMigrations returns the list of already executed migrations.
	// If reverse is true, the list is returned in descending order (most recent first).
	GetExecutedMigrations(ctx context.Context, reverse bool) ([]ExecutedMigration, error)

	// CleanDatabase drops or truncates all user tables in the database.
	CleanDatabase(ctx context.Context) error

	// ApplyMigrations applies a list of "up" migrations in sequence.
	// The onRunning, onSuccess, and onFailed callbacks are triggered accordingly for each migration.
	ApplyMigrations(
		ctx context.Context,
		migrations []Migration,
		onRunning func(migration *Migration),
		onSuccess func(migration *Migration),
		onFailed func(migration *Migration, err error),
	) error

	// UnapplyMigrations rolls back a list of "down" migrations in sequence.
	// The onRunning, onSuccess, and onFailed callbacks are triggered accordingly for each migration.
	UnapplyMigrations(
		ctx context.Context,
		migrations []Migration,
		onRunning func(migration *Migration),
		onSuccess func(migration *Migration),
		onFailed func(migration *Migration, err error),
	) error

	// Close gracefully closes the connection to the database or releases resources.
	Close() error
}

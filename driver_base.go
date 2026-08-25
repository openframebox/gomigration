package gomigration

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"
)

// baseDriver implements the parts of the Driver interface that are identical
// across backends, parameterized by a small sqlDialect. Concrete drivers
// embed it and only add their own constructor and CleanDatabase.
type baseDriver struct {
	db                 *sql.DB
	migrationTableName string
	dialect            sqlDialect
}

// Close closes the database connection.
func (b *baseDriver) Close() error {
	if b.db != nil {
		return b.db.Close()
	}
	return nil
}

// SetMigrationTableName sets the name of the migration tracking table. An
// invalid name (see sanitizeTableName) is rejected in favor of the default,
// since this method has no error return in the Driver interface.
func (b *baseDriver) SetMigrationTableName(name string) {
	if name == "" {
		name = "migrations"
	}
	if _, err := sanitizeTableName(name); err != nil {
		log.Printf("invalid migration table name %q, falling back to \"migrations\": %s\n", name, err)
		name = "migrations"
	}
	b.migrationTableName = name
}

func (b *baseDriver) quotedTableName() string {
	return b.dialect.quoteIdent(b.migrationTableName)
}

// clearMigrationHistory deletes all rows from the migrations table without
// dropping the table itself. CleanDatabase implementations call this when
// the migrations table exists, so a subsequent Migrate() re-applies every
// migration instead of finding stale records that make it look like
// everything is already up to date.
func (b *baseDriver) clearMigrationHistory(ctx context.Context) error {
	query := fmt.Sprintf(`DELETE FROM %s`, b.quotedTableName())
	_, err := b.db.ExecContext(ctx, query)
	return err
}

// CreateMigrationsTable creates the migration table if it doesn't exist.
func (b *baseDriver) CreateMigrationsTable(ctx context.Context) error {
	query := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			name VARCHAR(255) PRIMARY KEY NOT NULL,
			executed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`, b.quotedTableName())
	_, err := b.db.ExecContext(ctx, query)
	return err
}

// GetExecutedMigrations returns a list of previously executed migrations, optionally in reverse order.
func (b *baseDriver) GetExecutedMigrations(ctx context.Context, reverse bool) ([]ExecutedMigration, error) {
	order := "ASC"
	if reverse {
		order = "DESC"
	}

	query := fmt.Sprintf(`SELECT name, executed_at FROM %s ORDER BY name %s`, b.quotedTableName(), order)
	rows, err := b.db.QueryContext(ctx, query)
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

// withLock runs fn while holding the dialect's cross-process advisory lock,
// if the dialect defines one. This prevents two processes from racing on the
// same migrations table.
//
// GET_LOCK/pg_advisory_lock are scoped to the connection/session that took
// them, but *sql.DB freely hands out and reuses pooled connections, so
// acquiring and releasing through b.db directly can't guarantee both calls
// land on the same one. A single *sql.Conn is checked out of the pool for
// the whole locked section instead, so the lock and its later release are
// guaranteed to run on the same session.
func (b *baseDriver) withLock(ctx context.Context, fn func(ctx context.Context) error) error {
	if b.dialect.lock == nil {
		return fn(ctx)
	}

	conn, err := b.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire connection for migration lock: %w", err)
	}
	defer conn.Close()

	unlock, err := b.dialect.lock(ctx, conn, b.migrationTableName)
	if err != nil {
		return err
	}
	defer func() {
		if err := unlock(ctx); err != nil {
			log.Printf("failed to release migration lock: %s\n", err)
		}
	}()

	return fn(ctx)
}

// ApplyMigrations applies a batch of "up" migrations with optional callbacks.
// Each migration's UpScript and bookkeeping insert run inside a single
// transaction, and the whole batch is guarded by a cross-process lock where
// the dialect supports one.
func (b *baseDriver) ApplyMigrations(
	ctx context.Context,
	migrations []Migration,
	onRunning func(migration *Migration),
	onSuccess func(migration *Migration),
	onFailed func(migration *Migration, err error),
) error {
	return b.withLock(ctx, func(ctx context.Context) error {
		for i := range migrations {
			mig := migrations[i]

			if onRunning != nil {
				onRunning(&mig)
			}

			if err := b.applyOne(ctx, mig); err != nil {
				if onFailed != nil {
					onFailed(&mig, err)
				}
				return fmt.Errorf("failed to apply migration %s: %w", mig.Name(), err)
			}

			if onSuccess != nil {
				onSuccess(&mig)
			}
		}
		return nil
	})
}

// UnapplyMigrations rolls back a batch of "down" migrations with optional callbacks.
// Each migration's DownScript and bookkeeping delete run inside a single
// transaction, and the whole batch is guarded by a cross-process lock where
// the dialect supports one.
func (b *baseDriver) UnapplyMigrations(
	ctx context.Context,
	migrations []Migration,
	onRunning func(migration *Migration),
	onSuccess func(migration *Migration),
	onFailed func(migration *Migration, err error),
) error {
	return b.withLock(ctx, func(ctx context.Context) error {
		for i := range migrations {
			mig := migrations[i]

			if onRunning != nil {
				onRunning(&mig)
			}

			if err := b.unapplyOne(ctx, mig); err != nil {
				if onFailed != nil {
					onFailed(&mig, err)
				}
				return fmt.Errorf("failed to unapply migration %s: %w", mig.Name(), err)
			}

			if onSuccess != nil {
				onSuccess(&mig)
			}
		}
		return nil
	})
}

// applyOne runs a single migration's UpScript and its bookkeeping insert
// inside one transaction, so a mid-script failure can't leave the migration
// half-applied.
func (b *baseDriver) applyOne(ctx context.Context, m Migration) error {
	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	if script := m.UpScript(); script != "" {
		if _, err := tx.ExecContext(ctx, script); err != nil {
			return err
		}
	}

	query := fmt.Sprintf(`INSERT INTO %s (name, executed_at) VALUES (%s, %s)`,
		b.quotedTableName(), b.dialect.placeholder(1), b.dialect.placeholder(2))
	if _, err := tx.ExecContext(ctx, query, m.Name(), time.Now()); err != nil {
		return fmt.Errorf("failed to record migration: %w", err)
	}

	return tx.Commit()
}

// unapplyOne runs a single migration's DownScript and its bookkeeping delete
// inside one transaction.
func (b *baseDriver) unapplyOne(ctx context.Context, m Migration) error {
	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	if script := m.DownScript(); script != "" {
		if _, err := tx.ExecContext(ctx, script); err != nil {
			return err
		}
	}

	query := fmt.Sprintf(`DELETE FROM %s WHERE name = %s`, b.quotedTableName(), b.dialect.placeholder(1))
	if _, err := tx.ExecContext(ctx, query, m.Name()); err != nil {
		return fmt.Errorf("failed to remove migration record: %w", err)
	}

	return tx.Commit()
}

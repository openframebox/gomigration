// Package gomigration provides tools for managing database migrations in Go projects.
// It supports creating, applying, rolling back, and listing migrations using a
// customizable driver interface.
package gomigration

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

// GoMigration is the main struct for managing and executing database migrations.
type GoMigration struct {
	driver            Driver
	migrationFilesDir string
	debugSql          bool
	migrations        map[string]Migration
	mu                sync.Mutex
}

// New creates a new instance of GoMigration using the provided configuration.
// It validates and sets defaults for missing fields, checks for the migration
// directory, and applies configuration to the driver.
func New(config *Config) (*GoMigration, error) {
	if config == nil {
		return nil, ErrConfigNotProvided
	}
	if config.Driver == nil {
		return nil, ErrDriverNotProvided
	}

	if config.MigrationFilesDir == "" {
		config.MigrationFilesDir = "migrations"
	}
	if config.MigrationTableName == "" {
		config.MigrationTableName = "migrations"
	}

	if _, err := sanitizeTableName(config.MigrationTableName); err != nil {
		return nil, fmt.Errorf("invalid migration table name: %w", err)
	}

	if !migrationDirExists(config.MigrationFilesDir) {
		return nil, fmt.Errorf("migration directory %q does not exist", config.MigrationFilesDir)
	}

	config.Driver.SetMigrationTableName(config.MigrationTableName)

	return &GoMigration{
		driver:            config.Driver,
		migrationFilesDir: config.MigrationFilesDir,
		debugSql:          config.DebugSql,
		migrations:        make(map[string]Migration),
	}, nil
}

// Register adds one or more Migration instances to the internal registry.
// It ensures no duplicate migration names are registered.
func (q *GoMigration) Register(migrations ...Migration) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	for _, migration := range migrations {
		name := migration.Name()
		if name == "" {
			return ErrMigrationNameNotProvided
		}
		if _, exists := q.migrations[name]; exists {
			return fmt.Errorf("migration %s registered more than once", name)
		}
		q.migrations[name] = migration
	}

	return nil
}

// Create generates a new migration file using the given name.
// The generated file includes a timestamp prefix and basic template content.
func (q *GoMigration) Create(fileName string) error {
	if fileName == "" {
		return ErrMigrationNameNotProvided
	}

	migrationName, err := sanitizeMigrationName(fileName)
	if err != nil {
		return err
	}

	migrationName = fmt.Sprintf("%s_%s", time.Now().Format("20060102150405"), migrationName)
	migrationFileName := fmt.Sprintf("%s/%s.go", q.migrationFilesDir, migrationName)

	if fileExists(migrationFileName) {
		return ErrMigrationFileAlreadyExists
	}

	template, err := migrationFileTemplate(getPackageNameFromMigrationDir(q.migrationFilesDir), migrationName)
	if err != nil {
		return err
	}

	err = os.WriteFile(migrationFileName, []byte(template), 0644)
	if err != nil {
		return err
	}
	log.Printf("migration file created: %s\n", migrationFileName)

	return nil
}

// Migrate applies all pending migrations in the correct order.
// It skips migrations that have already been executed.
func (q *GoMigration) Migrate(ctx context.Context) error {
	if err := q.driver.CreateMigrationsTable(ctx); err != nil {
		return err
	}

	executedMigrations, err := q.driver.GetExecutedMigrations(ctx, false)
	if err != nil {
		return err
	}

	executedMap := make(map[string]struct{}, len(executedMigrations))
	for _, m := range executedMigrations {
		executedMap[m.Name] = struct{}{}
	}

	migrationsToApply := make([]Migration, 0, len(q.migrations))
	for _, name := range getSortedMigrationName(q.migrations) {
		migration := q.migrations[name]
		if _, found := executedMap[migration.Name()]; !found {
			migrationsToApply = append(migrationsToApply, migration)
		}
	}

	if len(migrationsToApply) == 0 {
		log.Println("✅ No migrations to run")
		return nil
	}

	log.Printf("🚀 Applying %d migration(s)...\n", len(migrationsToApply))

	return q.driver.ApplyMigrations(
		ctx,
		migrationsToApply,
		func(m *Migration) {
			log.Printf("📦 Migrating: %s\n", (*m).Name())
			if q.debugSql {
				log.Println("🧾 Running SQL:")
				fmt.Println("================================================")
				fmt.Println((*m).UpScript())
				fmt.Println("================================================")
			}
		},
		func(m *Migration) {
			log.Printf("✅ Migrated: %s\n", (*m).Name())
		},
		func(m *Migration, err error) {
			log.Printf("❌ Migration failed: %s - %s\n", (*m).Name(), err)
		},
	)
}

// Fresh wipes the database clean and reapplies all registered migrations from scratch.
func (q *GoMigration) Fresh(ctx context.Context) error {
	log.Println("🧹 Cleaning database...")

	if err := q.driver.CleanDatabase(ctx); err != nil {
		return fmt.Errorf("failed to clean database: %w", err)
	}

	log.Println("🚀 Running fresh migrations...")

	if err := q.Migrate(ctx); err != nil {
		return fmt.Errorf("failed to run migrations after cleaning: %w", err)
	}

	log.Println("✅ Fresh migration completed successfully")
	return nil
}

// Reset rolls back all applied migrations and reapplies them from scratch.
func (q *GoMigration) Reset(ctx context.Context) error {
	executedMigrations, err := q.driver.GetExecutedMigrations(ctx, true)
	if err != nil {
		return fmt.Errorf("failed to get executed migrations: %w", err)
	}

	if len(executedMigrations) == 0 {
		log.Println("✅ No migrations to reset")
		return nil
	}

	log.Printf("🔁 Resetting %d executed migration(s)...\n", len(executedMigrations))

	if err := q.Rollback(ctx, len(executedMigrations)); err != nil {
		return fmt.Errorf("rollback failed during reset: %w", err)
	}

	if err := q.Migrate(ctx); err != nil {
		return fmt.Errorf("migration failed during reset: %w", err)
	}

	log.Println("✅ Migration reset completed successfully")
	return nil
}

// Rollback undoes the last `step` number of executed migrations.
func (q *GoMigration) Rollback(ctx context.Context, step int) error {
	if step <= 0 {
		return ErrInvalidRollbackStep
	}

	executedMigrations, err := q.driver.GetExecutedMigrations(ctx, true)
	if err != nil {
		return err
	}

	if len(executedMigrations) == 0 {
		log.Println("✅ No migrations to rollback")
		return nil
	}

	if step > len(executedMigrations) {
		step = len(executedMigrations)
	}

	migrationMap := make(map[string]Migration, len(q.migrations))
	for _, m := range q.migrations {
		migrationMap[m.Name()] = m
	}

	migrationsToRollback := make([]Migration, 0, step)
	for i := range step {
		executedMigration := executedMigrations[i]
		if migration, found := migrationMap[executedMigration.Name]; found {
			migrationsToRollback = append(migrationsToRollback, migration)
		} else {
			log.Printf("⚠️  Migration not found for: %s\n", executedMigration.Name)
		}
	}

	if len(migrationsToRollback) == 0 {
		log.Println("✅ No migrations to rollback")
		return nil
	}

	log.Printf("🔁 Rolling back %d migration(s)...\n", len(migrationsToRollback))

	return q.driver.UnapplyMigrations(
		ctx,
		migrationsToRollback,
		func(m *Migration) {
			log.Printf("🔄 Rolling back: %s\n", (*m).Name())
			if q.debugSql {
				log.Println("🧾 Running SQL:")
				fmt.Println("================================================")
				fmt.Println((*m).DownScript())
				fmt.Println("================================================")
			}
		},
		func(m *Migration) {
			log.Printf("✅ Rolled back: %s\n", (*m).Name())
		},
		func(m *Migration, err error) {
			log.Printf("❌ Rollback failed: %s - %s\n", (*m).Name(), err)
		},
	)
}

// Clean drops all database tables and objects managed by the migration system.
func (q *GoMigration) Clean(ctx context.Context) error {
	log.Println("🧹 Cleaning database...")

	if err := q.driver.CleanDatabase(ctx); err != nil {
		return fmt.Errorf("failed to clean database: %w", err)
	}

	log.Println("✅ Database cleaned successfully")
	return nil
}

// List returns all registered migrations along with their execution status.
func (q *GoMigration) List(ctx context.Context) (RegisteredMigrationList, error) {
	if err := q.driver.CreateMigrationsTable(ctx); err != nil {
		return nil, err
	}

	executedMigrations, err := q.driver.GetExecutedMigrations(ctx, false)
	if err != nil {
		return nil, err
	}

	executedMap := make(map[string]struct {
		Executed   bool
		ExecutedAt *time.Time
	}, len(executedMigrations))

	for _, m := range executedMigrations {
		executedMap[m.Name] = struct {
			Executed   bool
			ExecutedAt *time.Time
		}{
			Executed:   true,
			ExecutedAt: &m.ExecutedAt,
		}
	}

	registeredMigrations := make(RegisteredMigrationList, 0, len(q.migrations))

	for _, k := range getSortedMigrationName(q.migrations) {
		migration := q.migrations[k]
		name := migration.Name()
		executed := executedMap[name]

		registeredMigrations = append(registeredMigrations, RegisteredMigration{
			Name:       name,
			UpScript:   migration.UpScript(),
			DownScript: migration.DownScript(),
			IsExecuted: executed.Executed,
			ExecutedAt: executed.ExecutedAt,
		})
	}

	return registeredMigrations, nil
}

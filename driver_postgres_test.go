package gomigration

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
)

func setupMockDBPostgres(t *testing.T) (*sql.DB, sqlmock.Sqlmock, *PostgresDriver) {
	db, mock, err := sqlmock.New(
		sqlmock.MonitorPingsOption(true),
	)
	assert.NoError(t, err)

	driver := &PostgresDriver{
		baseDriver: baseDriver{
			db:                 db,
			migrationTableName: "migrations",
			dialect:            postgresDialect,
		},
	}

	return db, mock, driver
}

func TestNewPostgresDriver(t *testing.T) {
	// Create a mock database connection
	db, mock, driver := setupMockDBPostgres(t)
	defer db.Close()

	// Simulate a successful ping to the DB
	mock.ExpectPing().WillReturnError(nil)

	// Test that the driver is initialized correctly
	assert.NotNil(t, driver)
}

func TestCreateMigrationsTablePostgresDriver(t *testing.T) {
	db, mock, driver := setupMockDBPostgres(t)
	defer db.Close()

	mock.ExpectExec(`CREATE TABLE IF NOT EXISTS`).WillReturnResult(sqlmock.NewResult(1, 1))

	err := driver.CreateMigrationsTable(context.Background())
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSetMigrationTableNamePostgresDriver(t *testing.T) {
	driver := &PostgresDriver{}

	driver.SetMigrationTableName("")
	assert.Equal(t, "migrations", driver.migrationTableName)

	driver.SetMigrationTableName("custom_migrations")
	assert.Equal(t, "custom_migrations", driver.migrationTableName)

	// Test invalid migration table name falls back to the default
	driver.SetMigrationTableName("bad name; DROP TABLE users;")
	assert.Equal(t, "migrations", driver.migrationTableName)
}

func TestGetExecutedMigrationsPostgresDriver(t *testing.T) {
	db, mock, driver := setupMockDBPostgres(t)
	defer db.Close()

	rows := sqlmock.NewRows([]string{"name", "executed_at"}).
		AddRow("migration_1", time.Now()).
		AddRow("migration_2", time.Now())

	mock.ExpectQuery(`SELECT name, executed_at FROM "migrations" ORDER BY name ASC`).
		WillReturnRows(rows)

	migrations, err := driver.GetExecutedMigrations(context.Background(), false)
	assert.NoError(t, err)
	assert.Len(t, migrations, 2)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestCleanDatabasePostgresDriver(t *testing.T) {
	db, mock, driver := setupMockDBPostgres(t)
	defer db.Close()

	// Mock finding tables
	tableRows := sqlmock.NewRows([]string{"tablename"}).
		AddRow("table1").
		AddRow("table2")

	mock.ExpectQuery(`SELECT tablename FROM pg_tables WHERE schemaname = 'public';`).
		WillReturnRows(tableRows)

	// Mock dropping tables
	mock.ExpectExec(`DROP TABLE IF EXISTS "table1", "table2" CASCADE;`).
		WillReturnResult(sqlmock.NewResult(0, 0))

	err := driver.CleanDatabase(context.Background())
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestCleanDatabasePostgresDriverExcludesMigrationsTable(t *testing.T) {
	db, mock, driver := setupMockDBPostgres(t)
	defer db.Close()

	tableRows := sqlmock.NewRows([]string{"tablename"}).
		AddRow("table1").
		AddRow("migrations")

	mock.ExpectQuery(`SELECT tablename FROM pg_tables WHERE schemaname = 'public';`).
		WillReturnRows(tableRows)

	// Only "table1" should be dropped; "migrations" must be excluded.
	mock.ExpectExec(`DROP TABLE IF EXISTS "table1" CASCADE;`).
		WillReturnResult(sqlmock.NewResult(0, 0))

	// The migrations table survives, but its history rows must be cleared.
	mock.ExpectExec(`DELETE FROM "migrations"`).WillReturnResult(sqlmock.NewResult(0, 0))

	err := driver.CleanDatabase(context.Background())
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestApplyMigrationsPostgresDriver(t *testing.T) {
	db, mock, driver := setupMockDBPostgres(t)
	defer db.Close()

	mig := &mockMigrationPostgresDriver{
		name: "migration1",
		up:   "CREATE TABLE test (id INT);",
		down: "DROP TABLE test;",
	}

	mock.ExpectExec(`SELECT pg_advisory_lock`).
		WithArgs("gomigration:migrations").
		WillReturnResult(sqlmock.NewResult(0, 0))

	mock.ExpectBegin()
	mock.ExpectExec("CREATE TABLE test \\(id INT\\);").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`INSERT INTO`).WithArgs("migration1", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	mock.ExpectExec(`SELECT pg_advisory_unlock`).
		WithArgs("gomigration:migrations").
		WillReturnResult(sqlmock.NewResult(0, 0))

	err := driver.ApplyMigrations(context.Background(), []Migration{mig}, nil, nil, nil)
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestUnapplyMigrationsPostgresDriver(t *testing.T) {
	db, mock, driver := setupMockDBPostgres(t)
	defer db.Close()

	mig := &mockMigrationPostgresDriver{
		name: "migration1",
		up:   "CREATE TABLE test (id INT);",
		down: "DROP TABLE test;",
	}

	mock.ExpectExec(`SELECT pg_advisory_lock`).
		WithArgs("gomigration:migrations").
		WillReturnResult(sqlmock.NewResult(0, 0))

	mock.ExpectBegin()
	mock.ExpectExec(mig.down).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`DELETE FROM`).WithArgs(mig.name).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	mock.ExpectExec(`SELECT pg_advisory_unlock`).
		WithArgs("gomigration:migrations").
		WillReturnResult(sqlmock.NewResult(0, 0))

	err := driver.UnapplyMigrations(context.Background(), []Migration{mig}, nil, nil, nil)
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestApplyMigrationsPostgresDriverRollsBackOnFailure(t *testing.T) {
	db, mock, driver := setupMockDBPostgres(t)
	defer db.Close()

	mig := &mockMigrationPostgresDriver{
		name: "migration1",
		up:   "CREATE TABLE test (id INT);",
	}

	mock.ExpectExec(`SELECT pg_advisory_lock`).
		WithArgs("gomigration:migrations").
		WillReturnResult(sqlmock.NewResult(0, 0))

	mock.ExpectBegin()
	mock.ExpectExec("CREATE TABLE test \\(id INT\\);").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`INSERT INTO`).WithArgs("migration1", sqlmock.AnyArg()).
		WillReturnError(assert.AnError)
	mock.ExpectRollback()

	mock.ExpectExec(`SELECT pg_advisory_unlock`).
		WithArgs("gomigration:migrations").
		WillReturnResult(sqlmock.NewResult(0, 0))

	err := driver.ApplyMigrations(context.Background(), []Migration{mig}, nil, nil, nil)
	assert.Error(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- Supporting mock types ---

type mockMigrationPostgresDriver struct {
	name string
	up   string
	down string
}

func (m *mockMigrationPostgresDriver) Name() string       { return m.name }
func (m *mockMigrationPostgresDriver) UpScript() string   { return m.up }
func (m *mockMigrationPostgresDriver) DownScript() string { return m.down }

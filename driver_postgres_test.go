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
		db:                 db,
		migrationTableName: "migrations",
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

	mock.ExpectExec(`CREATE TABLE IF NOT EXISTS migrations`).WillReturnResult(sqlmock.NewResult(1, 1))

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
}

func TestGetExecutedMigrationsPostgresDriver(t *testing.T) {
	db, mock, driver := setupMockDBPostgres(t)
	defer db.Close()

	rows := sqlmock.NewRows([]string{"name", "executed_at"}).
		AddRow("migration_1", time.Now()).
		AddRow("migration_2", time.Now())

	mock.ExpectQuery(`SELECT name, executed_at FROM migrations ORDER BY name ASC;`).
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

func TestApplyMigrationsPostgresDriver(t *testing.T) {
	db, mock, driver := setupMockDBPostgres(t)
	defer db.Close()

	mig := &mockMigrationPostgresDriver{
		name: "migration1",
		up:   "CREATE TABLE test (id INT);",
		down: "DROP TABLE test;",
	}

	mock.ExpectExec("CREATE TABLE test \\(id INT\\);").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`INSERT INTO migrations`).WithArgs("migration1", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

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

	mock.ExpectExec(mig.down).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`DELETE FROM migrations WHERE name = \$1`).WithArgs(mig.name).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err := driver.UnapplyMigrations(context.Background(), []Migration{mig}, nil, nil, nil)
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestExecuteMigrationSQLPostgresDriver(t *testing.T) {
	db, mock, driver := setupMockDBPostgres(t)
	defer db.Close()

	mock.ExpectExec(`SOME SQL STATEMENT`).WillReturnResult(sqlmock.NewResult(0, 0))

	err := driver.executeMigrationSQL(context.Background(), "SOME SQL STATEMENT")
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestInsertExecutedMigrationPostgresDriver(t *testing.T) {
	db, mock, driver := setupMockDBPostgres(t)
	defer db.Close()

	mock.ExpectExec(`INSERT INTO migrations`).WithArgs("migration_name", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err := driver.insertExecutedMigration(context.Background(), "migration_name", time.Now())
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRemoveExecutedMigrationPostgresDriver(t *testing.T) {
	db, mock, driver := setupMockDBPostgres(t)
	defer db.Close()

	mock.ExpectExec(`DELETE FROM migrations WHERE name = \$1`).WithArgs("migration_name").
		WillReturnResult(sqlmock.NewResult(1, 1))

	err := driver.removeExecutedMigration(context.Background(), "migration_name")
	assert.NoError(t, err)
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

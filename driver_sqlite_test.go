package gomigration

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
)

func setupMockDBSqlite(t *testing.T) (*sql.DB, sqlmock.Sqlmock, *SqliteDriver) {
	db, mock, err := sqlmock.New(
		sqlmock.MonitorPingsOption(true),
	)
	assert.NoError(t, err)

	driver := &SqliteDriver{
		db:                 db,
		migrationTableName: "migrations",
	}

	return db, mock, driver
}

func TestNewSqliteDriver(t *testing.T) {
	// Create a mock database connection
	db, mock, driver := setupMockDBSqlite(t)
	defer db.Close()

	// Simulate a successful ping to the DB
	mock.ExpectPing().WillReturnError(nil)

	// Test that the driver is initialized correctly
	assert.NotNil(t, driver)
}

func TestCreateMigrationsTableSqliteDriver(t *testing.T) {
	// Create a mock database connection
	db, mock, driver := setupMockDBSqlite(t)
	defer db.Close()

	// Simulate a successful table creation
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS migrations").WillReturnResult(sqlmock.NewResult(1, 1))

	// Call CreateMigrationTable
	err := driver.CreateMigrationsTable(context.Background())
	assert.NoError(t, err)
}

func TestSetMigrationTableNameSqliteDriver(t *testing.T) {
	driver := &SqliteDriver{}

	// Test default migration table name
	driver.SetMigrationTableName("")
	assert.Equal(t, "migrations", driver.migrationTableName)

	// Test custom migration table name
	driver.SetMigrationTableName("custom_migrations")
	assert.Equal(t, "custom_migrations", driver.migrationTableName)
}

func TestGetExecutedMigrationsSqliteDriver(t *testing.T) {
	// Create a mock database connection
	db, mock, driver := setupMockDBSqlite(t)
	defer db.Close()

	// Simulate the query to fetch migrations
	rows := sqlmock.NewRows([]string{"name", "executed_at"}).
		AddRow("migration_1", time.Now()).
		AddRow("migration_2", time.Now())

	mock.ExpectQuery("SELECT name, executed_at FROM migrations").
		WillReturnRows(rows)

	// Call GetExecutedMigrations
	migrations, err := driver.GetExecutedMigrations(context.Background(), false)
	assert.NoError(t, err)
	assert.Len(t, migrations, 2)
	assert.Equal(t, "migration_1", migrations[0].Name)
}

func TestCleanDatabaseSqliteDriver(t *testing.T) {
	db, mock, driver := setupMockDBSqlite(t)
	defer db.Close()

	ctx := context.Background()

	// 1. Expect disabling foreign key checks
	mock.ExpectExec(`PRAGMA foreign_keys = OFF;`).WillReturnResult(sqlmock.NewResult(0, 0))

	// 2. Expect selecting all table names
	mock.ExpectQuery(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%';`).
		WillReturnRows(
			sqlmock.NewRows([]string{"name"}).
				AddRow("users").
				AddRow("products"),
		)

	// 3. Expect dropping tables individually (SQLite doesn't support multiple drops in one statement)
	mock.ExpectExec(`DROP TABLE IF EXISTS "users";`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`DROP TABLE IF EXISTS "products";`).WillReturnResult(sqlmock.NewResult(0, 0))

	// 4. Expect re-enabling foreign key checks
	mock.ExpectExec(`PRAGMA foreign_keys = ON;`).WillReturnResult(sqlmock.NewResult(0, 0))

	// Act
	err := driver.CleanDatabase(ctx)

	assert.NoError(t, err)

	// Assert all expectations were met
	err = mock.ExpectationsWereMet()

	assert.NoError(t, err, "there were unfulfilled expectations")
}

func TestApplyMigrationsSqliteDriver(t *testing.T) {
	db, mock, driver := setupMockDBSqlite(t)
	defer db.Close()

	mig := &mockMigrationSqliteDriver{
		name: "migration1",
		up:   "CREATE TABLE test (id INTEGER);",
		down: "DROP TABLE test;",
	}

	mock.ExpectExec("CREATE TABLE test \\(id INTEGER\\);").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`INSERT INTO migrations`).WithArgs("migration1", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err := driver.ApplyMigrations(context.Background(), []Migration{mig}, nil, nil, nil)
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestUnapplyMigrationsSqliteDriver(t *testing.T) {
	db, mock, driver := setupMockDBSqlite(t)
	defer db.Close()

	mig := &mockMigrationSqliteDriver{
		name: "migration1",
		up:   "CREATE TABLE test (id INTEGER);",
		down: "DROP TABLE test;",
	}

	mock.ExpectExec(mig.down).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`DELETE FROM migrations WHERE name = ?`).WithArgs(mig.name).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err := driver.UnapplyMigrations(context.Background(), []Migration{mig}, nil, nil, nil)
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestExecuteMigrationSQLSqliteDriver(t *testing.T) {
	db, mock, driver := setupMockDBSqlite(t)
	defer db.Close()

	mock.ExpectExec(`SOME SQL STATEMENT`).WillReturnResult(sqlmock.NewResult(0, 0))

	err := driver.executeMigrationSQL(context.Background(), "SOME SQL STATEMENT")
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestInsertExecutedMigrationSqliteDriver(t *testing.T) {
	db, mock, driver := setupMockDBSqlite(t)
	defer db.Close()

	mock.ExpectExec(`INSERT INTO migrations`).WithArgs("migration_name", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err := driver.insertExecutedMigration(context.Background(), "migration_name", time.Now())
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRemoveExecutedMigrationSqliteDriver(t *testing.T) {
	db, mock, driver := setupMockDBSqlite(t)
	defer db.Close()

	mock.ExpectExec(`DELETE FROM migrations WHERE name = ?`).WithArgs("migration_name").
		WillReturnResult(sqlmock.NewResult(1, 1))

	err := driver.removeExecutedMigration(context.Background(), "migration_name")
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- Supporting mock types ---

type mockMigrationSqliteDriver struct {
	name string
	up   string
	down string
}

func (m *mockMigrationSqliteDriver) Name() string       { return m.name }
func (m *mockMigrationSqliteDriver) UpScript() string   { return m.up }
func (m *mockMigrationSqliteDriver) DownScript() string { return m.down }

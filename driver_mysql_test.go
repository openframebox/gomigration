package gomigration

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
)

func setupMockDBMySql(t *testing.T) (*sql.DB, sqlmock.Sqlmock, *MySqlDriver) {
	db, mock, err := sqlmock.New(
		sqlmock.MonitorPingsOption(true),
	)
	assert.NoError(t, err)

	driver := &MySqlDriver{
		db:                 db,
		migrationTableName: "migrations",
	}

	return db, mock, driver
}

func TestNewMySqlDriver(t *testing.T) {
	// Create a mock database connection
	db, mock, driver := setupMockDBMySql(t)
	defer db.Close()

	// Simulate a successful ping to the DB
	mock.ExpectPing().WillReturnError(nil)

	// Test that the driver is initialized correctly
	assert.NotNil(t, driver)
}

func TestCreateMigrationsTableMySqlDriver(t *testing.T) {
	// Create a mock database connection
	db, mock, driver := setupMockDBMySql(t)
	defer db.Close()

	// Simulate a successful table creation
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS migrations").WillReturnResult(sqlmock.NewResult(1, 1))

	// Call CreateMigrationsTable
	err := driver.CreateMigrationsTable(context.Background())
	assert.NoError(t, err)
}

func TestSetMigrationTableNameMySqlDriver(t *testing.T) {
	driver := &MySqlDriver{}

	// Test default migration table name
	driver.SetMigrationTableName("")
	assert.Equal(t, "migrations", driver.migrationTableName)

	// Test custom migration table name
	driver.SetMigrationTableName("custom_migrations")
	assert.Equal(t, "custom_migrations", driver.migrationTableName)
}

func TestGetExecutedMigrations(t *testing.T) {
	// Create a mock database connection
	db, mock, driver := setupMockDBMySql(t)
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

func TestCleanDatabaseMySqlDriver(t *testing.T) {
	db, mock, driver := setupMockDBMySql(t)
	defer db.Close()

	ctx := context.Background()

	// 1. Expect disabling foreign key checks
	mock.ExpectExec(`SET FOREIGN_KEY_CHECKS = 0;`).WillReturnResult(sqlmock.NewResult(0, 0))

	// 2. Expect selecting all table names
	mock.ExpectQuery(`SELECT table_name FROM information_schema\.tables WHERE table_schema = DATABASE\(\);`).
		WillReturnRows(
			sqlmock.NewRows([]string{"table_name"}).
				AddRow("users").
				AddRow("products"),
		)

	// 3. Expect dropping tables
	mock.ExpectExec(`DROP TABLE ` + "`users`, `products`;").WillReturnResult(sqlmock.NewResult(0, 0))

	// 4. Expect re-enabling foreign key checks
	mock.ExpectExec(`SET FOREIGN_KEY_CHECKS = 1;`).WillReturnResult(sqlmock.NewResult(0, 0))

	// Act
	err := driver.CleanDatabase(ctx)

	assert.NoError(t, err)

	// Assert all expectations were met
	err = mock.ExpectationsWereMet()

	assert.NoError(t, err, "there were unfulfilled expectations")
}

func TestApplyMigrationsMySqlDriver(t *testing.T) {
	db, mock, driver := setupMockDBMySql(t)
	defer db.Close()

	mig := &mockMigrationMySqlDriver{
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

func TestUnapplyMigrationsMySqlDriver(t *testing.T) {
	db, mock, driver := setupMockDBMySql(t)
	defer db.Close()

	mig := &mockMigrationMySqlDriver{
		name: "migration1",
		up:   "CREATE TABLE test (id INT);",
		down: "DROP TABLE test;",
	}

	mock.ExpectExec(mig.down).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`DELETE FROM migrations WHERE name = ?`).WithArgs(mig.name).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err := driver.UnapplyMigrations(context.Background(), []Migration{mig}, nil, nil, nil)
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestExecuteMigrationSQLMySqlDriver(t *testing.T) {
	db, mock, driver := setupMockDBMySql(t)
	defer db.Close()

	mock.ExpectExec(`SOME SQL STATEMENT`).WillReturnResult(sqlmock.NewResult(0, 0))

	err := driver.executeMigrationSQL(context.Background(), "SOME SQL STATEMENT")
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestInsertExecutedMigrationMySqlDriver(t *testing.T) {
	db, mock, driver := setupMockDBMySql(t)
	defer db.Close()

	mock.ExpectExec(`INSERT INTO migrations`).WithArgs("migration_name", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err := driver.insertExecutedMigration(context.Background(), "migration_name", time.Now())
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRemoveExecutedMigrationMySqlDriver(t *testing.T) {
	db, mock, driver := setupMockDBMySql(t)
	defer db.Close()

	mock.ExpectExec(`DELETE FROM migrations WHERE name = ?`).WithArgs("migration_name").
		WillReturnResult(sqlmock.NewResult(1, 1))

	err := driver.removeExecutedMigration(context.Background(), "migration_name")
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- Supporting mock types ---

type mockMigrationMySqlDriver struct {
	name string
	up   string
	down string
}

func (m *mockMigrationMySqlDriver) Name() string       { return m.name }
func (m *mockMigrationMySqlDriver) UpScript() string   { return m.up }
func (m *mockMigrationMySqlDriver) DownScript() string { return m.down }

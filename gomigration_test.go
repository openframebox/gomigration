package gomigration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type mockDriver struct {
	mock.Mock
	Driver
}

func (m *mockDriver) SetMigrationTableName(name string) {
	m.Called(name)
}

func (m *mockDriver) CreateMigrationsTable(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *mockDriver) GetExecutedMigrations(ctx context.Context, includeRollbacked bool) ([]ExecutedMigration, error) {
	args := m.Called(ctx, includeRollbacked)
	return args.Get(0).([]ExecutedMigration), args.Error(1)
}

func (m *mockDriver) ApplyMigrations(ctx context.Context, migrations []Migration, before, after func(*Migration), onError func(*Migration, error)) error {
	args := m.Called(ctx, migrations)
	return args.Error(0)
}

func (m *mockDriver) UnapplyMigrations(ctx context.Context, migrations []Migration, before, after func(*Migration), onError func(*Migration, error)) error {
	args := m.Called(ctx, migrations)
	return args.Error(0)
}

func (m *mockDriver) CleanDatabase(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func TestGoMigration_New_ErrorNilConfig(t *testing.T) {
	q, err := New(nil)
	assert.Nil(t, q)
	assert.Equal(t, ErrConfigNotProvided, err)
}

func TestGoMigration_New_ErrorNilDriver(t *testing.T) {
	cfg := &Config{}
	q, err := New(cfg)
	assert.Nil(t, q)
	assert.Equal(t, ErrDriverNotProvided, err)
}

func TestGoMigration_Register_Duplicate(t *testing.T) {
	q := &GoMigration{migrations: make(map[string]Migration)}

	migration := dummyMigration{name: "001_create_users"}

	err := q.Register(migration)
	assert.NoError(t, err)

	err = q.Register(migration)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "registered more than once")
}

func TestGoMigration_Migrate_NoMigrations(t *testing.T) {
	ctx := context.TODO()
	driver := new(mockDriver)
	driver.On("CreateMigrationsTable", ctx).Return(nil)
	driver.On("GetExecutedMigrations", ctx, false).Return([]ExecutedMigration{}, nil)

	q := &GoMigration{
		driver:     driver,
		migrations: map[string]Migration{},
	}

	err := q.Migrate(ctx)
	assert.NoError(t, err)
	driver.AssertExpectations(t)
}

func TestGoMigration_Fresh_Success(t *testing.T) {
	ctx := context.TODO()
	driver := new(mockDriver)
	driver.On("CleanDatabase", ctx).Return(nil)
	driver.On("CreateMigrationsTable", ctx).Return(nil)
	driver.On("GetExecutedMigrations", ctx, false).Return([]ExecutedMigration{}, nil)

	q := &GoMigration{
		driver:     driver,
		migrations: map[string]Migration{},
	}

	err := q.Fresh(ctx)
	assert.NoError(t, err)
	driver.AssertExpectations(t)
}

func TestGoMigration_Reset_NoExecuted(t *testing.T) {
	ctx := context.TODO()
	driver := new(mockDriver)
	driver.On("GetExecutedMigrations", ctx, true).Return([]ExecutedMigration{}, nil)

	q := &GoMigration{
		driver: driver,
	}

	err := q.Reset(ctx)
	assert.NoError(t, err)
	driver.AssertExpectations(t)
}

func TestGoMigration_Clean_Error(t *testing.T) {
	ctx := context.TODO()
	driver := new(mockDriver)
	driver.On("CleanDatabase", ctx).Return(errors.New("clean error"))

	q := &GoMigration{
		driver: driver,
	}

	err := q.Clean(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to clean database")
	driver.AssertExpectations(t)
}

func TestGoMigration_List(t *testing.T) {
	ctx := context.TODO()
	driver := new(mockDriver)
	driver.On("CreateMigrationsTable", ctx).Return(nil)
	driver.On("GetExecutedMigrations", ctx, false).Return([]ExecutedMigration{{Name: "001_create_users", ExecutedAt: time.Now()}}, nil)

	migration := dummyMigration{name: "001_create_users"}
	q := &GoMigration{
		driver:     driver,
		migrations: map[string]Migration{"001_create_users": migration},
	}

	list, err := q.List(ctx)
	assert.NoError(t, err)
	assert.Len(t, list, 1)
	assert.True(t, list[0].IsExecuted)
	driver.AssertExpectations(t)
}

func TestSetMigrationFilesDir(t *testing.T) {
	q := &GoMigration{}
	q.SetMigrationFilesDir("migrations")
	assert.Equal(t, "migrations", q.migrationFilesDir)
}

// dummyMigration is a simple implementation of the Migration interface for testing.
type dummyMigration struct {
	name string
}

func (d dummyMigration) Name() string {
	return d.name
}

func (d dummyMigration) UpScript() string {
	return "CREATE TABLE dummy (id INT);"
}

func (d dummyMigration) DownScript() string {
	return "DROP TABLE dummy;"
}

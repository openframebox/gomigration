package gomigration

import "errors"

var (
	ErrConfigNotProvided          = errors.New("config not provided")
	ErrDriverNotProvided          = errors.New("driver not provided")
	ErrMigrationDirNotProvided    = errors.New("migration directory not provided")
	ErrMigrationDirNotExists      = errors.New("migration directory does not exist")
	ErrMigrationNameNotProvided   = errors.New("migration name not provided")
	ErrMigrationFileAlreadyExists = errors.New("migration file already exists")
	ErrMigrationFileNotFound      = errors.New("migration file not found")
	ErrInvalidRollbackStep        = errors.New("invalid rollback step")
	ErrEmbeddedFSNotProvided      = errors.New("embedded fs not provided")
	ErrGoMigrationNotProvided     = errors.New("gomigration instance not provided")
)

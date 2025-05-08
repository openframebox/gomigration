package gomigration

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSanitizeMigrationName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
		wantErr  bool
	}{
		{"My Migration-Name", "my_migration_name", false},
		{"invalid name!!", "", true},
		{"   spaced name   ", "spaced_name", false},
		{"Name-With-Dashes", "name_with_dashes", false},
		{"", "", true},
	}

	for _, tt := range tests {
		result, err := sanitizeMigrationName(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("sanitizeMigrationName(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if result != tt.expected {
			t.Errorf("sanitizeMigrationName(%q) = %v, want %v", tt.input, result, tt.expected)
		}
	}
}

func TestSanitizeTableName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
		wantErr  bool
	}{
		{"valid_table_name", "valid_table_name", false},
		{"invalid table!", "", true},
		{"AnotherOne123", "AnotherOne123", false},
	}

	for _, tt := range tests {
		result, err := sanitizeTableName(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("sanitizeTableName(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if result != tt.expected {
			t.Errorf("sanitizeTableName(%q) = %v, want %v", tt.input, result, tt.expected)
		}
	}
}

func TestMigrationNameToStructName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
		wantErr  bool
	}{
		{"20240426123456_create_users_table", "M20240426123456CreateUsersTable", false},
		{"invalid_name_without_timestamp", "", true},
	}

	for _, tt := range tests {
		result, err := migrationNameToStructName(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("migrationNameToStructName(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if result != tt.expected {
			t.Errorf("migrationNameToStructName(%q) = %v, want %v", tt.input, result, tt.expected)
		}
	}
}

func TestGetPackageNameFromMigrationDir(t *testing.T) {
	result1 := getPackageNameFromMigrationDir("migrations")
	result2 := getPackageNameFromMigrationDir("src/custompkg")
	result3 := getPackageNameFromMigrationDir("db/migrations")

	assert.Equal(t, "migrations", result1)
	assert.Equal(t, "custompkg", result2)
	assert.Equal(t, "migrations", result3)
}

func TestMigrationFileTemplate(t *testing.T) {
	code, err := migrationFileTemplate("migrations", "20240426123456_create_users_table")

	assert.NoError(t, err)
	assert.Contains(t, code, "package migrations")
	assert.Contains(t, code, "type M20240426123456CreateUsersTable struct")
	assert.Contains(t, code, "func (m *M20240426123456CreateUsersTable) Name() string")
	assert.Contains(t, code, "return \"20240426123456_create_users_table\"")
}

func TestGetSortedMigrationName(t *testing.T) {
	migrations := map[string]Migration{
		"b_migration": nil,
		"a_migration": nil,
		"c_migration": nil,
	}

	sorted := getSortedMigrationName(migrations)

	assert := assert.New(t)
	assert.Equal([]string{"a_migration", "b_migration", "c_migration"}, sorted)
}

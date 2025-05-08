package gomigration

import (
	"fmt"
	"time"
)

type ExecutedMigration struct {
	Name       string    `json:"name"`
	ExecutedAt time.Time `json:"executed_at"`
}

type Config struct {
	Driver             Driver
	MigrationFilesDir  string
	MigrationTableName string
	DebugSql           bool
}

type Migration interface {
	Name() string
	UpScript() string
	DownScript() string
}

type RegisteredMigration struct {
	Name       string
	UpScript   string
	DownScript string
	IsExecuted bool
	ExecutedAt *time.Time
}

type RegisteredMigrationList []RegisteredMigration

func (m RegisteredMigrationList) Print() {
	var tableData [][]string
	tableData = append(tableData, []string{"Migration Name", "Is Executed", "Executed At"})

	for _, migration := range m {
		executedAt := "N/A"
		if migration.ExecutedAt != nil {
			executedAt = migration.ExecutedAt.Format(time.RFC3339)
		}
		row := []string{
			migration.Name,
			fmt.Sprintf("%t", migration.IsExecuted),
			executedAt,
		}
		tableData = append(tableData, row)
	}

	printTable(tableData)
}

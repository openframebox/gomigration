package gomigration

import (
	"fmt"
	"go/format"
	"os"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// fileExists checks if a file with the given name exists.
func fileExists(fileName string) bool {
	_, err := os.Stat(fileName)
	return !os.IsNotExist(err)
}

// migrationDirExists checks if a directory for migration files exists.
func migrationDirExists(migrationFilesDir string) bool {
	_, err := os.Stat(migrationFilesDir)
	return !os.IsNotExist(err)
}

// printTable prints a 2D slice of strings as a formatted table.
func printTable(data [][]string) {
	if len(data) == 0 {
		fmt.Println("No data to display.")
		return
	}

	colWidths := make([]int, len(data[0]))
	for _, row := range data {
		for colIdx, col := range row {
			if len(col) > colWidths[colIdx] {
				colWidths[colIdx] = len(col)
			}
		}
	}

	printRow := func(row []string) {
		fmt.Print("|")
		for i, col := range row {
			format := fmt.Sprintf(" %%-%ds |", colWidths[i])
			fmt.Printf(format, col)
		}
		fmt.Println()
	}

	printSeparator := func() {
		fmt.Print("+")
		for _, width := range colWidths {
			fmt.Print(strings.Repeat("-", width+2) + "+")
		}
		fmt.Println()
	}

	printSeparator()
	printRow(data[0])
	printSeparator()

	for _, row := range data[1:] {
		printRow(row)
	}
	printSeparator()
}

// sanitizeMigrationName transforms a migration name into a standardized format
// and validates it. Returns an error if the name contains invalid characters.
func sanitizeMigrationName(name string) (string, error) {
	name = strings.ReplaceAll(name, "-", "_")
	name = strings.ReplaceAll(name, " ", "_")
	name = strings.ToLower(name)
	name = strings.TrimSpace(name)
	name = strings.Trim(name, "_")
	if len(name) > 200 {
		name = name[:200]
	}

	valid := regexp.MustCompile(`^[a-zA-Z0-9_]+$`)
	if !valid.MatchString(name) {
		return "", fmt.Errorf("invalid migration name: %s", name)
	}

	return name, nil
}

// sanitizeTableName validates the table name. Returns an error if it contains
// invalid characters.
func sanitizeTableName(name string) (string, error) {
	valid := regexp.MustCompile(`^[a-zA-Z0-9_]+$`)
	if !valid.MatchString(name) {
		return "", fmt.Errorf("invalid table name: %s", name)
	}

	return name, nil
}

// migrationNameToStructName converts a migration file name (with timestamp prefix)
// to a Go struct name used in the migration template.
func migrationNameToStructName(migrationName string) (string, error) {
	re := regexp.MustCompile(`^\d{14}_`)
	matches := re.FindStringSubmatch(migrationName)
	if len(matches) == 0 {
		return "", fmt.Errorf("invalid migration name: %s", migrationName)
	}
	timestamp := strings.TrimRight(matches[0], "_")
	nameWithoutTimestamp := strings.TrimPrefix(migrationName, timestamp)

	parts := strings.Split(nameWithoutTimestamp, "_")
	for i, part := range parts {
		parts[i] = cases.Title(language.English).String(part)
	}

	structName := fmt.Sprintf("M%s%s", timestamp, strings.Join(parts, ""))
	return structName, nil
}

// getPackageNameFromMigrationDir returns the last segment of the migrationFilesDir,
// which is used as the package name.
func getPackageNameFromMigrationDir(migrationFilesDir string) string {
	parts := strings.Split(migrationFilesDir, "/")
	if len(parts) == 0 {
		return "migrations"
	}
	return parts[len(parts)-1]
}

// migrationFileTemplate generates a Go file template for a new migration
// using the specified package and migration name. It returns formatted Go source code.
func migrationFileTemplate(packageName string, migrationName string) (string, error) {
	structName, err := migrationNameToStructName(migrationName)
	if err != nil {
		return "", err
	}

	migrationTemplate := fmt.Sprintf(`
		package %s

		type %s struct {}

		func (m *%s) Name() string {
		    // Don't change this name
			return "%s"
		}

		func (m *%s) UpScript() string {
		    // Write your migration SQL here
			return ""
		}

		func (m *%s) DownScript() string {
			// Write your rollback SQL here
			return ""
		}
	`,
		packageName,
		structName,
		structName,
		migrationName,
		structName,
		structName,
	)

	formatted, err := format.Source([]byte(migrationTemplate))
	if err != nil {
		return "", err
	}

	return string(formatted), nil
}

// getSortedMigrationName returns a sorted list of migration names
// from a map of migration structs.
func getSortedMigrationName(migrations map[string]Migration) []string {
	keys := []string{}
	for k := range migrations {
		keys = append(keys, k)
	}

	sort.Strings(keys)
	return keys
}

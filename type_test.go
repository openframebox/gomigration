package gomigration

import (
	"bytes"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// helper to capture output
func captureOutput(f func()) string {
	old := os.Stdout // keep backup
	r, w, _ := os.Pipe()
	os.Stdout = w

	f() // call the function

	_ = w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	return buf.String()
}

func TestRegisteredMigrationList_Print(t *testing.T) {
	// Setup time for testing
	now := time.Now()

	// Create sample data
	migrations := RegisteredMigrationList{
		{Name: "create_orders", IsExecuted: true, ExecutedAt: &now},
		{Name: "add_customer_id", IsExecuted: false, ExecutedAt: nil},
	}

	// Capture output of the Print() method
	output := captureOutput(func() {
		migrations.Print()
	})

	// Assertions
	assert.Contains(t, output, "Migration Name")
	assert.Contains(t, output, "Is Executed")
	assert.Contains(t, output, "Executed At")
	assert.Contains(t, output, "create_orders")
	assert.Contains(t, output, now.Format(time.RFC3339)) // Check formatted time
	assert.Contains(t, output, "add_customer_id")
	assert.Contains(t, output, "N/A") // Check for non-executed migration's "Executed At" field
}

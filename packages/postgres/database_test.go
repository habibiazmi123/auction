package postgres

import "testing"

func TestNumericMigrationNames(t *testing.T) {
	for _, name := range []string{"001.sql", "001_init.sql", "12-add-index.sql"} {
		if !isNumericMigration(name) {
			t.Errorf("expected %q to be a migration", name)
		}
	}
	for _, name := range []string{"README.sql", "init.sql", ".sql"} {
		if isNumericMigration(name) {
			t.Errorf("expected %q to be ignored", name)
		}
	}
}

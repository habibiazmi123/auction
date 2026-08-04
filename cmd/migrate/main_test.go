package main

import "testing"

func TestMigrationDir(t *testing.T) {
	tests := map[string]string{
		"user":         "migrations/user",
		"product":      "migrations/product",
		"auction":      "migrations/auction",
		"transaction":  "migrations/transaction",
		"notification": "migrations/notification",
	}
	for service, want := range tests {
		if got := migrationDir(service); got != want {
			t.Errorf("migrationDir(%q) = %q, want %q", service, got, want)
		}
	}
}

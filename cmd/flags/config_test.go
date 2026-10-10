package flags

import "testing"

func TestNormalizeDatabaseType(t *testing.T) {
	tests := map[string]string{
		"":           DatabaseTypeSQLite,
		"sqlite":     DatabaseTypeSQLite,
		" SQLite ":   DatabaseTypeSQLite,
		"SQLITE":     DatabaseTypeSQLite,
		"postgres":   DatabaseTypePostgres,
		" postgres":  DatabaseTypePostgres,
		"mariadb":    DatabaseTypeMySQL,
		"postgresql": DatabaseTypePostgres,
		"pg":         DatabaseTypePostgres,
		"pgx":        DatabaseTypePostgres,
	}

	for input, want := range tests {
		if got := NormalizeDatabaseType(input); got != want {
			t.Fatalf("NormalizeDatabaseType(%q) = %q, want %q", input, got, want)
		}
	}
}

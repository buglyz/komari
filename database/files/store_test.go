package files

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/komari-monitor/komari/cmd/flags"
	"github.com/komari-monitor/komari/database/dbcore"
)

func TestReplaceReadAndSyncDirectory(t *testing.T) {
	flags.DatabaseType = flags.DatabaseTypeSQLite
	flags.DatabaseFile = "file:stored_files_test?mode=memory&cache=shared"
	db := dbcore.GetDBInstance()
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}

	namespace := "stored-files-test"
	owner := "demo"
	entries := map[string][]byte{
		"komari-plugin.json": []byte(`{"name":"Demo","short":"demo"}`),
		"dist/index.html":    []byte("<html></html>"),
	}
	if err := ReplaceDirectory(namespace, ScopePlugin, owner, entries); err != nil {
		t.Fatal(err)
	}
	data, err := ReadFile(namespace, ScopePlugin, owner, "dist/index.html")
	if err != nil || string(data) != "<html></html>" {
		t.Fatalf("ReadFile() = %q, %v", data, err)
	}

	root := t.TempDir()
	if err := MaterializeDirectory(namespace, ScopePlugin, owner, root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "komari-plugin.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "dist", "index.html"), []byte("updated"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SyncDirectory(namespace, ScopePlugin, owner, root); err != nil {
		t.Fatal(err)
	}
	data, err = ReadFile(namespace, ScopePlugin, owner, "dist/index.html")
	if err != nil || string(data) != "updated" {
		t.Fatalf("synced ReadFile() = %q, %v", data, err)
	}
}

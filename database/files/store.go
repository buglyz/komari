// Package files persists themes, plugins and plugin data in the primary DB.
package files

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/komari-monitor/komari/database/dbcore"
	"github.com/komari-monitor/komari/database/models"
	"gorm.io/gorm"
)

const (
	ScopeTheme      = "theme"
	ScopePlugin     = "plugin"
	ScopePluginData = "plugin_data"
	ScopeSystem     = "system"
)

// NamespaceForDir returns a stable namespace for a configured logical root.
// Tests use temporary roots, preventing their SQL rows from colliding.
func NamespaceForDir(root string) string {
	absolute, err := filepath.Abs(root)
	if err == nil {
		return filepath.Clean(absolute)
	}
	return filepath.Clean(root)
}

func objectID(namespace, scope, owner, name string) string {
	sum := sha256.Sum256([]byte(namespace + "\x00" + scope + "\x00" + owner + "\x00" + name))
	return hex.EncodeToString(sum[:])
}

func normalizePath(name string) (string, error) {
	name = filepath.ToSlash(strings.TrimSpace(name))
	name = strings.TrimPrefix(name, "./")
	if name == "" || name == "." || strings.HasPrefix(name, "/") || strings.Contains(name, "\\") {
		return "", fmt.Errorf("invalid stored file path %q", name)
	}
	clean := filepath.ToSlash(filepath.Clean(name))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || len(clean) > 512 {
		return "", fmt.Errorf("invalid stored file path %q", name)
	}
	return clean, nil
}

// ReplaceDirectory atomically replaces one logical directory with files.
func ReplaceDirectory(namespace, scope, owner string, entries map[string][]byte) error {
	if namespace == "" || scope == "" || owner == "" {
		return errors.New("file directory identity is required")
	}
	db := dbcore.GetDBInstance()
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where(map[string]any{"namespace": namespace, "scope": scope, "owner": owner}).Delete(&models.StoredFile{}).Error; err != nil {
			return fmt.Errorf("clear stored directory: %w", err)
		}
		for name, data := range entries {
			clean, err := normalizePath(name)
			if err != nil {
				return err
			}
			row := models.StoredFile{
				ID: objectID(namespace, scope, owner, clean), Namespace: namespace,
				Scope: scope, Owner: owner, Path: clean, Mode: 0o644,
				Data: append([]byte(nil), data...),
			}
			if err := tx.Create(&row).Error; err != nil {
				return fmt.Errorf("save stored file %q: %w", clean, err)
			}
		}
		return nil
	})
}

// PutFile upserts one SQL-backed file without replacing its sibling files.
func PutFile(namespace, scope, owner, name string, data []byte, mode uint32) error {
	if namespace == "" || scope == "" || owner == "" {
		return errors.New("file identity is required")
	}
	clean, err := normalizePath(name)
	if err != nil {
		return err
	}
	if mode == 0 {
		mode = 0o644
	}
	row := models.StoredFile{
		ID: objectID(namespace, scope, owner, clean), Namespace: namespace,
		Scope: scope, Owner: owner, Path: clean, Mode: mode,
		Data: append([]byte(nil), data...),
	}
	return dbcore.GetDBInstance().Save(&row).Error
}

// DeleteFile removes one SQL-backed file.
func DeleteFile(namespace, scope, owner, name string) error {
	clean, err := normalizePath(name)
	if err != nil {
		return err
	}
	result := dbcore.GetDBInstance().Where(map[string]any{
		"namespace": namespace, "scope": scope, "owner": owner, "path": clean,
	}).Delete(&models.StoredFile{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// ReadFile reads one SQL-backed file.
func ReadFile(namespace, scope, owner, name string) ([]byte, error) {
	clean, err := normalizePath(name)
	if err != nil {
		return nil, err
	}
	var row models.StoredFile
	err = dbcore.GetDBInstance().Where(map[string]any{
		"namespace": namespace, "scope": scope, "owner": owner, "path": clean,
	}).First(&row).Error
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), row.Data...), nil
}

// ListFiles returns all files in one SQL-backed directory.
func ListFiles(namespace, scope, owner string) ([]models.StoredFile, error) {
	var rows []models.StoredFile
	err := dbcore.GetDBInstance().Where(map[string]any{
		"namespace": namespace, "scope": scope, "owner": owner,
	}).Order("path").Find(&rows).Error
	return rows, err
}

// ListOwners returns logical directories owned by a scope.
func ListOwners(namespace, scope string) ([]string, error) {
	var owners []string
	err := dbcore.GetDBInstance().Model(&models.StoredFile{}).
		Where(map[string]any{"namespace": namespace, "scope": scope}).
		Distinct("owner").Pluck("owner", &owners).Error
	sort.Strings(owners)
	return owners, err
}

// DeleteDirectory removes one SQL-backed directory.
func DeleteDirectory(namespace, scope, owner string) error {
	return dbcore.GetDBInstance().Where(map[string]any{
		"namespace": namespace, "scope": scope, "owner": owner,
	}).Delete(&models.StoredFile{}).Error
}

// MaterializeDirectory restores SQL-backed files into a runtime cache.
func MaterializeDirectory(namespace, scope, owner, root string) error {
	if err := os.RemoveAll(root); err != nil {
		return fmt.Errorf("clear runtime file cache: %w", err)
	}
	rows, err := ListFiles(namespace, scope, owner)
	if err != nil {
		return fmt.Errorf("list stored files: %w", err)
	}
	for _, row := range rows {
		path := filepath.Join(root, filepath.FromSlash(row.Path))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("create runtime file directory: %w", err)
		}
		mode := os.FileMode(row.Mode & 0o777)
		if mode == 0 {
			mode = 0o644
		}
		if err := os.WriteFile(path, row.Data, mode); err != nil {
			return fmt.Errorf("write runtime file %q: %w", row.Path, err)
		}
	}
	return nil
}

// RestoreOrImport restores a SQL directory into its runtime cache. If the
// database has no copy yet, an existing legacy directory is imported once.
func RestoreOrImport(namespace, scope, owner, root string) (bool, error) {
	rows, err := ListFiles(namespace, scope, owner)
	if err != nil {
		return false, fmt.Errorf("list stored files: %w", err)
	}
	if len(rows) > 0 {
		return true, MaterializeDirectory(namespace, scope, owner, root)
	}
	if _, err := os.Stat(root); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if err := SyncDirectory(namespace, scope, owner, root); err != nil {
		return false, fmt.Errorf("import legacy directory: %w", err)
	}
	return true, nil
}

// EnsureDirectory reports whether a SQL-backed directory exists and imports a
// legacy runtime directory when no SQL copy exists. Unlike RestoreOrImport it
// does not rewrite an already materialized cache on every request.
func EnsureDirectory(namespace, scope, owner, root string) (bool, error) {
	rows, err := ListFiles(namespace, scope, owner)
	if err != nil {
		return false, fmt.Errorf("list stored files: %w", err)
	}
	if len(rows) > 0 {
		return true, nil
	}
	return RestoreOrImport(namespace, scope, owner, root)
}

// SyncDirectory snapshots a runtime cache back into SQL.
func SyncDirectory(namespace, scope, owner, root string) error {
	entries := make(map[string][]byte)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		name, err := normalizePath(relative)
		if err != nil {
			return err
		}
		entries[name] = data
		return nil
	})
	if err != nil {
		return fmt.Errorf("read runtime files: %w", err)
	}
	return ReplaceDirectory(namespace, scope, owner, entries)
}

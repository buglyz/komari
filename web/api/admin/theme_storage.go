package admin

import (
	"errors"
	"os"
	"path/filepath"
	"sort"

	"github.com/komari-monitor/komari/database/files"
)

func themeNamespace() string {
	return files.NamespaceForDir(filepath.Join(".", "data", "theme"))
}

func themeDir(short string) string {
	return filepath.Join(".", "data", "theme", short)
}

func restoreTheme(short string) (bool, error) {
	return files.RestoreOrImport(themeNamespace(), files.ScopeTheme, short, themeDir(short))
}

func themeExists(short string) (bool, error) {
	found, err := restoreTheme(short)
	if err != nil {
		return false, err
	}
	return found, nil
}

func installedThemeShorts() ([]string, error) {
	owners, err := files.ListOwners(themeNamespace(), files.ScopeTheme)
	if err != nil {
		return nil, err
	}
	if len(owners) == 0 {
		entries, readErr := os.ReadDir(filepath.Dir(themeDir("placeholder")))
		if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
			return nil, readErr
		}
		for _, entry := range entries {
			if !entry.IsDir() || !isValidMarketShort(entry.Name()) {
				continue
			}
			if _, err := restoreTheme(entry.Name()); err != nil {
				return nil, err
			}
			owners = append(owners, entry.Name())
		}
	}
	sort.Strings(owners)
	return owners, nil
}

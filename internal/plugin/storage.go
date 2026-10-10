package plugin

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/komari-monitor/komari/database/files"
	"github.com/komari-monitor/komari/database/models"
	logger "github.com/komari-monitor/komari/utils/log"
)

func pluginNamespace() string {
	return files.NamespaceForDir(DataDir)
}

func pluginDir(short string) string {
	return filepath.Join(DataDir, short)
}

func pluginStorageDir(short string) string {
	return filepath.Join(StorageDir, short)
}

func restorePlugin(short string) (bool, error) {
	namespace := pluginNamespace()
	rows, err := files.ListFiles(namespace, files.ScopePlugin, short)
	if err != nil {
		return false, err
	}
	if len(rows) > 0 {
		if _, err := os.Stat(filepath.Join(pluginDir(short), manifestFile)); err == nil {
			return true, nil
		}
		return true, files.MaterializeDirectory(namespace, files.ScopePlugin, short, pluginDir(short))
	}
	return files.RestoreOrImport(namespace, files.ScopePlugin, short, pluginDir(short))
}

func restorePluginStorage(short string) (bool, error) {
	return files.RestoreOrImport(pluginNamespace(), files.ScopePluginData, short, pluginStorageDir(short))
}

func ensurePlugin(short string) error {
	found, err := restorePlugin(short)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("plugin %q is not installed: %w", short, os.ErrNotExist)
	}
	return nil
}

func installedPluginShorts() ([]string, error) {
	namespace := pluginNamespace()
	owners, err := files.ListOwners(namespace, files.ScopePlugin)
	if err != nil {
		return nil, err
	}
	if len(owners) == 0 {
		entries, readErr := os.ReadDir(DataDir)
		if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
			return nil, readErr
		}
		for _, entry := range entries {
			if !entry.IsDir() || !validShort(entry.Name()) {
				continue
			}
			if _, err := restorePlugin(entry.Name()); err != nil {
				return nil, err
			}
			owners = append(owners, entry.Name())
		}
	}
	sort.Strings(owners)
	return owners, nil
}

func readInstalledManifest(short string) (info models.Plugin, err error) {
	if err := ensurePlugin(short); err != nil {
		return info, err
	}
	return readManifest(pluginDir(short))
}

func syncPluginStorage(short string) error {
	if err := files.SyncDirectory(pluginNamespace(), files.ScopePlugin, short, pluginDir(short)); err != nil {
		return fmt.Errorf("persist plugin files: %w", err)
	}
	if _, err := os.Stat(pluginStorageDir(short)); err == nil {
		if err := files.SyncDirectory(pluginNamespace(), files.ScopePluginData, short, pluginStorageDir(short)); err != nil {
			return fmt.Errorf("persist plugin data: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (m *Manager) persistAfterJob(short string) {
	if err := syncPluginStorage(short); err != nil {
		logger.Errorf("plugin", "failed to persist plugin %q after execution: %v", short, err)
	}
}

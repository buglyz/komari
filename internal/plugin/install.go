package plugin

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/komari-monitor/komari/database/files"
	"github.com/komari-monitor/komari/database/models"
)

// InstallZip validates a plugin ZIP and stores it in SQL. DataDir/<short> is
// only a runtime cache used by the JavaScript engine and may be regenerated.
// The archive must contain komari-plugin.json at its root. Archive limits
// mirror the theme package format; path-traversal entries reject the whole
// package instead of being skipped. Reinstalling over a running plugin
// unloads it first and restores it to its persisted enabled state when the
// extraction succeeds.
func InstallZip(zipPath string) (models.Plugin, error) {
	var info models.Plugin
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return info, fmt.Errorf("failed to open ZIP file: %v", err)
	}
	defer r.Close()

	if err := validatePluginArchive(r.File); err != nil {
		return info, err
	}

	var manifest *zip.File
	for _, f := range r.File {
		if f.Name == manifestFile {
			manifest = f
			break
		}
	}
	if manifest == nil {
		return info, fmt.Errorf("plugin manifest %s not found, not a valid plugin package", manifestFile)
	}

	rc, err := manifest.Open()
	if err != nil {
		return info, fmt.Errorf("failed to read plugin manifest: %v", err)
	}
	configData, readErr := io.ReadAll(io.LimitReader(rc, maxPluginManifestSize+1))
	_ = rc.Close()
	if readErr != nil {
		return info, fmt.Errorf("failed to read plugin manifest: %v", readErr)
	}
	if len(configData) > maxPluginManifestSize {
		return info, fmt.Errorf("plugin manifest exceeds the %d byte limit", maxPluginManifestSize)
	}
	if err := json.Unmarshal(configData, &info); err != nil {
		return info, fmt.Errorf("invalid plugin manifest: %v", err)
	}
	if err := validateManifest(&info); err != nil {
		return info, err
	}
	if err := CheckKomariVersion(info.Komari); err != nil {
		return info, err
	}

	entries := make(map[string][]byte)
	for _, file := range r.File {
		if file.FileInfo().IsDir() {
			continue
		}
		name, err := storedArchivePath(file.Name)
		if err != nil {
			return info, err
		}
		content, err := readArchiveFile(file, maxPluginFileSize)
		if err != nil {
			return info, err
		}
		entries[name] = content
	}
	if _, ok := entries[info.Entry]; !ok {
		return info, fmt.Errorf("plugin entry %s does not exist", info.Entry)
	}
	for _, page := range info.Pages {
		if page.Type == models.PageTypeIframe {
			if _, ok := entries[page.File]; !ok {
				return info, fmt.Errorf("plugin page %s does not exist", page.File)
			}
		}
	}

	if err := global.unload(info.Short); err != nil && !errors.Is(err, errNotLoaded) {
		return info, fmt.Errorf("failed to unload running plugin %q before reinstall: %w", info.Short, err)
	}

	dir := filepath.Join(DataDir, info.Short)
	namespace := files.NamespaceForDir(DataDir)
	if err := files.ReplaceDirectory(namespace, files.ScopePlugin, info.Short, entries); err != nil {
		return info, err
	}
	if err := files.MaterializeDirectory(namespace, files.ScopePlugin, info.Short, dir); err != nil {
		return info, err
	}
	if global.stateStore().get(info.Short).Enabled {
		if err := global.restartPlugin(info.Short); err != nil {
			return info, fmt.Errorf("plugin reinstalled but reload failed: %w", err)
		}
	}
	return info, nil
}

func validatePluginArchive(files []*zip.File) error {
	if len(files) > maxPluginArchiveFiles {
		return fmt.Errorf("plugin archive has more than %d files", maxPluginArchiveFiles)
	}
	var total uint64
	for _, file := range files {
		if _, err := storedArchivePath(file.Name); err != nil {
			return err
		}
		if file.FileInfo().IsDir() {
			continue
		}
		if file.UncompressedSize64 > maxPluginFileSize {
			return fmt.Errorf("plugin file %s exceeds the %d byte limit", file.Name, maxPluginFileSize)
		}
		total += file.UncompressedSize64
		if total > maxPluginExtractedSize {
			return fmt.Errorf("plugin archive exceeds the %d byte extraction limit", maxPluginExtractedSize)
		}
	}
	return nil
}

func storedArchivePath(name string) (string, error) {
	if strings.Contains(name, "\\") || strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("plugin archive contains an invalid path %q", name)
	}
	clean := path.Clean(name)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("plugin archive contains an invalid path %q", name)
	}
	return clean, nil
}

func readArchiveFile(file *zip.File, limit uint64) ([]byte, error) {
	rc, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("open plugin file %s: %w", file.Name, err)
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, int64(limit)+1))
	if err != nil {
		return nil, fmt.Errorf("read plugin file %s: %w", file.Name, err)
	}
	if uint64(len(data)) > limit {
		return nil, fmt.Errorf("plugin file %s exceeds the %d byte limit", file.Name, limit)
	}
	return data, nil
}

func extractPluginArchive(files []*zip.File, dir string) error {
	for _, f := range files {
		path := filepath.Join(dir, f.Name)
		if !withinDir(path, dir) {
			return fmt.Errorf("plugin archive contains an invalid path %q", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(path, 0755); err != nil {
				return fmt.Errorf("failed to create directory: %v", err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return fmt.Errorf("failed to create directory: %v", err)
		}
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("failed to open archive file: %v", err)
		}
		outFile, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
		if err != nil {
			_ = rc.Close()
			return fmt.Errorf("failed to create file: %v", err)
		}
		_, copyErr := io.Copy(outFile, rc)
		_ = outFile.Close()
		_ = rc.Close()
		if copyErr != nil {
			return fmt.Errorf("failed to extract file: %v", copyErr)
		}
	}
	return nil
}

// withinDir reports whether path stays inside dir after cleaning.
func withinDir(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
}

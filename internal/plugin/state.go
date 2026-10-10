package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/komari-monitor/komari/database/dbcore"
	"github.com/komari-monitor/komari/database/files"
	"github.com/komari-monitor/komari/database/models"
	logger "github.com/komari-monitor/komari/utils/log"
)

// State persists plugin lifecycle state in the primary database. The path is
// retained only as a namespace source for compatibility with existing callers.
type State struct {
	mu        sync.Mutex
	path      string
	namespace string
	current   map[string]PluginState
}

// PluginState is the persisted state of one plugin.
type PluginState struct {
	Enabled                 bool      `json:"enabled"`
	ApprovedPermissionsHash string    `json:"approved_permissions_hash"`
	LastError               string    `json:"last_error"`
	UpdatedAt               time.Time `json:"updated_at"`
}

func openState(path string) *State {
	return &State{path: path, namespace: files.NamespaceForDir(filepath.Dir(path))}
}

func (s *State) get(short string) PluginState {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensure()
	return s.current[short]
}

func (s *State) set(short string, st PluginState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensure()
	st.UpdatedAt = time.Now().UTC()
	s.current[short] = st
	s.saveLocked()
}

func (s *State) delete(short string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensure()
	if _, ok := s.current[short]; ok {
		delete(s.current, short)
		if err := dbcore.GetDBInstance().Where(map[string]any{
			"namespace": s.namespace, "short": short,
		}).Delete(&models.PluginState{}).Error; err != nil {
			logger.Errorf("plugin", "failed to delete plugin state %q: %v", short, err)
		}
	}
}

func (s *State) ensure() {
	if s.current != nil {
		return
	}
	s.current = map[string]PluginState{}
	var rows []models.PluginState
	if err := dbcore.GetDBInstance().Where(map[string]any{"namespace": s.namespace}).Find(&rows).Error; err != nil {
		logger.Errorf("plugin", "failed to load plugin state from database: %v", err)
		return
	}
	for _, row := range rows {
		s.current[row.Short] = PluginState{
			Enabled:                 row.Enabled,
			ApprovedPermissionsHash: row.ApprovedPermissionsHash,
			LastError:               row.LastError,
			UpdatedAt:               time.Unix(0, row.UpdatedAt).UTC(),
		}
	}
	if len(rows) == 0 {
		data, err := os.ReadFile(s.path)
		if err == nil {
			var legacy struct {
				Plugins map[string]PluginState `json:"plugins"`
			}
			if json.Unmarshal(data, &legacy) == nil && legacy.Plugins != nil {
				s.current = legacy.Plugins
				s.saveLocked()
			}
		}
	}
}

func (s *State) saveLocked() {
	db := dbcore.GetDBInstance()
	for short, state := range s.current {
		row := models.PluginState{
			Namespace:               s.namespace,
			Short:                   short,
			Enabled:                 state.Enabled,
			ApprovedPermissionsHash: state.ApprovedPermissionsHash,
			LastError:               state.LastError,
			UpdatedAt:               state.UpdatedAt.UnixNano(),
		}
		if err := db.Save(&row).Error; err != nil {
			logger.Errorf("plugin", "failed to save plugin state %q: %v", short, err)
		}
	}
}

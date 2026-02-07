package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

const ConfigFileName = "config.json"

// ConfigManager handles configuration persistence.
type ConfigManager struct {
	configPath string
	mu         sync.Mutex
}

// NewConfigManager creates a new manager using the user config directory.
func NewConfigManager() (*ConfigManager, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	
	appDir := filepath.Join(configDir, "aether-realist")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		return nil, err
	}

	return &ConfigManager{
		configPath: filepath.Join(appDir, ConfigFileName),
	}, nil
}

// Load reads config from disk. Returns nil if file not found.
func (cm *ConfigManager) Load() (*SessionConfig, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	data, err := os.ReadFile(cm.configPath)
	if os.IsNotExist(err) {
		return nil, nil // No config yet
	}
	if err != nil {
		return nil, err
	}

	var config SessionConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

// Save writes config to disk.
func (cm *ConfigManager) Save(config *SessionConfig) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(cm.configPath, data, 0644)
}

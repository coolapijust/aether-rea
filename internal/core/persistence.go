package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

const ConfigFileName = "config.json"

// DefaultConfig returns the recommended default configuration.
func DefaultConfig() *SessionConfig {
	return &SessionConfig{
		ServerAddr:    "dev.softx.eu.org",
		ServerPort:    443,
		ServerPath:    "/aether",
		ListenAddr:    "127.0.0.1:1080",
		HttpProxyAddr: "127.0.0.1:1081",
		MaxPadding:    128,
		RecordPayloadBytes: DefaultMaxRecordPayload,
		SessionPoolMin: 4,
		SessionPoolMax: 8,
		PerfCaptureEnabled: false,
		PerfCaptureOnConnect: true,
		PerfLogPath: "logs/perf/client-perf.log",
		Rotation: RotationConfig{
			Enabled:       true,
			MinIntervalMs: 300000, // 5 min
			MaxIntervalMs: 600000, // 10 min
			PreWarmMs:     10000,  // 10 sec
		},
		BypassCN: true,
		BlockAds: true,
	}
}

// ConfigManager handles configuration persistence.
type ConfigManager struct {
	configPath string
	mu         sync.Mutex
}

// NewConfigManager creates a new manager. 
// It checks the current directory first, then falls back to user config directory.
func NewConfigManager() (*ConfigManager, error) {
	// 1. Check current directory
	localPath := ConfigFileName
	if _, err := os.Stat(localPath); err == nil {
		return &ConfigManager{configPath: localPath}, nil
	}

	// 2. Fallback to user config directory
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

// GetConfigPath returns the path to the configuration file.
func (cm *ConfigManager) GetConfigPath() string {
	return cm.configPath
}

// Load reads config from disk. Returns defaults if file not found.
func (cm *ConfigManager) Load() (*SessionConfig, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	defaults := DefaultConfig()

	data, err := os.ReadFile(cm.configPath)
	if os.IsNotExist(err) {
		return defaults, nil // Return defaults if no config yet
	}
	if err != nil {
		return nil, err
	}

	config := *defaults // Start with defaults
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

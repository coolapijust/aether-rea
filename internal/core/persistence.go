package core

import (
	"encoding/json"
	"errors"
	"fmt"
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

func isWritableFile(path string) bool {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

// NewConfigManager creates a new manager. 
// It checks the current directory first, then falls back to user config directory.
func NewConfigManager() (*ConfigManager, error) {
	// 1. Check current directory
	localPath := ConfigFileName
	if st, err := os.Stat(localPath); err == nil && !st.IsDir() {
		// If config exists but isn't writable (common when installed under protected dirs),
		// fall back to the user config directory so GUI "Save" works.
		if isWritableFile(localPath) {
			return &ConfigManager{configPath: localPath}, nil
		}
	}

	// 2. Fallback to user config directory
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("user config dir: %w", err)
	}
	
	appDir := filepath.Join(configDir, "aether-realist")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		return nil, fmt.Errorf("create config dir %s: %w", appDir, err)
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
		return nil, fmt.Errorf("read %s: %w", cm.configPath, err)
	}

	config := *defaults // Start with defaults
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parse %s: %w", cm.configPath, err)
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

	dir := filepath.Dir(cm.configPath)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create dir %s: %w", dir, err)
		}
	}
	if err := os.WriteFile(cm.configPath, data, 0644); err != nil {
		// If the chosen path became unwritable (e.g., config shipped read-only),
		// return a path-qualified error to make GUI/CLI diagnostics obvious.
		if errors.Is(err, os.ErrPermission) {
			return fmt.Errorf("write %s: permission denied", cm.configPath)
		}
		return fmt.Errorf("write %s: %w", cm.configPath, err)
	}
	return nil
}

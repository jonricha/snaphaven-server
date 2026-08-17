package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type Config struct {
	ServerID            string `json:"server_id,omitempty"`
	SyncDirectory       string `json:"sync_directory"`
	GRPCPort            string `json:"grpc_port"`
	CertDirectory       string `json:"cert_directory"`
	AutoStartOnBoot     bool   `json:"auto_start_on_boot"`
	OpenBrowserOnLaunch bool   `json:"open_browser_on_launch"`
	IsFirstRun          bool   `json:"is_first_run,omitempty"`
}

type ConfigManager struct {
	mu         sync.RWMutex
	filePath   string
	Config     Config
	isFirstRun bool
}

func GetDefaultConfigPath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		home, err := os.UserHomeDir()
		if err != nil {
			return "config.json", nil
		}
		configDir = home
	}
	appDir := filepath.Join(configDir, "SnapHaven")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		return "config.json", nil
	}
	return filepath.Join(appDir, "config.json"), nil
}

func NewConfigManager(customPath string) (*ConfigManager, error) {
	path := customPath
	if path == "" {
		var err error
		path, err = GetDefaultConfigPath()
		if err != nil {
			path = "config.json"
		}
	}

	homeDir, _ := os.UserHomeDir()
	defaultSync := filepath.Join(homeDir, "snaphaven")

	cm := &ConfigManager{
		filePath: path,
		Config: Config{
			SyncDirectory:       defaultSync,
			GRPCPort:            ":50005",
			CertDirectory:       "certs",
			AutoStartOnBoot:     false,
			OpenBrowserOnLaunch: false,
		},
	}

	if err := cm.Load(); err != nil {
		if os.IsNotExist(err) {
			cm.isFirstRun = true
			cm.Config.IsFirstRun = true
			cm.Save()
		}
	}

	if cm.Config.ServerID == "" {
		token, err := GenerateToken()
		if err == nil {
			cm.Config.ServerID = token
		} else {
			cm.Config.ServerID = "snaphaven-server"
		}
		cm.Save()
	}

	return cm, nil
}

func (cm *ConfigManager) Load() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	data, err := os.ReadFile(cm.filePath)
	if err != nil {
		return err
	}

	var loaded Config
	if err := json.Unmarshal(data, &loaded); err != nil {
		return err
	}

	cm.Config = loaded
	return nil
}

func (cm *ConfigManager) Save() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	data, err := json.MarshalIndent(cm.Config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(cm.filePath, data, 0644)
}

func (cm *ConfigManager) Update(newCfg Config) error {
	cm.mu.Lock()
	cm.Config = newCfg
	cm.mu.Unlock()

	return cm.Save()
}

func (cm *ConfigManager) IsFirstRun() bool {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	res := cm.isFirstRun || cm.Config.IsFirstRun
	if res {
		cm.isFirstRun = false
		cm.Config.IsFirstRun = false
		go cm.Save()
	}
	return res
}

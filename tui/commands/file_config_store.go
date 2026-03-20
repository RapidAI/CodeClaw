package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/RapidAI/CodeClaw/corelib"
)

// FileConfigStore 实现 config.ConfigStore，直接读写本地 JSON 文件。
type FileConfigStore struct {
	path string
}

// NewFileConfigStore 创建基于文件的 ConfigStore。
func NewFileConfigStore(dataDir string) *FileConfigStore {
	return &FileConfigStore{path: filepath.Join(dataDir, "config.json")}
}

// LoadConfig 从文件加载配置。
func (s *FileConfigStore) LoadConfig() (corelib.AppConfig, error) {
	var cfg corelib.AppConfig
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("read config: %w", err)
	}
	if len(data) == 0 {
		return cfg, nil
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}

// SaveConfig 将配置写入文件。
func (s *FileConfigStore) SaveConfig(cfg corelib.AppConfig) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir config dir: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write config tmp: %w", err)
	}
	return os.Rename(tmp, s.path)
}

package config

import "github.com/RapidAI/CodeClaw/corelib"

// ConfigStore abstracts configuration persistence so that ConfigManager
// does not depend on any concrete application struct.
type ConfigStore interface {
	LoadConfig() (corelib.AppConfig, error)
	SaveConfig(cfg corelib.AppConfig) error
}

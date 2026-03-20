package main

import (
	"fmt"
	"time"
)

// GetEnvCheckInterval returns the environment check interval in days
func (a *App) GetEnvCheckInterval() int {
	config, err := a.LoadConfig()
	if err != nil {
		return 7 // Default to 7 days
	}
	if config.EnvCheckInterval < 2 || config.EnvCheckInterval > 30 {
		return 7 // Default to 7 days if invalid
	}
	return config.EnvCheckInterval
}

// SetEnvCheckInterval sets the environment check interval in days (2-30)
func (a *App) SetEnvCheckInterval(days int) error {
	if days < 2 || days > 30 {
		return fmt.Errorf("interval must be between 2 and 30 days")
	}
	
	config, err := a.LoadConfig()
	if err != nil {
		return err
	}
	
	config.EnvCheckInterval = days
	return a.SaveConfig(config)
}

// ShouldCheckEnvironment checks if it's time to remind the user about environment check
func (a *App) ShouldCheckEnvironment() bool {
	config, err := a.LoadConfig()
	if err != nil {
		return false
	}
	
	// Don't remind if pause_env_check is disabled
	if !config.PauseEnvCheck {
		return false
	}
	
	// If never checked before, don't remind (only after first check)
	if config.LastEnvCheckTime == "" {
		return false
	}
	
	// Parse last check time
	lastCheck, err := time.Parse(time.RFC3339, config.LastEnvCheckTime)
	if err != nil {
		return false
	}
	
	// Get interval (default to 7 days if not set or invalid)
	interval := config.EnvCheckInterval
	if interval < 2 || interval > 30 {
		interval = 7
	}
	
	// Check if enough days have passed
	durationSinceCheck := time.Since(lastCheck)
	daysSinceCheck := int(durationSinceCheck.Hours() / 24)
	
	return daysSinceCheck >= interval
}

// UpdateLastEnvCheckTime updates the last environment check time to now
func (a *App) UpdateLastEnvCheckTime() {
	config, err := a.LoadConfig()
	if err != nil {
		return
	}
	
	config.LastEnvCheckTime = time.Now().Format(time.RFC3339)
	a.SaveConfig(config)
}

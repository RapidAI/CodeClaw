package config

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"
	"time"
)

type Config struct {
	Keys     []string  `json:"keys"`
	Accounts []Account `json:"accounts"`
}

type Account struct {
	Name        string    `json:"name"`
	Token       string    `json:"token"`
	Status      string    `json:"status"`
	LastUsed    time.Time `json:"lastUsed"`
	LastChecked time.Time `json:"lastChecked"`
	ErrorCount  int       `json:"errorCount"`
}

var globalConfig *Config

func LoadConfig() (*Config, error) {
	data, err := os.ReadFile("config.json")
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	globalConfig = &cfg
	log.Printf("Loaded config: %d accounts", len(cfg.Accounts))
	return &cfg, nil
}

func SaveConfig(cfg *Config) error {
	data, _ := json.MarshalIndent(cfg, "", "  ")
	return os.WriteFile("config.json", data, 0644)
}

func (c *Config) GetNextToken() string {
	if len(c.Accounts) == 0 {
		return ""
	}
	for i := range c.Accounts {
		if c.Accounts[i].Status == "active" {
			return c.Accounts[i].Token
		}
	}
	return c.Accounts[0].Token
}

func (c *Config) MarkTokenFailed(token string) {}
func (c *Config) MarkTokenActive(token string) {}

func (c *Config) AddAccount(name, cookieString string) error {
	token := extractTokenFromCookie(cookieString)
	if token == "" {
		return fmt.Errorf("无法从 cookie 中提取 token")
	}

	for _, acc := range c.Accounts {
		if acc.Token == token {
			return fmt.Errorf("该账号已存在")
		}
	}

	c.Accounts = append(c.Accounts, Account{
		Name:   name,
		Token:  token,
		Status: "active",
	})

	return SaveConfig(c)
}

func (c *Config) RemoveAccount(token string) error {
	for i, acc := range c.Accounts {
		if acc.Token == token {
			c.Accounts = append(c.Accounts[:i], c.Accounts[i+1:]...)
			return SaveConfig(c)
		}
	}
	return fmt.Errorf("账号不存在")
}

func (c *Config) GetAccounts() []Account {
	return c.Accounts
}

func (c *Config) GetAccountStats() map[string]interface{} {
	active, failed, unknown := 0, 0, 0
	for _, acc := range c.Accounts {
		switch acc.Status {
		case "active":
			active++
		case "failed":
			failed++
		default:
			unknown++
		}
	}

	return map[string]interface{}{
		"total":   len(c.Accounts),
		"active":  active,
		"failed":  failed,
		"unknown": unknown,
	}
}

func extractTokenFromCookie(cookieString string) string {
	re := regexp.MustCompile(`token=([a-f0-9]+)`)
	matches := re.FindStringSubmatch(cookieString)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

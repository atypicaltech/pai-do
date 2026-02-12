package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Enabled      bool
	BotToken     string
	AllowedUsers []string
	Sessions     SessionConfig
	Security     SecurityConfig
	Response     ResponseConfig
	Server       ServerConfig
	Memory       MemoryConfig
}

type SessionConfig struct {
	TimeoutMinutes int
	MaxConcurrent  int
	DefaultWorkDir string
	DefaultModel   string
	ResetHour      int // Hour of day (0-23) for daily session reset. -1 to disable.
}

type SecurityConfig struct {
	RequirePassphrase  bool
	RateLimitPerMinute int
}

type ResponseConfig struct {
	Format          string
	ForwardProgress bool
}

type ServerConfig struct {
	Port int
}

type MemoryConfig struct {
	Enabled      bool
	BasePath     string
	MaxSummaries int
}

func LoadConfig() (*Config, error) {
	home, _ := os.UserHomeDir()
	paiDir := os.Getenv("PAI_DIR")
	if paiDir == "" {
		paiDir = filepath.Join(home, ".claude")
	}

	settingsPath := filepath.Join(paiDir, "settings.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return nil, fmt.Errorf("settings.json not found at %s: %w", settingsPath, err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("invalid settings.json: %w", err)
	}

	// Parse env section
	var env map[string]string
	if rawEnv, ok := raw["env"]; ok {
		json.Unmarshal(rawEnv, &env)
	}

	botToken := env["TELEGRAM_BOT_TOKEN"]
	if botToken == "" {
		return nil, fmt.Errorf("TELEGRAM_BOT_TOKEN not found in settings.json -> env")
	}

	// Parse telegramBridge section
	var tb map[string]json.RawMessage
	if rawTB, ok := raw["telegramBridge"]; ok {
		json.Unmarshal(rawTB, &tb)
	}

	cfg := &Config{
		Enabled:      jsonBool(tb, "enabled", false),
		BotToken:     botToken,
		AllowedUsers: jsonStringSlice(tb, "allowed_users"),
		Sessions: SessionConfig{
			TimeoutMinutes: jsonIntNested(tb, "sessions", "timeout_minutes", 240),
			MaxConcurrent:  jsonIntNested(tb, "sessions", "max_concurrent", 2),
			DefaultWorkDir: resolveHome(jsonStringNested(tb, "sessions", "default_work_dir", "~/projects")),
			DefaultModel:   jsonStringNested(tb, "sessions", "default_model", "claude-sonnet-4-5-20250929"),
			ResetHour:      jsonIntNested(tb, "sessions", "reset_hour", 4),
		},
		Security: SecurityConfig{
			RequirePassphrase:  jsonBoolNested(tb, "security", "require_passphrase", false),
			RateLimitPerMinute: jsonIntNested(tb, "security", "rate_limit_per_minute", 10),
		},
		Response: ResponseConfig{
			Format:          jsonStringNested(tb, "response", "format", "concise"),
			ForwardProgress: jsonBoolNested(tb, "response", "forward_progress", true),
		},
		Server: ServerConfig{
			Port: jsonIntNested(tb, "server", "port", 7777),
		},
		Memory: MemoryConfig{
			Enabled:      jsonBoolNested(tb, "memory", "enabled", true),
			BasePath:     resolveHome(jsonStringNested(tb, "memory", "base_path", "/mnt/pai-data/memory")),
			MaxSummaries: jsonIntNested(tb, "memory", "max_summaries", 5),
		},
	}

	return cfg, nil
}

func resolveHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}

// JSON helper functions

func jsonBool(m map[string]json.RawMessage, key string, def bool) bool {
	if v, ok := m[key]; ok {
		var b bool
		if json.Unmarshal(v, &b) == nil {
			return b
		}
	}
	return def
}

func jsonStringSlice(m map[string]json.RawMessage, key string) []string {
	if v, ok := m[key]; ok {
		var s []string
		if json.Unmarshal(v, &s) == nil {
			return s
		}
	}
	return nil
}

func jsonNested(m map[string]json.RawMessage, section string) map[string]json.RawMessage {
	if v, ok := m[section]; ok {
		var nested map[string]json.RawMessage
		if json.Unmarshal(v, &nested) == nil {
			return nested
		}
	}
	return nil
}

func jsonIntNested(m map[string]json.RawMessage, section, key string, def int) int {
	nested := jsonNested(m, section)
	if nested == nil {
		return def
	}
	if v, ok := nested[key]; ok {
		var i int
		if json.Unmarshal(v, &i) == nil {
			return i
		}
	}
	return def
}

func jsonStringNested(m map[string]json.RawMessage, section, key, def string) string {
	nested := jsonNested(m, section)
	if nested == nil {
		return def
	}
	if v, ok := nested[key]; ok {
		var s string
		if json.Unmarshal(v, &s) == nil {
			return s
		}
	}
	return def
}

func jsonBoolNested(m map[string]json.RawMessage, section, key string, def bool) bool {
	nested := jsonNested(m, section)
	if nested == nil {
		return def
	}
	if v, ok := nested[key]; ok {
		var b bool
		if json.Unmarshal(v, &b) == nil {
			return b
		}
	}
	return def
}

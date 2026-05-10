package config

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
)

type Config struct {
	mu   sync.RWMutex
	file string
	data map[string]any
}

var Global = New()

func configFilePath() string {
	var dir string
	switch runtime.GOOS {
	case "darwin":
		dir = filepath.Join(os.Getenv("HOME"), "Library", "Application Support")
	case "windows":
		dir = os.Getenv("APPDATA")
	default:
		dir = filepath.Join(os.Getenv("HOME"), ".config")
	}
	if dir == "" {
		dir = "."
	}
	return filepath.Join(dir, "proxy-config.json")
}

func New() *Config {
	cfg := &Config{
		file: configFilePath(),
		data: make(map[string]any),
	}
	cfg.loadDefaults()
	cfg.loadFile()
	return cfg
}

func (c *Config) loadDefaults() {
	// Codex 配置
	c.setDefault("deepseek_key", os.Getenv("DEEPSEEK_API_KEY"))
	c.setDefault("deepseek_base", getEnvDefault("DEEPSEEK_BASE", "https://api.deepseek.com"))
	c.setDefault("default_model", getEnvDefault("DEFAULT_MODEL", "deepseek-v4-pro"))
	c.setDefault("model_mapping", map[string]string{
		"gpt-5.5": "deepseek-v4-pro",
		"gpt-5":   "deepseek-v4-pro",
	})
	c.setDefault("reasoning_effort", getEnvDefault("REASONING_EFFORT", "high"))
	c.setDefault("max_position_embeddings", parseEnvInt("MAX_POSITION_EMBEDDINGS", 272000))
	c.setDefault("max_output_tokens", parseEnvInt("MAX_OUTPUT_TOKENS", 65536))
	c.setDefault("temperature", 0.6)
	c.setDefault("top_p", 0.95)
	c.setDefault("tool_use_enforcement", true)
	c.setDefault("tool_use_prompt", "")
	c.setDefault("web_fetch_max_urls", parseEnvInt("WEB_FETCH_MAX_URLS", 5))
	c.setDefault("web_fetch_timeout", parseEnvInt("WEB_FETCH_TIMEOUT", 15))
	c.setDefault("web_fetch_max_body", parseEnvInt("WEB_FETCH_MAX_BODY", 32768))
	c.setDefault("enable_reasoning_cache", true)
	c.setDefault("reasoning_cache_ttl", getEnvDefault("REASONING_CACHE_TTL", "300"))
}

// 允许通过 API 更新的配置键
var allowedKeys = map[string]bool{
	"deepseek_key": true, "deepseek_base": true,
	"default_model": true, "model_mapping": true,
	"reasoning_effort": true, "max_position_embeddings": true, "max_output_tokens": true,
	"temperature": true, "top_p": true, "tool_use_enforcement": true, "tool_use_prompt": true,
	"web_fetch_max_urls": true, "web_fetch_timeout": true, "web_fetch_max_body": true,
	"enable_reasoning_cache": true, "reasoning_cache_ttl": true,
}

func (c *Config) setDefault(key string, value any) {
	if _, exists := c.data[key]; !exists {
		c.data[key] = value
	}
}

func (c *Config) loadFile() {
	data, err := os.ReadFile(c.file)
	if err != nil {
		return // 文件不存在就使用默认值
	}
	var fileData map[string]any
	if err := json.Unmarshal(data, &fileData); err != nil {
		slog.Warn("config file parse error", "file", c.file, "error", err)
		return
	}
	for k, v := range fileData {
		if allowedKeys[k] {
			c.data[k] = v
		}
	}
}

func (c *Config) saveFile() {
	data, err := json.MarshalIndent(c.data, "", "  ")
	if err != nil {
		slog.Warn("config marshal error", "error", err)
		return
	}
	dir := filepath.Dir(c.file)
	if err := os.MkdirAll(dir, 0755); err != nil {
		slog.Warn("config dir create error", "error", err)
		return
	}
	if err := os.WriteFile(c.file, data, 0644); err != nil {
		slog.Warn("config save error", "error", err)
	}
}

func (c *Config) Get(key string) any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.data[key]
}

func (c *Config) GetString(key string) string {
	v := c.Get(key)
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func (c *Config) GetBool(key string) bool {
	v := c.Get(key)
	if b, ok := v.(bool); ok {
		return b
	}
	return false
}

func (c *Config) GetInt(key string) int {
	v := c.Get(key)
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	}
	return 0
}

func (c *Config) GetFloat(key string) float64 {
	v := c.Get(key)
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	}
	return 0
}

func (c *Config) GetMap(key string) map[string]any {
	v := c.Get(key)
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

func (c *Config) Update(updates map[string]any) map[string]string {
	errs := make(map[string]string)
	c.mu.Lock()
	for k, v := range updates {
		if !allowedKeys[k] {
			errs[k] = "key not allowed"
			continue
		}
		c.data[k] = v
	}
	c.mu.Unlock()
	c.saveFile()
	return errs
}

func (c *Config) Reload() {
	c.mu.Lock()
	c.loadFile()
	c.mu.Unlock()
}

func (c *Config) ConfigDict() map[string]any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	cp := make(map[string]any, len(c.data))
	for k, v := range c.data {
		cp[k] = v
	}
	return cp
}

func getEnvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func parseEnvInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

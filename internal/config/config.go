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
		dir = filepath.Join(os.Getenv("HOME"), ".dp2codex")
	case "windows":
		dir = filepath.Join(os.Getenv("APPDATA"), "dp2codex")
	default:
		dir = filepath.Join(os.Getenv("HOME"), ".dp2codex")
	}
	if dir == "" {
		dir = "."
	}
	return filepath.Join(dir, "config.json")
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
	c.setDefault("enable_reasoning_cache", true)
	c.setDefault("reasoning_cache_ttl", getEnvDefault("REASONING_CACHE_TTL", "300"))
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
		c.data[k] = v
	}
	slog.Info("loaded config file", "file", c.file)
}

// SetAPIKey 设置 API Key（CLI 参数优先于环境变量和配置文件）
func (c *Config) SetAPIKey(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data["deepseek_key"] = key
}

// SetBaseURL 设置 API Base URL
func (c *Config) SetBaseURL(base string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data["deepseek_base"] = base
}

// SetModel 设置默认模型
func (c *Config) SetModel(model string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data["default_model"] = model
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

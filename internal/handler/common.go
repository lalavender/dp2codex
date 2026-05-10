package handler

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"dp2codex/internal/config"
	"dp2codex/internal/deepseek"
)

func generateID() string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%d", time.Now().UnixNano()))))
}

func generatePrefixedID(prefix string) string {
	return prefix + "_" + generateID()
}

func now() int64 {
	return time.Now().Unix()
}

func since(t time.Time) float64 {
	return time.Since(t).Seconds()
}

func newDSClient(source string) *deepseek.Client {
	baseURL := config.Global.GetString("deepseek_base")
	apiKey := config.Global.GetString("deepseek_key")
	return deepseek.NewClient(baseURL, apiKey)
}

func respToMap(resp *deepseek.ChatResponse) map[string]any {
	data, _ := json.Marshal(resp)
	var result map[string]any
	json.Unmarshal(data, &result)
	return result
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

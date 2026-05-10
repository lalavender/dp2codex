package handler

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"dp2codex/internal/config"
	"dp2codex/internal/deepseek"
)

// flushHTTP 刷新 SSE 缓冲区：优先 ResponseController（HTTP/2 等场景更可靠）
func flushHTTP(w http.ResponseWriter) {
	rc := http.NewResponseController(w)
	if err := rc.Flush(); err != nil {
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
}

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

// dumpChatReqMessages 将 chatReq 中的 messages 序列化为可读字符串，用于错误诊断
func dumpChatReqMessages(chatReq map[string]any) string {
	messages, ok := chatReq["messages"]
	if !ok {
		return "<no messages>"
	}
	data, err := json.MarshalIndent(messages, "", "  ")
	if err != nil {
		return fmt.Sprintf("<marshal error: %v>", err)
	}
	s := string(data)
	if len(s) > 4000 {
		return s[:4000] + "...(truncated)"
	}
	return s
}

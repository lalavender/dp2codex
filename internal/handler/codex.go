package handler

import (
	"encoding/json"
	"net/http"

	"dp2codex/internal/config"
)

func CodexModels(w http.ResponseWriter, r *http.Request) {
	modelMapping := config.Global.GetMap("model_mapping")
	defaultModel := config.Global.GetString("default_model")
	maxPosition := config.Global.GetInt("max_position_embeddings")
	maxOutput := config.Global.GetInt("max_output_tokens")

	if maxPosition <= 0 {
		maxPosition = 272000
	}
	if maxOutput <= 0 {
		maxOutput = 65536
	}

	models := []map[string]any{
		{
			"id": "gpt-5.5",
			"object": "model",
			"capabilities": map[string]any{
				"type":    "chat",
				"tool_use": true,
				"streaming": true,
				"reasoning_effort": map[string]any{
					"type":  "enum",
					"default": "high",
					"choices": []string{"low", "medium", "high"},
				},
			},
			"context_window": maxPosition,
			"max_output_tokens": maxOutput,
			"model_slug": "gpt-5.5",
			"provider": "OpenAI",
		},
		{
			"id": "gpt-5",
			"object": "model",
			"capabilities": map[string]any{
				"type":    "chat",
				"tool_use": true,
				"streaming": true,
				"reasoning_effort": map[string]any{
					"type":    "enum",
					"default": "high",
					"choices": []string{"low", "medium", "high"},
				},
			},
			"context_window": maxPosition,
			"max_output_tokens": maxOutput,
			"model_slug": "gpt-5",
			"provider": "OpenAI",
		},
		{
			"id": defaultModel,
			"object": "model",
			"capabilities": map[string]any{
				"type":    "chat",
				"tool_use": true,
				"streaming": true,
				"reasoning_effort": map[string]any{
					"type":    "enum",
					"default": "high",
					"choices": []string{"low", "medium", "high"},
				},
			},
			"context_window": maxPosition,
			"max_output_tokens": maxOutput,
			"model_slug": defaultModel,
			"provider": "OpenAI",
		},
	}

	_ = modelMapping

	resp := map[string]any{
		"data": models,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func CodexAnalytics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{}`))
}

func CodexPlugins(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"data":[]}`))
}

func CodexWham(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"apps": []any{},
	})
}

func CodexBackendFallback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{}`))
}

// CodexRPC 处理 Codex WebSocket RPC 调用
func CodexRPC(method string, params any) any {
	switch method {
	case "account/rateLimits/read":
		return map[string]any{
			"result": map[string]any{
				"rateLimits": []map[string]any{},
			},
		}
	case "config/requirements/read":
		return map[string]any{"result": map[string]any{}}
	case "modelProvider/capabilities/read":
		return map[string]any{
			"result": map[string]any{
				"provider": "OpenAI",
				"capabilities": []string{
					"streaming", "tool_use", "function_calling",
					"image_input", "reasoning_effort",
				},
			},
		}
	case "experimentalFeatures/list":
		return map[string]any{"result": []any{}}
	case "account/read":
		return map[string]any{
			"result": map[string]any{
				"id":    "local",
				"name":  "Local User",
				"email": "local@localhost",
			},
		}
	case "model/list":
		return map[string]any{"result": []any{}}
	case "account/login":
		return map[string]any{"result": map[string]any{"status": "logged_in"}}
	default:
		if containsAny(method, "/read", "/list") {
			return map[string]any{
				"result": map[string]any{
					"updated": []any{},
				},
			}
		}
		if containsAny(method, "mcpServer/", "skills/", "device/") {
			return map[string]any{
				"result": map[string]any{
					"updated": []any{},
				},
			}
		}
		return nil
	}
}

func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if stringsContains(s, sub) {
			return true
		}
	}
	return false
}

func stringsContains(s, substr string) bool {
	return len(s) >= len(substr) && containsStr(s, substr)
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

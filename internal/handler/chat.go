package handler

import (
	"encoding/json"
	"io"
	"net/http"

	"dp2codex/internal/config"
	"dp2codex/internal/deepseek"
	"dp2codex/internal/stats"
)

func ChatCompletions(w http.ResponseWriter, r *http.Request) {
	stats.RecordRequest()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error":"read body failed"}`, 400)
		return
	}

	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, 400)
		stats.RecordError(400)
		return
	}

	client := deepseek.NewClient(
		config.Global.GetString("deepseek_base"),
		config.Global.GetString("deepseek_key"),
	)

	stream, _ := req["stream"].(bool)
	if stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		if _, ok := w.(http.Flusher); !ok {
			http.Error(w, `{"error":"streaming not supported"}`, 500)
			return
		}
		stats.IncrementActiveStreams()
		defer stats.DecrementActiveStreams()

		err := client.ChatStream(req, "codex", func(delta deepseek.Delta, isLast bool, usage *deepseek.Usage) {
			resp := map[string]any{
				"id":      "chatcmpl-" + generateID(),
				"object":  "chat.completion.chunk",
				"created": now(),
				"model":   req["model"],
				"choices": []map[string]any{
					{
						"index": 0,
						"delta": map[string]any{},
					},
				},
			}
			if delta.Content != "" {
				resp["choices"].([]map[string]any)[0]["delta"].(map[string]any)["content"] = delta.Content
			}
			if delta.Role != "" {
				resp["choices"].([]map[string]any)[0]["delta"].(map[string]any)["role"] = delta.Role
			}
			if delta.ReasoningContent != "" {
				resp["choices"].([]map[string]any)[0]["delta"].(map[string]any)["reasoning_content"] = delta.ReasoningContent
			}
			if isLast {
				resp["choices"].([]map[string]any)[0]["finish_reason"] = "stop"
				if usage != nil {
					resp["usage"] = usage
				}
			}
			data, _ := json.Marshal(resp)
			w.Write([]byte("data: "))
			w.Write(data)
			w.Write([]byte("\n\n"))
			flushHTTP(w)
		})
		w.Write([]byte("data: [DONE]\n\n"))
		flushHTTP(w)

		if err != nil {
			stats.RecordUpstreamError(err.Error())
		}
	} else {
		resp, err := client.Chat(req, "codex")
		if err != nil {
			stats.RecordUpstreamError(err.Error())
			http.Error(w, `{"error":"upstream error"}`, 502)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

func ListModels(w http.ResponseWriter, r *http.Request) {
	models := map[string]any{
		"object": "list",
		"data": []map[string]any{
			{"id": "gpt-5.5", "object": "model", "created": now(), "owned_by": "system"},
			{"id": "gpt-5", "object": "model", "created": now(), "owned_by": "system"},
			{"id": "deepseek-v4-pro", "object": "model", "created": now(), "owned_by": "system"},
			{"id": "deepseek-v4-flash", "object": "model", "created": now(), "owned_by": "system"},
		},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(models)
}

func HealthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status":  "ok",
		"version": "1.0.0",
	})
}

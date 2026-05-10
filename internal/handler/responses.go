package handler

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"dp2codex/internal/cache"
	"dp2codex/internal/config"
	"dp2codex/internal/deepseek"
	"dp2codex/internal/protocol"
	"dp2codex/internal/stats"
)

func compactReq(data map[string]any) slog.Attr {
	var items, tools int
	var itemTypes []string
	if input, ok := data["input"].([]any); ok {
		items = len(input)
		typeCounts := make(map[string]int)
		for _, item := range input {
			if it, ok := item.(map[string]any); ok {
				typ, _ := it["type"].(string)
				if typ == "" {
					typ = "unknown"
				}
				typeCounts[typ]++
			}
		}
		for t, c := range typeCounts {
			itemTypes = append(itemTypes, fmt.Sprintf("%s:%d", t, c))
		}
	}
	if t, ok := data["tools"].([]any); ok {
		tools = len(t)
	}
	model, _ := data["model"].(string)
	stream, _ := data["stream"].(bool)
	return slog.Group("req",
		"model", model,
		"stream", stream,
		"input_items", items,
		"input_types", itemTypes,
		"tools", tools,
	)
}

var reasoningCache = newReasoningCache()

// ResponsesHTTP 处理 POST /v1/responses
func ResponsesHTTP(w http.ResponseWriter, r *http.Request) {
	stats.RecordRequest()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error":"read body failed"}`, 400)
		return
	}

	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		slog.Info("bad request body", "body", stats.Sanitize(string(body)))
		http.Error(w, `{"error":"invalid json"}`, 400)
		stats.RecordError(400)
		return
	}
	slog.Debug("responses", compactReq(data))

	// 空 input 检查
	input := data["input"]
	if input == nil || input == "" {
		if instructions, ok := data["instructions"].(string); ok && instructions != "" {
			// Codex v0.130: instructions-only 请求，无 user input
			// 快速返回成功响应，让 Codex 继续发送带 input 的请求
			modelName, _ := data["model"].(string)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"id":          "resp_" + generateID()[:12],
				"object":      "response",
				"created":     time.Now().Unix(),
				"status":      "completed",
				"model":       modelName,
				"output":      []any{},
				"output_text": "",
				"usage": map[string]any{
					"input_tokens":  0,
					"output_tokens": 0,
					"total_tokens":  0,
				},
			})
			return
		} else {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"id":      "resp_empty",
				"object":  "response",
				"created": time.Now().Unix(),
				"output":  []any{},
			})
			return
		}
	}

	// 提取 session ID
	sessionID := extractSessionID(data)

	// 模型映射
	modelName, _ := data["model"].(string)
	modelMapping := config.Global.GetMap("model_mapping")
	mappedModel := protocol.MapModel(modelName, modelMapping)
	data["model"] = mappedModel

	// 推理缓存
	cachedReasoning := ""
	if config.Global.GetBool("enable_reasoning_cache") {
		if r, ok := reasoningCache.Get("codex", sessionID); ok {
			cachedReasoning = r
		}
	}

	// 判断是否首轮
	isFirstRound := isFirstResponseRound(data)

	// 转换请求
	chatReq := protocol.ResponsesToChat(data, cachedReasoning, isFirstRound)
	if chatReq == nil {
		http.Error(w, `{"error":"invalid request"}`, 400)
		return
	}

	// 首轮注入
	if isFirstRound {
		injectFirstRound(chatReq)
	}

	// stream
	if stream, ok := data["stream"].(bool); ok && stream {
		streamResponses(w, chatReq, mappedModel, sessionID)
		return
	}

	// 非流式
	client := newDSClient("codex")
	resp, err := client.Chat(chatReq, "codex")
	if err != nil {
		slog.Error("non-stream upstream error",
			"error", err,
			"model", mappedModel,
			"chatReq_dump", dumpChatReqMessages(chatReq),
		)
		stats.RecordUpstreamError(err.Error())
		http.Error(w, fmt.Sprintf(`{"error":"upstream error: %s"}`, err), 502)
		return
	}

	// 缓存 reasoning
	cacheReasoning("codex", sessionID, resp)

	// 转换回 Responses 格式
	respObj := respToMap(resp)
	responsesObj := protocol.ChatToResponses(respObj, mappedModel)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(responsesObj)
}

func streamResponses(w http.ResponseWriter, chatReq map[string]any, model, sessionID string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	if _, ok := w.(http.Flusher); !ok {
		http.Error(w, `{"error":"streaming not supported"}`, 500)
		return
	}

	stats.IncrementActiveStreams()
	defer stats.DecrementActiveStreams()

	respID := "resp_" + generateID()[:12]
	outputItemID := "item_" + generateID()[:8]
	created := time.Now().Unix()

	responseObj := map[string]any{
		"id": respID, "object": "response", "created": created,
		"model": model, "output": []any{},
	}

	// response.created — Codex 查找 "response" 字段
	w.Write([]byte(protocol.BuildSSEEvent("response.created", map[string]any{
		"type": "response.created", "response": responseObj,
	})))
	flushHTTP(w)

	// response.in_progress — Codex 忽略此事件但发送以保持兼容
	w.Write([]byte(protocol.BuildSSEEvent("response.in_progress", map[string]any{
		"type": "response.in_progress", "response": responseObj,
	})))
	flushHTTP(w)

	// output_item.added — Codex 查找 "item" 字段
	w.Write([]byte(protocol.BuildSSEEvent("response.output_item.added", map[string]any{
		"type":         "response.output_item.added",
		"output_index": 0,
		"item": map[string]any{
			"id": outputItemID, "type": "message", "role": "assistant",
			"status":  "in_progress",
			"content": []map[string]any{},
		},
	})))
	flushHTTP(w)

	// content_part.added
	w.Write([]byte(protocol.BuildSSEEvent("response.content_part.added", map[string]any{
		"type":         "response.content_part.added",
		"output_index": 0, "content_index": 0,
		"part": map[string]any{"type": "output_text", "text": ""},
	})))
	flushHTTP(w)

	client := newDSClient("codex")
	var fullReasoning string
	var reasoningItemSent bool
	var reasoningItemID string
	var fullContent string
	var streamDone bool
	var hasToolCalls bool
	var toolCalls []deepseek.ToolCall

	err := client.ChatStream(chatReq, "codex", func(delta deepseek.Delta, isLast bool, usage *deepseek.Usage) {
		if delta.ReasoningContent != "" {
			// 首次收到 reasoning 时，发送 reasoning output_item
			if !reasoningItemSent {
				reasoningItemSent = true
				reasoningItemID = "item_" + generateID()[:8]
				w.Write([]byte(protocol.BuildSSEEvent("response.output_item.added", map[string]any{
					"type":         "response.output_item.added",
					"output_index": 0,
					"item": map[string]any{
						"id": reasoningItemID, "type": "message", "role": "assistant",
						"status": "in_progress",
						"content": []map[string]any{
							{"type": "reasoning_text", "text": "", "annotations": []any{}},
						},
					},
				})))
				flushHTTP(w)
			}
			fullReasoning += delta.ReasoningContent
			// Responses API：推理过程增量下发，避免界面长时间无输出
			w.Write([]byte(protocol.BuildSSEEvent("response.reasoning_text.delta", map[string]any{
				"type":          "response.reasoning_text.delta",
				"output_index":  0,
				"content_index": 0,
				"delta":         delta.ReasoningContent,
			})))
			flushHTTP(w)
		}

		if delta.Content != "" {
			fullContent += delta.Content
			w.Write([]byte(protocol.BuildSSEEvent("response.output_text.delta", map[string]any{
				"type":         "response.output_text.delta",
				"output_index": 0, "content_index": 0, "delta": delta.Content,
			})))
			flushHTTP(w)
		}

		if len(delta.ToolCalls) > 0 {
			hasToolCalls = true
			for _, tc := range delta.ToolCalls {
				if tc.ID != "" {
					toolCalls = append(toolCalls, tc)
				} else if len(toolCalls) > 0 {
					last := &toolCalls[len(toolCalls)-1]
					last.Function.Arguments += tc.Function.Arguments
				}
			}
		}

		if isLast {
			streamDone = true
			cacheReasoning("codex", sessionID, &deepseek.ChatResponse{
				Choices: []deepseek.Choice{
					{Message: deepseek.Message{ReasoningContent: fullReasoning}},
				},
			})

			// 如果有 reasoning，先完成 reasoning output_item
			if reasoningItemSent {
				w.Write([]byte(protocol.BuildSSEEvent("response.output_item.done", map[string]any{
					"type":         "response.output_item.done",
					"output_index": 0,
					"item": map[string]any{
						"id": reasoningItemID, "type": "message", "role": "assistant",
						"status": "completed",
						"content": []map[string]any{
							{"type": "reasoning_text", "text": fullReasoning, "annotations": []any{}},
						},
					},
				})))
				flushHTTP(w)
			}

			// 如果有 tool call，先发送 function_call 输出项
			if hasToolCalls {
				for _, tc := range toolCalls {
					fcItemID := "item_" + generateID()[:8]
					w.Write([]byte(protocol.BuildSSEEvent("response.output_item.added", map[string]any{
						"type":         "response.output_item.added",
						"output_index": 0,
						"item": map[string]any{
							"id": fcItemID, "type": "function_call",
							"call_id":   tc.ID,
							"name":      tc.Function.Name,
							"arguments": tc.Function.Arguments,
						},
					})))
					flushHTTP(w)

					w.Write([]byte(protocol.BuildSSEEvent("response.output_item.done", map[string]any{
						"type":         "response.output_item.done",
						"output_index": 0,
						"item": map[string]any{
							"id": fcItemID, "type": "function_call",
							"status":    "completed",
							"call_id":   tc.ID,
							"name":      tc.Function.Name,
							"arguments": tc.Function.Arguments,
						},
					})))
					flushHTTP(w)
				}
			}

			// output_item.done — Codex 处理此事件，从 "item" 反序列化
			w.Write([]byte(protocol.BuildSSEEvent("response.output_item.done", map[string]any{
				"type":         "response.output_item.done",
				"output_index": 0,
				"item": map[string]any{
					"id": outputItemID, "type": "message", "role": "assistant",
					"status":  "completed",
					"content": buildOutputContent(fullReasoning, fullContent),
				},
			})))
			flushHTTP(w)

			// response.completed — Codex 从 "response" 字段反序列化
			completedResponse := map[string]any{
				"id": respID,
			}
			if usage != nil {
				completedResponse["usage"] = map[string]any{
					"input_tokens":  usage.PromptTokens,
					"output_tokens": usage.CompletionTokens,
					"total_tokens":  usage.PromptTokens + usage.CompletionTokens,
				}
			}
			w.Write([]byte(protocol.BuildSSEEvent("response.completed", map[string]any{
				"type":     "response.completed",
				"response": completedResponse,
			})))
			flushHTTP(w)
		}
	})
	if err != nil && !streamDone {
		slog.Error("chat stream error",
			"error", err,
			"model", model,
			"chatReq_dump", dumpChatReqMessages(chatReq),
		)
		w.Write([]byte(protocol.BuildSSEEvent("response.completed", map[string]any{
			"type": "response.completed",
			"response": map[string]any{
				"id": respID,
			},
		})))
		flushHTTP(w)
	}
}

func extractSessionID(data map[string]any) string {
	// 1. 优先使用 metadata 中的会话 ID
	if meta, ok := data["metadata"].(map[string]any); ok {
		if id, _ := meta["conversation_id"].(string); id != "" {
			return id
		}
		if id, _ := meta["session_id"].(string); id != "" {
			return id
		}
	}
	// 2. 从 input 第一条消息提取稳定标识
	if input, ok := data["input"].([]any); ok && len(input) > 0 {
		if first, ok := input[0].(map[string]any); ok {
			// 尝试 string content
			if content, ok := first["content"].(string); ok && content != "" {
				h := sha256.Sum256([]byte(content))
				return fmt.Sprintf("hash_%x", h[:8])
			}
			// 尝试数组 content（取第一个 text 部分）
			if contentArr, ok := first["content"].([]any); ok && len(contentArr) > 0 {
				if part, ok := contentArr[0].(map[string]any); ok {
					if text, ok := part["text"].(string); ok && text != "" {
						h := sha256.Sum256([]byte(text))
						return fmt.Sprintf("hash_%x", h[:8])
					}
				}
			}
		}
	}
	// 3. 最后兜底
	return generateID()
}

func newReasoningCache() *cache.MemCache {
	return cache.NewMemCache()
}

func isFirstResponseRound(data map[string]any) bool {
	items, _ := data["input"].([]any)
	for _, item := range items {
		if it, ok := item.(map[string]any); ok {
			if role, _ := it["role"].(string); role == "assistant" {
				return false
			}
			if typ, _ := it["type"].(string); typ == "function_call" {
				return false
			}
		}
	}
	return true
}

func injectFirstRound(chatReq map[string]any) {
	if config.Global.GetBool("tool_use_enforcement") {
		prompt := config.Global.GetString("tool_use_prompt")
		if prompt != "" {
			// 转换 messages 类型为 map
			messages, ok := chatReq["messages"].([]protocol.ChatMessage)
			if ok && len(messages) > 0 && messages[0].Role == "system" {
				if s, ok := messages[0].Content.(string); ok {
					messages[0].Content = prompt + "\n\n" + s
					chatReq["messages"] = messages
				}
			}
		}
	}
}

func cacheReasoning(source, sessionID string, resp *deepseek.ChatResponse) {
	if !config.Global.GetBool("enable_reasoning_cache") {
		return
	}
	if len(resp.Choices) > 0 && resp.Choices[0].Message.ReasoningContent != "" {
		ttl := parseTTL(config.Global.GetString("reasoning_cache_ttl"))
		reasoningCache.Set(source, sessionID, resp.Choices[0].Message.ReasoningContent, ttl)
	}
}

func parseTTL(s string) time.Duration {
	if s == "" {
		return 5 * time.Minute
	}
	d, err := time.ParseDuration(s)
	if err == nil {
		return d
	}
	var secs int
	if _, err := fmt.Sscanf(s, "%d", &secs); err == nil {
		return time.Duration(secs) * time.Second
	}
	return 5 * time.Minute
}

// buildOutputContent 构建包含 reasoning 和 text 的输出内容
func buildOutputContent(reasoning, text string) []map[string]any {
	var content []map[string]any
	if reasoning != "" {
		content = append(content, map[string]any{
			"type": "reasoning_text", "text": reasoning, "annotations": []any{},
		})
	}
	if text != "" {
		content = append(content, map[string]any{
			"type": "output_text", "text": text, "annotations": []any{},
		})
	}
	return content
}

package protocol

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"
)

// ExtractMessageItems 从 Responses API input 中提取消息列表
func ExtractMessageItems(input any) []ChatMessage {
	switch v := input.(type) {
	case string:
		if v == "" {
			return nil
		}
		return []ChatMessage{{Role: "user", Content: v}}
	case []any:
		return extractItems(v)
	}
	return nil
}

func extractItems(items []any) []ChatMessage {
	var messages []ChatMessage

	for _, item := range items {
		it, ok := item.(map[string]any)
		if !ok {
			continue
		}
		typ, _ := it["type"].(string)
		switch typ {
		case "message":
			msg := ChatMessage{Role: "user", Content: ""}
			if role, ok := it["role"].(string); ok {
				msg.Role = MapRole(role)
			}
			if content, ok := it["content"]; ok {
				switch c := content.(type) {
				case string:
					msg.Content = c
				case []any:
					var parts []ContentPart
					for _, p := range c {
						if pm, ok := p.(map[string]any); ok {
							cp := ContentPart{}
							if t, ok := pm["type"].(string); ok {
								cp.Type = mapContentType(t)
							}
							if t, ok := pm["text"].(string); ok {
								cp.Text = t
							}
							parts = append(parts, cp)
						}
					}
					msg.Content = parts
				}
			}
			messages = append(messages, msg)

		case "function_call":
			// 转换为 assistant + tool_call
			call := ChatMessage{Role: "assistant", Content: ""}
			name, _ := it["name"].(string)
			args, _ := it["arguments"].(string)
			callID, _ := it["call_id"].(string)

			// 如果 callID 为空则生成一个
			if callID == "" {
				h := sha256.Sum256([]byte(name + args + fmt.Sprintf("%d", time.Now().UnixNano())))
				callID = fmt.Sprintf("fc_%x", h[:8])
			}

			call.ToolCalls = []ToolCall{{
				ID:   callID,
				Type: "function",
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: name, Arguments: args},
			}}
			if status, ok := it["status"].(string); ok && status == "completed" {
				// 已完成的 function_call 忽略
			}
			messages = append(messages, call)

		case "function_call_output":
			callID, _ := it["call_id"].(string)
			output, _ := it["output"].(string)
			messages = append(messages, ChatMessage{
				Role:       "tool",
				ToolCallID: callID,
				Content:    output,
			})

		case "web_search_call":
			// Codex 的 web_search 调用，转为空 assistant 消息
			messages = append(messages, ChatMessage{
				Role:    "assistant",
				Content: "",
			})

		case "file_search_call":
			// 类似 web_search_call
			messages = append(messages, ChatMessage{
				Role:    "assistant",
				Content: "",
			})
		}
	}

	return messages
}

// FixToolMessageOrdering 修复 tool 消息顺序以确保 DeepSeek 要求
// DeepSeek 要求: assistant -> tool -> tool (按 call_id 排序)
func FixToolMessageOrdering(messages []ChatMessage) []ChatMessage {
	if len(messages) < 2 {
		return messages
	}

	var result []ChatMessage
	for i := 0; i < len(messages); i++ {
		if messages[i].Role == "tool" && (i == 0 || messages[i-1].Role != "assistant") {
			// 找到最近的 assistant
			inserted := false
			for j := len(result) - 1; j >= 0; j-- {
				if result[j].Role == "assistant" {
					// 在 assistant 后插入
					result = append(result[:j+1], append([]ChatMessage{messages[i]}, result[j+1:]...)...)
					inserted = true
					break
				}
			}
			if !inserted {
				result = append(result, messages[i])
			}
		} else {
			result = append(result, messages[i])
		}
	}
	return result
}

// EnsureAssistantReasoning 确保 assistant 消息中包含 reasoning_content
func EnsureAssistantReasoning(messages []ChatMessage, cachedReasoning string) []ChatMessage {
	if cachedReasoning == "" {
		return messages
	}
	for i := range messages {
		if messages[i].Role == "assistant" && messages[i].ReasoningContent == "" {
			messages[i].ReasoningContent = cachedReasoning
		}
	}
	return messages
}

// ResponsesToChat 将 OpenAI Responses API 请求转换为 DeepSeek Chat 格式
func ResponsesToChat(data map[string]any, cachedReasoning string, isFirstRound bool) map[string]any {
	messages := ExtractMessageItems(data["input"])
	if len(messages) == 0 {
		return nil
	}

	messages = FixToolMessageOrdering(messages)

	// 处理 instructions 字段（Codex v0.130+）
	if instructions, ok := data["instructions"].(string); ok && instructions != "" {
		systemMsg := ChatMessage{Role: "system", Content: instructions}
		messages = append([]ChatMessage{systemMsg}, messages...)
	}

	// 注入缓存的 reasoning_content
	if cachedReasoning != "" {
		messages = EnsureAssistantReasoning(messages, cachedReasoning)
	}

	// 构建 Chat Request
	chatReq := map[string]any{
		"messages": messages,
	}

	// 模型映射
	if model, ok := data["model"].(string); ok && model != "" {
		// model mapping handled by caller
		chatReq["model"] = model
	}

	// 参数传递
	if temp, ok := data["temperature"].(float64); ok {
		chatReq["temperature"] = temp
	}
	if topP, ok := data["top_p"].(float64); ok {
		chatReq["top_p"] = topP
	}
	if maxTokens, ok := data["max_output_tokens"].(float64); ok {
		chatReq["max_tokens"] = int(maxTokens)
	}

	// stream
	if stream, ok := data["stream"].(bool); ok && stream {
		chatReq["stream"] = true
	}

	// tools
	if tools, ok := data["tools"].([]any); ok {
		var chatTools []map[string]any
		for _, t := range tools {
			if tm, ok := t.(map[string]any); ok {
				if normalized := normalizeTool(tm); normalized != nil {
					chatTools = append(chatTools, normalized)
				}
			}
		}
		if len(chatTools) > 0 {
			chatReq["tools"] = chatTools
			chatReq["tool_choice"] = "auto"
		}
	}

	return chatReq
}

// ChatToResponses 将 DeepSeek Chat 响应转换为 OpenAI Responses 格式
func ChatToResponses(chatResp map[string]any, model string) map[string]any {
	choices, _ := chatResp["choices"].([]any)
	if len(choices) == 0 {
		return nil
	}

	first, _ := choices[0].(map[string]any)
	msg, _ := first["message"].(map[string]any)

	resp := map[string]any{
		"id":      chatResp["id"],
		"object":  "response",
		"created": chatResp["created"],
		"model":   model,
	}

	var output []map[string]any

	// reasoning_content
	rc, _ := msg["reasoning_content"].(string)
	if rc != "" {
		output = append(output, map[string]any{
			"type": "message",
			"role": "assistant",
			"content": []map[string]any{
				{
					"type": "reasoning_text",
					"text": rc,
					"annotations": []any{},
				},
			},
		})
	}

	// content text
	if content, ok := msg["content"].(string); ok && content != "" {
		output = append(output, map[string]any{
			"type": "message",
			"role": "assistant",
			"content": []map[string]any{
				{
					"type": "output_text",
					"text": content,
					"annotations": []any{},
				},
			},
		})
	}

	// tool_calls
	if toolCalls, ok := msg["tool_calls"].([]any); ok {
		for _, tc := range toolCalls {
			tcm, _ := tc.(map[string]any)
			fn, _ := tcm["function"].(map[string]any)
			output = append(output, map[string]any{
				"type":      "function_call",
				"id":        tcm["id"],
				"name":      fn["name"],
				"arguments": fn["arguments"],
				"status":    "completed",
				"call_id":   tcm["id"],
			})
		}
	}

	if len(output) == 0 {
		output = append(output, map[string]any{
			"type": "message",
			"role": "assistant",
			"content": []map[string]any{
				{
					"type": "output_text",
					"text": "",
					"annotations": []any{},
				},
			},
		})
	}

	resp["output"] = output

	// usage
	if usage, ok := chatResp["usage"].(map[string]any); ok {
		resp["usage"] = map[string]any{
			"input_tokens":  usage["prompt_tokens"],
			"output_tokens": usage["completion_tokens"],
			"total_tokens":  usage["total_tokens"],
		}
	}

	return resp
}

// BuildSSEEvent 构建 SSE 事件字符串
func BuildSSEEvent(eventType string, data any) string {
	jsonStr := toJSON(data)
	return fmt.Sprintf("event: %s\ndata: %s\n\n", eventType, jsonStr)
}

// BuildResponseSSE 构建 Responses API 的 SSE 事件序列
func BuildResponseSSE(chatResp map[string]any, model string) []string {
	var events []string

	respID := generateID("resp")
	created := time.Now().Unix()
	outputItemID := generateID("item")

	// response.created
	events = append(events, BuildSSEEvent("response.created", map[string]any{
		"type": "response.created",
		"data": map[string]any{
			"id":      respID,
			"object":  "response",
			"created": created,
			"model":   model,
			"output":  []any{},
		},
	}))

	// response.in_progress
	events = append(events, BuildSSEEvent("response.in_progress", map[string]any{
		"type": "response.in_progress",
		"data": map[string]any{
			"id":      respID,
			"object":  "response",
			"created": created,
			"model":   model,
			"output":  []any{},
		},
	}))

	choices, _ := chatResp["choices"].([]any)
	if len(choices) == 0 {
		events = append(events, BuildSSEEvent("response.completed", map[string]any{
			"type": "response.completed",
			"data": map[string]any{
				"id":      respID,
				"object":  "response",
				"created": created,
				"model":   model,
				"output":  []any{},
			},
		}))
		return events
	}

	first, _ := choices[0].(map[string]any)
	msg, _ := first["message"].(map[string]any)

	// reasoning_content
	rc, _ := msg["reasoning_content"].(string)
	if rc != "" {
		events = append(events, BuildSSEEvent("response.output_item.added", map[string]any{
			"type": "response.output_item.added",
			"data": map[string]any{
				"output_index": 0,
				"item": map[string]any{
					"id":   outputItemID + "-reasoning",
					"type": "message",
					"role": "assistant",
					"content": []map[string]any{{
						"type":        "reasoning_text",
						"text":        rc,
						"annotations": []any{},
					}},
				},
			},
		}))
	}

	// content text
	if content, ok := msg["content"].(string); ok && content != "" {
		if rc == "" {
			events = append(events, BuildSSEEvent("response.output_item.added", map[string]any{
				"type": "response.output_item.added",
				"data": map[string]any{
					"output_index": 0,
					"item": map[string]any{
						"id":   outputItemID,
						"type": "message",
						"role": "assistant",
						"content": []map[string]any{},
					},
				},
			}))
		}
		events = append(events, BuildSSEEvent("response.content_part.added", map[string]any{
			"type": "response.content_part.added",
			"data": map[string]any{
				"output_index": 0,
				"content_index": 0,
				"part": map[string]any{
					"type": "output_text",
					"text": "",
				},
			},
		}))
		events = append(events, BuildSSEEvent("response.output_text.delta", map[string]any{
			"type": "response.output_text.delta",
			"data": map[string]any{
				"output_index": 0,
				"content_index": 0,
				"delta": content,
			},
		}))
	}

	// tool_calls
	if toolCalls, ok := msg["tool_calls"].([]any); ok {
		for _, tc := range toolCalls {
			tcm, _ := tc.(map[string]any)
			fn, _ := tcm["function"].(map[string]any)
			itemID := outputItemID + "-" + tcm["id"].(string)
			events = append(events, BuildSSEEvent("response.output_item.added", map[string]any{
				"type": "response.output_item.added",
				"data": map[string]any{
					"output_index": 0,
					"item": map[string]any{
						"id":        itemID,
						"type":      "function_call",
						"status":    "completed",
						"name":      fn["name"],
						"call_id":   tcm["id"],
						"arguments": fn["arguments"],
					},
				},
			}))
		}
	}

	// response.completed
	respMap := map[string]any{
		"id":      respID,
		"object":  "response",
		"created": created,
		"model":   model,
	}

	// 构建完整 output
	var fullOutput []map[string]any

	if rc, ok := msg["reasoning_content"].(string); ok && rc != "" {
		fullOutput = append(fullOutput, map[string]any{
			"id":   outputItemID + "-reasoning",
			"type": "message",
			"role": "assistant",
			"content": []map[string]any{
				{"type": "reasoning_text", "text": rc, "annotations": []any{}},
			},
		})
	}
	if content, ok := msg["content"].(string); ok {
		var contentParts []map[string]any
		if content != "" {
			contentParts = append(contentParts, map[string]any{
				"type": "output_text", "text": content, "annotations": []any{},
			})
		}
		if len(contentParts) > 0 {
			fullOutput = append(fullOutput, map[string]any{
				"id":      outputItemID,
				"type":    "message",
				"role":    "assistant",
				"content": contentParts,
			})
		}
	}
	if toolCalls, ok := msg["tool_calls"].([]any); ok {
		for _, tc := range toolCalls {
			tcm, _ := tc.(map[string]any)
			fn, _ := tcm["function"].(map[string]any)
			fullOutput = append(fullOutput, map[string]any{
				"id":        outputItemID + "-" + tcm["id"].(string),
				"type":      "function_call",
				"status":    "completed",
				"name":      fn["name"],
				"call_id":   tcm["id"],
				"arguments": fn["arguments"],
			})
		}
	}
	respMap["output"] = fullOutput

	if usage, ok := chatResp["usage"].(map[string]any); ok {
		respMap["usage"] = map[string]any{
			"input_tokens":  usage["prompt_tokens"],
			"output_tokens": usage["completion_tokens"],
			"total_tokens":  usage["total_tokens"],
		}
	}

	events = append(events, BuildSSEEvent("response.completed", map[string]any{
		"type": "response.completed",
		"data": respMap,
	}))

	return events
}

func toJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func generateID(prefix string) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())))
	return prefix + "_" + fmt.Sprintf("%x", h[:12])
}

// StreamDelta 流式 delta 数据结构
type StreamDelta struct {
	Content      string `json:"content,omitempty"`
	Reasoning    string `json:"reasoning,omitempty"`
	ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
	FinishReason string `json:"finish_reason,omitempty"`
}

// normalizeTool 将 OpenAI v2 工具格式转为 DeepSeek 支持的格式
func normalizeTool(tm map[string]any) map[string]any {
	// 如果已经有 function 子对象
	if fn, hasFn := tm["function"]; hasFn {
		if fnMap, ok := fn.(map[string]any); ok {
			// 检查 function 中是否有 name
			if _, hasName := fnMap["name"]; !hasName {
				// 从顶层获取 name
				if name, ok := tm["name"]; ok {
					fnMap["name"] = name
				}
			}
		}
		return tm
	}
	// 检查工具类型，非 function 类型跳过
	if typ, ok := tm["type"].(string); ok && typ != "function" {
		return nil
	}
	// 将扁平字段移到 function 子对象中
	fn := make(map[string]any)
	for _, k := range []string{"name", "description", "parameters"} {
		if v, ok := tm[k]; ok {
			fn[k] = v
			delete(tm, k)
		}
	}
	tm["function"] = fn
	return tm
}

// MapRole 将 OpenAI Responses API 的角色映射到 DeepSeek Chat 支持的角色
func MapRole(role string) string {
	switch role {
	case "developer":
		return "system"
	default:
		return role
	}
}

func mapContentType(t string) string {
	switch t {
	case "input_text", "output_text":
		return "text"
	default:
		return t
	}
}

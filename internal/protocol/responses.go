package protocol

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
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
					var reasoningText string
					for _, p := range c {
						if pm, ok := p.(map[string]any); ok {
							partType := "text"
							if t, ok := pm["type"].(string); ok {
								partType = mapContentType(t)
							}
							text, _ := pm["text"].(string)
							// 将 reasoning_text 提取到 ReasoningContent 字段（DeepSeek 要求回传）
							if partType == "reasoning_text" {
								reasoningText += text
								continue
							}
							parts = append(parts, ContentPart{Type: partType, Text: text})
						}
					}
					msg.Content = parts
					msg.ReasoningContent = reasoningText
				}
			}
			messages = append(messages, msg)

		case "function_call":
			// 转换为 assistant + tool_call，合并连续的 function_call 到同一个 assistant 消息
			name, _ := it["name"].(string)
			args, _ := it["arguments"].(string)
			callID, _ := it["call_id"].(string)

			if callID == "" {
				h := sha256.Sum256([]byte(name + args + fmt.Sprintf("%d", time.Now().UnixNano())))
				callID = fmt.Sprintf("fc_%x", h[:8])
			}

			tc := ToolCall{
				ID:   callID,
				Type: "function",
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: name, Arguments: args},
			}

			// 合并到前一个 assistant 消息（处理多 tool_call 场景）
			if len(messages) > 0 && messages[len(messages)-1].Role == "assistant" {
				messages[len(messages)-1].ToolCalls = append(messages[len(messages)-1].ToolCalls, tc)
			} else {
				messages = append(messages, ChatMessage{
					Role:      "assistant",
					Content:   "",
					ToolCalls: []ToolCall{tc},
				})
			}

		case "function_call_output":
			callID, _ := it["call_id"].(string)
			output, _ := it["output"].(string)
			messages = append(messages, ChatMessage{
				Role:       "tool",
				ToolCallID: callID,
				Content:    output,
			})

		case "web_search_call", "file_search_call":
			// 跳过: 这些在 Chat API 中没有对等物，创建空 assistant 消息会破坏 tool_call 链
			slog.Debug("skipping non-chat item", "type", typ)
			continue
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
// 优先级：消息自身的 ReasoningContent > Content parts 中的 reasoning_text > 缓存值
func EnsureAssistantReasoning(messages []ChatMessage, cachedReasoning string) []ChatMessage {
	for i := range messages {
		if messages[i].Role != "assistant" {
			continue
		}
		// 1. 尝试从 Content parts 中提取 reasoning_text
		if messages[i].ReasoningContent == "" {
			if parts, ok := messages[i].Content.([]ContentPart); ok {
				for _, p := range parts {
					if p.Type == "reasoning_text" {
						messages[i].ReasoningContent = p.Text
						break
					}
				}
			}
		}
		// 2. 如果仍然为空，使用缓存的 reasoning
		if messages[i].ReasoningContent == "" && cachedReasoning != "" {
			messages[i].ReasoningContent = cachedReasoning
		}
	}
	return messages
}

// MergeConsecutiveAssistant 合并连续的 assistant 消息
// Codex 可能把 function_call 和 assistant message 作为独立 item 发送，
// 导致生成多个 assistant 消息（一个有 tool_calls，一个有 content），破坏 tool_call 链
func MergeConsecutiveAssistant(messages []ChatMessage) []ChatMessage {
	if len(messages) < 2 {
		return messages
	}
	var result []ChatMessage
	result = append(result, messages[0])
	for i := 1; i < len(messages); i++ {
		curr := messages[i]
		prev := &result[len(result)-1]

		if prev.Role == "assistant" && curr.Role == "assistant" {
			// 合并 tool_calls
			prev.ToolCalls = append(prev.ToolCalls, curr.ToolCalls...)

			// 合并 content
			mergeAssistantContent(prev, curr)

			// 合并 reasoning
			if curr.ReasoningContent != "" {
				if prev.ReasoningContent == "" {
					prev.ReasoningContent = curr.ReasoningContent
				} else {
					prev.ReasoningContent += curr.ReasoningContent
				}
			}

			slog.Debug("merged consecutive assistant messages",
				"tool_calls", len(prev.ToolCalls),
				"has_content", prev.Content != nil,
			)
			continue
		}
		result = append(result, curr)
	}
	return result
}

// mergeAssistantContent 将 curr 的 content 合并到 prev 中
func mergeAssistantContent(prev *ChatMessage, curr ChatMessage) {
	if curr.Content == nil {
		return
	}
	switch c := curr.Content.(type) {
	case string:
		if c == "" {
			return
		}
		prevStr, isStr := prev.Content.(string)
		if (isStr && prevStr == "") || prev.Content == nil {
			prev.Content = c
		}
	case []ContentPart:
		if len(c) == 0 {
			return
		}
		if prevParts, ok := prev.Content.([]ContentPart); ok {
			prev.Content = append(prevParts, c...)
		} else {
			prev.Content = c
		}
	}
}

// ValidateToolCallPairs 验证每个 assistant 的 tool_calls 都有对应的 tool 响应
// 移除没有响应的孤立 tool_calls，防止上游 API 报错
func ValidateToolCallPairs(messages []ChatMessage) []ChatMessage {
	// 收集所有 tool 响应的 call_id
	toolResponseIDs := make(map[string]bool)
	for _, msg := range messages {
		if msg.Role == "tool" && msg.ToolCallID != "" {
			toolResponseIDs[msg.ToolCallID] = true
		}
	}

	var result []ChatMessage
	for _, msg := range messages {
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			var validCalls []ToolCall
			var orphaned []string
			for _, tc := range msg.ToolCalls {
				if toolResponseIDs[tc.ID] {
					validCalls = append(validCalls, tc)
				} else {
					orphaned = append(orphaned, fmt.Sprintf("%s(id=%s)", tc.Function.Name, tc.ID))
				}
			}
			if len(orphaned) > 0 {
				slog.Warn("removing orphaned tool_calls without responses",
					"orphaned", orphaned,
					"kept", len(validCalls),
				)
			}
			msg.ToolCalls = validCalls

			// 跳过完全空的 assistant 消息
			contentStr, isStr := msg.Content.(string)
			if len(msg.ToolCalls) == 0 && ((isStr && contentStr == "") || msg.Content == nil) {
				slog.Debug("removing empty assistant message after orphan cleanup")
				continue
			}
		}
		result = append(result, msg)
	}
	return result
}

// LogMessagesDebug 仅在 debug 日志级别输出完整消息结构，避免拖慢热路径
func LogMessagesDebug(label string, messages []ChatMessage) {
	var parts []string
	for i, msg := range messages {
		info := fmt.Sprintf("[%d] role=%s", i, msg.Role)
		switch c := msg.Content.(type) {
		case string:
			if len(c) > 80 {
				info += fmt.Sprintf(" content=%q...", c[:80])
			} else if c != "" {
				info += fmt.Sprintf(" content=%q", c)
			}
		case []ContentPart:
			info += fmt.Sprintf(" content_parts=%d", len(c))
		}
		if len(msg.ToolCalls) > 0 {
			var names []string
			for _, tc := range msg.ToolCalls {
				names = append(names, fmt.Sprintf("%s(id=%s)", tc.Function.Name, tc.ID))
			}
			info += fmt.Sprintf(" tool_calls=[%s]", strings.Join(names, ", "))
		}
		if msg.ToolCallID != "" {
			info += fmt.Sprintf(" tool_call_id=%s", msg.ToolCallID)
		}
		if msg.ReasoningContent != "" {
			info += fmt.Sprintf(" reasoning_len=%d", len(msg.ReasoningContent))
		}
		parts = append(parts, info)
	}
	slog.Debug(label, "messages", strings.Join(parts, " | "))
}

// ResponsesToChat 将 OpenAI Responses API 请求转换为 DeepSeek Chat 格式
func ResponsesToChat(data map[string]any, cachedReasoning string, isFirstRound bool) map[string]any {
	messages := ExtractMessageItems(data["input"])
	if len(messages) == 0 {
		slog.Warn("ResponsesToChat: no messages extracted from input")
		return nil
	}

	LogMessagesDebug("extracted messages (raw)", messages)

	messages = MergeConsecutiveAssistant(messages)
	messages = FixToolMessageOrdering(messages)
	messages = ValidateToolCallPairs(messages)

	LogMessagesDebug("messages (after merge+fix+validate)", messages)

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

	slog.Debug("ResponsesToChat complete", "total_messages", len(messages), "has_tools", chatReq["tools"] != nil)
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
					"type":        "reasoning_text",
					"text":        rc,
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
					"type":        "output_text",
					"text":        content,
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
					"type":        "output_text",
					"text":        "",
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
						"id":      outputItemID,
						"type":    "message",
						"role":    "assistant",
						"content": []map[string]any{},
					},
				},
			}))
		}
		events = append(events, BuildSSEEvent("response.content_part.added", map[string]any{
			"type": "response.content_part.added",
			"data": map[string]any{
				"output_index":  0,
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
				"output_index":  0,
				"content_index": 0,
				"delta":         content,
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
	Content      string     `json:"content,omitempty"`
	Reasoning    string     `json:"reasoning,omitempty"`
	ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
	FinishReason string     `json:"finish_reason,omitempty"`
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

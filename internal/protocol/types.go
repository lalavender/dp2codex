package protocol

// ==================== OpenAI Chat Completions (DeepSeek 原生格式) ====================

type ChatRequest struct {
	Model       string         `json:"model"`
	Messages    []ChatMessage  `json:"messages"`
	Stream      bool           `json:"stream,omitempty"`
	Temperature *float64       `json:"temperature,omitempty"`
	TopP        *float64       `json:"top_p,omitempty"`
	MaxTokens   int            `json:"max_tokens,omitempty"`
	Stop        []string       `json:"stop,omitempty"`
	Tools       []ToolDef      `json:"tools,omitempty"`
	ToolChoice  any            `json:"tool_choice,omitempty"`  // "auto", "none", or specific
	Thinking    *ThinkingParam `json:"thinking,omitempty"`    // DeepSeek 专用
}

type ThinkingParam struct {
	Type string `json:"type"`
}

type ChatMessage struct {
	Role         string     `json:"role"` // "system", "user", "assistant", "tool"
	Content      any        `json:"content,omitempty"` // string or []ContentPart
	ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID   string     `json:"tool_call_id,omitempty"`
	Name         string     `json:"name,omitempty"`
	ReasoningContent string `json:"reasoning_content"` // DeepSeek 专用
}

type ContentPart struct {
	Type     string `json:"type"`               // "text", "image_url"
	Text     string `json:"text"`
	ImageURL *struct {
		URL string `json:"url"`
	} `json:"image_url,omitempty"`
}

type ToolDef struct {
	Type     string   `json:"type"`     // "function"
	Function Function `json:"function"`
}

type Function struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

type ToolCall struct {
	ID       string `json:"id,omitempty"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type ChatChoice struct {
	Index        int          `json:"index"`
	Message      ChatMessage  `json:"message,omitempty"`
	Delta        ChatMessage  `json:"delta,omitempty"`
	FinishReason string       `json:"finish_reason,omitempty"`
}

type ChatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type ChatResponse struct {
	ID                string       `json:"id"`
	Object            string       `json:"object"`
	Created           int64        `json:"created"`
	Model             string       `json:"model"`
	Choices           []ChatChoice `json:"choices"`
	Usage             *ChatUsage   `json:"usage,omitempty"`
}

// DeepSeek 流式 delta 可能包含 reasoning_content
type ChatDelta struct {
	Role              string `json:"role,omitempty"`
	Content           string `json:"content,omitempty"`
	ReasoningContent  string `json:"reasoning_content,omitempty"`
}

// ==================== OpenAI Responses API ====================

type ResponseRequest struct {
	Input     any                  `json:"input"`     // string or []ResponseInputItem
	Model     string               `json:"model"`
	Tools     []ResponseTool       `json:"tools,omitempty"`
	Stream    bool                 `json:"stream,omitempty"`
	// 其他参数
	Temperature *float64           `json:"temperature,omitempty"`
	TopP        *float64           `json:"top_p,omitempty"`
	MaxOutputTokens int            `json:"max_output_tokens,omitempty"`
	Metadata    map[string]string  `json:"metadata,omitempty"`
}

type ResponseTool struct {
	Type        string      `json:"type"` // "function", "web_fetch", etc.
	Name        string      `json:"name,omitempty"`
	Description string      `json:"description,omitempty"`
	Parameters  any         `json:"parameters,omitempty"`
	Function    *Function   `json:"function,omitempty"`
}

type ResponseInputItem struct {
	Type       string `json:"type"` // "message", "function_call", "function_call_output", "file_search_call", "web_search_call"
	Role       string `json:"role,omitempty"`
	Content    any    `json:"content,omitempty"` // string or []ContentPart
	CallID     string `json:"call_id,omitempty"`
	Output     string `json:"output,omitempty"`
	Name       string `json:"name,omitempty"`
	Arguments  string `json:"arguments,omitempty"`
	Status     string `json:"status,omitempty"`
}

type ResponseOutput struct {
	ID         string                 `json:"id"`
	Object     string                 `json:"object"`
	Created    int64                  `json:"created"`
	Model      string                 `json:"model"`
	Output     []ResponseOutputItem   `json:"output"`
	Usage      *ResponseUsage         `json:"usage,omitempty"`
}

type ResponseOutputItem struct {
	ID       string                 `json:"id"`
	Type     string                 `json:"type"` // "message", "function_call", "web_search_call"
	Status   string                 `json:"status,omitempty"`
	Role     string                 `json:"role,omitempty"`
	Content  []ResponseContentPart  `json:"content,omitempty"`
	Name     string                 `json:"name,omitempty"`
	CallID   string                 `json:"call_id,omitempty"`
	Output   string                 `json:"output,omitempty"`
	Arguments string                `json:"arguments,omitempty"`
}

type ResponseContentPart struct {
	Type string `json:"type"` // "input_text", "output_text", "reasoning_text", "refusal"
	Text string `json:"text,omitempty"`
	Annotations []any `json:"annotations,omitempty"`
}

type ResponseUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// SSE 事件
type ResponseSSEEvent struct {
	Type string `json:"type"`
	Data any    `json:"data,omitempty"`
}

package deepseek

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"dp2codex/internal/config"
)

type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
}

func NewClient(baseURL, apiKey string) *Client {
	if baseURL == "" {
		baseURL = "https://api.deepseek.com"
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 300 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:    100,
				MaxConnsPerHost: 20,
				IdleConnTimeout: 90 * time.Second,
			},
		},
	}
}

type ChatResponse struct {
	ID      string            `json:"id"`
	Object  string            `json:"object"`
	Created int64             `json:"created"`
	Model   string            `json:"model"`
	Choices []Choice          `json:"choices"`
	Usage   *Usage            `json:"usage,omitempty"`
}

type Choice struct {
	Index        int              `json:"index"`
	Message      Message          `json:"message,omitempty"`
	Delta        Delta            `json:"delta,omitempty"`
	FinishReason string           `json:"finish_reason,omitempty"`
}

type Message struct {
	Role              string     `json:"role"`
	Content           string     `json:"content,omitempty"`
	ReasoningContent  string     `json:"reasoning_content,omitempty"`
	ToolCalls         []ToolCall `json:"tool_calls,omitempty"`
}

type Delta struct {
	Role              string     `json:"role,omitempty"`
	Content           string     `json:"content,omitempty"`
	ReasoningContent  string     `json:"reasoning_content,omitempty"`
	ToolCalls         []ToolCall `json:"tool_calls,omitempty"`
}

type ToolCall struct {
	ID       string `json:"id,omitempty"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type StreamCallback func(delta Delta, isLast bool, usage *Usage)

// Chat 非流式请求
func (c *Client) Chat(request map[string]any, source string) (*ChatResponse, error) {
	baseURL := config.Global.GetString("deepseek_base")
	apiKey := config.Global.GetString("deepseek_key")

	requestURL := baseURL + "/v1/chat/completions"
	body, _ := json.Marshal(request)

	req, err := http.NewRequest("POST", requestURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("upstream %d: %s", resp.StatusCode, string(respBody))
	}

	var chatResp ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &chatResp, nil
}

// ChatStream 流式请求
func (c *Client) ChatStream(request map[string]any, source string, cb StreamCallback) error {
	baseURL := config.Global.GetString("deepseek_base")
	apiKey := config.Global.GetString("deepseek_key")

	request["stream"] = true
	requestURL := baseURL + "/v1/chat/completions"
	reqBody, _ := json.Marshal(request)

	req, err := http.NewRequest("POST", requestURL, bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("stream request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("upstream %d: %s", resp.StatusCode, string(respBody))
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 65536), 65536)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var streamResp struct {
			Choices []struct {
				Index        int    `json:"index"`
				Delta        Delta  `json:"delta"`
				FinishReason string `json:"finish_reason,omitempty"`
			} `json:"choices"`
			Usage *Usage `json:"usage,omitempty"`
		}

		if err := json.Unmarshal([]byte(data), &streamResp); err != nil {
			slog.Debug("stream parse error", "error", err)
			continue
		}

		if len(streamResp.Choices) == 0 {
			continue
		}

		choice := streamResp.Choices[0]
		delta := choice.Delta

		isLast := choice.FinishReason != ""
		var usage *Usage
		if isLast && streamResp.Usage != nil {
			usage = streamResp.Usage
		}

		if cb != nil {
			cb(delta, isLast, usage)
		}
	}

	return scanner.Err()
}

// Compact 调用 DeepSeek 压缩对话
func (c *Client) Compact(messages []map[string]any, source string) (string, error) {
	systemMsg := "You are a conversation compression assistant. Compress the following conversation into a concise summary, preserving all key information, decisions, and context. Output only the summary."
	request := map[string]any{
		"model": config.Global.GetString("default_model"),
		"messages": []map[string]any{
			{"role": "system", "content": systemMsg},
			{"role": "user", "content": messages},
		},
		"max_tokens": 2048,
		"temperature": 0.3,
	}

	resp, err := c.Chat(request, source)
	if err != nil {
		return "", err
	}
	if len(resp.Choices) > 0 {
		return resp.Choices[0].Message.Content, nil
	}
	return "", fmt.Errorf("no response from upstream")
}

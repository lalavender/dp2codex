package deepseek

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
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
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		httpClient: &http.Client{
			// 不设全局 Timeout，流式连接会被误杀
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					c, err := dialer.DialContext(ctx, network, addr)
					if err != nil {
						return nil, err
					}
					if tcp, ok := c.(*net.TCPConn); ok {
						_ = tcp.SetNoDelay(true)
					}
					return c, nil
				},
				MaxIdleConns:          100,
				MaxConnsPerHost:       20,
				IdleConnTimeout:       90 * time.Second,
				ResponseHeaderTimeout: 60 * time.Second, // 仅等待首个响应头
				DisableCompression:    true,             // SSE 不需 gzip，减少缓冲延迟
			},
		},
	}
}

type ChatResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   *Usage   `json:"usage,omitempty"`
}

type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message,omitempty"`
	Delta        Delta   `json:"delta,omitempty"`
	FinishReason string  `json:"finish_reason,omitempty"`
}

type Message struct {
	Role             string     `json:"role"`
	Content          string     `json:"content,omitempty"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
}

type Delta struct {
	Role             string     `json:"role,omitempty"`
	Content          string     `json:"content,omitempty"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
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
	baseURL := strings.TrimRight(c.baseURL, "/")
	apiKey := strings.TrimSpace(c.apiKey)
	if baseURL == "" {
		baseURL = strings.TrimRight(config.Global.GetString("deepseek_base"), "/")
	}
	if apiKey == "" {
		apiKey = strings.TrimSpace(config.Global.GetString("deepseek_key"))
	}
	if apiKey == "" {
		return nil, fmt.Errorf("missing DeepSeek API key")
	}

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
	baseURL := strings.TrimRight(c.baseURL, "/")
	apiKey := strings.TrimSpace(c.apiKey)
	if baseURL == "" {
		baseURL = strings.TrimRight(config.Global.GetString("deepseek_base"), "/")
	}
	if apiKey == "" {
		apiKey = strings.TrimSpace(config.Global.GetString("deepseek_key"))
	}
	if apiKey == "" {
		return fmt.Errorf("missing DeepSeek API key")
	}

	request["stream"] = true
	// 请求在最后一个 chunk 中返回 usage 统计
	request["stream_options"] = map[string]any{"include_usage": true}

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

	// ReadBytes 按行解析 SSE，避免 Scanner 额外拷贝；大行缓冲区减轻上游粘包延迟
	br := bufio.NewReaderSize(resp.Body, 256*1024)

	var lastUsage *Usage

	for {
		line, err := br.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("stream read: %w", err)
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		if !bytes.HasPrefix(line, []byte("data: ")) {
			continue
		}

		data := strings.TrimSpace(string(line[len("data: "):]))
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

		// 保存 usage（可能在独立的最后一个 chunk 中）
		if streamResp.Usage != nil {
			lastUsage = streamResp.Usage
		}

		if len(streamResp.Choices) == 0 {
			continue
		}

		choice := streamResp.Choices[0]
		delta := choice.Delta

		isLast := choice.FinishReason != ""
		var usage *Usage
		if isLast {
			// 优先使用流式返回的 usage，其次使用之前保存的
			if streamResp.Usage != nil {
				usage = streamResp.Usage
			} else {
				usage = lastUsage
			}
		}

		if cb != nil {
			cb(delta, isLast, usage)
		}
	}

	return nil
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
		"max_tokens":  2048,
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

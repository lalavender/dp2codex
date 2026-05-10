package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"dp2codex/internal/config"
)

var urlRegex = regexp.MustCompile(`https?://[^\s<>"']+`)

// ExtractURLs 从文本中提取 URL
func ExtractURLs(text string) []string {
	matches := urlRegex.FindAllString(text, -1)
	seen := make(map[string]bool)
	var result []string
	for _, u := range matches {
		if !seen[u] {
			seen[u] = true
			result = append(result, u)
		}
	}
	return result
}

// HasURLsInMessages 检查消息中是否含 URL
func HasURLsInMessages(messages []map[string]any) bool {
	for _, msg := range messages {
		if content, ok := msg["content"].(string); ok {
			if urlRegex.MatchString(content) {
				return true
			}
		}
		if content, ok := msg["content"].([]any); ok {
			for _, part := range content {
				if m, ok := part.(map[string]any); ok {
					if text, ok := m["text"].(string); ok {
						if urlRegex.MatchString(text) {
							return true
						}
					}
				}
			}
		}
	}
	return false
}

// EnsureWebFetchTool 确保 tools 中包含 web_fetch 工具定义
func EnsureWebFetchTool(tools []map[string]any) []map[string]any {
	for _, t := range tools {
		if name, ok := t["name"].(string); ok && name == "web_fetch" {
			return tools
		}
	}
	tools = append(tools, map[string]any{
		"name":        "web_fetch",
		"description": "Fetch the content of a URL. Returns the HTTP response body as text.",
	})
	return tools
}

// PrefetchURLs 同步预取 URL 并注入到消息末尾
func PrefetchURLs(messages []map[string]any) []map[string]any {
	if len(messages) == 0 {
		return messages
	}
	lastUserIdx := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if role, ok := messages[i]["role"].(string); ok && role == "user" {
			lastUserIdx = i
			break
		}
	}
	if lastUserIdx < 0 {
		return messages
	}

	msg := messages[lastUserIdx]
	content, ok := msg["content"].(string)
	if !ok {
		return messages
	}

	urls := ExtractURLs(content)
	if len(urls) == 0 {
		return messages
	}

	maxURLs := config.Global.GetInt("web_fetch_max_urls")
	timeout := config.Global.GetInt("web_fetch_timeout")
	maxBody := config.Global.GetInt("web_fetch_max_body")
	if maxURLs <= 0 {
		maxURLs = 5
	}
	if timeout <= 0 {
		timeout = 15
	}
	if maxBody <= 0 {
		maxBody = 32768
	}

	if len(urls) > maxURLs {
		urls = urls[:maxURLs]
	}

	client := &http.Client{Timeout: time.Duration(timeout) * time.Second}
	var results []string

	for _, u := range urls {
		if IsPrivateURL(u) {
			slog.Warn("SSRF blocked prefetch", "url", u)
			continue
		}
		body, err := fetchURL(client, u, maxBody)
		if err != nil {
			slog.Warn("prefetch failed", "url", u, "error", err)
			results = append(results, fmt.Sprintf("[URL: %s]\n[Error: %s]", u, err))
		} else {
			results = append(results, fmt.Sprintf("[URL: %s]\n%s", u, body))
		}
	}

	if len(results) > 0 {
		newContent := content + "\n\n--- fetched URL contents ---\n" + strings.Join(results, "\n\n")
		msg["content"] = newContent
	}

	return messages
}

func fetchURL(client *http.Client, url string, maxBody int) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; JinDX/1.0)")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxBody)))
	if err != nil {
		return "", err
	}

	text := stripHTML(string(body))
	if len(text) > maxBody {
		text = text[:maxBody]
	}
	return text, nil
}

func stripHTML(s string) string {
	tagRe := regexp.MustCompile(`<[^>]*>`)
	s = tagRe.ReplaceAllString(s, " ")
	wsRe := regexp.MustCompile(`\s+`)
	s = wsRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// ExecuteWebFetch 服务端执行 web_fetch 工具
func ExecuteWebFetch(argsJSON string) (string, error) {
	var args struct {
		URL    string `json:"url"`
		Method string `json:"method,omitempty"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if args.URL == "" {
		return "", fmt.Errorf("url is required")
	}
	if IsPrivateURL(args.URL) {
		return "", fmt.Errorf("blocked private URL: %s", args.URL)
	}

	jinaURL := "https://r.jina.ai/" + args.URL
	client := &http.Client{Timeout: 30 * time.Second}

	body, err := fetchURL(client, jinaURL, 65536)
	if err != nil {
		body, err = fetchURL(client, args.URL, 65536)
		if err != nil {
			return "", err
		}
	}
	return body, nil
}

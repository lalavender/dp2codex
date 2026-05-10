package server

import (
	"log/slog"
	"net/http"
	"time"

	"dp2codex/internal/admin"
	"dp2codex/internal/cache"
	"dp2codex/internal/handler"
)

func NewHTTPServer(port string) *http.Server {
	mux := http.NewServeMux()

	// Routes API
	mux.HandleFunc("POST /v1/chat/completions", handler.ChatCompletions)
	mux.HandleFunc("POST /v1/responses", handler.ResponsesHTTP)
	mux.HandleFunc("POST /v1/responses/compact", handler.ResponsesCompact)
	mux.HandleFunc("GET /v1/models", handler.ListModels)
	mux.HandleFunc("GET /health", handler.HealthCheck)

	mux.HandleFunc("GET /backend-api/codex/models", handler.CodexModels)
	mux.HandleFunc("GET /backend-api/models", handler.CodexModels)
	mux.HandleFunc("POST /backend-api/codex/analytics-events/events", handler.CodexAnalytics)
	mux.HandleFunc("POST /backend-api/analytics-events/events", handler.CodexAnalytics)
	mux.HandleFunc("GET /backend-api/plugins/featured", handler.CodexPlugins)
	mux.HandleFunc("POST /backend-api/wham/apps", handler.CodexWham)
	mux.HandleFunc("GET /backend-api/", handler.CodexBackendFallback)
	mux.HandleFunc("POST /backend-api/", handler.CodexBackendFallback)

	// 别名路由（无 /v1 前缀）
	mux.HandleFunc("POST /chat/completions", handler.ChatCompletions)
	mux.HandleFunc("POST /responses", handler.ResponsesHTTP)
	mux.HandleFunc("POST /responses/compact", handler.ResponsesCompact)
	mux.HandleFunc("GET /models", handler.ListModels)

	// cc-switch 转发代理模式
	mux.HandleFunc("GET /", forwardProxyMiddleware(handler.ListModels))
	mux.HandleFunc("POST /", forwardProxyMiddleware(handler.ResponsesHTTP))

	slog.Info("HTTP server starting", "port", port)

	// 日志中间件包装
	logged := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slog.Info("INCOMING", "method", r.Method, "path", r.URL.Path, "host", r.Host)
		mux.ServeHTTP(w, r)
	})

	return &http.Server{
		Addr:         ":" + port,
		Handler:      logged,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 300 * time.Second,
	}
}

func NewAdminServer(port string, rc *cache.MemCache) *http.Server {
	mux := http.NewServeMux()
	api := admin.NewAdminAPI(rc)

	// 管理面板
	mux.HandleFunc("GET /", admin.AdminPage)
	mux.HandleFunc("GET /config", api.GetConfig)
	mux.HandleFunc("POST /config", api.SetConfig)
	mux.HandleFunc("GET /stats", api.GetStats)
	mux.HandleFunc("GET /sessions", api.GetSessions)
	mux.HandleFunc("GET /logs", api.GetLogs)
	mux.HandleFunc("GET /health", api.Health)

	slog.Info("Admin server starting", "port", port)
	return &http.Server{
		Addr:    ":" + port,
		Handler: admin.Middleware(mux),
	}
}

// forwardProxyMiddleware 处理 cc-switch 的转发代理模式
// 检测请求路径是否以 http:// 或 https:// 开头
func forwardProxyMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if len(path) > 7 && (path[:7] == "http://" || path[:8] == "https://") {
			// 提取真实路径（去掉 scheme://host 前缀）
			// cc-switch 编码方式: /http://host/path -> /path
			for i := 0; i < len(path); i++ {
				if i > 0 && path[i] == '/' && stringsCountBefore(path, i) >= 3 {
					r.URL.Path = path[i:]
					break
				}
			}
		}
		next(w, r)
	}
}

func stringsCountBefore(s string, idx int) int {
	count := 0
	for i := 0; i < idx; i++ {
		if s[i] == '/' {
			count++
		}
	}
	return count
}

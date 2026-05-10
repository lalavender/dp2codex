package admin

import (
	"net/http"
	"strings"

	"dp2codex/internal/config"
)

// Middleware 管理 API 认证中间件
// HTML 页面（GET /）无需认证，API 端点需要 Bearer token
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// HTML 页面无需认证
		if r.Method == "GET" && (r.URL.Path == "/" || r.URL.Path == "") {
			next.ServeHTTP(w, r)
			return
		}

		key := config.Global.GetString("deepseek_key")
		if key == "" {
			next.ServeHTTP(w, r)
			return
		}

		auth := r.Header.Get("Authorization")
		if auth == "" {
			w.Header().Set("WWW-Authenticate", `Bearer realm="admin"`)
			http.Error(w, `{"error":"unauthorized"}`, 401)
			return
		}

		token := strings.TrimPrefix(auth, "Bearer ")
		if token == key {
			next.ServeHTTP(w, r)
			return
		}

		http.Error(w, `{"error":"forbidden"}`, 403)
	})
}

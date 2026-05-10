package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"dp2codex/internal/config"
	"dp2codex/internal/logging"
	"dp2codex/internal/server"
)

var version = "dev"

func envInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func main() {
	listen := flag.String("listen", "", "监听地址（默认 :9090；或环境变量 DP2CODEX_LISTEN）")
	debug := flag.Bool("debug", false, "调试日志（DEBUG 级别）")
	showVer := flag.Bool("version", false, "打印版本后退出")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "dp2codex — Codex CLI 的 DeepSeek 本地代理（仅 HTTP API，无 Web 控制台）\n\n")
		fmt.Fprintf(os.Stderr, "用法:\n")
		fmt.Fprintf(os.Stderr, "  dp2codex [选项] <deepseek-api-key>\n")
		fmt.Fprintf(os.Stderr, "  export DEEPSEEK_API_KEY=<key> && dp2codex [选项]\n\n")
		fmt.Fprintf(os.Stderr, "必填: DeepSeek API Key（第一个参数，或环境变量 DEEPSEEK_API_KEY）。\n\n")
		fmt.Fprintf(os.Stderr, "选项:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\n环境与日志:\n")
		fmt.Fprintf(os.Stderr, "  DP2CODEX_LISTEN             监听地址，默认 :9090\n")
		fmt.Fprintf(os.Stderr, "  DEEPSEEK_BASE               上游 API Base URL\n")
		fmt.Fprintf(os.Stderr, "  DEFAULT_MODEL               默认模型名\n")
		fmt.Fprintf(os.Stderr, "  DP2CODEX_LOG_DIR            日志目录（默认平台数据目录下 logs/）\n")
		fmt.Fprintf(os.Stderr, "  DP2CODEX_LOG_MAX_MB         单日志文件上限(MB)，默认 10；超限轮转\n")
		fmt.Fprintf(os.Stderr, "  DP2CODEX_LOG_MAX_FILES      轮转备份个数，默认 5（含当前活跃文件外的历史）\n")
		fmt.Fprintf(os.Stderr, "  DP2CODEX_LOG_MAX_AGE_DAYS   保留天数，默认 7；超时删除整个过期文件\n")
	}
	flag.Parse()

	if *showVer {
		fmt.Println(version)
		os.Exit(0)
	}

	key := strings.TrimSpace(os.Getenv("DEEPSEEK_API_KEY"))
	if args := flag.Args(); len(args) >= 1 {
		key = strings.TrimSpace(args[0])
	}
	if key == "" {
		fmt.Fprintf(os.Stderr, "错误: 缺少 DeepSeek API Key（请传入第一个参数或设置 DEEPSEEK_API_KEY）。\n\n")
		flag.Usage()
		os.Exit(1)
	}

	addr := strings.TrimSpace(*listen)
	if addr == "" {
		addr = strings.TrimSpace(os.Getenv("DP2CODEX_LISTEN"))
	}
	if addr == "" {
		addr = ":9090"
	}

	mb := envInt("DP2CODEX_LOG_MAX_MB", 10)
	logCfg := logging.Config{
		Dir:      strings.TrimSpace(os.Getenv("DP2CODEX_LOG_DIR")),
		MaxSize:  int64(mb) * 1024 * 1024,
		MaxFiles: envInt("DP2CODEX_LOG_MAX_FILES", 5),
		MaxAge:   envInt("DP2CODEX_LOG_MAX_AGE_DAYS", 7),
		Debug:    *debug,
	}

	if err := logging.Setup(logCfg); err != nil {
		fmt.Fprintf(os.Stderr, "警告: 日志初始化失败: %v（仅 stderr）\n", err)
		logging.SetupSimple(*debug)
	}

	config.Global.SetAPIKey(key)

	printBanner(addr)

	httpSrv := server.NewHTTPServer(addr)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP server error", "error", err)
			cancel()
		}
	}()

	slog.Info("server started", "listen", addr)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		slog.Info("received signal, shutting down", "signal", sig)
	case <-ctx.Done():
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	_ = httpSrv.Shutdown(shutdownCtx)
	slog.Info("shutdown complete")
}

func printBanner(listenAddr string) {
	base := config.Global.GetString("deepseek_base")
	model := config.Global.GetString("default_model")
	logDir := logging.LogDir("")

	fmt.Fprintf(os.Stderr, `
  ┌─────────────────────────────────────┐
  │         dp2codex %-18s │
  │   DeepSeek → Codex（HTTP 代理）    │
  ├─────────────────────────────────────┤
  │  Listen:   %-24s │
  │  Upstream: %-24s │
  │  Model:    %-24s │
  │  Logs:     %-24s │
  │  OS/Arch:  %-10s / %-10s │
  └─────────────────────────────────────┘
`,
		truncate(version, 18),
		truncate(listenAddr, 24),
		truncate(base, 24),
		truncate(model, 24),
		truncate(logDir, 24),
		runtime.GOOS, runtime.GOARCH,
	)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"dp2codex/internal/cache"
	"dp2codex/internal/cert"
	"dp2codex/internal/config"
	"dp2codex/internal/server"
)

const (
	proxyPort  = "9090"
	tlsPort    = "8444"
	adminPort  = "8090"
	tunnelPort = "8443"
)

func main() {
	slog.Info("Proxy starting...")

	// 初始化配置
	_ = config.Global

	// 生成 TLS 证书
	cert.EnsureCerts()

	// 初始化缓存
	memCache := cache.NewMemCache()

	// 创建 HTTP 服务器
	httpSrv := server.NewHTTPServer(proxyPort)

	// 创建 HTTPS 服务器（共享路由）
	tlsSrv := server.NewTLSServer(tlsPort, httpSrv.Handler)

	// 创建管理面板服务器
	adminSrv := server.NewAdminServer(adminPort, memCache)

	// 启动服务器
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// HTTP
	go func() {
		slog.Info("HTTP server listening", "port", proxyPort)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP server error", "error", err)
			cancel()
		}
	}()

	// HTTPS
	if tlsSrv != nil {
		go func() {
			slog.Info("HTTPS server listening", "port", tlsPort)
			if err := server.ServeTLS(tlsSrv); err != nil && err != http.ErrServerClosed {
				slog.Error("HTTPS server error", "error", err)
				cancel()
			}
		}()
	}

	// 管理面板
	go func() {
		slog.Info("Admin server listening", "port", adminPort)
		if err := adminSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Admin server error", "error", err)
			cancel()
		}
	}()

	// CONNECT 隧道
	go func() {
		slog.Info("CONNECT tunnel listening", "port", tunnelPort)
		if err := server.RunConnectTunnel(tunnelPort, ":"+proxyPort); err != nil {
			slog.Error("Tunnel server error", "error", err)
			cancel()
		}
	}()

	slog.Info("All servers started",
		"http", ":"+proxyPort,
		"https", ":"+tlsPort,
		"admin", ":"+adminPort,
		"tunnel", ":"+tunnelPort,
	)

	// 等待信号
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-sigCh:
		slog.Info("Shutting down...")
	case <-ctx.Done():
	}

	// 优雅关闭
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	httpSrv.Shutdown(shutdownCtx)
	if tlsSrv != nil {
		tlsSrv.Shutdown(shutdownCtx)
	}
	adminSrv.Shutdown(shutdownCtx)

	slog.Info("Shutdown complete")
}

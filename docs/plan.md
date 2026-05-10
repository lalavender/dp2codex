# dp2codex 计划

## 项目目标

用 Go 语言完整重写 JinDX（Python FastAPI 项目），功能是将 OpenAI Responses API 和 Anthropic Messages API 转换为 DeepSeek Chat Completions API，使 Codex CLI 和 Claude Code 能够使用 DeepSeek 模型。

## 架构

```
main.go → 启动 4 个服务器（HTTP/HTTPS/Admin/Tunnel）
├── internal/config:   JSON 持久化配置
├── internal/cert:     TLS 证书自生成
├── internal/deepseek: DeepSeek API 客户端
├── internal/protocol: 协议转换（Responses ↔ Chat, Anthropic ↔ Chat）
├── internal/handler:  HTTP 请求处理器
├── internal/cache:    推理缓存（Redis + 内存）
├── internal/web:      URL 预取 & SSRF 防护
├── internal/stats:    统计 & 日志
└── internal/admin:    管理面板
```

## 实现状态

参见 docs/finish.md 和 docs/todo.md

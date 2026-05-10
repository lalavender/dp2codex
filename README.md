# dp2codex — Codex CLI 的 DeepSeek 代理

将 Codex CLI 的 OpenAI Responses API 转换为 DeepSeek Chat Completions API，使 Codex CLI 可以直接使用 DeepSeek 模型（如 deepseek-v4-pro、deepseek-v4-flash）。

## 项目说明

dp2codex 是一个本地 API 代理，运行四个服务：

| 端口 | 协议 | 用途 |
|------|------|------|
| 9090 | HTTP | Codex Responses API + DeepSeek Chat API |
| 8444 | HTTPS | TLS 劫持（无需配置环境变量） |
| 8090 | HTTP | 管理面板（配置、统计、日志） |
| 8443 | TCP | CONNECT 隧道代理 |

## 快速开始

### 编译

```bash
cd dp2codex
go build -o dp2codex .
```

### 启动

```bash
export DEEPSEEK_API_KEY=sk-your-key-here
./dp2codex
```

首次启动会自动生成 TLS 证书（`certs/` 目录），输出如下：

```
Proxy starting...
certificates generated successfully
HTTP server starting  port=9090
Admin server starting  port=8090
All servers started  http=:9090 https=:8444 admin=:8090 tunnel=:8443
```

## Codex CLI 配置方式

### 1. 配置 Codex 使用自定义提供商

编辑 `~/.codex/config.toml`：

```toml
model_provider = "custom"
model = "deepseek-v4-pro"

[model_providers.custom]
wire_api = "responses"
base_url = "http://localhost:9090/v1"
requires_openai_auth = true
```

### 2. 设置环境变量启动 Codex

启动 Codex CLI 前必须设置环境变量：

```bash
export OPENAI_API_KEY=sk-your-deepseek-key
export OPENAI_BASE_URL=http://localhost:9090/v1
export NO_PROXY="localhost,127.0.0.1,::1"
unset HTTPS_PROXY https_proxy HTTP_PROXY http_proxy ALL_PROXY all_proxy

# 启动 Codex CLI（交互模式）
codex

# 或者单次命令
codex exec --skip-git-repo-check "hi"
```

**注意事项：**

- `NO_PROXY` 必须设置，否则系统代理（如 Clash Verge）会劫持本地连接
- 所有 `HTTPS_PROXY` / `HTTP_PROXY` 环境变量必须取消设置
- `OPENAI_BASE_URL` 必须指向代理的 HTTP 地址（不是 HTTPS）

## 验证代理是否工作

### 1. 健康检查

```bash
curl http://localhost:9090/health
```

### 2. 模型列表

```bash
curl http://localhost:9090/v1/models
```

### 3. Chat Completions 测试

```bash
curl -X POST http://localhost:9090/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $DEEPSEEK_API_KEY" \
  -d '{
    "model": "deepseek-v4-pro",
    "messages": [{"role": "user", "content": "你好，请用一句话介绍自己"}],
    "stream": false
  }' | jq .
```

### 4. 流式 Chat Completions

```bash
curl -N -X POST http://localhost:9090/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $DEEPSEEK_API_KEY" \
  -d '{
    "model": "deepseek-v4-pro",
    "messages": [{"role": "user", "content": "从1数到5"}],
    "stream": true
  }'
```

### 5. Responses API 测试（Codex CLI 使用的协议）

```bash
curl -X POST http://localhost:9090/v1/responses \
  -H "Content-Type: application/json" \
  -d '{
    "input": [{"type": "message", "role": "user", "content": "2+3等于几？"}],
    "model": "gpt-5.5"
  }' | jq .
```

### 6. 流式 Responses API

```bash
curl -N -X POST http://localhost:9090/v1/responses \
  -H "Content-Type: application/json" \
  -d '{
    "input": [{"type": "message", "role": "user", "content": "2+3等于几？"}],
    "model": "gpt-5.5",
    "stream": true
  }'
```

### 7. 对话压缩测试

```bash
curl -X POST http://localhost:9090/v1/responses/compact \
  -H "Content-Type: application/json" \
  -d '{
    "input": [
      {"type": "message", "role": "user", "content": "你好"},
      {"type": "message", "role": "assistant", "content": "你好！有什么可以帮你的？"},
      {"type": "message", "role": "user", "content": "今天天气怎么样？"}
    ]
  }' | jq .
```

### 8. 管理面板

启动后访问 http://localhost:8090 ，可直接在网页中：

- 配置 DeepSeek API Key
- 调整推理力度
- 查看实时统计（请求数、缓存命中率、活跃流数）
- 查看日志
- 一键复制环境变量

## 配置说明

### 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `DEEPSEEK_API_KEY` | — | DeepSeek API 密钥 |
| `DEEPSEEK_BASE` | `https://api.deepseek.com` | DeepSeek API 地址 |
| `DEFAULT_MODEL` | `deepseek-v4-pro` | 默认模型名 |
| `REASONING_EFFORT` | `high` | 推理力度 (low/medium/high) |
| `MAX_POSITION_EMBEDDINGS` | `272000` | 上下文窗口大小 |
| `MAX_OUTPUT_TOKENS` | `65536` | 最大输出 token 数 |
| `ENABLE_REASONING_CACHE` | `true` | 启用推理缓存 |
| `REASONING_CACHE_TTL` | `300` | 缓存有效期（秒） |
| `WEB_FETCH_MAX_URLS` | `5` | 每轮预取最大 URL 数 |
| `WEB_FETCH_TIMEOUT` | `15` | 预取超时（秒） |

### 运行时配置

访问管理面板 http://localhost:8090 可实时修改以上配置。修改后即时生效，无需重启。

## 常见问题

### Codex 报 "stream disconnected before completion"

SSE 事件格式不匹配导致。确保使用最新版本的 dp2codex。

### Codex 连接不上代理

检查环境变量：
1. `OPENAI_BASE_URL` 是否指向 `http://localhost:9090/v1`
2. `NO_PROXY` 是否包含 `localhost,127.0.0.1,::1`
3. 所有 `HTTPS_PROXY` / `HTTP_PROXY` 是否已取消设置
4. 系统代理软件（如 Clash Verge）是否拦截了本地流量

### 请求返回 502 Bad Gateway

DeepSeek API 不可达，检查：
1. `DEEPSEEK_API_KEY` 是否有效
2. `DEEPSEEK_BASE` 是否正确
3. 网络是否能访问 `api.deepseek.com`

## 架构

```
Codex CLI → POST /v1/responses (OpenAI Responses 格式)
  → protocol.ResponsesToChat() 转换为 DeepSeek Chat 格式
  → deepseek.Client.ChatStream() 调用 DeepSeek API
  → SSE 流式响应转换回 Responses 格式
  → Codex CLI 接收响应

功能串联：
- 模型映射 (gpt-5.5 → deepseek-v4-pro)
- 推理缓存（跨轮次 thinking 连续性）
- URL 预取 + SSRF 防护
- 多 tool call 合并
- tool 消息排序修复
- 对话压缩
```

### SSE 事件格式

dp2codex 遵循 Codex CLI 的 SSE 事件格式要求。核心事件序列：

```
response.created       → 建立响应会话
response.in_progress   → 处理中
response.output_item.added → 添加输出项
response.content_part.added → 添加内容块
response.output_text.delta  → 流式文本（重复）
response.output_item.done   → 输出项完成
response.completed     → 响应完成
```

## HTTPS 劫持模式

### 端口转发（macOS）

```bash
sudo sysctl -w net.inet.ip.forwarding=1
echo "rdr pass on lo0 inet proto tcp from any to any port 443 -> 127.0.0.1 port 8444" | sudo pfctl -ef -
```

### hosts 配置

```
127.0.0.1 api.openai.com
```

### 验证

```bash
curl -k https://localhost:8444/v1/health
curl -k https://api.openai.com/v1/models
```

## 项目文件结构

```
├── main.go                     # 入口
├── internal/
│   ├── config/                 # 运行时配置
│   ├── cert/                   # TLS 证书生成
│   ├── protocol/               # 协议转换（核心）
│   ├── handler/                # HTTP 路由处理器
│   ├── deepseek/               # DeepSeek API 客户端
│   ├── cache/                  # 推理缓存（Redis + 内存）
│   ├── web/                    # URL 预取 & SSRF 防护
│   ├── stats/                  # 统计 & 日志
│   ├── server/                 # 多服务器管理
│   └── admin/                  # 管理面板
├── docs/
│   ├── plan.md                 # 项目计划
│   ├── todo.md                 # 待办
│   └── finish.md               # 已完成
└── README.md
```

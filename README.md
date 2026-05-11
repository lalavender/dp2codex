# dp2codex — Codex CLI 的 DeepSeek 代理

将 Codex CLI 的 OpenAI Responses API 转换为 DeepSeek Chat Completions API，使 Codex CLI 可以直接使用 DeepSeek 模型（如 deepseek-v4-pro、deepseek-v4-flash）。

## 项目说明

dp2codex 是一个**命令行启动的本地 HTTP API 代理**（无 Web 管理界面）：将 Codex CLI 使用的 OpenAI Responses 协议转换为 DeepSeek Chat Completions，监听单个地址（默认 `:9090`）。

## 快速开始

### 编译

```bash
cd dp2codex
go build -o dp2codex .
```

### 配置 API Key

在二进制同目录下创建 `.env` 文件：

```bash
echo "DEEPSEEK_API_KEY=sk-your-key-here" > .env
```

或设置环境变量：

```bash
export DEEPSEEK_API_KEY=sk-your-key-here
```

### 启动

```bash
./dp2codex
```

日志默认写入平台数据目录下的 `logs/dp2codex.log`（如 macOS/Linux：`~/.dp2codex/logs/`），支持按大小轮转与按天数的保留清理。

### 自定义监听地址

```bash
export DP2CODEX_LISTEN=127.0.0.1:9090
./dp2codex
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

```bash
export OPENAI_API_KEY=sk-your-deepseek-key
export OPENAI_BASE_URL=http://localhost:9090/v1
export NO_PROXY="localhost,127.0.0.1,::1"
unset HTTPS_PROXY https_proxy HTTP_PROXY http_proxy ALL_PROXY all_proxy

codex
```

**注意事项：**

- `NO_PROXY` 必须设置，否则系统代理（如 Clash Verge）会劫持本地连接
- 所有 `HTTPS_PROXY` / `HTTP_PROXY` 环境变量必须取消设置
- `OPENAI_BASE_URL` 必须指向代理的 HTTP 地址（不是 HTTPS）

## 验证代理是否工作

### 健康检查

```bash
curl http://localhost:9090/health
```

### 模型列表

```bash
curl http://localhost:9090/v1/models
```

### Chat Completions 测试

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

### 流式 Chat Completions

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

### Responses API 测试（Codex CLI 使用的协议）

```bash
curl -X POST http://localhost:9090/v1/responses \
  -H "Content-Type: application/json" \
  -d '{
    "input": [{"type": "message", "role": "user", "content": "2+3等于几？"}],
    "model": "gpt-5.5"
  }' | jq .
```

### 流式 Responses API

```bash
curl -N -X POST http://localhost:9090/v1/responses \
  -H "Content-Type: application/json" \
  -d '{
    "input": [{"type": "message", "role": "user", "content": "2+3等于几？"}],
    "model": "gpt-5.5",
    "stream": true
  }'
```

### 对话压缩测试

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

## 配置说明

### API Key 读取优先级

1. 环境变量 `DEEPSEEK_API_KEY`
2. 二进制同目录下的 `.env` 文件

### 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `DEEPSEEK_API_KEY` | — | DeepSeek API 密钥（必填） |
| `DP2CODEX_LISTEN` | `:9090` | HTTP 监听地址 |
| `DEEPSEEK_BASE` | `https://api.deepseek.com` | DeepSeek API 地址 |
| `DEFAULT_MODEL` | `deepseek-v4-pro` | 默认模型名 |
| `DP2CODEX_LOG_DIR` | 平台默认数据目录下 `logs` | 日志目录 |
| `DP2CODEX_LOG_MAX_MB` | `10` | 单文件上限(MB)，超出后轮转 |
| `DP2CODEX_LOG_MAX_FILES` | `5` | 轮转保留的历史文件个数 |
| `DP2CODEX_LOG_MAX_AGE_DAYS` | `7` | 超过该天数的日志文件会被删除 |
| `REASONING_EFFORT` | `high` | 推理力度 (low/medium/high) |
| `MAX_POSITION_EMBEDDINGS` | `272000` | 上下文窗口大小 |
| `MAX_OUTPUT_TOKENS` | `65536` | 最大输出 token 数 |
| `ENABLE_REASONING_CACHE` | `true` | 启用推理缓存 |
| `REASONING_CACHE_TTL` | `300` | 缓存有效期（秒） |

可选配置文件 `~/.dp2codex/config.json`（模型映射等）。

### 模型映射

| Codex 请求模型 | 实际调用模型 |
|---------------|-------------|
| `gpt-5.5` | `deepseek-v4-pro` |
| `gpt-5` | `deepseek-v4-pro` |

可通过 `~/.dp2codex/config.json` 中的 `model_mapping` 自定义映射。

## 常见问题

### Codex 报 "stream disconnected before completion"

SSE 事件格式不匹配导致。确保使用最新版本的 dp2codex。

### Codex 连接不上代理

检查：
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
```

### SSE 事件格式

dp2codex 遵循 Codex CLI 的 SSE 事件格式要求。核心事件序列：

```
response.created           → 建立响应会话
response.in_progress       → 处理中
response.output_item.added → 添加输出项
response.content_part.added → 添加内容块
response.output_text.delta → 流式文本（重复）
response.output_item.done  → 输出项完成
response.completed         → 响应完成
```

## 项目文件结构

```
├── main.go                     # 入口
├── go.mod / go.sum             # Go 模块定义
├── .env.example                # 环境变量模板
│
├── internal/
│   ├── config/config.go        # JSON 持久化配置
│   ├── protocol/
│   │   ├── types.go            # 数据结构定义
│   │   ├── modelmap.go         # 模型名称映射
│   │   └── responses.go        # 协议转换（Responses ↔ Chat）
│   ├── handler/
│   │   ├── responses.go        # Responses API 处理（含推理缓存）
│   │   ├── chat.go             # Chat Completions API 处理
│   │   ├── codex.go            # Codex 专用 API（模型列表等）
│   │   └── common.go           # 公共辅助函数
│   ├── deepseek/client.go      # DeepSeek API 客户端（流式/非流式）
│   ├── cache/
│   │   ├── cache.go            # 缓存接口
│   │   ├── memory.go           # 内存缓存实现（带 TTL + 轮转清理）
│   │   └── reasoning.go        # 推理缓存封装
│   ├── server/http.go          # HTTP 服务器 & 路由注册
│   ├── logging/logging.go      # 日志初始化 & 轮转
│   └── stats/stats.go          # 统计计数 & 日志脱敏
│
└── docs/
    ├── plan.md                 # 实施计划
    ├── todo.md                 # 待完成
    └── finish.md               # 已完成
```

## 致谢

本项目参考了 [JinDX](https://github.com/daxian10086/JinDX) 项目的设计思路与架构，感谢原作者的开源贡献。

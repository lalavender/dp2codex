# dp2codex 计划

## 当前目标

保持 Codex CLI 的 OpenAI Responses API 到 DeepSeek Chat Completions API 的转换链路可用，并重点补齐以下交付：

1. 将 `Responses` 推理缓存改为 `Redis 优先、内存回退`
2. 修复 Redis 首次连接失败后无法恢复的问题，提升缓存命中率
3. 生成可直接构建运行的 `Dockerfile`

## 本次实施步骤

1. 审查 `internal/handler/responses.go`、`internal/protocol/responses.go`、`internal/cache/*`
2. 将会话级 reasoning 缓存接入 Redis，并保留本地内存回退
3. 补齐容器构建文件，保留 `docker-compose.yml` 中的 Redis 编排
4. 执行构建和诊断检查，确认改动可用

## 实现状态

参见 `docs/finish.md` 和 `docs/todo.md`

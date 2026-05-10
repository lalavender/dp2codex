# 多平台构建示例（需在构建参数中传入密钥运行时不写入镜像）:
#   docker build -t dp2codex .
#   docker run --rm -e DEEPSEEK_API_KEY=sk-... -p 9090:9090 dp2codex
FROM golang:alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /dp2codex .

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /dp2codex /usr/local/bin/dp2codex
ENV DP2CODEX_LISTEN=0.0.0.0:9090
EXPOSE 9090
ENTRYPOINT ["/usr/local/bin/dp2codex"]

# syntax=docker/dockerfile:1
FROM golang:alpine AS builder

# 设置构建参数，获取构建时间（精确到分钟）
ARG BUILD_DATE
ARG TARGETPLATFORM
ARG TARGETARCH
ARG TARGETOS
ENV VERSION=${BUILD_DATE}

WORKDIR /app

# 拷贝 go.mod/go.sum 或自动生成
COPY . .
RUN if [ ! -f go.mod ]; then go mod init conflux; fi \
    && go mod tidy

# 构建二进制文件，支持多架构
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags "-s -w -X main.Version=$VERSION" -o conflux

# ----------- 运行阶段 -----------
FROM alpine:latest

# 安装 CA 证书
RUN apk add --no-cache ca-certificates

WORKDIR /data/conflux

# 拷贝二进制文件
COPY --from=builder /app/conflux /conflux

# 暴露端口
EXPOSE 80

# 启动应用
ENTRYPOINT ["/conflux"] 
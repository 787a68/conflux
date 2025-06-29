# syntax=docker/dockerfile:1
FROM golang:alpine AS builder

# 设置构建参数，获取构建时间（精确到分钟）
ARG BUILD_DATE
ENV VERSION=${BUILD_DATE}

WORKDIR /app

# 拷贝 go.mod/go.sum 或自动生成
COPY . .
RUN if [ ! -f go.mod ]; then go mod init conflux; fi \
    && go mod tidy

# 构建二进制文件，静态编译，极致精简
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-s -w -X main.Version=$VERSION" -o conflux

# ----------- 运行阶段 -----------
FROM scratch

WORKDIR /data/conflux

# 拷贝二进制文件
COPY --from=builder /app/conflux /conflux

# 创建日志和数据目录
RUN mkdir -p /data/conflux/log

# 启动应用
ENTRYPOINT ["/conflux"] 
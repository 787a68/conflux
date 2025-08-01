# syntax=docker/dockerfile:1.5
FROM --platform=$BUILDPLATFORM golang:alpine AS builder

# 构建参数
ARG BUILD_DATE
ARG TARGETPLATFORM
ARG TARGETARCH
ARG TARGETOS
ENV VERSION=${BUILD_DATE}

WORKDIR /app

# 拷贝源码
COPY . .

# 如果没有 go.mod 则自动初始化，并 tidy 一次
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    if [ ! -f go.mod ]; then go mod init conflux; fi && \
    go mod tidy

# 编译程序
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    echo "编译架构: $TARGETARCH, 系统: $TARGETOS, 版本: $VERSION" && \
    CGO_ENABLED=0 \
    GOOS=$TARGETOS \
    GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.Version=$VERSION" -o /conflux .

# ----------- 运行阶段 -----------

FROM --platform=$TARGETPLATFORM alpine:latest

RUN apk add --no-cache ca-certificates binutils
WORKDIR /data/conflux

# 拷贝编译好的文件并瘦身
COPY --from=builder /conflux /conflux
RUN strip --strip-unneeded /conflux

EXPOSE 80
ENTRYPOINT ["/conflux"]

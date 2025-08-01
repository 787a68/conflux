# syntax=docker/dockerfile:1.5
FROM --platform=$BUILDPLATFORM golang:alpine AS builder

ARG BUILD_DATE
ARG TARGETPLATFORM
ARG TARGETARCH
ARG TARGETOS
ENV VERSION=${BUILD_DATE}

WORKDIR /app
COPY . .

# 如果没有 go.mod 则自动初始化，并 tidy 一次
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    if [ ! -f go.mod ]; then go mod init conflux; fi && \
    go mod tidy

# 编译程序并瘦身：加入 strip 逻辑（构建阶段完成）
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    echo "编译架构: $TARGETARCH, 系统: $TARGETOS, 版本: $VERSION" && \
    CGO_ENABLED=0 \
    GOOS=$TARGETOS \
    GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w -X main.Version=$VERSION" -o /conflux . && \
    strip --strip-unneeded /conflux

# ----------- 运行阶段 -----------

FROM --platform=$TARGETPLATFORM alpine:latest

# 仅保留必要运行依赖（移除 binutils，减少体积）
RUN apk add --no-cache ca-certificates

WORKDIR /data/conflux
COPY --from=builder /conflux /conflux

EXPOSE 80
ENTRYPOINT ["/conflux"]

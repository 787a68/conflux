# syntax=docker/dockerfile:1.5
FROM --platform=$BUILDPLATFORM golang:alpine AS builder

ARG BUILD_DATE
ARG TARGETPLATFORM
ARG TARGETARCH
ARG TARGETOS
ENV VERSION=${BUILD_DATE}

# ✅ 安装 strip（构建阶段才需要）
RUN apk add --no-cache binutils

WORKDIR /app
COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    if [ ! -f go.mod ]; then go mod init conflux; fi && \
    go mod tidy

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

RUN apk add --no-cache ca-certificates

WORKDIR /data/conflux
COPY --from=builder /conflux /conflux

EXPOSE 80
ENTRYPOINT ["/conflux"]

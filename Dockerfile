# syntax=docker/dockerfile:1.4
FROM --platform=$BUILDPLATFORM golang:alpine AS builder

ARG TARGETPLATFORM
ARG BUILD_DATE
ENV VERSION=${BUILD_DATE}

WORKDIR /app

COPY . .
RUN if [ ! -f go.mod ]; then go mod init conflux; fi \
    && go mod tidy

# 自动匹配目标架构构建
RUN GOARCH=$(echo $TARGETPLATFORM | cut -d '/' -f2) \
    && CGO_ENABLED=0 GOOS=linux GOARCH=$GOARCH \
    go build -ldflags "-s -w -X main.Version=$VERSION" -o conflux

# ----------- 运行阶段 -----------
FROM alpine:latest

RUN apk add --no-cache ca-certificates

WORKDIR /data/conflux

COPY --from=builder /app/conflux /conflux

EXPOSE 80

ENTRYPOINT ["/conflux"]

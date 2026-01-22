# Build stage
FROM golang:1.24.10-alpine AS builder

# 安装必要的构建工具
RUN apk add --no-cache make git

WORKDIR /src

# 复制 go.mod 和 go.sum 先下载依赖（利用 Docker 缓存）
COPY go.mod go.sum ./
RUN go mod download

# 复制所有源代码
COPY . .

# 构建应用
RUN GOPROXY=https://goproxy.cn make build

# Runtime stage
FROM alpine:3.19

# 安装运行时依赖
RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    && cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime \
    && echo "Asia/Shanghai" > /etc/timezone

# 创建非 root 用户
RUN addgroup -g 1000 appuser && \
    adduser -D -u 1000 -G appuser appuser

WORKDIR /app

# 从 builder 阶段复制编译好的二进制文件
COPY --from=builder /src/bin/product ./product

# 复制默认配置文件（作为模板）
COPY --from=builder /src/configs/config.docker.yaml ./config.default.yaml

# 创建配置文件目录
RUN mkdir -p /app/configs && chown -R appuser:appuser /app

# 切换到非 root 用户
USER appuser

# 暴露端口（HTTP 和 gRPC）
EXPOSE 8002 9002

# 启动应用
# -conf 参数指定配置文件路径
CMD ["./product", "-conf", "/app/configs/config.yaml"]

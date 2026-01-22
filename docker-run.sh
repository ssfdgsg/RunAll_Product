#!/bin/bash

# Product Service Docker 运行脚本
# 连接到已存在的 Redis (容器名: product) 和 RabbitMQ (容器名: resource)

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# 配置变量
IMAGE_NAME="product-service"
IMAGE_TAG="latest"
CONTAINER_NAME="product-service"
NETWORK_NAME="runall-network"

# 依赖服务容器名
REDIS_CONTAINER="product"
RABBITMQ_CONTAINER="resource"

echo -e "${GREEN}=== Product Service Docker 部署 ===${NC}"

# 1. 检查依赖容器是否运行
echo -e "${YELLOW}[1/6] 检查依赖服务...${NC}"
if ! docker ps | grep -q ${REDIS_CONTAINER}; then
    echo -e "${RED}✗ Redis 容器 '${REDIS_CONTAINER}' 未运行！${NC}"
    exit 1
fi
if ! docker ps | grep -q ${RABBITMQ_CONTAINER}; then
    echo -e "${RED}✗ RabbitMQ 容器 '${RABBITMQ_CONTAINER}' 未运行！${NC}"
    exit 1
fi
echo -e "${GREEN}✓ 依赖服务运行正常${NC}"

# 2. 创建自定义网络（如果不存在）
echo -e "${YELLOW}[2/6] 配置 Docker 网络...${NC}"
if ! docker network inspect ${NETWORK_NAME} >/dev/null 2>&1; then
    docker network create ${NETWORK_NAME}
    echo -e "${GREEN}✓ 创建网络: ${NETWORK_NAME}${NC}"
else
    echo -e "${GREEN}✓ 网络已存在: ${NETWORK_NAME}${NC}"
fi

# 3. 将依赖容器加入网络
echo -e "${YELLOW}[3/6] 连接依赖服务到网络...${NC}"
docker network connect ${NETWORK_NAME} ${REDIS_CONTAINER} 2>/dev/null || echo "  Redis 已在网络中"
docker network connect ${NETWORK_NAME} ${RABBITMQ_CONTAINER} 2>/dev/null || echo "  RabbitMQ 已在网络中"
echo -e "${GREEN}✓ 依赖服务已连接到网络${NC}"

# 4. 构建镜像
echo -e "${YELLOW}[4/6] 构建 Docker 镜像...${NC}"
docker build -t ${IMAGE_NAME}:${IMAGE_TAG} .
if [ $? -ne 0 ]; then
    echo -e "${RED}✗ 镜像构建失败！${NC}"
    exit 1
fi
echo -e "${GREEN}✓ 镜像构建成功${NC}"

# 5. 停止并删除旧容器
echo -e "${YELLOW}[5/6] 清理旧容器...${NC}"
if [ "$(docker ps -aq -f name=${CONTAINER_NAME})" ]; then
    docker stop ${CONTAINER_NAME} 2>/dev/null || true
    docker rm ${CONTAINER_NAME} 2>/dev/null || true
    echo -e "${GREEN}✓ 旧容器已清理${NC}"
fi

# 6. 生成配置文件
echo -e "${YELLOW}[6/6] 生成配置文件...${NC}"
mkdir -p ./runtime-configs

# 提示用户输入数据库配置
echo -e "${YELLOW}请输入数据库配置（直接回车使用默认值）：${NC}"
read -p "数据库主机 [localhost]: " DB_HOST
DB_HOST=${DB_HOST:-localhost}
read -p "数据库端口 [5433]: " DB_PORT
DB_PORT=${DB_PORT:-5433}
read -p "数据库名称 [product]: " DB_NAME
DB_NAME=${DB_NAME:-product}
read -p "数据库用户 [postgres]: " DB_USER
DB_USER=${DB_USER:-postgres}
read -p "数据库密码 [123456]: " DB_PASSWORD
DB_PASSWORD=${DB_PASSWORD:-123456}

cat > ./runtime-configs/config.yaml <<EOF
server:
  http:
    addr: 0.0.0.0:8002
    timeout: 1s
  grpc:
    addr: 0.0.0.0:9002
    timeout: 1s
data:
  database:
    driver: postgresql
    source: postgresql://${DB_USER}:${DB_PASSWORD}@host.docker.internal:${DB_PORT}/${DB_NAME}
  redis:
    addr: ${REDIS_CONTAINER}:6379
    password: ""
    db: 0
    read_timeout: 0.2s
    write_timeout: 0.2s
  rabbitmq:
    url: amqp://guest:guest@${RABBITMQ_CONTAINER}:5672/
    queue: resource.instance.created
    exchange: resource.events
EOF

echo -e "${GREEN}✓ 配置文件已生成${NC}"

# 7. 启动容器
echo -e "${YELLOW}启动容器...${NC}"
docker run -d \
    --name ${CONTAINER_NAME} \
    --network ${NETWORK_NAME} \
    --add-host=host.docker.internal:host-gateway \
    -p 8002:8002 \
    -p 9002:9002 \
    -v $(pwd)/runtime-configs:/app/configs:ro \
    -e TZ=Asia/Shanghai \
    --restart unless-stopped \
    ${IMAGE_NAME}:${IMAGE_TAG}

if [ $? -ne 0 ]; then
    echo -e "${RED}✗ 容器启动失败！${NC}"
    exit 1
fi

echo -e "${GREEN}✓ 容器启动成功${NC}"
echo ""
echo -e "${GREEN}=== 部署完成 ===${NC}"
echo -e "容器名称: ${CONTAINER_NAME}"
echo -e "HTTP 端口: ${GREEN}http://localhost:8002${NC}"
echo -e "gRPC 端口: ${GREEN}localhost:9002${NC}"
echo ""
echo -e "网络拓扑:"
echo -e "  ${CONTAINER_NAME} ──┬──> ${REDIS_CONTAINER} (Redis)"
echo -e "                      ├──> ${RABBITMQ_CONTAINER} (RabbitMQ)"
echo -e "                      └──> host.docker.internal (PostgreSQL)"
echo ""
echo -e "${YELLOW}常用命令：${NC}"
echo -e "  查看日志: ${GREEN}docker logs -f ${CONTAINER_NAME}${NC}"
echo -e "  停止服务: ${GREEN}docker stop ${CONTAINER_NAME}${NC}"
echo -e "  重启服务: ${GREEN}docker restart ${CONTAINER_NAME}${NC}"
echo -e "  查看网络: ${GREEN}docker network inspect ${NETWORK_NAME}${NC}"

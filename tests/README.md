# API 测试文件说明

本目录包含用于测试 Product Service 各个接口的 HTTP 文件。

## 文件列表

- `product.http` - 商品查询接口测试（HTTP）
- `order.http` - 订单创建接口测试（购买商品）
- `order_resource.http` - 订单资源查询接口测试（gRPC + HTTP）
- `seckill.http` - 秒杀相关测试（gRPC + Redis 操作）

## 使用方法

### 1. 使用 VS Code REST Client 插件

安装插件：
```
Name: REST Client
Id: humao.rest-client-vscode
```

使用方式：
1. 打开 `.http` 文件
2. 点击请求上方的 `Send Request` 链接
3. 查看右侧响应结果

### 2. 使用 IntelliJ IDEA / WebStorm

IDEA 系列 IDE 原生支持 `.http` 文件：
1. 打开 `.http` 文件
2. 点击请求左侧的绿色运行按钮
3. 查看底部响应结果

### 3. 使用 curl 命令

从 `.http` 文件中复制请求，转换为 curl 命令：

```bash
# 示例：获取商品列表
curl -X GET "http://localhost:8000/v1/products" \
  -H "Content-Type: application/json"

# 示例：带参数的请求
curl -X GET "http://localhost:8000/v1/products?min_price=1000&max_price=10000&sort_by=SORT_BY_PRICE&sort_order=ASC" \
  -H "Content-Type: application/json"
```

### 4. 使用 grpcurl 测试 gRPC 接口

安装 grpcurl：
```bash
# macOS
brew install grpcurl

# Windows (使用 Scoop)
scoop install grpcurl

# 或从 GitHub 下载：https://github.com/fullstorydev/grpcurl/releases
```

使用示例：
```bash
# 列出所有服务
grpcurl -plaintext localhost:9000 list

# 调用秒杀初始化接口
grpcurl -plaintext -d '{"product_id": 1001, "stock": 100}' \
  localhost:9000 api.product.v1.SeckillService/InitSeckill

# 获取当前秒杀信息
grpcurl -plaintext localhost:9000 \
  api.product.v1.SeckillService/GetCurrentSeckill
```

## 测试前准备

### 1. 启动服务

```bash
# 构建项目
make build

# 运行服务
./bin/product -conf ./configs/config.yaml
```

### 2. 准备测试数据

#### 初始化数据库

```sql
-- 连接数据库
psql -h localhost -p 5433 -U postgres -d product

-- 插入商品规格
INSERT INTO product_specs (id, cpu, memory, gpu, image, config_json, created_at)
VALUES 
  (1, 2, 4096, 0, 'ubuntu:22.04', '{"disk_type":"SSD","disk_size":100}', NOW()),
  (2, 4, 8192, 0, 'ubuntu:22.04', '{"disk_type":"SSD","disk_size":200}', NOW()),
  (3, 8, 16384, 1, 'ubuntu:22.04', '{"disk_type":"SSD","disk_size":500}', NOW());

-- 插入商品
INSERT INTO products (id, name, description, status, price, spec_id, created_at, updated_at)
VALUES 
  (1001, '基础型实例', '适合轻量级应用', 1, 9900, 1, NOW(), NOW()),
  (1002, '标准型实例', '适合中等负载应用', 1, 19900, 2, NOW(), NOW()),
  (1003, 'GPU计算型', '适合AI训练和推理', 1, 99900, 3, NOW(), NOW());
```

#### 初始化 Redis 秒杀数据

```bash
# 连接 Redis
redis-cli -h 172.27.59.28 -p 6379

# 清空旧数据
DEL seckill:stock seckill:req_seq seckill:uid2req seckill:stream_orders seckill:product_id

# 设置秒杀商品
SET seckill:product_id 1001
SET seckill:stock 100
SET seckill:req_seq 0
```

## 测试流程

### 商品查询测试

1. 打开 `product.http`
2. 依次执行各个查询请求
3. 验证返回的商品列表、分页、排序等功能

### 秒杀流程测试

1. 使用 grpcurl 初始化秒杀：
   ```bash
   grpcurl -plaintext -d '{"product_id": 1001, "stock": 100}' \
     localhost:9000 api.product.v1.SeckillService/InitSeckill
   ```
2. 验证 Redis 数据：
   ```bash
   redis-cli -h 172.27.59.28 -p 6379
   GET seckill:product_id
   GET seckill:stock
   ```
3. 使用 Lua 脚本模拟 BFF 扣库存
4. 查看 Redis Stream 中的订单消息
5. 启动商品域服务消费 Stream
6. 查询数据库验证订单创建

### 订单查询测试

1. 确保已有订单数据（通过秒杀或正常购买创建）
2. 使用 `order_resource.http` 查询订单和资源信息
3. 验证订单状态、金额、关联的资源ID等信息

### 资源查询测试

1. 确保订单已支付并创建资源
2. 使用 `order_resource.http` 查询订单关联的资源信息
3. 验证资源状态（由资源域管理）

## 环境变量配置

可以在 `.http` 文件顶部修改环境变量：

```http
### 基础配置
@baseUrl = http://localhost:8000
@contentType = application/json
```

如需测试不同环境，修改 `@baseUrl` 即可：
- 本地开发：`http://localhost:8000`
- 测试环境：`http://test.example.com`
- 生产环境：`http://api.example.com`

## 注意事项

1. 订单服务（OrderService）已实现，提供订单和资源查询功能
2. 秒杀功能主要通过 Redis Stream 消费，不直接暴露 HTTP 接口
3. 资源管理由资源域负责，商品域只负责生成 ResourceID 和发送 MQ 消息
4. 测试前请确保数据库、Redis、RabbitMQ 服务已启动
5. 数据库端口为 5433（非默认 5432），Redis 地址为 172.27.59.28:6379
6. 商品域不再使用 "instance" 术语，统一使用 "resource" 表示资源

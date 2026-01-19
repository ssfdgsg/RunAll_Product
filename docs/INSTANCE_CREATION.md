# 实例创建通用流程

## 概述

将秒杀和普通订单的实例创建逻辑统一为通用的 `CreateInstance` 方法，实现代码复用。

## 架构设计

```
┌─────────────────────────────────────────────────────────────┐
│                    实例创建流程                              │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  触发源                                                      │
│  ├─ 秒杀：Redis Stream 消费                                 │
│  └─ 订单：订单支付成功回调                                   │
│                                                             │
│  ↓                                                          │
│                                                             │
│  InstanceUsecase.CreateInstance()                          │
│  ├─ 1. 生成实例 ID                                          │
│  ├─ 2. 查询商品信息                                         │
│  ├─ 3. 发送 MQ 消息到 Resource Domain                       │
│  └─ 4. 写入数据库日志                                       │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

## 核心方法

### CreateInstance

```go
func (uc *InstanceUsecase) CreateInstance(
    ctx context.Context,
    productID int64,
    userID int64,
    source string,    // "SECKILL" 或 "ORDER"
    sourceID string,  // StreamID 或 OrderID
) error
```

**参数说明**：
- `productID`: 商品 ID
- `userID`: 用户 ID（int64）
- `source`: 来源标识
  - `"SECKILL"`: 秒杀
  - `"ORDER"`: 普通订单
- `sourceID`: 来源 ID
  - 秒杀：Redis Stream 消息 ID（如 `"1234567890-0"`）
  - 订单：订单 ID（如 `"ORD123456"`）

## 使用场景

### 1. 秒杀场景

```go
// internal/service/seckill.go
func (s *SeckillOrderService) HandleSeckillOrder(ctx, streamID, uid) error {
    userID, _ := strconv.ParseInt(uid, 10, 64)
    
    // 调用通用方法
    return s.uc.CreateInstance(ctx, s.productID, userID, "SECKILL", streamID)
}
```

### 2. 普通订单场景

```go
// internal/service/order.go
func (s *OrderService) HandleOrderPaid(ctx, orderID, productID, userID) error {
    // 调用通用方法
    return s.uc.CreateInstance(ctx, productID, userID, "ORDER", orderID)
}
```

## 数据库表结构

### instance_logs 表

```sql
CREATE TABLE instance_logs (
    id BIGSERIAL PRIMARY KEY,
    product_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    instance_id BIGINT NOT NULL,
    source VARCHAR(20) NOT NULL,        -- SECKILL / ORDER
    source_id VARCHAR(64) NOT NULL,     -- StreamID / OrderID
    status VARCHAR(20) NOT NULL DEFAULT 'PROCESSING',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);
```

**字段说明**：
- `source`: 区分来源（秒杀 / 订单）
- `source_id`: 追溯原始请求（StreamID / OrderID）

**查询示例**：
```sql
-- 查询秒杀创建的实例
SELECT * FROM instance_logs WHERE source = 'SECKILL';

-- 查询订单创建的实例
SELECT * FROM instance_logs WHERE source = 'ORDER';

-- 通过 StreamID 追溯
SELECT * FROM instance_logs WHERE source = 'SECKILL' AND source_id = '1234567890-0';

-- 通过 OrderID 追溯
SELECT * FROM instance_logs WHERE source = 'ORDER' AND source_id = 'ORD123456';
```

## MQ 消息格式

### Protobuf 定义

```protobuf
message Event {
  string event_type = 1;              // "INSTANCE_CREATED"
  int64 instance_id = 2;              // 实例 ID
  string user_id = 3;                 // 用户 ID（字符串格式）
  string name = 4;                    // 实例名称
  InstanceSpec spec = 5;              // 实例规格
  google.protobuf.Timestamp timestamp = 6;  // 时间戳
}

message InstanceSpec {
  int32 cpus = 1;
  int32 memory_mb = 2;
  int32 gpu = 3;
  string image = 4;
}
```

### 发送方式

**直接投递到 Queue**（不使用 Exchange）：

```go
ch.PublishWithContext(ctx,
    "",           // exchange 留空
    queue,        // routing key == queue 名
    false, false,
    amqp.Publishing{
        ContentType: "application/octet-stream",
        Body:        body,
    },
)
```

## 配置

### configs/config.yaml

```yaml
data:
  rabbitmq:
    url: amqp://guest:guest@localhost:5672/
    queue: resource.instance.created
```

### internal/conf/conf.proto

```protobuf
message RabbitMQ {
  string url = 1;
  string queue = 2;
}
```

## 依赖注入

### internal/biz/biz.go

```go
var ProviderSet = wire.NewSet(
    NewGreeterUsecase,
    NewInstanceUsecase,  // 通用实例用例
)
```

### internal/data/data.go

```go
var ProviderSet = wire.NewSet(
    NewData,
    NewGreeterRepo,
    NewInstanceRepo,           // 通用实例仓储
    NewMQPublisher,            // MQ 发布器
    NewInstanceIDGenerator,    // ID 生成器
)
```

## 扩展示例

### 添加新的触发源

假设需要支持"活动赠送"场景：

```go
// internal/service/activity.go
func (s *ActivityService) HandleGiftInstance(ctx, activityID, productID, userID) error {
    // 调用通用方法
    return s.uc.CreateInstance(ctx, productID, userID, "ACTIVITY", activityID)
}
```

数据库中会记录：
```
source: "ACTIVITY"
source_id: "ACT123456"
```

## 监控指标

### 按来源统计

```sql
-- 统计各来源的实例创建数量
SELECT source, COUNT(*) as count
FROM instance_logs
GROUP BY source;

-- 统计各来源的成功率
SELECT 
    source,
    COUNT(*) as total,
    SUM(CASE WHEN status = 'SUCCESS' THEN 1 ELSE 0 END) as success,
    ROUND(100.0 * SUM(CASE WHEN status = 'SUCCESS' THEN 1 ELSE 0 END) / COUNT(*), 2) as success_rate
FROM instance_logs
GROUP BY source;
```

### 按时间统计

```sql
-- 最近 1 小时的实例创建趋势
SELECT 
    date_trunc('minute', created_at) as minute,
    source,
    COUNT(*) as count
FROM instance_logs
WHERE created_at >= NOW() - INTERVAL '1 hour'
GROUP BY minute, source
ORDER BY minute DESC;
```

## 测试

### 单元测试

```go
func TestCreateInstance(t *testing.T) {
    tests := []struct {
        name     string
        source   string
        sourceID string
    }{
        {"秒杀场景", "SECKILL", "1234567890-0"},
        {"订单场景", "ORDER", "ORD123456"},
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := uc.CreateInstance(ctx, 1001, 123, tt.source, tt.sourceID)
            assert.NoError(t, err)
        })
    }
}
```

### 集成测试

```bash
# 1. 启动服务
go run cmd/product/main.go -conf configs/config.yaml

# 2. 秒杀场景：推送 Stream 消息
redis-cli XADD stream:seckill:1001 * uid 123 ts 1234567890

# 3. 订单场景：调用订单支付回调
curl -X POST http://localhost:8000/v1/orders/123/paid

# 4. 查看数据库
psql -U postgres -d product_db -c "SELECT * FROM instance_logs ORDER BY created_at DESC LIMIT 10;"

# 5. 查看 RabbitMQ
rabbitmqadmin list queues
```

## 优势

1. **代码复用**：秒杀和订单共享同一套实例创建逻辑
2. **易于扩展**：新增触发源只需传入不同的 `source` 和 `sourceID`
3. **统一监控**：所有实例创建记录在同一张表，便于统计分析
4. **可追溯性**：通过 `source_id` 可以追溯到原始请求

## 注意事项

1. **UserID 格式转换**：Product Domain 使用 int64，Resource Domain 使用 string（UUID），需要在 `formatUserID()` 中实现转换逻辑
2. **Instance ID 生成**：需要实现 `InstanceIDGenerator` 接口，建议使用雪花算法
3. **幂等性保证**：建议在 `instance_logs` 表添加唯一索引 `(source, source_id)` 防止重复创建
4. **事务处理**：如果需要保证 MQ 发送和数据库写入的一致性，考虑使用本地消息表或 Saga 模式

# 秒杀 MQ 集成流程

## 流程图

```
┌─────────────────────────────────────────────────────────────────────┐
│                         秒杀完整流程                                 │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  1. BFF 层执行 Lua 脚本                                              │
│     ├─ 扣减库存                                                      │
│     ├─ 防重检查                                                      │
│     └─ 推送消息到 Redis Stream                                       │
│                                                                     │
│  2. Product Service 消费 Stream                                     │
│     ├─ 生成实例 ID (InstanceIDGenerator)                            │
│     ├─ 查询商品信息 (GetProductByID)                                │
│     ├─ 发送 MQ 消息到 Resource Domain                               │
│     │  └─ EventType: INSTANCE_CREATED                              │
│     │  └─ Payload: {instanceID, userID, spec...}                  │
│     └─ 写入数据库日志 (seckill_logs)                                │
│                                                                     │
│  3. Resource Domain 消费 MQ                                         │
│     ├─ 创建 K8s 资源                                                │
│     ├─ 分配域名/网络                                                │
│     └─ 回传实例状态                                                 │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

## 核心组件

### 1. MQ Publisher (`internal/data/mq.go`)

**职责**：发布实例创建事件到 RabbitMQ

**接口**：
```go
type MQPublisher interface {
    PublishInstanceCreated(ctx, spec) error
}
```

**实现**：
- 连接 RabbitMQ
- 声明 Exchange（topic 类型）
- 序列化 Protobuf 消息
- 发布到指定 routing key

### 2. Instance ID Generator (`internal/data/instance_id_generator.go`)

**职责**：生成全局唯一的实例 ID

**接口**：
```go
type InstanceIDGenerator interface {
    Generate(ctx) (int64, error)
}
```

**实现建议**：
- 雪花算法（Snowflake）
- Redis INCR
- 数据库序列
- UUID 转 int64

### 3. Seckill Usecase (`internal/biz/seckill.go`)

**处理流程**：
```go
func ProcessSeckillOrder(ctx, productID, userID, streamID) error {
    // 1. 生成实例 ID
    instanceID := idGenerator.Generate(ctx)
    
    // 2. 查询商品信息
    product := repo.GetProductByID(ctx, productID)
    
    // 3. 发送 MQ 消息
    mqPublisher.PublishInstanceCreated(ctx, InstanceSpec{
        InstanceID: instanceID,
        UserID:     userID,
        Name:       product.Name,
        CPU:        product.CPU,
        Memory:     product.Memory,
        GPU:        product.GPU,
        Image:      product.Image,
    })
    
    // 4. 写入数据库日志
    repo.SaveSeckillLog(ctx, &SeckillLog{
        ProductID:  productID,
        UserID:     userID,
        InstanceID: instanceID,
        StreamID:   streamID,
        Status:     "PROCESSING",
    })
}
```

## MQ 消息格式

### Protobuf 定义 (`api/mq/event.proto`)

```protobuf
message Event {
  EventType event_type = 1;      // INSTANCE_CREATED
  int64 instance_id = 2;          // 实例 ID
  int64 user_id = 3;              // 用户 ID
  string name = 4;                // 实例名称
  InstanceSpec spec = 5;          // 实例规格
  int64 timestamp = 6;            // 时间戳
}

message InstanceSpec {
  int32 cpus = 1;                 // CPU 核数
  int32 memory_mb = 2;            // 内存（MB）
  string gpu = 3;                 // GPU 型号
  string image = 4;               // 镜像
}
```

### 消息示例

```json
{
  "event_type": "INSTANCE_CREATED",
  "instance_id": 1234567890,
  "user_id": 123,
  "name": "GPU 计算实例",
  "spec": {
    "cpus": 8,
    "memory_mb": 32768,
    "gpu": "A100",
    "image": "ubuntu:22.04"
  },
  "timestamp": 1705478400
}
```

## 配置

### 1. 更新 `internal/conf/conf.proto`

```protobuf
message Data {
  message RabbitMQ {
    string url = 1;           // amqp://user:pass@host:port/
    string exchange = 2;      // resource.events
    string routing_key = 3;   // instance.created
  }
  RabbitMQ rabbitmq = 3;
}
```

### 2. 更新 `configs/config.yaml`

```yaml
data:
  rabbitmq:
    url: "amqp://guest:guest@localhost:5672/"
    exchange: "resource.events"
    routing_key: "instance.created"
```

### 3. 生成配置代码

```bash
make config
```

## 依赖注入

### 1. 更新 `internal/data/data.go`

```go
var ProviderSet = wire.NewSet(
    NewData,
    NewSeckillRepo,
    NewMQPublisher,           // 添加
    NewInstanceIDGenerator,   // 添加
)
```

### 2. 更新 `cmd/product/wire.go`

```go
func wireApp(*conf.Server, *conf.Data, log.Logger) (*kratos.App, func(), error) {
    panic(wire.Build(
        server.ProviderSet,
        data.ProviderSet,
        biz.ProviderSet,
        service.ProviderSet,
        newApp,
        
        // Redis 客户端
        data.NewRedisClient,
        
        // 秒杀相关
        newSeckillOrderService,
        newSeckillStreamServer,
    ))
}
```

### 3. 生成 Wire 代码

```bash
cd cmd/product
wire
```

## 数据库表结构

### seckill_logs 表

```sql
CREATE TABLE seckill_logs (
    id BIGSERIAL PRIMARY KEY,
    product_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    instance_id BIGINT NOT NULL,      -- 新增字段
    stream_id VARCHAR(64) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'PROCESSING',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_seckill_logs_instance_id ON seckill_logs(instance_id);
```

## 实现 Instance ID Generator

### 方案 1: 雪花算法

```go
import "github.com/bwmarrin/snowflake"

type snowflakeGenerator struct {
    node *snowflake.Node
}

func NewInstanceIDGenerator(logger log.Logger) biz.InstanceIDGenerator {
    node, _ := snowflake.NewNode(1) // 节点 ID
    return &snowflakeGenerator{node: node}
}

func (g *snowflakeGenerator) Generate(ctx context.Context) (int64, error) {
    return g.node.Generate().Int64(), nil
}
```

### 方案 2: Redis INCR

```go
type redisIDGenerator struct {
    rdb *redis.Client
    key string
}

func NewInstanceIDGenerator(rdb *redis.Client, logger log.Logger) biz.InstanceIDGenerator {
    return &redisIDGenerator{
        rdb: rdb,
        key: "instance:id:seq",
    }
}

func (g *redisIDGenerator) Generate(ctx context.Context) (int64, error) {
    return g.rdb.Incr(ctx, g.key).Result()
}
```

### 方案 3: 数据库序列

```go
func (g *dbIDGenerator) Generate(ctx context.Context) (int64, error) {
    var id int64
    err := g.db.Raw("SELECT nextval('instance_id_seq')").Scan(&id).Error
    return id, err
}
```

## 测试

### 1. 单元测试

```go
func TestProcessSeckillOrder(t *testing.T) {
    // Mock dependencies
    mockRepo := &mockSeckillRepo{}
    mockMQ := &mockMQPublisher{}
    mockIDGen := &mockIDGenerator{nextID: 1001}
    
    uc := biz.NewSeckillUsecase(mockRepo, mockMQ, mockIDGen, logger)
    
    err := uc.ProcessSeckillOrder(ctx, 1001, 123, "stream-id-1")
    assert.NoError(t, err)
    assert.Equal(t, 1, mockMQ.publishCount)
    assert.Equal(t, 1, mockRepo.saveCount)
}
```

### 2. 集成测试

```bash
# 启动 RabbitMQ
docker run -d --name rabbitmq -p 5672:5672 -p 15672:15672 rabbitmq:3-management

# 启动服务
go run cmd/product/main.go -conf configs/config.yaml

# 推送测试消息
redis-cli XADD stream:seckill:1001 * uid 123 ts 1234567890

# 查看 RabbitMQ 管理界面
open http://localhost:15672
```

## 监控

### 1. MQ 发布指标

```go
// 在 mq.go 中添加
var (
    mqPublishTotal = prometheus.NewCounterVec(...)
    mqPublishDuration = prometheus.NewHistogramVec(...)
)
```

### 2. 查看 RabbitMQ 队列

```bash
# 查看 Exchange
rabbitmqadmin list exchanges

# 查看消息数量
rabbitmqadmin list queues
```

### 3. 数据库查询

```sql
-- 查看最近的秒杀记录
SELECT * FROM seckill_logs 
ORDER BY created_at DESC 
LIMIT 10;

-- 统计各状态数量
SELECT status, COUNT(*) 
FROM seckill_logs 
GROUP BY status;
```

## 故障排查

### 问题 1: MQ 连接失败

**症状**：日志显示 "failed to connect rabbitmq"

**解决**：
1. 检查 RabbitMQ 是否启动
2. 检查配置文件中的 URL
3. 检查网络连通性

### 问题 2: 消息未发送

**症状**：数据库有记录，但 Resource Domain 未收到消息

**解决**：
1. 检查 Exchange 是否创建
2. 检查 routing key 是否正确
3. 查看 RabbitMQ 管理界面

### 问题 3: Instance ID 冲突

**症状**：数据库插入失败，提示 instance_id 重复

**解决**：
1. 检查 ID 生成器实现
2. 确保分布式环境下 ID 唯一性
3. 添加数据库唯一索引

## 下一步

1. 实现 Instance ID Generator（雪花算法）
2. 完善商品信息查询逻辑
3. 添加 MQ 消息重试机制
4. 实现状态更新接口
5. 添加监控和告警

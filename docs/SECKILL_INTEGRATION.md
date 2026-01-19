# 秒杀功能集成指南

## 快速开始

### 1. 数据库迁移

执行数据库迁移脚本：

```bash
psql -U postgres -d product_db -f migrations/001_create_seckill_logs.sql
```

### 2. 配置 Redis

确保 `configs/config.yaml` 中配置了 Redis：

```yaml
data:
  redis:
    addr: "localhost:6379"
    password: ""
    db: 0
    read_timeout: 2s
    write_timeout: 2s
```

### 3. 依赖注入配置

在 `cmd/product/wire.go` 中添加：

```go
//go:build wireinject
// +build wireinject

package main

import (
	"product/internal/biz"
	"product/internal/conf"
	"product/internal/data"
	"product/internal/server"
	"product/internal/service"

	"github.com/go-kratos/kratos/v2"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/transport"
	"github.com/google/wire"
)

// wireApp 初始化应用
func wireApp(*conf.Server, *conf.Data, log.Logger) (*kratos.App, func(), error) {
	panic(wire.Build(
		server.ProviderSet,
		data.ProviderSet,
		biz.ProviderSet,
		service.ProviderSet,
		newApp,
		
		// 添加 Redis 客户端
		data.NewRedisClient,
		
		// 添加秒杀相关
		newSeckillOrderService,
		newSeckillStreamServer,
	))
}

// newSeckillOrderService 创建秒杀订单服务
func newSeckillOrderService(uc *biz.SeckillUsecase, logger log.Logger) *service.SeckillOrderService {
	// TODO: 从配置文件读取 productID
	productID := int64(1001)
	return service.NewSeckillOrderService(uc, productID, logger)
}

// newSeckillStreamServer 创建秒杀 Stream 服务器
func newSeckillStreamServer(
	rdb *redis.Client,
	logger log.Logger,
	handler *service.SeckillOrderService,
) transport.Server {
	// TODO: 从配置文件读取 productID
	productID := int64(1001)
	return server.NewSeckillStreamServer(rdb, logger, handler, productID)
}

// newApp 创建应用
func newApp(
	logger log.Logger,
	hs *server.HTTPServer,
	gs *server.GRPCServer,
	ss transport.Server, // 秒杀 Stream 服务器
) *kratos.App {
	return kratos.New(
		kratos.ID(id),
		kratos.Name(Name),
		kratos.Version(Version),
		kratos.Metadata(map[string]string{}),
		kratos.Logger(logger),
		kratos.Server(
			hs,
			gs,
			ss, // 注册秒杀 Stream 服务器
		),
	)
}
```

### 4. 生成 Wire 代码

```bash
cd cmd/product
wire
```

### 5. 启动服务

```bash
go run cmd/product/main.go -conf configs/config.yaml
```

## 验证功能

### 1. 检查 Stream 服务器启动

查看日志输出：

```
[INFO] seckill stream server started: productID=1001 stream=stream:seckill:1001 group=g1 consumer=seckill-consumer-1001
```

### 2. BFF 层推送测试消息

使用 Redis CLI 模拟 BFF 层推送消息：

```bash
redis-cli XADD stream:seckill:1001 * uid 123 ts 1234567890
```

### 3. 检查数据库日志

```sql
SELECT * FROM seckill_logs ORDER BY created_at DESC LIMIT 10;
```

应该看到新插入的记录：

```
 id | product_id | user_id |    stream_id     |   status   |         created_at         
----+------------+---------+------------------+------------+----------------------------
  1 |       1001 |     123 | 1234567890-0     | PROCESSING | 2026-01-17 10:00:00+00
```

## 多商品支持

如果需要支持多个商品的秒杀，可以修改 `wire.go`：

```go
// newSeckillStreamServers 创建多个秒杀 Stream 服务器
func newSeckillStreamServers(
	rdb *redis.Client,
	logger log.Logger,
	uc *biz.SeckillUsecase,
) []transport.Server {
	productIDs := []int64{1001, 1002, 1003} // 从配置读取
	servers := make([]transport.Server, 0, len(productIDs))
	
	for _, productID := range productIDs {
		handler := service.NewSeckillOrderService(uc, productID, logger)
		srv := server.NewSeckillStreamServer(rdb, logger, handler, productID)
		servers = append(servers, srv)
	}
	
	return servers
}

// newApp 修改为接收多个服务器
func newApp(
	logger log.Logger,
	hs *server.HTTPServer,
	gs *server.GRPCServer,
	seckillServers []transport.Server,
) *kratos.App {
	servers := []transport.Server{hs, gs}
	servers = append(servers, seckillServers...)
	
	return kratos.New(
		kratos.ID(id),
		kratos.Name(Name),
		kratos.Version(Version),
		kratos.Logger(logger),
		kratos.Server(servers...),
	)
}
```

## 监控和调试

### 1. 查看 Stream 信息

```bash
# 查看 Stream 长度
redis-cli XLEN stream:seckill:1001

# 查看消费者组信息
redis-cli XINFO GROUPS stream:seckill:1001

# 查看待处理消息
redis-cli XPENDING stream:seckill:1001 g1
```

### 2. 查看日志

```bash
# 查看服务日志
tail -f logs/product.log | grep seckill
```

### 3. 数据库查询

```sql
-- 统计各状态的数量
SELECT status, COUNT(*) FROM seckill_logs GROUP BY status;

-- 查看最近的失败记录
SELECT * FROM seckill_logs WHERE status = 'FAILED' ORDER BY created_at DESC LIMIT 10;

-- 查看某个用户的秒杀记录
SELECT * FROM seckill_logs WHERE user_id = 123 ORDER BY created_at DESC;
```

## 故障排查

### 问题 1: Stream 服务器未启动

**症状**：日志中没有 "seckill stream server started" 消息

**解决**：
1. 检查 Redis 配置是否正确
2. 检查 Wire 依赖注入是否正确
3. 运行 `wire` 重新生成代码

### 问题 2: 消息未被消费

**症状**：Stream 中有消息，但数据库没有记录

**解决**：
1. 检查消费者组是否创建：`redis-cli XINFO GROUPS stream:seckill:1001`
2. 查看服务日志是否有错误
3. 检查数据库连接是否正常

### 问题 3: 消息重复消费

**症状**：同一个 stream_id 在数据库中有多条记录

**解决**：
1. 添加唯一索引：`CREATE UNIQUE INDEX idx_seckill_logs_stream_id_unique ON seckill_logs(stream_id);`
2. 在代码中添加去重逻辑

## 性能调优

### 1. 调整批量消费数量

```go
// 在 NewSeckillStreamServer 中修改
count: 256, // 增加批量消费数量
```

### 2. 调整超时时间

```go
// 减少阻塞时间，提高响应速度
block: 1 * time.Second,

// 减少重新认领时间，加快失败重试
claimIdle: 5 * time.Second,
```

### 3. 数据库批量插入

修改 `internal/data/seckill.go`，实现批量插入：

```go
func (r *seckillRepo) SaveSeckillLogBatch(ctx context.Context, logs []*biz.SeckillLog) error {
	pos := make([]*seckillLogPO, len(logs))
	for i, log := range logs {
		pos[i] = &seckillLogPO{
			ProductID: log.ProductID,
			UserID:    log.UserID,
			StreamID:  log.StreamID,
			Status:    log.Status,
		}
	}
	return r.data.db.WithContext(ctx).CreateInBatches(pos, 100).Error
}
```

## 下一步

1. 实现订单创建逻辑
2. 添加状态更新接口
3. 实现结果查询 API
4. 添加监控指标
5. 配置告警规则

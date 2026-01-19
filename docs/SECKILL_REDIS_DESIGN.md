# 秒杀 Redis 设计文档

## 概述

秒杀系统采用 Redis 作为核心存储，同一时间只支持一个秒杀商品。商品 ID 作为全局变量存储在 Redis 中，无需数据库表。

## Redis Key 设计

| Key | 类型 | 说明 | 过期时间 |
|-----|------|------|---------|
| `seckill:product_id` | String | 当前秒杀商品ID | 永久 |
| `seckill:stock` | String | 当前库存数量 | 永久 |
| `seckill:req_seq` | String | 请求序列号（自增） | 永久 |
| `seckill:uid2req` | Hash | 用户ID → 请求号映射 | 永久 |
| `seckill:stream_orders` | Stream | 订单消息流 | 永久 |

## 核心接口

### SeckillProductRepo

```go
type SeckillProductRepo interface {
    // InitSeckill 初始化秒杀（清空上次数据并设置新的 productID 和库存）
    InitSeckill(ctx context.Context, productID int64, stock int32) error
    
    // GetCurrentProductID 获取当前秒杀商品ID
    GetCurrentProductID(ctx context.Context) (int64, error)
    
    // GetStock 获取当前库存
    GetStock(ctx context.Context) (int32, error)
    
    // ClearSeckill 清空秒杀数据
    ClearSeckill(ctx context.Context) error
}
```

## 业务流程

### 1. 初始化秒杀

管理员调用 `InitSeckill` 接口：

```go
err := seckillUsecase.InitSeckill(ctx, productID, stock)
```

**执行步骤**：
1. 删除所有旧的秒杀数据（5个 Key）
2. 设置新的 `product_id`
3. 设置新的 `stock`
4. 初始化 `req_seq` 为 0

### 2. BFF 层秒杀抢购

BFF 层执行 Lua 脚本（`goods.lua`）：

```lua
-- KEYS[1]=seckill:stock
-- KEYS[2]=seckill:req_seq
-- KEYS[3]=seckill:uid2req
-- KEYS[4]=seckill:stream_orders
-- ARGV[1]=uid
-- ARGV[2]=ttl_sec (保留参数)
-- ARGV[3]=now_ms
-- ARGV[4]=stream_maxlen

-- 返回值：
--   {0} 库存不足
--   {2, old_req} 用户已购买
--   {1, req, sid} 抢购成功
```

**脚本逻辑**：
1. 检查用户是否已购买（`HGET uid2req`）
2. 检查库存是否充足（`GET stock`）
3. 扣减库存（`DECRBY stock 1`）
4. 生成请求号（`INCR req_seq`）
5. 记录用户购买（`HSET uid2req`）
6. 推送到订单流（`XADD stream_orders`）

**注意**：BFF 层不需要传入 `productID`，因为同一时间只有一个秒杀商品。

### 3. 商品域消费订单流

商品域监听 `seckill:stream_orders`：

```go
// 从 Redis 获取当前秒杀商品ID
productID, err := seckillRepo.GetCurrentProductID(ctx)

// 创建订单
order := &Order{
    ProductID: productID,
    UserID:    msg.UID,
    ReqID:     msg.ReqID,
    Source:    "SECKILL",
    Status:    "PAID",
}
```

### 4. 清空秒杀数据

管理员可以手动清空秒杀数据：

```go
err := seckillUsecase.ClearSeckill(ctx)
```

## 与正常购买的区别

| 场景 | productID 来源 | req_id 生成 | 订单状态 |
|------|---------------|------------|---------|
| 秒杀/直接购买 | Redis 全局变量 | Redis INCR | 直接 PAID |
| 正常购买 | 请求参数传入 | 随机生成（雪花ID） | 直接 PAID |

## 优势

1. **高性能**：所有操作在 Redis 内存中完成
2. **原子性**：Lua 脚本保证扣库存和生成请求号的原子性
3. **简化设计**：无需数据库表，减少维护成本
4. **灵活切换**：可快速切换秒杀商品

## 注意事项

1. **单商品限制**：同一时间只能有一个秒杀商品
2. **数据持久化**：Redis 需配置持久化（AOF/RDB）
3. **清理策略**：秒杀结束后需手动清理数据或初始化新秒杀
4. **监控告警**：需监控 Redis 内存使用和 Stream 长度

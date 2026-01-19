# 订单表添加 req_id 字段变更说明

## 变更概述

为订单表（orders）添加 `req_id` 字段，用于实现幂等性控制和防止重复下单。

## 变更时间

2026-01-18

## 变更内容

### 1. 数据库表结构变更

#### orders 表新增字段

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| req_id | BIGINT | NOT NULL | Redis 生成的请求号，用于幂等/判重 |

#### 新增唯一索引

```sql
CREATE UNIQUE INDEX uk_orders_product_req ON orders(product_id, req_id);
```

**索引说明**：
- 组合唯一索引确保同一商品的同一请求号只能创建一次订单
- 防止用户重复提交订单
- 支持 Redis 生成的请求号进行幂等性控制

### 2. 领域模型变更

#### internal/biz/instance.go

```go
type Order struct {
    ID          int64
    UserID      string
    ProductID   int64
    ReqID       int64  // 新增字段
    Amount      int64
    InstanceID  *int64
    Status      int32
    CreatedAt   time.Time
    PaidAt      *time.Time
    CompletedAt *time.Time
}
```

### 3. 持久化对象变更

#### internal/data/instance.go

```go
type orderPO struct {
    ID          int64          `gorm:"primaryKey"`
    UserID      string         `gorm:"column:user_id;type:uuid;not null;index"`
    ProductID   int64          `gorm:"column:product_id;not null;index;uniqueIndex:uk_product_req"`
    ReqID       int64          `gorm:"column:req_id;not null;uniqueIndex:uk_product_req"` // 新增
    Amount      int64          `gorm:"column:amount;not null"`
    InstanceID  sql.NullInt64  `gorm:"column:instance_id;index"`
    Status      int32          `gorm:"column:status;not null;default:0"`
    CreatedAt   time.Time      `gorm:"column:created_at;autoCreateTime"`
    PaidAt      sql.NullTime   `gorm:"column:paid_at"`
    CompletedAt sql.NullTime   `gorm:"column:completed_at"`
}
```

**GORM 标签说明**：
- `uniqueIndex:uk_product_req`：与 product_id 组成联合唯一索引
- `not null`：字段不能为空

### 4. 文档更新

已更新以下文档：

- ✅ `AGENTS.md` - 项目规范文档
- ✅ `CLAUDE.md` - 开发指南文档
- ✅ `README.md` - 项目说明文档
- ✅ `docs/DATABASE_SCHEMA.md` - 数据库设计文档

### 5. 数据库迁移脚本

#### 新建表脚本

- `migrations/001_create_tables.sql` - 完整的表创建脚本（包含 req_id 字段）

#### 增量迁移脚本

- `migrations/002_add_req_id_to_orders.sql` - 为现有订单表添加 req_id 字段

## 使用场景

### 1. 防止重复下单

```go
// BFF 层生成请求号
reqID := redis.Incr("order:req_id:" + productID)

// 创建订单时传入 reqID
order := &Order{
    UserID:    userID,
    ProductID: productID,
    ReqID:     reqID,
    Amount:    amount,
}

// 如果 (product_id, req_id) 已存在，数据库会返回唯一约束冲突错误
err := repo.CreateOrder(ctx, order)
```

### 2. 幂等性保证

```go
// 用户重复提交相同的订单请求
// 第一次：成功创建订单
// 第二次：返回唯一约束冲突，可以查询已存在的订单返回给用户
if errors.Is(err, gorm.ErrDuplicatedKey) {
    // 查询已存在的订单
    existingOrder, _ := repo.GetOrderByProductAndReqID(ctx, productID, reqID)
    return existingOrder, nil
}
```

### 3. Redis 请求号生成

```redis
# 为每个商品维护独立的请求号序列
INCR order:req_id:1001  # 商品 1001 的请求号
INCR order:req_id:1002  # 商品 1002 的请求号
```

## 数据迁移步骤

### 方案 A：新建数据库（推荐用于开发环境）

```bash
# 1. 删除旧数据库
psql -U postgres -c "DROP DATABASE IF EXISTS product_db;"

# 2. 创建新数据库
psql -U postgres -c "CREATE DATABASE product_db;"

# 3. 执行完整建表脚本
psql -U postgres -d product_db -f migrations/001_create_tables.sql
```

### 方案 B：增量迁移（推荐用于生产环境）

```bash
# 执行增量迁移脚本
psql -U postgres -d product_db -f migrations/002_add_req_id_to_orders.sql
```

**注意事项**：
- 增量迁移会为现有订单填充 `req_id = id` 作为默认值
- 生产环境建议在低峰期执行迁移
- 迁移前请备份数据库

## 验证步骤

### 1. 验证表结构

```sql
-- 查看 orders 表结构
\d orders

-- 应该看到 req_id 字段和 uk_orders_product_req 唯一索引
```

### 2. 验证唯一约束

```sql
-- 插入测试数据
INSERT INTO orders (id, user_id, product_id, req_id, amount, status)
VALUES (1, '123e4567-e89b-12d3-a456-426614174000', 1001, 1, 10000, 0);

-- 尝试插入相同的 (product_id, req_id)，应该失败
INSERT INTO orders (id, user_id, product_id, req_id, amount, status)
VALUES (2, '123e4567-e89b-12d3-a456-426614174000', 1001, 1, 10000, 0);
-- ERROR: duplicate key value violates unique constraint "uk_orders_product_req"
```

### 3. 验证代码

```bash
# 运行测试
go test ./internal/biz/... -v
go test ./internal/data/... -v

# 构建项目
make build
```

## 回滚方案

如果需要回滚此变更：

```sql
-- 1. 删除唯一索引
DROP INDEX IF EXISTS uk_orders_product_req;

-- 2. 删除 req_id 字段
ALTER TABLE orders DROP COLUMN IF EXISTS req_id;
```

## 影响范围

### 需要修改的代码模块

1. **订单创建逻辑**：需要在创建订单时传入 `req_id`
2. **订单查询逻辑**：可以新增按 `(product_id, req_id)` 查询的方法
3. **API 层**：Proto 定义需要添加 `req_id` 字段（如果暴露给外部）

### 不受影响的模块

- 实例创建流程（instance_logs 表）
- 秒杀流程（seckill_products 表）
- 商品管理（products 表）

## 后续工作

1. [ ] 更新 Proto 定义（如果需要暴露给外部 API）
2. [ ] 实现订单创建时的 req_id 生成逻辑
3. [ ] 添加按 (product_id, req_id) 查询订单的方法
4. [ ] 更新单元测试和集成测试
5. [ ] 更新 API 文档

## 参考资料

- [AGENTS.md](../AGENTS.md) - 项目规范
- [DATABASE_SCHEMA.md](./DATABASE_SCHEMA.md) - 数据库设计
- [GORM 唯一索引文档](https://gorm.io/docs/indexes.html)

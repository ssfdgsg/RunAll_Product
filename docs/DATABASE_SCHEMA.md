# 数据库设计文档

## 表结构

### 1. product_specs（商品规格表）

**说明**：值对象，一经创建不可修改。若规格变动应创建新规格并发布新商品。

| 字段 | 类型 | 说明 |
|------|------|------|
| id | BIGSERIAL | 主键（自增） |
| cpu | INT | CPU 核数 |
| memory | INT | 内存（GB） |
| gpu | INT | GPU 数量（默认 0） |
| image | VARCHAR(255) | 镜像 ID 或名称 |
| config_json | JSONB | 扩展配置（如磁盘类型、带宽等） |
| created_at | TIMESTAMPTZ | 创建时间 |

### 2. products（商品表）

**说明**：商品聚合根，关联规格表。

| 字段 | 类型 | 说明 |
|------|------|------|
| id | BIGSERIAL | 主键（自增） |
| name | VARCHAR(128) | 套餐名称 |
| description | TEXT | 详细描述 |
| status | SMALLINT | 1=ENABLED, 0=DISABLED |
| price | BIGINT | 单价（分） |
| spec_id | BIGINT | 关联规格 ID（外键） |
| created_at | TIMESTAMPTZ | 创建时间 |
| updated_at | TIMESTAMPTZ | 更新时间 |

### 3. orders（订单表）

**说明**：订单聚合根。支持两种来源：秒杀/直接购买、正常购买。

| 字段 | 类型 | 说明 |
|------|------|------|
| id | BIGINT | 主键（雪花 ID） |
| user_id | UUID | 用户 ID |
| product_id | BIGINT | 商品 ID（外键） |
| req_id | BIGINT | 请求号（秒杀：Redis INCR；正常购买：随机生成），与 product_id 组成唯一索引 |
| amount | BIGINT | 订单金额（分） |
| instance_id | BIGINT | 资源实例 ID（创建后填充，可为空） |
| status | SMALLINT | 0=PENDING, 1=PAID, 2=CANCELLED, 3=COMPLETED |
| source | VARCHAR(20) | SECKILL=秒杀/直接购买, NORMAL=正常购买 |
| created_at | TIMESTAMPTZ | 下单时间 |
| paid_at | TIMESTAMPTZ | 支付时间（可为空） |
| completed_at | TIMESTAMPTZ | 完成时间（可为空） |

### 4. instance_logs（实例创建日志表）

**说明**：记录所有实例创建请求（秒杀和普通订单）。

**⚠️ 重要**：此表由**资源域（Resource Domain）**管理，商品域不直接操作此表。

| 字段 | 类型 | 说明 |
|------|------|------|
| id | BIGSERIAL | 主键（自增） |
| product_id | BIGINT | 商品 ID |
| user_id | UUID | 用户 ID |
| instance_id | BIGINT | 实例 ID |
| source | VARCHAR(20) | 来源：SECKILL / ORDER |
| source_id | VARCHAR(64) | 来源 ID（秒杀为 StreamID，订单为 OrderID） |
| status | VARCHAR(20) | PROCESSING / SUCCESS / FAILED |
| created_at | TIMESTAMPTZ | 创建时间 |
| updated_at | TIMESTAMPTZ | 更新时间 |

**职责划分**：
- **商品域**：生成 instance_id，发送 MQ 消息
- **资源域**：监听 MQ，创建实例，写入 instance_logs

## 索引设计

```sql
-- products 表
CREATE INDEX idx_products_spec_id ON products(spec_id);
CREATE INDEX idx_products_status ON products(status);

-- orders 表
CREATE UNIQUE INDEX uk_orders_product_req ON orders(product_id, req_id);
CREATE INDEX idx_orders_user_id ON orders(user_id);
CREATE INDEX idx_orders_product_id ON orders(product_id);
CREATE INDEX idx_orders_instance_id ON orders(instance_id);
CREATE INDEX idx_orders_status ON orders(status);
CREATE INDEX idx_orders_source ON orders(source);

-- instance_logs 表
CREATE INDEX idx_instance_logs_product_id ON instance_logs(product_id);
CREATE INDEX idx_instance_logs_user_id ON instance_logs(user_id);
CREATE INDEX idx_instance_logs_instance_id ON instance_logs(instance_id);
CREATE INDEX idx_instance_logs_source ON instance_logs(source);
CREATE INDEX idx_instance_logs_source_id ON instance_logs(source_id);
```

## 数据流转

### 秒杀/直接购买流程

```
1. BFF 层执行 Lua 脚本 → Redis Stream
   ├─ 扣减库存
   ├─ 生成 req_id (Redis INCR)
   └─ 推送消息 {uid, req_id, ts}
2. Product Service 消费 Stream
   ├─ 创建订单 (status=PAID, source=SECKILL, req_id=Redis值)
   ├─ 生成 instance_id
   ├─ 查询 products + product_specs
   ├─ 更新 orders.instance_id
   └─ 发送 MQ 消息到 Resource Domain
3. Resource Domain 监听 MQ
   ├─ 创建 K8s 实例
   └─ 回调更新 orders (status=COMPLETED)
```

### 正常购买流程

```
1. 用户下单
   ├─ 生成 req_id (随机，如雪花ID)
   └─ 创建 orders (status=PENDING, source=NORMAL, req_id=随机值)
2. 用户支付 → 更新 orders (status=PAID, paid_at=now)
3. Product Service 处理支付成功事件
   ├─ 生成 instance_id
   ├─ 查询 products + product_specs
   ├─ 更新 orders.instance_id
   └─ 发送 MQ 消息到 Resource Domain
4. Resource Domain 监听 MQ
   ├─ 创建 K8s 实例
   └─ 回调更新 orders (status=COMPLETED)
```

## 查询示例

### 查询商品及规格

```sql
SELECT 
    p.id, p.name, p.description, p.status, p.price,
    s.cpu, s.memory, s.gpu, s.image, s.config_json
FROM products p
JOIN product_specs s ON p.spec_id = s.id
WHERE p.id = $1;
```

### 查询用户订单

```sql
SELECT 
    o.id, o.product_id, o.amount, o.instance_id, o.status,
    o.created_at, o.paid_at, o.completed_at,
    p.name as product_name
FROM orders o
JOIN products p ON o.product_id = p.id
WHERE o.user_id = $1
ORDER BY o.created_at DESC;
```

### 查询实例创建日志

```sql
SELECT 
    il.id, il.product_id, il.user_id, il.instance_id,
    il.source, il.source_id, il.status,
    il.created_at, il.updated_at,
    p.name as product_name
FROM instance_logs il
JOIN products p ON il.product_id = p.id
WHERE il.user_id = $1
ORDER BY il.created_at DESC;
```

### 统计秒杀成功率

```sql
SELECT 
    source,
    status,
    COUNT(*) as count
FROM instance_logs
WHERE created_at >= NOW() - INTERVAL '1 day'
GROUP BY source, status
ORDER BY source, status;
```

## 数据迁移

执行迁移脚本：

```bash
psql -U postgres -d product_db -f migrations/001_create_tables.sql
```

## 注意事项

1. **UUID 类型**：user_id 使用 PostgreSQL 原生 UUID 类型
2. **金额精度**：price 和 amount 使用 BIGINT（单位：分）
3. **规格不可变**：product_specs 创建后不可修改
4. **外键约束**：products.spec_id 和 orders.product_id 有外键约束
5. **索引优化**：根据查询模式添加了复合索引

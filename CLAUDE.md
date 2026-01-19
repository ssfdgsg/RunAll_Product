# CLAUDE.md - Product Service 开发指南

> **环境**: Git Bash on Windows | **框架**: Kratos v2 + Wire + GORM

---

## 项目概述

Product Service 是 RunAll 平台的商品域微服务，属于三大核心域之一：

| 域 | 职责 |
|---|------|
| User Domain | 账号注册/登录、JWT签发、用户状态管理 |
| **Product Domain** | 商品管理、订单处理 ← 本服务 |
| Resource Domain | 实例生命周期、K8s交互、域名管理 |

---

## 快捷命令 (Commands)

```bash
# 生成 API 代码 (Proto -> Go)
make api

# 生成 Wire 依赖注入
cd cmd/product && wire

# 生成所有代码并整理依赖
make generate

# 构建项目
make build

# 运行服务
./bin/product -conf configs/config.yaml
```

---

## 领域模型

### 设计原则

1. **快照不可变性**: 订单创建时必须保存商品快照，确保历史订单不受商品变更影响
2. **限界上下文解耦**: 订单不直接持有 InstanceID，通过领域事件与资源域交互
3. **金额精度安全**: 所有金额字段使用 int64（单位：分），禁止使用浮点数

### 核心概念区分

- **Product (商品)**: 可售卖的套餐/SKU，定义了规格和价格，是交易的标的物
- **Instance (实例)**: 用户购买商品后，由资源域创建的实际运行资源（K8s Pod/容器）

### Product (商品聚合根)

| 字段 | 类型 | 说明 |
|------|------|------|
| ProductID | int64 | 主键（雪花ID） |
| Name | string | 商品名称（如"基础型实例"、"GPU计算型"） |
| Description | string | 商品描述 |
| Status | enum | ENABLED / DISABLED（上下架状态） |
| Spec | ProductSpec | 商品规格配置（定义实例的资源配置） |
| Price | int64 | 商品价格（单位：分） |
| CreatedAt | time | 创建时间 |
| UpdatedAt | time | 更新时间 |

### ProductSpec (值对象 - 不可变)

商品规格定义了购买后创建的实例的资源配置。

| 字段 | 类型 | 说明 |
|------|------|------|
| CPU | int | CPU 核数 |
| Memory | int | 内存大小（单位：MB） |
| GPU | *GPUSpec | GPU 配置（可为 null） |
| Image | string | 容器镜像（如 "ubuntu:22.04"） |
| BillingCycle | enum | 计费周期：MONTHLY / YEARLY / PAY_AS_YOU_GO |
| ConfigJSON | json | 扩展配置（磁盘、网络等） |

### GPUSpec (值对象)

| 字段 | 类型 | 说明 |
|------|------|------|
| Model | string | GPU 型号（如 "A100", "V100"） |
| Count | int | GPU 数量 |

### Order (订单聚合根)

| 字段 | 类型 | 说明 |
|------|------|------|
| OrderID | int64 | 订单唯一标识（雪花ID） |
| UserID | int64 | 用户ID（关联 User Domain） |
| ProductID | int64 | 商品ID |
| ReqID | int64 | Redis 生成的请求号（用于幂等/判重），与 ProductID 组成唯一索引 |
| ProductSnapshot | ProductSnapshot | 下单时的商品快照（不可变） |
| TotalAmount | int64 | 订单总价（单位：分） |
| Status | enum | PENDING / PAID / CANCELLED / COMPLETED |
| PayStatus | enum | UNPAID / PAID / REFUNDED |
| CreatedAt | time | 创建时间 |
| UpdatedAt | time | 更新时间 |
| PaidAt | *time | 支付时间（可为 null） |
| CancelledAt | *time | 取消时间（可为 null） |
| CompletedAt | *time | 完成时间（可为 null） |

### ProductSnapshot (值对象 - 订单内嵌)

| 字段 | 类型 | 说明 |
|------|------|------|
| ProductID | int64 | 原商品ID（仅供追溯） |
| Name | string | 下单时的商品名称 |
| Price | int64 | 下单时的单价（分） |
| Spec | ProductSpec | 下单时的配置快照 |

### 领域事件

| 事件 | 触发时机 | 消费方 |
|------|----------|--------|
| OrderCreatedEvent | 订单创建成功 | - |
| OrderPaidEvent | 订单支付成功 | Resource Domain（创建实例） |
| OrderCancelledEvent | 订单取消 | Resource Domain（释放资源） |
| OrderCompletedEvent | 订单完成 | - |

### 数据库表结构

```sql
-- 商品规格表（值对象，不可变）
CREATE TABLE product_specs (
    id BIGSERIAL NOT NULL,
    cpu INT NOT NULL COMMENT 'CPU 核数',
    memory INT NOT NULL COMMENT '内存（MB）',
    gpu INT DEFAULT 0 COMMENT 'GPU 数量',
    image VARCHAR(255) NOT NULL COMMENT '容器镜像',
    config_json JSONB COMMENT '扩展配置',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (id)
);

-- 商品表
CREATE TABLE products (
    id BIGINT NOT NULL,
    name VARCHAR(128) NOT NULL COMMENT '商品名称',
    description TEXT COMMENT '商品描述',
    status SMALLINT DEFAULT 1 COMMENT '1-启用 0-禁用',
    price BIGINT NOT NULL COMMENT '价格（分）',
    spec_id BIGINT NOT NULL COMMENT '关联规格ID',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (id),
    FOREIGN KEY (spec_id) REFERENCES product_specs(id)
);

-- 订单表
CREATE TABLE orders (
    id BIGINT NOT NULL,
    user_id UUID NOT NULL COMMENT '用户ID',
    product_id BIGINT NOT NULL COMMENT '商品ID',
    req_id BIGINT NOT NULL COMMENT 'Redis生成的请求号（幂等/判重）',
    amount BIGINT NOT NULL COMMENT '订单金额（分）',
    instance_id BIGINT COMMENT '实例ID（支付后填充）',
    status SMALLINT NOT NULL DEFAULT 0 COMMENT '0-待支付 1-已支付 2-已取消 3-已完成',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    paid_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    PRIMARY KEY (id),
    UNIQUE KEY uk_product_req (product_id, req_id),
    FOREIGN KEY (product_id) REFERENCES products(id)
);

-- 实例创建日志表（由资源域管理）
-- 商品域不直接操作此表
CREATE TABLE instance_logs (
    id BIGSERIAL NOT NULL,
    product_id BIGINT NOT NULL COMMENT '商品ID',
    user_id UUID NOT NULL COMMENT '用户ID',
    instance_id BIGINT NOT NULL COMMENT '实例ID',
    source VARCHAR(20) NOT NULL COMMENT '来源：SECKILL / ORDER',
    source_id VARCHAR(64) NOT NULL COMMENT '来源ID',
    status VARCHAR(20) NOT NULL DEFAULT 'PROCESSING',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (id)
);

-- 秒杀商品配置表
CREATE TABLE seckill_products (
    id BIGINT NOT NULL,
    product_id BIGINT NOT NULL COMMENT '关联的商品ID',
    status SMALLINT NOT NULL DEFAULT 1 COMMENT '1-启用 0-禁用',
    stock INT NOT NULL DEFAULT 0 COMMENT '库存数量',
    start_time TIMESTAMPTZ NOT NULL COMMENT '秒杀开始时间',
    end_time TIMESTAMPTZ NOT NULL COMMENT '秒杀结束时间',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    created_by VARCHAR(64) COMMENT '创建人（管理员ID）',
    PRIMARY KEY (id),
    UNIQUE KEY uk_product_id (product_id)
);
```

---

## 架构分层 (Kratos DDD)

严格遵循分层依赖规则，**禁止跨层调用**：

```
┌─────────────────────────────────────────────────────┐
│  API Layer (api/product/v1/)                        │
│  - Proto 定义契约                                    │
│  - make api 生成 pb.go / grpc.pb.go / http.pb.go    │
└─────────────────────────────────────────────────────┘
                         ↓
┌─────────────────────────────────────────────────────┐
│  Service Layer (internal/service/)                  │
│  - 实现 Proto 定义的 RPC 方法                        │
│  - Proto ↔ Biz 对象转换 (防腐层)                     │
│  - 调用 Biz UseCase，禁止直接调用 Data              │
└─────────────────────────────────────────────────────┘
                         ↓
┌─────────────────────────────────────────────────────┐
│  Biz Layer (internal/biz/)                          │
│  - 领域模型 (Product, Order)                         │
│  - 定义 Repo 接口 (ProductRepo, OrderRepo)          │
│  - 禁止出现 gorm.* / sql.* 依赖                     │
└─────────────────────────────────────────────────────┘
                         ↓
┌─────────────────────────────────────────────────────┐
│  Data Layer (internal/data/)                        │
│  - 实现 Biz 定义的 Repo 接口                         │
│  - DB 模型 (PO) + GORM 操作                          │
│  - 管理数据库/缓存连接                               │
└─────────────────────────────────────────────────────┘
```

---

## 新增 API 标准流程

### Step 1: Proto 定义 (`api/product/v1/product.proto`)

```protobuf
service ProductService {
  // 商品接口
  rpc ListProduct (ListProductReq) returns (ListProductReply) {
    option (google.api.http) = { get: "/v1/products" };
  }
  rpc GetProduct (GetProductReq) returns (GetProductReply) {
    option (google.api.http) = { get: "/v1/products/{id}" };
  }
  
  // 订单接口
  rpc CreateOrder (CreateOrderReq) returns (CreateOrderReply) {
    option (google.api.http) = { post: "/v1/orders" body: "*" };
  }
  rpc GetOrder (GetOrderReq) returns (GetOrderReply) {
    option (google.api.http) = { get: "/v1/orders/{id}" };
  }
}
```

### Step 2: 生成代码

```bash
make api
```

### Step 3: Biz 层

```go
// internal/biz/product.go
type Product struct {
    ID          int64
    Name        string
    Description string
    Status      ProductStatus
    Spec        ProductSpec
    Price       uint32
}

type ProductRepo interface {
    GetByID(ctx context.Context, id int64) (*Product, error)
    List(ctx context.Context, filter ProductFilter) ([]*Product, error)
}

type ProductUsecase struct {
    repo ProductRepo
    log  *log.Helper
}
```

### Step 4: Data 层

```go
// internal/data/product.go
type productPO struct {
    ID          int64  `gorm:"primaryKey"`
    Name        string `gorm:"column:name"`
    Description string `gorm:"column:description"`
    Status      int    `gorm:"column:status"`
    CPU         int    `gorm:"column:cpu"`
    Memory      int    `gorm:"column:memory"`
    Price       uint32 `gorm:"column:price"`
}

func (productPO) TableName() string { return "products" }
```

### Step 5: Service 层

```go
// internal/service/product.go
type ProductService struct {
    v1.UnimplementedProductServiceServer
    productUc *biz.ProductUsecase
    orderUc   *biz.OrderUsecase
}
```

### Step 6: Wire 注入

```bash
cd cmd/product && wire
```

---

## 检查清单 (Checklist)

| # | 文件 | 操作 | 状态 |
|---|------|------|------|
| 1 | `api/product/v1/*.proto` | 定义 service + rpc + message | ☐ |
| 2 | - | `make api` | ☐ |
| 3 | `internal/biz/product.go` | Product 领域模型 + Repo + UseCase | ☐ |
| 4 | `internal/biz/order.go` | Order 领域模型 + Repo + UseCase | ☐ |
| 5 | `internal/biz/biz.go` | 添加到 ProviderSet | ☐ |
| 6 | `internal/data/product.go` | 实现 ProductRepo | ☐ |
| 7 | `internal/data/order.go` | 实现 OrderRepo | ☐ |
| 8 | `internal/data/data.go` | 添加到 ProviderSet | ☐ |
| 9 | `internal/service/product.go` | 实现 ProductService | ☐ |
| 10 | `internal/service/service.go` | 添加到 ProviderSet | ☐ |
| 11 | `internal/server/grpc.go` | 注册 gRPC 服务 | ☐ |
| 12 | `internal/server/http.go` | 注册 HTTP 服务 | ☐ |
| 13 | - | `make generate` | ☐ |

---

## 跨域交互（事件驱动）

Product Domain 与其他域通过领域事件解耦：

```
┌──────────────┐     创建订单时验证用户      ┌──────────────┐
│ User Domain  │ ◄─────────────────────────  │   Product    │
│              │     获取用户ID/状态          │   Domain     │
└──────────────┘                             │              │
                                             │ 商品 (Product)│
                                             │ 订单 (Order)  │
                                             └──────────────┘
                                                    │
                                                    │ 发布 OrderPaidEvent
                                                    │ (InstanceID + ProductSpec)
                                                    ▼
                                             ┌──────────────┐
                                             │  Resource    │
                                             │  Domain      │
                                             │              │
                                             │ 监听事件后：  │
                                             │ 1. 创建实例   │
                                             │   (K8s Pod)  │
                                             │ 2. 维护映射   │
                                             │   Order↔Instance │
                                             └──────────────┘
```

### 订单生命周期流程

```
用户下单 ──► Order 创建 ──► 保存 ProductSnapshot
                │
                ▼
           等待支付 (PENDING)
                │
        ┌───────┴───────┐
        ▼               ▼
   支付成功          取消订单
   (PAID)          (CANCELLED)
        │               │
        ▼               ▼
 发布 OrderPaidEvent  发布 OrderCancelledEvent
        │
        ▼
 Resource Domain 创建实例
        │
        ▼
   订单完成 (COMPLETED)
```

---

## 常见问题 (FAQ)

**Q: Proto 修改后不生效？**
```bash
make api
```

**Q: Wire 注入报错？**
1. 检查 `NewXxx` 是否添加到对应 `ProviderSet`
2. 重新生成: `make generate`

**Q: 找不到 protoc 插件？**
```bash
make init
```

---

## 文档同步规范

> **⚠️ 重要**: `AGENTS.md` 和 `CLAUDE.md` 必须保持同步更新。

| 文件 | 侧重内容 |
|------|----------|
| `AGENTS.md` | 项目规范、Agent 开发指南、代码风格约定 |
| `CLAUDE.md` | Kratos API 开发流程、快捷命令、检查清单 |

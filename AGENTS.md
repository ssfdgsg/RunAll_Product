﻿# 仓库指南

**注意：本文档主要使用中文进行交流和撰写。**

## 项目概述

Product Service 是 RunAll 平台的商品域微服务，负责商品管理和订单处理。

### RunAll 平台架构

```
┌─────────────────────────────────────────────────────────────┐
│                      RunAll Platform                        │
├─────────────┬─────────────────┬─────────────────────────────┤
│ User Domain │ Product Domain  │      Resource Domain        │
│  (用户域)    │   (商品域)       │        (资源域)              │
│             │   ← 本服务 →     │                             │
└─────────────┴─────────────────┴─────────────────────────────┘
```

### 领域职责划分

| 域 | 职责 |
|---|------|
| User Domain | 账号注册/登录、JWT签发、用户状态管理、角色权限 |
| **Product Domain** | 商品管理、订单创建/支付/取消/完成 |
| Resource Domain | 实例生命周期、K8s交互、域名/网络管理 |

---

## Agent 开发指南

### 1. Agent 定义与职责
- **核心职责**: 每个 Agent 的核心职责、能力边界和业务目标应在 `internal/biz` 目录中清晰定义。Agent 应是围绕特定任务（Task-Specific）设计的。
- **接口协定**: Agent 对外暴露的 gRPC 服务和消息体（Protobuf）必须在 `api/` 目录下定义。这构成了 Agent 的公共 API 协定。

### 2. 工具（Tools）与能力
- **工具定义**: Agent 可使用的工具集（Tools）应在 `internal/biz` 中以接口形式抽象，具体实现在 `internal/data` 中完成。
- **工具调用**: Agent 的业务逻辑（`biz` 层）负责编排和调用这些工具，以完成复杂任务。
- **工具扩展**: 增加新工具时，需同时更新 `biz` 层的接口和 `data` 层的实现，并确保其可测试性。

### 3. 状态管理
- **会话状态**: Agent 的多轮对话历史、上下文等状态信息，应通过 `internal/data` 层的接口进行持久化和读取。
- **存储选型**: 推荐使用 Redis 等内存数据库来管理会话状态，以保证低延迟和高并发。

### 4. 测试规范
- **单元测试**: Agent 的核心逻辑（`biz` 层）必须有单元测试覆盖，特别是对于工具选择、任务分解等关键路径。
- **集成测试**: 完整的 Agent 工作流应编写集成测试，通过 `DI` 注入 `in-memory` 的 `data` 实现来模拟外部依赖。
- **场景模拟**: 测试用例应存储在 `testdata/` 目录下，模拟不同的用户输入和工具返回。

---

## 领域模型

### 设计原则

1. **快照不可变性**: 订单创建时必须保存商品快照，确保历史订单不受商品变更影响
2. **限界上下文解耦**: 订单不直接持有 InstanceID，通过领域事件与资源域交互
3. **金额精度安全**: 所有金额字段使用 int64（单位：分），禁止使用浮点数

### 核心概念区分

- **Product (商品)**: 可售卖的套餐/SKU，定义了规格和价格，是交易的标的物
- **Instance (实例)**: 用户购买商品后，由资源域创建的实际运行资源（K8s Pod/容器）
- **Order (订单)**: 记录用户的购买行为，**仅在支付完成后创建**，包含两种来源：
  - **秒杀/直接购买**：BFF 扣库存成功后，商品域消费 Stream 创建订单
  - **正常购买**：用户支付成功后，商品域处理支付回调创建订单

### 订单来源与 req_id 的使用

| 来源 | 支付时机 | req_id 生成方式 | 用途 | 订单状态 |
|------|---------|----------------|------|---------|
| 秒杀/直接购买 | BFF 扣库存时已完成 | Redis INCR（BFF层） | 幂等性控制，防止重复抢购 | 直接 PAID |
| 正常购买 | 用户主动支付 | 随机生成（雪花ID等） | 防止订单重复 | 直接 PAID |

**重要**：两种场景都是支付完成后才创建订单，不存在 PENDING 状态的订单。

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
| ReqID | int64 | 请求号（秒杀：Redis INCR；正常购买：随机生成），与 ProductID 组成唯一索引 |
| ProductSnapshot | ProductSnapshot | 下单时的商品快照（不可变） |
| TotalAmount | int64 | 订单总价（单位：分） |
| Status | enum | PAID（已支付） / COMPLETED（已完成） / CANCELLED（已取消） |
| Source | enum | SECKILL（秒杀/直接购买） / NORMAL（正常购买） |
| CreatedAt | time | 创建时间（即支付完成时间） |
| UpdatedAt | time | 更新时间 |
| PaidAt | time | 支付时间（与 CreatedAt 相同） |
| CompletedAt | *time | 完成时间（可为 null） |
| CancelledAt | *time | 取消时间（可为 null） |

**注意**：订单仅在支付完成后创建，不存在 PENDING 状态。

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

### Instance (实例 - 由资源域管理)

实例是用户购买商品后创建的实际运行资源，由 Resource Domain 负责生命周期管理。

| 字段 | 类型 | 说明 |
|------|------|------|
| InstanceID | int64 | 实例唯一标识（由商品域生成） |
| UserID | string | 所属用户（UUID） |
| OrderID | int64 | 关联订单（可追溯购买来源） |
| Status | enum | CREATING / RUNNING / STOPPED / DELETED |
| Spec | InstanceSpec | 实例规格（从 ProductSpec 复制） |
| CreatedAt | time | 创建时间 |

### 跨域关联

```
┌─────────────────────────────────────────────────────────────┐
│  商品域 (Product Domain)                                     │
│  - 管理商品 (Product) 和订单 (Order)                         │
│  - 订单支付成功后，生成 InstanceID 并发送创建事件             │
│  - 在 instance_logs 表记录实例创建请求                       │
└─────────────────────────────────────────────────────────────┘
                         │
                         │ OrderPaidEvent
                         │ (包含 InstanceID + ProductSpec)
                         ▼
┌─────────────────────────────────────────────────────────────┐
│  资源域 (Resource Domain)                                    │
│  - 监听事件，创建实际的 K8s 资源（Pod/容器）                  │
│  - 管理实例生命周期（启动/停止/删除）                         │
│  - 维护 OrderID ↔ InstanceID 映射关系                       │
└─────────────────────────────────────────────────────────────┘
```

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
    id BIGSERIAL NOT NULL,
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

CREATE INDEX idx_products_spec_id ON products(spec_id);
CREATE INDEX idx_products_status ON products(status);

-- 订单表
CREATE TABLE orders (
    id BIGINT NOT NULL,
    user_id UUID NOT NULL COMMENT '用户ID',
    product_id BIGINT NOT NULL COMMENT '商品ID',
    req_id BIGINT NOT NULL COMMENT '请求号（秒杀：Redis INCR；正常购买：随机生成）',
    amount BIGINT NOT NULL COMMENT '订单金额（分）',
    instance_id BIGINT COMMENT '实例ID（创建后填充）',
    status SMALLINT NOT NULL DEFAULT 0 COMMENT '0-待支付 1-已支付 2-已取消 3-已完成',
    source VARCHAR(20) NOT NULL DEFAULT 'NORMAL' COMMENT 'SECKILL-秒杀/直接购买 NORMAL-正常购买',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    paid_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    PRIMARY KEY (id),
    UNIQUE KEY uk_product_req (product_id, req_id),
    FOREIGN KEY (product_id) REFERENCES products(id)
);

CREATE INDEX idx_orders_user_id ON orders(user_id);
CREATE INDEX idx_orders_product_id ON orders(product_id);
CREATE INDEX idx_orders_instance_id ON orders(instance_id);
CREATE INDEX idx_orders_status ON orders(status);
CREATE INDEX idx_orders_source ON orders(source);

-- 实例创建日志表（由资源域管理）
-- 商品域不直接操作此表，仅通过 MQ 消息触发资源域写入
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

CREATE INDEX idx_instance_logs_product_id ON instance_logs(product_id);
CREATE INDEX idx_instance_logs_user_id ON instance_logs(user_id);
CREATE INDEX idx_instance_logs_instance_id ON instance_logs(instance_id);
CREATE INDEX idx_instance_logs_source ON instance_logs(source);
CREATE INDEX idx_instance_logs_source_id ON instance_logs(source_id);

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

CREATE INDEX idx_seckill_status ON seckill_products(status);
CREATE INDEX idx_seckill_time ON seckill_products(start_time, end_time);
```

`product_specs.config_json` 字段示例：
```json
{
  "disk_type": "SSD",
  "disk_size": 100,
  "bandwidth": 10
}
```

### 数据流转示例

**秒杀/直接购买流程**：
```
1. BFF 层执行 Lua 脚本
   ├─ 扣减库存
   ├─ 生成 req_id (Redis INCR)
   └─ 推送到 Redis Stream {uid, req_id, product_id}
2. 商品域消费 Stream
   ├─ 生成 order_id 和 instance_id
   ├─ 创建订单 (status=PAID, source=SECKILL, req_id=Redis值)
   └─ 发送 MQ 消息到资源域
3. 资源域监听 MQ → 创建实例 → 回调更新 orders (status=COMPLETED)
```

**正常购买流程**：
```
1. 用户支付成功 → 支付回调
2. 商品域处理支付回调
   ├─ 生成 req_id（随机大数）
   ├─ 生成 order_id 和 instance_id
   ├─ 创建订单 (status=PAID, source=NORMAL, req_id=随机值)
   └─ 发送 MQ 消息到资源域
3. 资源域监听 MQ → 创建实例 → 回调更新 orders (status=COMPLETED)
```

**关键点**：
- 两种场景都是**支付完成后**才创建订单记录
- 订单创建时 status 直接为 PAID（不存在 PENDING 状态）
- req_id 的唯一区别：秒杀用 Redis INCR，正常购买用随机大数

---

## 项目结构与模块组织

```
.
├── api/product/v1/       # Proto 定义 (gRPC + HTTP)
├── cmd/product/          # 入口 + Wire 依赖注入
├── configs/              # 运行时配置 (YAML)
├── internal/
│   ├── biz/              # 业务逻辑 (领域模型 + Repo接口)
│   ├── data/             # 数据访问 (Repo实现 + GORM)
│   ├── service/          # 服务实现 (Proto ↔ Biz 转换)
│   ├── server/           # HTTP/gRPC Server
│   └── conf/             # 配置结构体
├── third_party/          # 第三方 Proto
└── testdata/             # 测试数据
```

---

## 构建、测试和开发命令

```bash
# 安装 protoc 插件
make init

# 生成 API 代码 (pb.go, grpc.pb.go, http.pb.go)
make api

# 生成内部 proto 绑定
make config

# 构建项目
make build

# 生成所有代码 + go mod tidy
make generate

# 运行测试
go test ./...
```

---

## 代码风格与命名约定

- 使用 `gofmt`/`goimports` 格式化代码
- 包名使用小写，可用下划线（如 `internal/task_queue`）
- Protobuf 消息和服务使用 PascalCase
- 配置结构体使用描述性后缀（`ServerConfig`, `DataConfig`）

### 命名规范

| 类型 | 命名规则 | 示例 |
|------|----------|------|
| 领域模型 | PascalCase | `Product`, `Order` |
| Repo 接口 | 以 Repo 结尾 | `ProductRepo`, `OrderRepo` |
| 构造函数 | New 前缀 | `NewProductUsecase` |
| PO 模型 | 小写 + PO 后缀 | `productPO`, `orderPO` |

---

## 分层架构规范

```
┌─────────────────────────────────────────────────────┐
│  API Layer (api/)                                   │
│  - Proto 定义契约                                    │
└─────────────────────────────────────────────────────┘
                         ↓
┌─────────────────────────────────────────────────────┐
│  Service Layer (internal/service/)                  │
│  - 实现 RPC 方法，调用 Biz UseCase                   │
│  - 禁止直接调用 Data 层                              │
└─────────────────────────────────────────────────────┘
                         ↓
┌─────────────────────────────────────────────────────┐
│  Biz Layer (internal/biz/)                          │
│  - 领域模型 + Repo 接口                              │
│  - 禁止 import gorm/sql                             │
└─────────────────────────────────────────────────────┘
                         ↓
┌─────────────────────────────────────────────────────┐
│  Data Layer (internal/data/)                        │
│  - 实现 Repo 接口                                    │
│  - GORM 操作 + 缓存管理                              │
└─────────────────────────────────────────────────────┘
```

---

## 测试指南

- 使用 table-driven 单元测试，包含 `name`, `input`, `want` 字段
- 测试数据存放在 `testdata/` 目录
- 集成测试使用 in-memory 适配器，避免外部依赖
- 关键路径（订单创建、支付流程）必须有测试覆盖

---

## 提交与合并请求指南

- 遵循 Conventional Commits（`feat:`, `fix:`, `chore:`）
- PR 需说明问题、方案、验证命令
- 确保生成文件已更新（`make api`, `make config`）
- 不要混合无关重构和功能开发

---

## 开发约束

### 行为约束
- **禁止拉取 Gocache**: 严禁提交缓存制品到版本控制
- **写操作管控**: 文件写入前需明确列出变更计划
- **禁止提供数据SQL文件**

### 代码风格
- **框架**: Kratos v2 + Google Wire + GORM
- **日志**: 使用 `github.com/go-kratos/kratos/v2/log`
- **错误处理**: 优先处理 `err != nil`
- **上下文**: `context.Context` 作为第一个参数

---

## 文档同步规范

> **重要**: `AGENTS.md` 和 `CLAUDE.md` 必须保持同步更新。

| 文件 | 侧重内容 |
|------|----------|
| `AGENTS.md` | 项目规范、Agent 开发指南、代码风格约定 |
| `CLAUDE.md` | Kratos API 开发流程、快捷命令、检查清单 |

**同步规则**:
- 修改任一文件的规范/流程/约束时，必须同时更新另一个文件
- 共同部分（分层架构、领域模型）必须保持一致

# Product Service

Product 服务是 RunAll 平台的商品域微服务，负责商品管理和订单处理。

## 系统架构

本服务是 RunAll 平台的一部分，采用 DDD 分层架构：

```
┌─────────────────────────────────────────────────────────────┐
│                      RunAll Platform                        │
├─────────────┬─────────────────┬─────────────────────────────┤
│ User Domain │ Product Domain  │      Resource Domain        │
│  (用户域)    │   (商品域)       │        (资源域)              │
│             │   ← 本服务 →     │                             │
└─────────────┴─────────────────┴─────────────────────────────┘
```

## 领域职责

Product Domain 负责：
- **商品管理**: 商品的 CRUD、上下架、规格配置
- **订单管理**: 订单创建、支付、取消、完成
- **实例协调**: 生成实例ID，发送创建事件给资源域

### 核心概念

- **Product (商品)**: 可售卖的套餐/SKU，定义了规格和价格
- **Instance (实例)**: 用户购买商品后，由资源域创建的实际运行资源（K8s Pod）

### 领域模型

**Product (商品聚合根)**
| 字段 | 类型 | 说明 |
|------|------|------|
| ProductID | int64 | 主键（雪花ID） |
| Name | string | 商品名称 |
| Description | string | 商品描述 |
| Status | enum | ENABLED / DISABLED |
| SpecID | int64 | 关联规格ID |
| Price | int64 | 价格（分） |

**Order (订单聚合根)**
| 字段 | 类型 | 说明 |
|------|------|------|
| OrderID | int64 | 雪花ID |
| UserID | UUID | 用户ID |
| ProductID | int64 | 商品ID |
| ReqID | int64 | Redis生成的请求号（幂等/判重） |
| InstanceID | int64 | 实例ID（支付后填充） |
| Status | enum | PENDING / PAID / CANCELLED / COMPLETED |
| Amount | int64 | 金额（分） |

### 数据库表结构

```sql
-- 商品规格表
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
```

## 技术栈

- **框架**: Kratos v2
- **依赖注入**: Google Wire
- **数据库**: PostgreSQL + GORM
- **缓存**: Redis
- **协议**: gRPC + HTTP (RESTful)

## 快速开始

### 环境准备

```bash
# 安装依赖工具
make init
```

### 生成代码

```bash
# 生成 API 代码
make api

# 生成 Wire 依赖注入
cd cmd/product && wire

# 生成所有代码
make generate
```

### 构建运行

```bash
# 构建
make build

# 运行
./bin/product -conf ./configs/config.yaml
```

### Docker 部署

```bash
# 构建镜像
docker build -t product-service .

# 运行容器
docker run --rm -p 8000:8000 -p 9000:9000 \
  -v ./configs:/data/conf \
  product-service
```

## 项目结构

```
.
├── api/product/v1/       # Proto 定义
├── cmd/product/          # 入口 + Wire
├── configs/              # 配置文件
├── internal/
│   ├── biz/              # 业务逻辑 (领域层)
│   ├── data/             # 数据访问 (基础设施层)
│   ├── service/          # 服务实现 (应用层)
│   ├── server/           # HTTP/gRPC Server
│   └── conf/             # 配置结构体
└── third_party/          # 第三方 Proto
```

## 服务端口

| 协议 | 端口 | 说明 |
|------|------|------|
| HTTP | 8000 | RESTful API |
| gRPC | 9000 | gRPC 服务 |

## 相关文档

- [CLAUDE.md](./CLAUDE.md) - API 开发流程指南
- [AGENTS.md](./AGENTS.md) - Agent 开发规范

## License

MIT License

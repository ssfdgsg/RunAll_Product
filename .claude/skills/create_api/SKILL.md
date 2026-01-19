# Skill: create_api

## 描述
创建新的 Kratos API，遵循项目的 DDD 分层架构和标准开发流程。

## 触发条件
当用户请求创建新的 API、添加新接口、新增服务端点时触发。

## 执行步骤

### Step 1: Proto 定义
在 `api/<service>/v1/<service>.proto` 中定义：

```protobuf
syntax = "proto3";

package <service>.v1;

import "google/api/annotations.proto";

option go_package = "product/api/<service>/v1;<service>v1";

service <Service>Service {
  rpc Get<Entity> (Get<Entity>Req) returns (Get<Entity>Reply) {
    option (google.api.http) = {
      get: "/v1/<entities>/{id}"
    };
  }
  
  rpc List<Entity> (List<Entity>Req) returns (List<Entity>Reply) {
    option (google.api.http) = {
      get: "/v1/<entities>"
    };
  }
  
  rpc Create<Entity> (Create<Entity>Req) returns (Create<Entity>Reply) {
    option (google.api.http) = {
      post: "/v1/<entities>"
      body: "*"
    };
  }
}
```

**规范**:
- HTTP 路由必须包含 `/v1/` 版本前缀
- POST/PUT 请求添加 `body: "*"`

### Step 2: 生成代码
```bash
make api
```

### Step 3: Biz 层 (`internal/biz/<entity>.go`)

```go
package biz

import (
    "context"
    "github.com/go-kratos/kratos/v2/log"
)

// <Entity> 领域模型
type <Entity> struct {
    ID   int64
    Name string
}

// <Entity>Repo 数据仓库接口
type <Entity>Repo interface {
    GetByID(ctx context.Context, id int64) (*<Entity>, error)
    List(ctx context.Context) ([]*<Entity>, error)
    Create(ctx context.Context, e *<Entity>) (*<Entity>, error)
}

// <Entity>Usecase 业务用例
type <Entity>Usecase struct {
    repo <Entity>Repo
    log  *log.Helper
}

// New<Entity>Usecase 创建业务用例
func New<Entity>Usecase(repo <Entity>Repo, logger log.Logger) *<Entity>Usecase {
    return &<Entity>Usecase{repo: repo, log: log.NewHelper(logger)}
}
```

**铁律**:
- ✅ 定义 `Repo` 接口
- ✅ 注入 `log.Helper`
- ❌ 禁止 `import "gorm.io/gorm"`

### Step 4: Data 层 (`internal/data/<entity>.go`)

```go
package data

import (
    "context"
    "product/internal/biz"
    "github.com/go-kratos/kratos/v2/log"
)

type <entity>PO struct {
    ID   int64  `gorm:"primaryKey"`
    Name string `gorm:"column:name"`
}

func (<entity>PO) TableName() string { return "<entities>" }

type <entity>Repo struct {
    data *Data
    log  *log.Helper
}

func New<Entity>Repo(data *Data, logger log.Logger) biz.<Entity>Repo {
    return &<entity>Repo{data: data, log: log.NewHelper(logger)}
}

func (r *<entity>Repo) GetByID(ctx context.Context, id int64) (*biz.<Entity>, error) {
    var po <entity>PO
    if err := r.data.db.WithContext(ctx).First(&po, id).Error; err != nil {
        return nil, err
    }
    return &biz.<Entity>{ID: po.ID, Name: po.Name}, nil
}
```

### Step 5: Service 层 (`internal/service/<entity>.go`)

```go
package service

import (
    "context"
    v1 "product/api/<service>/v1"
    "product/internal/biz"
)

type <Entity>Service struct {
    v1.Unimplemented<Service>ServiceServer
    uc *biz.<Entity>Usecase
}

func New<Entity>Service(uc *biz.<Entity>Usecase) *<Entity>Service {
    return &<Entity>Service{uc: uc}
}

func (s *<Entity>Service) Get<Entity>(ctx context.Context, req *v1.Get<Entity>Req) (*v1.Get<Entity>Reply, error) {
    entity, err := s.uc.GetByID(ctx, req.Id)
    if err != nil {
        return nil, err
    }
    return &v1.Get<Entity>Reply{...}, nil
}
```

### Step 6: Server 注册

**gRPC** (`internal/server/grpc.go`):
```go
v1.Register<Service>ServiceServer(srv, <entity>)
```

**HTTP** (`internal/server/http.go`):
```go
v1.Register<Service>ServiceHTTPServer(srv, <entity>)
```

### Step 7: Wire 注入

更新 ProviderSet:
```go
// internal/biz/biz.go
var ProviderSet = wire.NewSet(New<Entity>Usecase, ...)

// internal/data/data.go  
var ProviderSet = wire.NewSet(New<Entity>Repo, ...)

// internal/service/service.go
var ProviderSet = wire.NewSet(New<Entity>Service, ...)
```

生成注入代码:
```bash
cd cmd/product && wire
```

### Step 8: 验证
```bash
make generate
make build
```

## 检查清单

| # | 文件 | 操作 |
|---|------|------|
| 1 | `api/<svc>/v1/*.proto` | 定义 service + rpc + message |
| 2 | - | `make api` |
| 3 | `internal/biz/<entity>.go` | 领域模型 + Repo 接口 + UseCase |
| 4 | `internal/biz/biz.go` | 添加到 ProviderSet |
| 5 | `internal/data/<entity>.go` | 实现 Repo 接口 |
| 6 | `internal/data/data.go` | 添加到 ProviderSet |
| 7 | `internal/service/<entity>.go` | 实现 Service |
| 8 | `internal/service/service.go` | 添加到 ProviderSet |
| 9 | `internal/server/grpc.go` | 注册 gRPC 服务 |
| 10 | `internal/server/http.go` | 注册 HTTP 服务 |
| 11 | - | `make generate` |

## 注意事项

- 严格遵循分层依赖：Service → Biz → Data
- Service 层禁止直接调用 Data 层
- Biz 层禁止引入 gorm/sql 依赖
- 所有导出类型和函数必须有注释

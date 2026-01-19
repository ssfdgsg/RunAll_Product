# Random - 高性能随机数生成工具库

## 快速开始

```go
import "your-project/pkg/random"

// 创建生成器
rng := random.NewXorShift64Star(12345)

// 生成随机数
val := rng.Rand3Bits()  // 0..7
```

## API 文档

### 创建生成器

```go
rng := random.NewXorShift64Star(seed uint64)
```

- `seed`: 种子值，传 0 使用默认种子
- 每个生成器实例独立，可并发使用多个实例

### 核心方法

#### RandNBits(bits uint8) uint64
生成指定位数的随机数

```go
val := rng.RandNBits(3)   // 0..7 (2^3-1)
val := rng.RandNBits(10)  // 0..1023 (2^10-1)
val := rng.RandNBits(16)  // 0..65535 (2^16-1)
```

#### 快捷方法

```go
val := rng.Rand3Bits()   // uint8:  0..7
val := rng.Rand8Bits()   // uint8:  0..255
val := rng.Rand16Bits()  // uint16: 0..65535
val := rng.Rand32Bits()  // uint32: 0..4294967295
val := rng.Rand64Bits()  // uint64: 全范围
```

#### RandRange(min, max uint64) uint64
生成指定范围的随机数 [min, max)

```go
val := rng.RandRange(10, 100)  // [10, 100)
val := rng.RandRange(0, 8)     // [0, 8) 等价于 Rand3Bits()
```

## 使用场景

### 1. 实例 ID 生成

```go
import "your-project/pkg/random"

type IDGenerator struct {
    rng *random.XorShift64Star
}

func NewIDGenerator(seed uint64) *IDGenerator {
    return &IDGenerator{
        rng: random.NewXorShift64Star(seed),
    }
}

func (g *IDGenerator) GenerateID() int64 {
    // 使用 3 位随机数作为 ID 的一部分
    randomPart := g.rng.Rand3Bits()
    // ... 组合其他部分
    return id
}
```

### 2. 负载均衡

```go
// 随机选择服务器
serverIndex := rng.RandRange(0, uint64(len(servers)))
server := servers[serverIndex]
```

### 3. 采样和抽样

```go
// 10% 采样率
if rng.RandRange(0, 100) < 10 {
    // 执行采样逻辑
}
```

## 性能特点

- **极快**: XorShift 算法，比标准库 `math/rand` 快数倍
- **轻量**: 仅 8 字节状态
- **无锁**: 每个实例独立，无需加锁

## 注意事项

⚠️ **不适用于加密场景**

此生成器是伪随机数生成器，不具备密码学安全性。如需加密级随机数，请使用 `crypto/rand`。

## 测试

```bash
# 运行测试
go test ./pkg/random -v

# 性能测试
go test ./pkg/random -bench=. -benchmem
```

## 算法说明

XorShift64* 是 XorShift 系列算法的变体，具有以下特点：

- 周期: 2^64 - 1
- 状态空间: 64 位
- 通过 BigCrush 测试套件
- 适合高频调用场景

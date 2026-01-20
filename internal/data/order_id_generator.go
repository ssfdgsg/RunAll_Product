package data

import (
	"context"
	"time"

	"product/internal/biz"

	"github.com/cespare/xxhash/v2"
	"github.com/go-kratos/kratos/v2/log"
)

type orderIDGenerator struct {
	log *log.Helper
}

// NewOrderIDGenerator 创建订单 ID 生成器
func NewOrderIDGenerator(logger log.Logger) biz.OrderIDGenerator {
	return &orderIDGenerator{
		log: log.NewHelper(logger),
	}
}

// Generate 生成订单 ID
// 结构：[0(1位)][timestamp(46位)][uid_hash(15位)]
// - 最高位为0，保证为正数
// - 接下来46位为时间戳（从 EpochMsStart 开始的毫秒数）
// - 最后15位为用户ID的哈希值
func (g *orderIDGenerator) Generate(ctx context.Context, userID string) (int64, error) {
	nowMs := time.Now().UnixMilli()

	// 计算相对时间戳（46位）
	relativeTime := (nowMs - EpochMsStart) & 0x3FFFFFFFFFFF // 取低46位

	// 计算用户ID哈希（15位）
	uidHash := int64(xxhash.Sum64String(userID) & 0x7FFF) // 取低15位

	// 组合：timestamp左移15位 | uid_hash
	// 最高位自动为0（因为我们只使用了61位：46+15）
	orderID := (relativeTime << 15) | uidHash

	return orderID, nil
}

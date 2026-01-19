package data

import (
	"context"
	"sync/atomic"
	"time"

	"product/internal/biz"

	"github.com/cespare/xxhash/v2"
	"github.com/go-kratos/kratos/v2/log"
)

// EpochMsStart 时间起点为 +UTC 2026-01-18 00:00:00
const EpochMsStart = 1768665600000

type instanceIDGenerator struct {
	log *log.Helper
}

// NewInstanceIDGenerator 创建实例 ID 生成器
func NewInstanceIDGenerator(logger log.Logger) biz.InstanceIDGenerator {
	return &instanceIDGenerator{
		log: log.NewHelper(logger),
	}
}

// Generate 生成实例 ID
func (g *instanceIDGenerator) Generate(ctx context.Context, uuid string) (int64, error) {
	nowMs := time.Now().UnixMilli()
	timeNow := (nowMs - EpochMsStart) << 3
	// 只取低 15 位作为 uid，确保左移 48 位后不会影响符号位
	uid := int64(xxhash.Sum64String(uuid)&0x7FFF) << 48
	random := RandomBit3()
	id := uid | timeNow | random
	return id, nil
}

func RandomBit3() int64 {
	p := atomic.Int64{}
	// Add returns the new value; subtract 1 to make first result 0.
	n := p.Add(1) - 1
	return int64(n & 7)
}

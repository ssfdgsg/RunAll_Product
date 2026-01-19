package service

import (
	"context"
	"product/internal/biz"

	"github.com/go-kratos/kratos/v2/log"
)

// SeckillOrderService 秒杀订单服务
type SeckillOrderService struct {
	uc        *biz.InstanceUsecase
	productID int64 // 当前服务处理的商品 ID
	log       *log.Helper
}

// NewSeckillOrderService 创建秒杀订单服务
func NewSeckillOrderService(uc *biz.InstanceUsecase, productID int64, logger log.Logger) *SeckillOrderService {
	return &SeckillOrderService{
		uc:        uc,
		productID: productID,
		log:       log.NewHelper(logger),
	}
}

// HandleSeckillOrder 处理秒杀订单（从 Stream 消费）
// streamID: Redis Stream 消息 ID（转换为 reqID 使用）
// uid: 用户 ID（UUID 字符串）
func (s *SeckillOrderService) HandleSeckillOrder(ctx context.Context, streamID string, uid string) error {
	s.log.Infof("handling seckill order: streamID=%s uid=%s", streamID, uid)

	// 将 streamID 转换为 int64 作为 reqID
	reqID := hashStreamID(streamID)
	if err := s.uc.CreateInstance(ctx, s.productID, uid, reqID); err != nil {
		s.log.Errorf("create instance failed: %v", err)
		return err
	}
	return nil
}

// hashStreamID 将 Redis Stream ID 转换为 int64
// Stream ID 格式：timestamp-sequence (如 "1609459200000-0")
func hashStreamID(streamID string) int64 {
	var hash int64
	for i := 0; i < len(streamID); i++ {
		hash = hash*31 + int64(streamID[i])
	}
	if hash < 0 {
		hash = -hash
	}
	return hash
}

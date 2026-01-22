package service

import (
	"context"
	"strings"
	
	"product/internal/biz"

	"github.com/go-kratos/kratos/v2/log"
)

// SeckillOrderService 秒杀订单服务
type SeckillOrderService struct {
	orderUC   *biz.OrderUsecase
	productID int64 // 当前服务处理的商品 ID
	log       *log.Helper
}

// NewSeckillOrderService 创建秒杀订单服务
func NewSeckillOrderService(orderUC *biz.OrderUsecase, productID int64, logger log.Logger) *SeckillOrderService {
	return &SeckillOrderService{
		orderUC:   orderUC,
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
	_, _, err := s.orderUC.CreateOrderFromSeckill(ctx, s.productID, uid, reqID)
	if err != nil {
		// 检查是否是唯一约束冲突（订单已存在）
		if isUniqueViolationError(err) {
			s.log.Warnf("order already exists (idempotent): streamID=%s uid=%s reqID=%d", streamID, uid, reqID)
			// 订单已存在，视为成功，返回 nil 以便 ACK 消息
			return nil
		}
		s.log.Errorf("create order failed: %v", err)
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

// isUniqueViolationError 检查是否是唯一约束冲突错误
func isUniqueViolationError(err error) bool {
	if err == nil {
		return false
	}
	errMsg := err.Error()
	return strings.Contains(errMsg, "23505") || 
	       strings.Contains(errMsg, "duplicate key") || 
	       strings.Contains(errMsg, "uq_orders_product_req")
}

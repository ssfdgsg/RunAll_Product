package service

import (
	"context"

	pb "product/api/product/v1"
	"product/internal/biz"

	"github.com/go-kratos/kratos/v2/log"
)

// SeckillService 秒杀管理服务（gRPC）
type SeckillService struct {
	pb.UnimplementedSeckillServiceServer

	uc  *biz.SeckillUsecase
	log *log.Helper
}

// NewSeckillService 创建秒杀管理服务
func NewSeckillService(uc *biz.SeckillUsecase, logger log.Logger) *SeckillService {
	return &SeckillService{
		uc:  uc,
		log: log.NewHelper(logger),
	}
}

// InitSeckill 初始化秒杀活动
func (s *SeckillService) InitSeckill(ctx context.Context, req *pb.InitSeckillReq) (*pb.InitSeckillReply, error) {
	s.log.Infof("init seckill: product_id=%d stock=%d", req.ProductId, req.Stock)

	if err := s.uc.InitSeckill(ctx, req.ProductId, req.Stock); err != nil {
		s.log.Errorf("init seckill failed: %v", err)
		return &pb.InitSeckillReply{
			Success: false,
			Message: err.Error(),
		}, nil
	}

	return &pb.InitSeckillReply{
		Success: true,
		Message: "秒杀活动初始化成功",
	}, nil
}

// GetCurrentSeckill 获取当前秒杀信息
func (s *SeckillService) GetCurrentSeckill(ctx context.Context, req *pb.GetCurrentSeckillReq) (*pb.GetCurrentSeckillReply, error) {
	productID, stock, err := s.uc.GetCurrentSeckill(ctx)
	if err != nil {
		// 如果没有活跃的秒杀，返回 active=false
		if err == biz.ErrNoActiveSeckill {
			return &pb.GetCurrentSeckillReply{
				Active: false,
			}, nil
		}
		s.log.Errorf("get current seckill failed: %v", err)
		return nil, err
	}

	return &pb.GetCurrentSeckillReply{
		ProductId: productID,
		Stock:     stock,
		Active:    true,
	}, nil
}

// ClearSeckill 清空秒杀数据
func (s *SeckillService) ClearSeckill(ctx context.Context, req *pb.ClearSeckillReq) (*pb.ClearSeckillReply, error) {
	s.log.Info("clearing seckill data")

	if err := s.uc.ClearSeckill(ctx); err != nil {
		s.log.Errorf("clear seckill failed: %v", err)
		return &pb.ClearSeckillReply{
			Success: false,
			Message: err.Error(),
		}, nil
	}

	return &pb.ClearSeckillReply{
		Success: true,
		Message: "秒杀数据已清空",
	}, nil
}

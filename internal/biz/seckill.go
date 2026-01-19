package biz

import (
	"context"

	"github.com/go-kratos/kratos/v2/log"
)

// SeckillProductRepo 秒杀商品仓储接口
type SeckillProductRepo interface {
	// InitSeckill 初始化秒杀（清空上次数据并设置新的 productID 和库存）
	// 同一时间只有一个秒杀商品，productID 作为全局变量存储在 Redis
	InitSeckill(ctx context.Context, productID int64, stock int32) error

	// GetCurrentProductID 获取当前秒杀商品ID
	GetCurrentProductID(ctx context.Context) (int64, error)

	// GetStock 获取当前库存
	GetStock(ctx context.Context) (int32, error)

	// ClearSeckill 清空秒杀数据
	ClearSeckill(ctx context.Context) error
}

// SeckillUsecase 秒杀业务用例
type SeckillUsecase struct {
	repo SeckillProductRepo
	log  *log.Helper
}

// NewSeckillUsecase 创建秒杀业务用例
func NewSeckillUsecase(repo SeckillProductRepo, logger log.Logger) *SeckillUsecase {
	return &SeckillUsecase{
		repo: repo,
		log:  log.NewHelper(logger),
	}
}

// InitSeckill 初始化秒杀（管理员操作）
// 清空上次秒杀数据并设置新的商品和库存
func (uc *SeckillUsecase) InitSeckill(ctx context.Context, productID int64, stock int32) error {
	// 验证库存
	if stock <= 0 {
		return ErrInvalidStock
	}

	uc.log.Infof("initializing seckill: productID=%d, stock=%d", productID, stock)
	return uc.repo.InitSeckill(ctx, productID, stock)
}

// GetCurrentSeckill 获取当前秒杀信息
func (uc *SeckillUsecase) GetCurrentSeckill(ctx context.Context) (productID int64, stock int32, err error) {
	productID, err = uc.repo.GetCurrentProductID(ctx)
	if err != nil {
		return 0, 0, err
	}

	stock, err = uc.repo.GetStock(ctx)
	if err != nil {
		return 0, 0, err
	}

	return productID, stock, nil
}

// ClearSeckill 清空秒杀数据（管理员操作）
func (uc *SeckillUsecase) ClearSeckill(ctx context.Context) error {
	uc.log.Info("clearing seckill data")
	return uc.repo.ClearSeckill(ctx)
}

// 错误定义
var (
	ErrInvalidStock    = &BizError{Code: 400, Message: "invalid stock: must be greater than 0"}
	ErrNoActiveSeckill = &BizError{Code: 404, Message: "no active seckill"}
)

// BizError 业务错误
type BizError struct {
	Code    int
	Message string
}

func (e *BizError) Error() string {
	return e.Message
}

package data

import (
	"context"
	"fmt"
	"strconv"

	"product/internal/biz"

	"github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/redis/go-redis/v9"
)

const (
	// Redis Keys - 与 BFF 层保持一致
	keyStock         = "seckill:stock"      // 库存
	keyReqSeq        = "req:seq"            // 请求序列号（注意：不是 seckill:req_seq）
	keyUID2ReqHash   = "uid2req"            // 用户ID -> 请求号映射（注意：无前缀）
	keyStreamOrders  = "stream:orders"      // 订单流（注意：不是 seckill:stream_orders）
	keyCurrentProdID = "seckill:product_id" // 当前秒杀商品ID
)

// seckillProductRepo 秒杀商品仓储实现
type seckillProductRepo struct {
	data *Data
	log  *log.Helper
}

// NewSeckillProductRepo 创建秒杀商品仓储
func NewSeckillProductRepo(data *Data, logger log.Logger) biz.SeckillProductRepo {
	return &seckillProductRepo{
		data: data,
		log:  log.NewHelper(logger),
	}
}

// InitSeckill 初始化秒杀（清空上次数据并设置新的 productID 和库存）
func (r *seckillProductRepo) InitSeckill(ctx context.Context, productID int64, stock int32) error {
	if r.data.redis == nil {
		return fmt.Errorf("redis client is not initialized")
	}

	// 使用 Pipeline 批量执行命令
	pipe := r.data.redis.Pipeline()

	// 1. 删除旧数据
	pipe.Del(ctx, keyStock)
	pipe.Del(ctx, keyReqSeq)
	pipe.Del(ctx, keyUID2ReqHash)
	pipe.Del(ctx, keyCurrentProdID)

	// 注意：不删除 Stream，因为消费者组依赖它
	// 如果需要清空 Stream，应该先删除消费者组

	// 2. 设置新数据
	pipe.Set(ctx, keyCurrentProdID, productID, 0) // 不过期
	pipe.Set(ctx, keyStock, stock, 0)
	pipe.Set(ctx, keyReqSeq, 0, 0) // 初始化请求序列号为 0

	// 执行 Pipeline
	_, err := pipe.Exec(ctx)
	if err != nil {
		r.log.Errorf("failed to init seckill: %v", err)
		return err
	}

	// 3. 初始化 Stream（如果不存在）
	// 使用 XADD 添加一个占位消息，然后立即删除
	// 这样可以确保 Stream 存在，方便后续创建消费者组
	streamExists, err := r.data.redis.Exists(ctx, keyStreamOrders).Result()
	if err != nil {
		r.log.Errorf("failed to check stream existence: %v", err)
		return err
	}

	if streamExists == 0 {
		// Stream 不存在，创建一个空的 Stream
		// 使用 XGROUP CREATE MKSTREAM 会自动创建 Stream
		r.log.Infof("stream does not exist, will be created by consumer group")
	}

	r.log.Infof("seckill initialized: productID=%d, stock=%d", productID, stock)
	return nil
}

// GetCurrentProductID 获取当前秒杀商品ID
func (r *seckillProductRepo) GetCurrentProductID(ctx context.Context) (int64, error) {
	if r.data.redis == nil {
		return 0, fmt.Errorf("redis client is not initialized")
	}

	val, err := r.data.redis.Get(ctx, keyCurrentProdID).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return 0, biz.ErrNoActiveSeckill
		}
		return 0, err
	}

	productID, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid product_id format: %v", err)
	}

	return productID, nil
}

// GetStock 获取当前库存
func (r *seckillProductRepo) GetStock(ctx context.Context) (int32, error) {
	if r.data.redis == nil {
		return 0, fmt.Errorf("redis client is not initialized")
	}

	val, err := r.data.redis.Get(ctx, keyStock).Result()
	if err != nil {
		if err == redis.Nil {
			return 0, nil
		}
		return 0, err
	}

	stock, err := strconv.ParseInt(val, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid stock format: %v", err)
	}

	return int32(stock), nil
}

// ClearSeckill 清空秒杀数据
func (r *seckillProductRepo) ClearSeckill(ctx context.Context) error {
	if r.data.redis == nil {
		return fmt.Errorf("redis client is not initialized")
	}

	// 删除所有秒杀相关的 Key
	keys := []string{
		keyStock,
		keyReqSeq,
		keyUID2ReqHash,
		keyStreamOrders,
		keyCurrentProdID,
	}

	err := r.data.redis.Del(ctx, keys...).Err()
	if err != nil {
		r.log.Errorf("failed to clear seckill: %v", err)
		return err
	}

	r.log.Info("seckill data cleared")
	return nil
}

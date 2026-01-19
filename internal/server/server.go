package server

import (
	"context"
	"product/internal/biz"
	"product/internal/conf"
	"product/internal/service"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/transport"
	"github.com/google/wire"
)

// ProviderSet is server providers.
var ProviderSet = wire.NewSet(
	NewGRPCServer,
	NewHTTPServer,
	NewRedisServer,
	NewSeckillStreamServers,
)

// NewSeckillStreamServers 创建秒杀 Stream 服务器（从 Redis 获取当前秒杀商品）
func NewSeckillStreamServers(
	c *conf.Server,
	rs *RedisServer,
	seckillUc *biz.SeckillUsecase,
	instanceUc *biz.InstanceUsecase,
	logger log.Logger,
) []transport.Server {
	helper := log.NewHelper(logger)

	if rs == nil || rs.Client() == nil {
		helper.Warn("redis not available, skip seckill stream servers")
		return nil
	}

	// 从 Redis 获取当前秒杀商品
	ctx := context.Background()
	productID, _, err := seckillUc.GetCurrentSeckill(ctx)
	if err != nil {
		if err == biz.ErrNoActiveSeckill {
			helper.Info("no active seckill, skip stream server creation")
			return nil
		}
		helper.Errorf("failed to get current seckill: %v", err)
		return nil
	}

	// 创建秒杀 Stream 服务器
	var servers []transport.Server
	rdb := rs.Client()

	handler := service.NewSeckillOrderService(instanceUc, productID, logger)
	server := NewSeckillStreamServer(rdb, logger, handler, productID)
	servers = append(servers, server)

	helper.Infof("created seckill stream server for product: %d", productID)
	return servers
}

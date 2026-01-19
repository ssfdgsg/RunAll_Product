package server

import (
	"context"
	"errors"
	"fmt"
	"product/internal/conf"
	"sync"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/transport"
	"github.com/redis/go-redis/v9"
)

var (
	redisClientInstance *redis.Client
	redisOnce           sync.Once
)

// RedisServer Redis 服务器包装
type RedisServer struct {
	client *redis.Client
	log    *log.Helper
}

// NewRedisServer 创建 Redis 服务器实例
func NewRedisServer(c *conf.Data, logger log.Logger) *RedisServer {
	helper := log.NewHelper(log.With(logger, "module", "server/redis"))

	if c.GetRedis() == nil || c.GetRedis().GetAddr() == "" {
		helper.Warn("redis configuration is missing, redis server not initialized")
		return nil
	}

	var initErr error
	redisOnce.Do(func() {
		redisClientInstance = redis.NewClient(&redis.Options{
			Addr:         c.GetRedis().GetAddr(),
			Password:     c.GetRedis().GetPassword(),
			DB:           int(c.GetRedis().GetDb()),
			ReadTimeout:  c.GetRedis().GetReadTimeout().AsDuration(),
			WriteTimeout: c.GetRedis().GetWriteTimeout().AsDuration(),
		})

		// 测试连接
		ctx := context.Background()
		if err := redisClientInstance.Ping(ctx).Err(); err != nil {
			helper.Errorf("failed to connect to redis: %v", err)
			initErr = err
			redisClientInstance = nil
			return
		}

		helper.Info("redis client initialized successfully (singleton)")
	})

	if initErr != nil || redisClientInstance == nil {
		return nil
	}

	return &RedisServer{
		client: redisClientInstance,
		log:    helper,
	}
}

// Start 启动 Redis 服务器（实现 Kratos Server 接口）
func (s *RedisServer) Start(ctx context.Context) error {
	if s == nil || s.client == nil {
		return nil
	}
	s.log.Info("redis server started")
	return nil
}

// Stop 停止 Redis 服务器（实现 Kratos Server 接口）
func (s *RedisServer) Stop(ctx context.Context) error {
	if s == nil || s.client == nil {
		return nil
	}
	s.log.Info("redis server stopping")
	return s.client.Close()
}

// Client 获取 Redis 客户端
func (s *RedisServer) Client() *redis.Client {
	if s == nil {
		return nil
	}
	return s.client
}

// ============================================================================
// Seckill Stream Server 秒杀服务区
// ============================================================================

// SeckillStreamHandler 秒杀 Stream 消息处理器接口
type SeckillStreamHandler interface {
	// HandleSeckillOrder 处理秒杀订单
	HandleSeckillOrder(ctx context.Context, streamID string, uid string) error
}

// SeckillStreamServer 秒杀 Stream 消费服务器
type SeckillStreamServer struct {
	rdb       *redis.Client
	productID int64
	stream    string
	group     string
	consumer  string
	block     time.Duration
	count     int64
	claimIdle time.Duration
	handler   SeckillStreamHandler
	log       *log.Helper
	cancel    context.CancelFunc
	wg        sync.WaitGroup
}

var _ transport.Server = (*SeckillStreamServer)(nil)

// NewSeckillStreamServer 创建秒杀 Stream 服务器
func NewSeckillStreamServer(
	rdb *redis.Client,
	logger log.Logger,
	handler SeckillStreamHandler,
	productID int64,
) transport.Server {
	// 使用固定的 stream key，与 BFF 层保持一致
	stream := "stream:orders"
	group := "g1"
	consumer := fmt.Sprintf("seckill-consumer-%d", productID)

	return &SeckillStreamServer{
		rdb:       rdb,
		productID: productID,
		stream:    stream,
		group:     group,
		consumer:  consumer,
		block:     2 * time.Second,
		count:     128,
		claimIdle: 10 * time.Second, // 10秒后重新认领
		handler:   handler,
		log:       log.NewHelper(logger),
	}
}

func (s *SeckillStreamServer) Start(ctx context.Context) error {
	if s.handler == nil {
		return fmt.Errorf("seckill handler is nil")
	}

	// 确保消费者组存在
	if err := s.ensureGroup(ctx); err != nil {
		return err
	}

	runCtx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel

	// 启动消费循环
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.consumeLoop(runCtx)
	}()

	// 启动重新认领循环
	if s.claimIdle > 0 {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.reclaimLoop(runCtx)
		}()
	}

	s.log.Infof("seckill stream server started: productID=%d stream=%s group=%s consumer=%s",
		s.productID, s.stream, s.group, s.consumer)
	return nil
}

func (s *SeckillStreamServer) Stop(ctx context.Context) error {
	if s.cancel != nil {
		s.cancel()
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.wg.Wait()
	}()

	select {
	case <-done:
		s.log.Infof("seckill stream server stopped: productID=%d", s.productID)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ensureGroup 确保消费者组存在
func (s *SeckillStreamServer) ensureGroup(ctx context.Context) error {
	// 使用 XGroupCreateMkStream 会自动创建 Stream（如果不存在）
	err := s.rdb.XGroupCreateMkStream(ctx, s.stream, s.group, "0").Err()
	if err != nil {
		// 如果消费者组已存在，忽略错误
		if err.Error() == "BUSYGROUP Consumer Group name already exists" {
			s.log.Infof("consumer group already exists: stream=%s group=%s", s.stream, s.group)
			return nil
		}
		s.log.Errorf("failed to create consumer group: %v", err)
		return err
	}
	s.log.Infof("consumer group created: stream=%s group=%s", s.stream, s.group)
	return nil
}

// consumeLoop 消费循环
func (s *SeckillStreamServer) consumeLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		res, err := s.rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    s.group,
			Consumer: s.consumer,
			Streams:  []string{s.stream, ">"},
			Count:    s.count,
			Block:    s.block,
		}).Result()

		if err != nil {
			if errors.Is(err, redis.Nil) || errors.Is(err, context.Canceled) {
				continue
			}
			s.log.Errorf("XReadGroup error: %v", err)
			time.Sleep(200 * time.Millisecond)
			continue
		}

		for _, strm := range res {
			for _, msg := range strm.Messages {
				uid := fmt.Sprint(msg.Values["uid"])
				if uid == "" || uid == "<nil>" {
					s.log.Warnf("missing uid field, msgID=%s values=%v", msg.ID, msg.Values)
					continue
				}

				// 业务交付处理
				if err := s.handler.HandleSeckillOrder(ctx, msg.ID, uid); err != nil {
					s.log.Errorf("handle failed, keep pending: streamID=%s uid=%s err=%v", msg.ID, uid, err)
					continue
				}

				// 确认消息
				if _, err := s.rdb.XAck(ctx, s.stream, s.group, msg.ID).Result(); err != nil {
					s.log.Errorf("XAck failed: msgID=%s err=%v", msg.ID, err)
				}
			}
		}
	}
}

// reclaimLoop 重新认领超时消息
func (s *SeckillStreamServer) reclaimLoop(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	start := "0-0"
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		msgs, next, err := s.rdb.XAutoClaim(ctx, &redis.XAutoClaimArgs{
			Stream:   s.stream,
			Group:    s.group,
			Consumer: s.consumer,
			MinIdle:  s.claimIdle,
			Start:    start,
			Count:    s.count,
		}).Result()

		if err != nil && err != redis.Nil {
			s.log.Errorf("XAutoClaim error: %v", err)
			continue
		}

		start = next
		if len(msgs) == 0 {
			start = "0-0"
			continue
		}

		for _, msg := range msgs {
			uid := fmt.Sprint(msg.Values["uid"])
			if uid == "" || uid == "<nil>" {
				continue
			}

			if err := s.handler.HandleSeckillOrder(ctx, msg.ID, uid); err != nil {
				s.log.Errorf("reclaim handle failed: streamID=%s uid=%s err=%v", msg.ID, uid, err)
				continue
			}

			_, _ = s.rdb.XAck(ctx, s.stream, s.group, msg.ID).Result()
		}
	}
}

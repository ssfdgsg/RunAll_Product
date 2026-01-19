package data

import (
	"errors"
	"product/internal/conf"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/wire"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// ProviderSet is data providers.
var ProviderSet = wire.NewSet(
	NewData,
	NewInstanceRepo,
	NewProductRepo,
	NewOrderRepo,
	NewMQPublisher,
	NewInstanceIDGenerator,
	NewSeckillProductRepo,
)

// Data .
type Data struct {
	db    *gorm.DB
	redis *redis.Client
}

// NewData .
func NewData(c *conf.Data, logger log.Logger) (*Data, func(), error) {
	helper := log.NewHelper(logger)
	if c == nil || c.GetDatabase() == nil || c.GetDatabase().GetSource() == "" {
		return nil, nil, errors.New("database configuration is missing")
	}

	// 初始化数据库
	db, err := gorm.Open(postgres.Open(c.GetDatabase().GetSource()), &gorm.Config{})
	if err != nil {
		return nil, nil, err
	}

	// 初始化 Redis
	var rdb *redis.Client
	if c.GetRedis() != nil && c.GetRedis().GetAddr() != "" {
		rdb = redis.NewClient(&redis.Options{
			Addr:         c.GetRedis().GetAddr(),
			Password:     c.GetRedis().GetPassword(),
			DB:           int(c.GetRedis().GetDb()),
			ReadTimeout:  c.GetRedis().GetReadTimeout().AsDuration(),
			WriteTimeout: c.GetRedis().GetWriteTimeout().AsDuration(),
		})
		helper.Info("redis client initialized")
	}

	cleanup := func() {
		// 关闭数据库连接
		sqlDB, err := db.DB()
		if err != nil {
			helper.Errorf("failed to obtain sql.DB from gorm: %v", err)
			return
		}
		if err := sqlDB.Close(); err != nil {
			helper.Errorf("failed to close database: %v", err)
			return
		}
		helper.Info("database connection closed")

		// 关闭 Redis 连接
		if rdb != nil {
			if err := rdb.Close(); err != nil {
				helper.Errorf("failed to close redis: %v", err)
				return
			}
			helper.Info("redis connection closed")
		}
	}

	return &Data{
		db:    db,
		redis: rdb,
	}, cleanup, nil
}

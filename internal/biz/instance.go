package biz

import (
	"context"
	"fmt"
	"time"

	"github.com/go-kratos/kratos/v2/log"
)

// 注意：InstanceLog 已移除，实例创建日志由资源域（Resource Domain）管理
// 商品域只负责：
// 1. 生成 InstanceID
// 2. 发送 MQ 消息给资源域
// 3. 更新订单的 instance_id 字段

// InstanceSpec 实例规格（用于 MQ 消息）
// 实例是用户购买商品后，由资源域创建的实际运行资源（K8s Pod）
type InstanceSpec struct {
	InstanceID int64  // 实例唯一标识（由商品域生成）
	UserID     string // 用户UUID，与 Resource Domain 一致
	Name       string // 实例名称（来自商品名称）
	CPU        int32  // CPU 核数
	Memory     int32  // 内存大小（MB）
	GPU        int32  // GPU 数量
	Image      string // 容器镜像
	ConfigJSON []byte // 扩展配置（JSON）
}

// InstanceInfo 实例信息（用于查询）
type InstanceInfo struct {
	InstanceID  int64        // 实例ID
	UserID      string       // 用户ID
	OrderID     int64        // 订单ID
	ProductID   int64        // 商品ID
	ProductName string       // 商品名称
	Spec        *ProductSpec // 商品规格
	Status      string       // 实例状态（从订单状态推断）
	CreatedAt   time.Time    // 创建时间
}

// InstanceFilter 实例查询过滤器
type InstanceFilter struct {
	UserID   string // 用户ID过滤
	Status   string // 状态过滤
	Page     uint32 // 页码
	PageSize uint32 // 每页大小
}

// Product 商品聚合根
// 商品是可售卖的套餐/SKU，定义了规格和价格，是交易的标的物
type Product struct {
	ID          int64
	Name        string       // 商品名称（如"基础型实例"、"GPU计算型"）
	Description string       // 商品描述
	Status      string       // ENABLED=上架, DISABLED=下架
	Price       int64        // 商品价格（单位：分）
	SpecID      int64        // 关联规格ID
	Spec        *ProductSpec // 关联的规格（定义实例的资源配置）
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ProductSpec 商品规格（值对象）
// 定义了购买商品后创建的实例的资源配置
type ProductSpec struct {
	ID         int64
	CPU        int32  // CPU 核数
	Memory     int32  // 内存大小（MB）
	GPU        int32  // GPU 数量
	Image      string // 容器镜像（如 "ubuntu:22.04"）
	ConfigJSON []byte // 扩展配置（磁盘、网络等）
	CreatedAt  time.Time
}

// Order 订单聚合根
type Order struct {
	OrderID     int64
	UserID      string // 用户UUID
	ProductID   int64  // 购买的商品ID
	ReqID       int64  // 请求号（秒杀：Redis INCR；正常购买：随机生成），与ProductID组成唯一索引
	Amount      int64  // 订单金额（单位：分）
	InstanceID  *int64 // 实例ID（创建后填充）
	Status      string // PENDING=待支付, PAID=已支付, CANCELLED=已取消, COMPLETED=已完成
	CreatedAt   time.Time
	PaidAt      *time.Time
	CompletedAt *time.Time
}

// MQPublisher MQ 发布器接口
type MQPublisher interface {
	// PublishInstanceCreated 发布实例创建事件
	PublishInstanceCreated(ctx context.Context, spec InstanceSpec) error
}

// InstanceIDGenerator 实例 ID 生成器接口
type InstanceIDGenerator interface {
	// Generate 生成实例 ID
	Generate(ctx context.Context, uuid string) (int64, error)
}

// InstanceRepo 实例仓储接口
type InstanceRepo interface {
	// GetProductByID 获取商品信息（包含规格）
	// 商品定义了可售卖的套餐，规格定义了实例的资源配置
	GetProductByID(ctx context.Context, productID int64) (*Product, error)

	// GetInstanceByID 根据实例ID获取实例信息
	GetInstanceByID(ctx context.Context, instanceID int64) (*InstanceInfo, error)

	// GetInstanceByOrderID 根据订单ID获取实例信息
	GetInstanceByOrderID(ctx context.Context, orderID int64) (*InstanceInfo, error)

	// ListInstances 查询实例列表
	ListInstances(ctx context.Context, filter InstanceFilter) ([]*InstanceInfo, int64, error)
}

// OrderRepo 订单仓储接口
type OrderRepo interface {
	// CreateOrder 创建订单
	CreateOrder(ctx context.Context, order *Order) error

	// GetOrderByID 根据订单ID获取订单
	GetOrderByID(ctx context.Context, orderID int64) (*Order, error)

	// UpdateOrderStatus 更新订单状态
	UpdateOrderStatus(ctx context.Context, orderID int64, status string) error
}

// InstanceUsecase 实例业务逻辑
type InstanceUsecase struct {
	instanceRepo InstanceRepo
	orderRepo    OrderRepo
	mqPublisher  MQPublisher
	idGenerator  InstanceIDGenerator
	log          *log.Helper
}

// NewInstanceUsecase 创建实例用例
func NewInstanceUsecase(
	instanceRepo InstanceRepo,
	orderRepo OrderRepo,
	mqPublisher MQPublisher,
	idGenerator InstanceIDGenerator,
	logger log.Logger,
) *InstanceUsecase {
	return &InstanceUsecase{
		instanceRepo: instanceRepo,
		orderRepo:    orderRepo,
		mqPublisher:  mqPublisher,
		idGenerator:  idGenerator,
		log:          log.NewHelper(logger),
	}
}

// CreateInstance 创建实例（统一入口）
// 支持两种场景：
// 1. 秒杀：reqID 由外部传入（Redis INCR 生成）
// 2. 正常购买：reqID 传 0，内部生成随机大数
func (uc *InstanceUsecase) CreateInstance(ctx context.Context, productID int64, userID string, reqID int64) error {
	uc.log.Infof("creating instance: productID=%d userID=%s reqID=%d", productID, userID, reqID)

	// 1. 如果 reqID 为 0，生成随机 req_id（正常购买场景）
	if reqID == 0 {
		var err error
		reqID, err = uc.idGenerator.Generate(ctx, userID)
		if err != nil {
			uc.log.Errorf("generate req_id failed: %v", err)
			return err
		}
		uc.log.Infof("generated req_id: %d", reqID)
	}

	// 2. 查询商品信息
	product, err := uc.instanceRepo.GetProductByID(ctx, productID)
	if err != nil {
		uc.log.Errorf("get product failed: %v", err)
		return err
	}

	if product.Spec == nil {
		uc.log.Errorf("product spec not found: productID=%d", productID)
		return fmt.Errorf("product spec not found")
	}

	// 3. 生成订单 ID
	orderID, err := uc.idGenerator.Generate(ctx, userID)
	if err != nil {
		uc.log.Errorf("generate order id failed: %v", err)
		return err
	}

	// 4. 生成实例 ID
	instanceID, err := uc.idGenerator.Generate(ctx, userID)
	if err != nil {
		uc.log.Errorf("generate instance id failed: %v", err)
		return err
	}
	uc.log.Infof("generated orderID=%d instanceID=%d", orderID, instanceID)

	// 5. 创建订单（一次性写入所有字段）
	now := time.Now()
	order := &Order{
		OrderID:    orderID,
		UserID:     userID,
		ProductID:  productID,
		ReqID:      reqID,
		Amount:     product.Price,
		InstanceID: &instanceID,
		Status:     "PAID", // 两种场景都是支付完成后才创建订单
		CreatedAt:  now,
		PaidAt:     &now,
	}

	if err := uc.orderRepo.CreateOrder(ctx, order); err != nil {
		uc.log.Errorf("create order failed: %v", err)
		return err
	}
	uc.log.Infof("order created: orderID=%d instanceID=%d reqID=%d", orderID, instanceID, reqID)

	// 6. 发送 MQ 消息给 Resource Domain
	spec := InstanceSpec{
		InstanceID: instanceID,
		UserID:     userID,
		Name:       product.Name,
		CPU:        product.Spec.CPU,
		Memory:     product.Spec.Memory,
		GPU:        product.Spec.GPU,
		Image:      product.Spec.Image,
		ConfigJSON: product.Spec.ConfigJSON,
	}

	if err := uc.mqPublisher.PublishInstanceCreated(ctx, spec); err != nil {
		uc.log.Errorf("publish mq message failed: %v", err)
		return err
	}
	uc.log.Infof("mq message published: instanceID=%d", instanceID)

	uc.log.Infof("instance created: orderID=%d instanceID=%d", orderID, instanceID)
	return nil
}

// CreateInstanceFromSeckill 秒杀场景创建实例
// reqID: Redis INCR 生成的请求号
func (uc *InstanceUsecase) CreateInstanceFromSeckill(ctx context.Context, productID int64, userID string, reqID int64) error {
	return uc.CreateInstance(ctx, productID, userID, reqID)
}

// CreateInstanceFromPayment 正常购买场景创建实例
// 支付成功后调用，内部生成随机 req_id
func (uc *InstanceUsecase) CreateInstanceFromPayment(ctx context.Context, productID int64, userID string) error {
	return uc.CreateInstance(ctx, productID, userID, 0)
}

// GetInstance 获取实例信息
func (uc *InstanceUsecase) GetInstance(ctx context.Context, instanceID int64) (*InstanceInfo, error) {
	return uc.instanceRepo.GetInstanceByID(ctx, instanceID)
}

// GetInstanceByOrder 根据订单ID获取实例信息
func (uc *InstanceUsecase) GetInstanceByOrder(ctx context.Context, orderID int64) (*InstanceInfo, error) {
	return uc.instanceRepo.GetInstanceByOrderID(ctx, orderID)
}

// ListInstances 查询实例列表
func (uc *InstanceUsecase) ListInstances(ctx context.Context, filter InstanceFilter) ([]*InstanceInfo, int64, error) {
	// 设置默认分页参数
	if filter.Page == 0 {
		filter.Page = 1
	}
	if filter.PageSize == 0 {
		filter.PageSize = 20
	}

	return uc.instanceRepo.ListInstances(ctx, filter)
}

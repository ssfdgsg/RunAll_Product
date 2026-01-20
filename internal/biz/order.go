package biz

import (
	"context"
	"errors"
	"time"

	"github.com/go-kratos/kratos/v2/log"
)

var (
	ErrProductNotFound = errors.New("product not found")
	ErrProductDisabled = errors.New("product is disabled")
	ErrInvalidUserID   = errors.New("invalid user id")
)

// ============================================================================
// 实例相关类型定义（Instance 是资源域概念，但商品域需要这些类型用于查询和 MQ）
// ============================================================================

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

// InstanceIDGenerator 实例 ID 生成器接口
type InstanceIDGenerator interface {
	// Generate 生成实例 ID
	Generate(ctx context.Context, uuid string) (int64, error)
}

// InstanceRepo 实例仓储接口（用于查询订单关联的实例信息）
type InstanceRepo interface {
	// GetProductByID 获取商品信息（包含规格）
	GetProductByID(ctx context.Context, productID int64) (*Product, error)

	// GetInstanceByID 根据实例ID获取实例信息
	GetInstanceByID(ctx context.Context, instanceID int64) (*InstanceInfo, error)

	// GetInstanceByOrderID 根据订单ID获取实例信息
	GetInstanceByOrderID(ctx context.Context, orderID int64) (*InstanceInfo, error)

	// ListInstances 查询实例列表
	ListInstances(ctx context.Context, filter InstanceFilter) ([]*InstanceInfo, int64, error)
}

// ============================================================================
// 订单相关类型定义
// ============================================================================

// Order 订单聚合根（与 DDL 对应）
type Order struct {
	ID          int64      // order_id (主键)
	UserID      string     // user_id (UUID)
	ProductID   int64      // product_id
	ReqID       int64      // req_id（请求号，与 product_id 组成唯一索引）
	Amount      int64      // amount（订单金额，单位：分）
	InstanceID  int64      // instance_id（资源实例ID，支付后填充）
	Status      string     // status: PENDING, PAID, CANCELLED, COMPLETED
	CreatedAt   time.Time  // created_at
	PaidAt      *time.Time // paid_at
	CompletedAt *time.Time // completed_at

	// 业务扩展字段（不在 DDL 中）
	ProductSnapshot *ProductSnapshot // 商品快照（业务逻辑需要）
	UpdatedAt       time.Time        // 业务更新时间
}

// ProductSnapshot 商品快照（值对象）
type ProductSnapshot struct {
	ProductID int64
	Name      string
	Price     int64
	Spec      *ProductSpec
}

// OrderRepo 订单仓储接口
type OrderRepo interface {
	Create(ctx context.Context, order *Order) error
	GetByID(ctx context.Context, orderID int64) (*Order, error)
	UpdateStatus(ctx context.Context, orderID int64, status string) error
}

// MQPublisher MQ 发布器接口
type MQPublisher interface {
	// PublishInstanceCreated 发布实例创建事件
	PublishInstanceCreated(ctx context.Context, spec InstanceSpec) error
}

// OrderIDGenerator 订单ID生成器接口
type OrderIDGenerator interface {
	Generate(ctx context.Context, userID string) (int64, error)
}

// OrderUsecase 订单业务逻辑
type OrderUsecase struct {
	orderRepo     OrderRepo
	productRepo   ProductRepo
	instanceRepo  InstanceRepo // 用于实例查询
	mqPublisher   MQPublisher
	orderIDGen    OrderIDGenerator
	instanceIDGen InstanceIDGenerator
	log           *log.Helper
}

// NewOrderUsecase 创建订单用例
func NewOrderUsecase(
	orderRepo OrderRepo,
	productRepo ProductRepo,
	instanceRepo InstanceRepo,
	mqPublisher MQPublisher,
	orderIDGen OrderIDGenerator,
	instanceIDGen InstanceIDGenerator,
	logger log.Logger,
) *OrderUsecase {
	return &OrderUsecase{
		orderRepo:     orderRepo,
		productRepo:   productRepo,
		instanceRepo:  instanceRepo,
		mqPublisher:   mqPublisher,
		orderIDGen:    orderIDGen,
		instanceIDGen: instanceIDGen,
		log:           log.NewHelper(logger),
	}
}

// CreateOrder 创建订单（统一入口）
// 支持两种场景：
// 1. 秒杀：reqID 由外部传入（Redis INCR 生成）
// 2. 正常购买：reqID 传 0，内部生成随机大数
// 返回：orderID, instanceID, error
func (uc *OrderUsecase) CreateOrder(ctx context.Context, productID int64, userID string, reqID int64) (int64, int64, error) {
	uc.log.Infof("creating order: productID=%d userID=%s reqID=%d", productID, userID, reqID)

	// 1. 如果 reqID 为 0，生成随机 req_id（正常购买场景）
	if reqID == 0 {
		var err error
		reqID, err = uc.orderIDGen.Generate(ctx, userID)
		if err != nil {
			uc.log.Errorf("generate req_id failed: %v", err)
			return 0, 0, err
		}
		uc.log.Infof("generated req_id: %d", reqID)
	}

	// 2. 查询商品信息
	product, err := uc.productRepo.GetByID(ctx, productID)
	if err != nil {
		uc.log.Errorf("get product failed: productID=%d err=%v", productID, err)
		return 0, 0, err
	}

	if product.Status != "ENABLED" {
		return 0, 0, ErrProductDisabled
	}

	if product.Spec == nil {
		uc.log.Errorf("product spec not found: productID=%d", productID)
		return 0, 0, errors.New("product spec not found")
	}

	// 3. 生成订单 ID
	orderID, err := uc.orderIDGen.Generate(ctx, userID)
	if err != nil {
		uc.log.Errorf("generate order id failed: %v", err)
		return 0, 0, err
	}

	// 4. 生成实例 ID
	instanceID, err := uc.instanceIDGen.Generate(ctx, userID)
	if err != nil {
		uc.log.Errorf("generate instance id failed: %v", err)
		return 0, 0, err
	}
	uc.log.Infof("generated orderID=%d instanceID=%d", orderID, instanceID)

	// 5. 创建订单（一次性写入所有字段）
	now := time.Now()
	order := &Order{
		ID:         orderID,
		UserID:     userID,
		ProductID:  productID,
		ReqID:      reqID,
		Amount:     product.Price,
		InstanceID: instanceID,
		Status:     "PAID", // 两种场景都是支付完成后才创建订单
		CreatedAt:  now,
		PaidAt:     &now,
	}

	if err := uc.orderRepo.Create(ctx, order); err != nil {
		uc.log.Errorf("create order failed: %v", err)
		return 0, 0, err
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
		return 0, 0, err
	}
	uc.log.Infof("mq message published: instanceID=%d", instanceID)

	uc.log.Infof("order created successfully: orderID=%d instanceID=%d", orderID, instanceID)
	return orderID, instanceID, nil
}

// PurchaseProduct 正常购买商品
func (uc *OrderUsecase) PurchaseProduct(ctx context.Context, userID string, productID int64) (*Order, int64, error) {
	if userID == "" {
		return nil, 0, ErrInvalidUserID
	}

	// 调用 CreateOrder 统一处理，reqID 传 0（内部生成随机大数）
	orderID, instanceID, err := uc.CreateOrder(ctx, productID, userID, 0)
	if err != nil {
		uc.log.Errorf("create order failed: %v", err)
		return nil, 0, err
	}

	// 查询刚创建的订单
	order, err := uc.orderRepo.GetByID(ctx, orderID)
	if err != nil {
		uc.log.Errorf("get order failed: %v", err)
		return nil, 0, err
	}

	uc.log.Infof("purchase completed: order_id=%d, instance_id=%d, user_id=%s, product_id=%d",
		orderID, instanceID, userID, productID)

	return order, instanceID, nil
}

// CreateOrderFromSeckill 秒杀场景创建订单
// reqID: Redis INCR 生成的请求号
func (uc *OrderUsecase) CreateOrderFromSeckill(ctx context.Context, productID int64, userID string, reqID int64) (int64, int64, error) {
	return uc.CreateOrder(ctx, productID, userID, reqID)
}

// GetOrderByID 根据订单ID获取订单
func (uc *OrderUsecase) GetOrderByID(ctx context.Context, orderID int64) (*Order, error) {
	return uc.orderRepo.GetByID(ctx, orderID)
}

// GetInstance 获取实例信息（通过订单查询）
func (uc *OrderUsecase) GetInstance(ctx context.Context, instanceID int64) (*InstanceInfo, error) {
	// 实例信息实际上是订单关联的资源信息
	// 这里需要通过 InstanceRepo 查询（因为需要跨表查询 orders + products）
	return uc.instanceRepo.GetInstanceByID(ctx, instanceID)
}

// GetInstanceByOrder 根据订单ID获取实例信息
func (uc *OrderUsecase) GetInstanceByOrder(ctx context.Context, orderID int64) (*InstanceInfo, error) {
	return uc.instanceRepo.GetInstanceByOrderID(ctx, orderID)
}

// ListInstances 查询实例列表
func (uc *OrderUsecase) ListInstances(ctx context.Context, filter InstanceFilter) ([]*InstanceInfo, int64, error) {
	// 设置默认分页参数
	if filter.Page == 0 {
		filter.Page = 1
	}
	if filter.PageSize == 0 {
		filter.PageSize = 20
	}

	return uc.instanceRepo.ListInstances(ctx, filter)
}

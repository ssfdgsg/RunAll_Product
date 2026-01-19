package data

import (
	"context"
	"database/sql"
	"time"

	"product/internal/biz"

	"github.com/go-kratos/kratos/v2/log"
)

type instanceRepo struct {
	data *Data
	log  *log.Helper
}

// NewInstanceRepo 创建实例仓储
func NewInstanceRepo(data *Data, logger log.Logger) biz.InstanceRepo {
	return &instanceRepo{
		data: data,
		log:  log.NewHelper(logger),
	}
}

// instanceLogPO 已移除
// 实例创建日志由资源域（Resource Domain）管理

// productPO 商品持久化对象
// 商品是可售卖的套餐/SKU
type productPO struct {
	ID          int64     `gorm:"primaryKey;autoIncrement;column:product_id"`
	Name        string    `gorm:"column:name;size:128;not null"`
	Description string    `gorm:"column:description;type:text"`
	Status      string    `gorm:"column:status;type:varchar(20);default:'ENABLED'"` // ENABLED=上架, DISABLED=下架
	Price       int64     `gorm:"column:price;not null"`                            // 单位：分
	SpecID      int64     `gorm:"column:spec_id;not null"`                          // 关联规格ID
	CreatedAt   time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt   time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (productPO) TableName() string {
	return "products"
}

// productSpecPO 商品规格持久化对象
// 规格定义了购买商品后创建的实例的资源配置
type productSpecPO struct {
	ID         int64     `gorm:"primaryKey;autoIncrement;column:spec_id"`
	CPU        int32     `gorm:"column:cpu;not null"`            // CPU 核数
	Memory     int32     `gorm:"column:memory;not null"`         // 内存（MB）
	GPU        int32     `gorm:"column:gpu;default:0"`           // GPU 数量
	Image      string    `gorm:"column:image;size:255;not null"` // 容器镜像
	ConfigJSON []byte    `gorm:"column:config_json;type:jsonb"`  // 扩展配置
	CreatedAt  time.Time `gorm:"column:created_at;autoCreateTime"`
}

func (productSpecPO) TableName() string {
	return "product_specs"
}

// orderPO 订单持久化对象
type orderPO struct {
	OrderID     int64         `gorm:"primaryKey"`
	UserID      string        `gorm:"column:user_id;type:uuid;not null;index"`                     // 用户UUID
	ProductID   int64         `gorm:"column:product_id;not null;index;uniqueIndex:uk_product_req"` // 购买的商品ID
	ReqID       int64         `gorm:"column:req_id;not null;uniqueIndex:uk_product_req"`           // 请求号（秒杀：Redis INCR；正常购买：随机）
	Amount      int64         `gorm:"column:amount;not null"`                                      // 订单金额（分）
	InstanceID  sql.NullInt64 `gorm:"column:instance_id;index"`                                    // 实例ID（创建后填充）
	Status      string        `gorm:"column:status;type:varchar(20);not null;default:PENDING"`     // PENDING/PAID/CANCELLED/COMPLETED
	CreatedAt   time.Time     `gorm:"column:created_at;autoCreateTime"`
	PaidAt      sql.NullTime  `gorm:"column:paid_at"`
	CompletedAt sql.NullTime  `gorm:"column:completed_at"`
}

func (orderPO) TableName() string {
	return "orders"
}

// GetProductByID 获取商品信息（包含规格）
// 商品定义了可售卖的套餐，规格定义了实例的资源配置
func (r *instanceRepo) GetProductByID(ctx context.Context, productID int64) (*biz.Product, error) {
	var productPo productPO
	if err := r.data.db.WithContext(ctx).First(&productPo, productID).Error; err != nil {
		r.log.Errorf("get product failed: productID=%d err=%v", productID, err)
		return nil, err
	}

	// 查询关联的规格
	var specPo productSpecPO
	if err := r.data.db.WithContext(ctx).First(&specPo, productPo.SpecID).Error; err != nil {
		r.log.Errorf("get product spec failed: specID=%d err=%v", productPo.SpecID, err)
		return nil, err
	}

	product := &biz.Product{
		ID:          productPo.ID,
		Name:        productPo.Name,
		Description: productPo.Description,
		Status:      productPo.Status,
		Price:       productPo.Price,
		SpecID:      productPo.SpecID,
		Spec: &biz.ProductSpec{
			ID:         specPo.ID,
			CPU:        specPo.CPU,
			Memory:     specPo.Memory,
			GPU:        specPo.GPU,
			Image:      specPo.Image,
			ConfigJSON: specPo.ConfigJSON,
			CreatedAt:  specPo.CreatedAt,
		},
		CreatedAt: productPo.CreatedAt,
		UpdatedAt: productPo.UpdatedAt,
	}

	return product, nil
}

// GetInstanceByID 根据实例ID获取实例信息
func (r *instanceRepo) GetInstanceByID(ctx context.Context, instanceID int64) (*biz.InstanceInfo, error) {
	var order orderPO
	err := r.data.db.WithContext(ctx).
		Where("instance_id = ?", instanceID).
		First(&order).Error
	if err != nil {
		r.log.Errorf("get order by instance_id failed: instanceID=%d err=%v", instanceID, err)
		return nil, err
	}

	return r.buildInstanceInfo(ctx, &order)
}

// GetInstanceByOrderID 根据订单ID获取实例信息
func (r *instanceRepo) GetInstanceByOrderID(ctx context.Context, orderID int64) (*biz.InstanceInfo, error) {
	var order orderPO
	err := r.data.db.WithContext(ctx).
		Where("order_id = ?", orderID).
		First(&order).Error
	if err != nil {
		r.log.Errorf("get order by order_id failed: orderID=%d err=%v", orderID, err)
		return nil, err
	}

	if !order.InstanceID.Valid {
		r.log.Errorf("order has no instance: orderID=%d", orderID)
		return nil, sql.ErrNoRows
	}

	return r.buildInstanceInfo(ctx, &order)
}

// ListInstances 查询实例列表
func (r *instanceRepo) ListInstances(ctx context.Context, filter biz.InstanceFilter) ([]*biz.InstanceInfo, int64, error) {
	query := r.data.db.WithContext(ctx).Model(&orderPO{}).
		Where("instance_id IS NOT NULL")

	// 用户ID过滤
	if filter.UserID != "" {
		query = query.Where("user_id = ?", filter.UserID)
	}

	// 状态过滤
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}

	// 统计总数
	var total int64
	if err := query.Count(&total).Error; err != nil {
		r.log.Errorf("count instances failed: err=%v", err)
		return nil, 0, err
	}

	// 分页查询
	var orders []orderPO
	offset := int((filter.Page - 1) * filter.PageSize)
	err := query.
		Order("created_at DESC").
		Limit(int(filter.PageSize)).
		Offset(offset).
		Find(&orders).Error
	if err != nil {
		r.log.Errorf("list instances failed: err=%v", err)
		return nil, 0, err
	}

	// 构建实例信息列表
	instances := make([]*biz.InstanceInfo, 0, len(orders))
	for i := range orders {
		instance, err := r.buildInstanceInfo(ctx, &orders[i])
		if err != nil {
			r.log.Warnf("build instance info failed: orderID=%d err=%v", orders[i].OrderID, err)
			continue
		}
		instances = append(instances, instance)
	}

	return instances, total, nil
}

// buildInstanceInfo 构建实例信息
func (r *instanceRepo) buildInstanceInfo(ctx context.Context, order *orderPO) (*biz.InstanceInfo, error) {
	if !order.InstanceID.Valid {
		return nil, sql.ErrNoRows
	}

	// 查询商品信息
	product, err := r.GetProductByID(ctx, order.ProductID)
	if err != nil {
		return nil, err
	}

	// 从订单状态推断实例状态
	status := "UNKNOWN"
	switch order.Status {
	case "PAID":
		status = "CREATING"
	case "COMPLETED":
		status = "RUNNING"
	case "CANCELLED":
		status = "DELETED"
	}

	return &biz.InstanceInfo{
		InstanceID:  order.InstanceID.Int64,
		UserID:      order.UserID,
		OrderID:     order.OrderID,
		ProductID:   order.ProductID,
		ProductName: product.Name,
		Spec:        product.Spec,
		Status:      status,
		CreatedAt:   order.CreatedAt,
	}, nil
}

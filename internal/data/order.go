package data

import (
	"context"
	"database/sql"
	"time"

	"product/internal/biz"

	"github.com/go-kratos/kratos/v2/log"
)

// orderPO 订单持久化对象（与 DDL 严格对应）
type orderPO struct {
	OrderID     int64         `gorm:"column:order_id;primaryKey"`
	ProductID   int64         `gorm:"column:product_id;not null"`
	Amount      int64         `gorm:"column:amount;not null"`
	InstanceID  sql.NullInt64 `gorm:"column:instance_id"`
	Status      string        `gorm:"column:status;not null;default:PENDING"`
	CreatedAt   time.Time     `gorm:"column:created_at;not null;default:CURRENT_TIMESTAMP"`
	PaidAt      sql.NullTime  `gorm:"column:paid_at"`
	CompletedAt sql.NullTime  `gorm:"column:completed_at"`
	UserID      string        `gorm:"column:user_id;type:uuid"` // UUID 类型
	ReqID       int64         `gorm:"column:req_id;not null;default:0"`
}

func (orderPO) TableName() string {
	return "orders"
}

type orderRepo struct {
	data *Data
	log  *log.Helper
}

// NewOrderRepo 创建订单仓储（旧接口，保持兼容）
func NewOrderRepo(data *Data, logger log.Logger) *orderRepo {
	return &orderRepo{
		data: data,
		log:  log.NewHelper(logger),
	}
}

// NewOrderRepoImpl 创建订单仓储实现（新接口）
func NewOrderRepoImpl(data *Data, logger log.Logger) biz.OrderRepo {
	return &orderRepo{
		data: data,
		log:  log.NewHelper(logger),
	}
}

// NewInstanceRepo 创建实例仓储（实际返回 orderRepo，因为实例查询基于订单）
func NewInstanceRepo(data *Data, logger log.Logger) biz.InstanceRepo {
	return &orderRepo{
		data: data,
		log:  log.NewHelper(logger),
	}
}

// Create 创建订单
func (r *orderRepo) Create(ctx context.Context, order *biz.Order) error {
	po := &orderPO{
		OrderID:   order.ID,
		UserID:    order.UserID,
		ProductID: order.ProductID,
		ReqID:     order.ReqID,
		Amount:    order.Amount,
		Status:    order.Status,
		CreatedAt: order.CreatedAt,
	}

	// 处理可空字段
	if order.InstanceID != 0 {
		po.InstanceID = sql.NullInt64{Int64: order.InstanceID, Valid: true}
	}
	if order.PaidAt != nil {
		po.PaidAt = sql.NullTime{Time: *order.PaidAt, Valid: true}
	}
	if order.CompletedAt != nil {
		po.CompletedAt = sql.NullTime{Time: *order.CompletedAt, Valid: true}
	}

	if err := r.data.db.WithContext(ctx).Create(po).Error; err != nil {
		r.log.Errorf("create order failed: %v", err)
		return err
	}

	return nil
}

// GetByID 根据订单ID获取订单
func (r *orderRepo) GetByID(ctx context.Context, orderID int64) (*biz.Order, error) {
	var po orderPO
	if err := r.data.db.WithContext(ctx).Where("order_id = ?", orderID).First(&po).Error; err != nil {
		r.log.Errorf("get order failed: %v", err)
		return nil, err
	}

	order := &biz.Order{
		ID:        po.OrderID,
		UserID:    po.UserID,
		ProductID: po.ProductID,
		ReqID:     po.ReqID,
		Amount:    po.Amount,
		Status:    po.Status,
		CreatedAt: po.CreatedAt,
	}

	// 处理可空字段
	if po.InstanceID.Valid {
		order.InstanceID = po.InstanceID.Int64
	}
	if po.PaidAt.Valid {
		order.PaidAt = &po.PaidAt.Time
	}
	if po.CompletedAt.Valid {
		order.CompletedAt = &po.CompletedAt.Time
	}

	return order, nil
}

// UpdateStatus 更新订单状态
func (r *orderRepo) UpdateStatus(ctx context.Context, orderID int64, status string) error {
	updates := map[string]interface{}{
		"status": status,
	}

	// 如果状态是已支付，更新支付时间
	if status == "PAID" {
		now := time.Now()
		updates["paid_at"] = sql.NullTime{Time: now, Valid: true}
	}

	// 如果状态是已完成，更新完成时间
	if status == "COMPLETED" {
		now := time.Now()
		updates["completed_at"] = sql.NullTime{Time: now, Valid: true}
	}

	if err := r.data.db.WithContext(ctx).Model(&orderPO{}).Where("order_id = ?", orderID).Updates(updates).Error; err != nil {
		r.log.Errorf("update order status failed: %v", err)
		return err
	}

	return nil
}

// 以下是旧接口方法，保持兼容性

// CreateOrder 创建订单（旧方法）
func (r *orderRepo) CreateOrder(ctx context.Context, order *biz.Order) error {
	return r.Create(ctx, order)
}

// GetOrderByID 根据订单ID获取订单（旧方法）
func (r *orderRepo) GetOrderByID(ctx context.Context, orderID int64) (*biz.Order, error) {
	return r.GetByID(ctx, orderID)
}

// UpdateOrderStatus 更新订单状态（旧方法）
func (r *orderRepo) UpdateOrderStatus(ctx context.Context, orderID int64, status string) error {
	return r.UpdateStatus(ctx, orderID, status)
}

// GetProductByID 获取商品信息（包含规格）
func (r *orderRepo) GetProductByID(ctx context.Context, productID int64) (*biz.Product, error) {
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
func (r *orderRepo) GetInstanceByID(ctx context.Context, instanceID int64) (*biz.InstanceInfo, error) {
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
func (r *orderRepo) GetInstanceByOrderID(ctx context.Context, orderID int64) (*biz.InstanceInfo, error) {
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
func (r *orderRepo) ListInstances(ctx context.Context, filter biz.InstanceFilter) ([]*biz.InstanceInfo, int64, error) {
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
func (r *orderRepo) buildInstanceInfo(ctx context.Context, order *orderPO) (*biz.InstanceInfo, error) {
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

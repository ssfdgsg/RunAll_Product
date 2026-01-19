package data

import (
	"context"
	"database/sql"
	"time"

	"product/internal/biz"

	"github.com/go-kratos/kratos/v2/log"
)

type orderRepo struct {
	data *Data
	log  *log.Helper
}

// NewOrderRepo 创建订单仓储
func NewOrderRepo(data *Data, logger log.Logger) biz.OrderRepo {
	return &orderRepo{
		data: data,
		log:  log.NewHelper(logger),
	}
}

// CreateOrder 创建订单
func (r *orderRepo) CreateOrder(ctx context.Context, order *biz.Order) error {
	po := &orderPO{
		OrderID:   order.OrderID,
		UserID:    order.UserID,
		ProductID: order.ProductID,
		ReqID:     order.ReqID,
		Amount:    order.Amount,
		Status:    order.Status,
		CreatedAt: order.CreatedAt,
	}

	// 处理可空字段
	if order.InstanceID != nil {
		po.InstanceID = sql.NullInt64{Int64: *order.InstanceID, Valid: true}
	}
	if order.PaidAt != nil {
		po.PaidAt = sql.NullTime{Time: *order.PaidAt, Valid: true}
	}
	if order.CompletedAt != nil {
		po.CompletedAt = sql.NullTime{Time: *order.CompletedAt, Valid: true}
	}

	if err := r.data.db.Debug().WithContext(ctx).Create(po).Error; err != nil {
		r.log.Errorf("create order failed: %v", err)
		return err
	}

	return nil
}

// GetOrderByID 根据订单ID获取订单
func (r *orderRepo) GetOrderByID(ctx context.Context, orderID int64) (*biz.Order, error) {
	var po orderPO
	if err := r.data.db.WithContext(ctx).Where("id = ?", orderID).First(&po).Error; err != nil {
		r.log.Errorf("get order failed: %v", err)
		return nil, err
	}

	order := &biz.Order{
		OrderID:   po.OrderID,
		UserID:    po.UserID,
		ProductID: po.ProductID,
		ReqID:     po.ReqID,
		Amount:    po.Amount,
		Status:    po.Status,
		CreatedAt: po.CreatedAt,
	}

	// 处理可空字段
	if po.InstanceID.Valid {
		order.InstanceID = &po.InstanceID.Int64
	}
	if po.PaidAt.Valid {
		order.PaidAt = &po.PaidAt.Time
	}
	if po.CompletedAt.Valid {
		order.CompletedAt = &po.CompletedAt.Time
	}

	return order, nil
}

// UpdateOrderStatus 更新订单状态
func (r *orderRepo) UpdateOrderStatus(ctx context.Context, orderID int64, status string) error {
	updates := map[string]interface{}{
		"status": status,
	}

	// 如果状态是已完成，更新完成时间
	if status == "COMPLETED" {
		now := time.Now()
		updates["completed_at"] = sql.NullTime{Time: now, Valid: true}
	}

	if err := r.data.db.WithContext(ctx).Model(&orderPO{}).Where("id = ?", orderID).Updates(updates).Error; err != nil {
		r.log.Errorf("update order status failed: %v", err)
		return err
	}

	return nil
}

package service

import (
	"context"

	v1 "product/api/product/v1"
	"product/internal/biz"

	"github.com/go-kratos/kratos/v2/log"
)

// InstanceService implements instance APIs.
type InstanceService struct {
	v1.UnimplementedInstanceServiceServer
	uc  *biz.InstanceUsecase
	log *log.Helper
}

// NewInstanceService creates an InstanceService.
func NewInstanceService(uc *biz.InstanceUsecase, logger log.Logger) *InstanceService {
	return &InstanceService{
		uc:  uc,
		log: log.NewHelper(logger),
	}
}

// GetInstance 获取实例详情
func (s *InstanceService) GetInstance(ctx context.Context, req *v1.GetInstanceReq) (*v1.GetInstanceReply, error) {
	instance, err := s.uc.GetInstance(ctx, req.GetInstanceId())
	if err != nil {
		s.log.Errorf("get instance failed: instanceID=%d err=%v", req.GetInstanceId(), err)
		return nil, err
	}

	return &v1.GetInstanceReply{
		Instance: toInstanceProto(instance),
	}, nil
}

// GetInstanceByOrder 根据订单ID获取实例
func (s *InstanceService) GetInstanceByOrder(ctx context.Context, req *v1.GetInstanceByOrderReq) (*v1.GetInstanceByOrderReply, error) {
	instance, err := s.uc.GetInstanceByOrder(ctx, req.GetOrderId())
	if err != nil {
		s.log.Errorf("get instance by order failed: orderID=%d err=%v", req.GetOrderId(), err)
		return nil, err
	}

	return &v1.GetInstanceByOrderReply{
		Instance: toInstanceProto(instance),
	}, nil
}

// ListInstances 查询实例列表
func (s *InstanceService) ListInstances(ctx context.Context, req *v1.ListInstancesReq) (*v1.ListInstancesReply, error) {
	filter := biz.InstanceFilter{
		UserID:   req.GetUserId(),
		Status:   req.GetStatus(),
		Page:     req.GetPage(),
		PageSize: req.GetPageSize(),
	}

	instances, total, err := s.uc.ListInstances(ctx, filter)
	if err != nil {
		s.log.Errorf("list instances failed: err=%v", err)
		return nil, err
	}

	protoInstances := make([]*v1.Instance, 0, len(instances))
	for _, instance := range instances {
		protoInstances = append(protoInstances, toInstanceProto(instance))
	}

	return &v1.ListInstancesReply{
		Instances: protoInstances,
		Page:      filter.Page,
		PageSize:  filter.PageSize,
		Total:     total,
	}, nil
}

// toInstanceProto 转换为 proto 实例对象
func toInstanceProto(instance *biz.InstanceInfo) *v1.Instance {
	protoInstance := &v1.Instance{
		InstanceId:  instance.InstanceID,
		UserId:      instance.UserID,
		OrderId:     instance.OrderID,
		ProductId:   instance.ProductID,
		ProductName: instance.ProductName,
		Status:      instance.Status,
		CreatedAt:   instance.CreatedAt.Unix(),
	}

	if instance.Spec != nil {
		protoInstance.Spec = &v1.ProductSpec{
			Cpu:        instance.Spec.CPU,
			Memory:     instance.Spec.Memory,
			Gpu:        instance.Spec.GPU,
			Image:      instance.Spec.Image,
			ConfigJson: string(instance.Spec.ConfigJSON),
		}
	}

	return protoInstance
}

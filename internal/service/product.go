package service

import (
	"context"
	"strings"

	"product/api/product/v1"
	"product/internal/biz"

	"github.com/go-kratos/kratos/v2/log"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// ProductService implements product APIs.
type ProductService struct {
	v1.UnimplementedProductServiceServer
	uc  *biz.ProductUsecase
	log *log.Helper
}

// NewProductService creates a ProductService.
func NewProductService(uc *biz.ProductUsecase, logger log.Logger) *ProductService {
	return &ProductService{
		uc:  uc,
		log: log.NewHelper(logger),
	}
}

// ListProduct lists products with filters, sorting, and pagination.
func (s *ProductService) ListProduct(ctx context.Context, req *v1.ListProductReq) (*v1.ListProductReply, error) {
	filter := s.buildFilter(req)
	products, total, err := s.uc.ListProducts(ctx, filter)
	if err != nil {
		return nil, err
	}

	mask := req.GetMask()
	result := make([]*v1.Product, 0, len(products))
	for _, product := range products {
		protoProduct := toProductProto(product)
		result = append(result, applyProductMask(protoProduct, mask))
	}

	return &v1.ListProductReply{
		Products: result,
		Page:     filter.Page,
		PageSize: filter.PageSize,
		Total:    total,
	}, nil
}

// CreateProduct creates a new product with spec.
func (s *ProductService) CreateProduct(ctx context.Context, req *v1.CreateProductReq) (*v1.CreateProductReply, error) {
	product := &biz.Product{
		Name:        req.GetName(),
		Description: req.GetDescription(),
		Price:       req.GetPrice(),
		Status:      "ENABLED", // 默认启用
		Spec: &biz.ProductSpec{
			CPU:        req.GetSpec().GetCpu(),
			Memory:     req.GetSpec().GetMemory(),
			GPU:        req.GetSpec().GetGpu(),
			Image:      req.GetSpec().GetImage(),
			ConfigJSON: []byte(req.GetSpec().GetConfigJson()),
		},
	}

	if err := s.uc.CreateProduct(ctx, product); err != nil {
		s.log.Errorf("create product failed: %v", err)
		return nil, err
	}

	return &v1.CreateProductReply{
		Product: toProductProto(product),
	}, nil
}

func (s *ProductService) buildFilter(req *v1.ListProductReq) biz.ProductFilter {
	filter := biz.ProductFilter{
		SortBy:    mapSortBy(req.GetSortBy()),
		SortOrder: mapSortOrder(req.GetSortOrder()),
	}

	if req.GetMinPrice() > 0 {
		value := req.GetMinPrice()
		filter.MinPrice = &value
	}
	if req.GetMaxPrice() > 0 {
		value := req.GetMaxPrice()
		filter.MaxPrice = &value
	}

	page := req.GetPage()
	if page == 0 {
		page = 1
	}
	pageSize := req.GetPageSize()
	if pageSize == 0 {
		pageSize = 20
	}

	filter.Page = page
	filter.PageSize = pageSize
	return filter
}

func mapSortBy(sortBy v1.SortBy) biz.ProductSortBy {
	switch sortBy {
	case v1.SortBy_SORT_BY_PRICE:
		return biz.ProductSortByPrice
	case v1.SortBy_SORT_BY_CPU:
		return biz.ProductSortByCPU
	case v1.SortBy_SORT_BY_MEMORY:
		return biz.ProductSortByMemory
	case v1.SortBy_SORT_BY_GPU:
		return biz.ProductSortByGPU
	default:
		return biz.ProductSortByUnspecified
	}
}

func mapSortOrder(order v1.SortOrder) biz.SortOrder {
	switch order {
	case v1.SortOrder_ASC:
		return biz.SortOrderAsc
	case v1.SortOrder_DESC:
		return biz.SortOrderDesc
	default:
		return biz.SortOrderUnspecified
	}
}

func toProductProto(product *biz.Product) *v1.Product {
	protoProduct := &v1.Product{
		Id:          product.ID,
		Name:        product.Name,
		Description: product.Description,
		Status:      statusToInt32(product.Status),
		Price:       product.Price,
	}
	if product.Spec != nil {
		protoProduct.Spec = &v1.ProductSpec{
			Cpu:        product.Spec.CPU,
			Memory:     product.Spec.Memory,
			Gpu:        product.Spec.GPU,
			Image:      product.Spec.Image,
			ConfigJson: string(product.Spec.ConfigJSON),
		}
	}
	return protoProduct
}

// statusToInt32 converts string status to int32 for proto
func statusToInt32(status string) int32 {
	switch status {
	case "ENABLED":
		return 1
	case "DISABLED":
		return 0
	default:
		return 0
	}
}

// _int32ToStatus converts int32 status to string for database
func _int32ToStatus(status int32) string {
	if status == 1 {
		return "ENABLED"
	}
	return "DISABLED"
}

func applyProductMask(product *v1.Product, mask *fieldmaskpb.FieldMask) *v1.Product {
	if product == nil {
		return nil
	}
	if mask == nil || len(mask.Paths) == 0 {
		return product
	}

	paths := normalizeMaskPaths(mask.Paths)
	allowed := func(path string) bool {
		return paths[path]
	}

	result := &v1.Product{}
	if allowed("id") {
		result.Id = product.Id
	}
	if allowed("name") {
		result.Name = product.Name
	}
	if allowed("description") {
		result.Description = product.Description
	}
	if allowed("status") {
		result.Status = product.Status
	}
	if allowed("price") {
		result.Price = product.Price
	}

	if allowed("spec") || hasSpecField(paths) {
		result.Spec = applySpecMask(product.Spec, paths)
	}
	return result
}

func applySpecMask(spec *v1.ProductSpec, paths map[string]bool) *v1.ProductSpec {
	if spec == nil {
		return nil
	}
	if paths["spec"] {
		return spec
	}

	result := &v1.ProductSpec{}
	if paths["spec.cpu"] {
		result.Cpu = spec.Cpu
	}
	if paths["spec.memory"] {
		result.Memory = spec.Memory
	}
	if paths["spec.gpu"] {
		result.Gpu = spec.Gpu
	}
	if paths["spec.image"] {
		result.Image = spec.Image
	}
	if paths["spec.config_json"] {
		result.ConfigJson = spec.ConfigJson
	}

	return result
}

func normalizeMaskPaths(paths []string) map[string]bool {
	result := make(map[string]bool, len(paths))
	for _, path := range paths {
		trimmed := strings.TrimSpace(path)
		if trimmed == "" {
			continue
		}
		result[trimmed] = true
	}
	return result
}

func hasSpecField(paths map[string]bool) bool {
	for path := range paths {
		if strings.HasPrefix(path, "spec.") {
			return true
		}
	}
	return false
}

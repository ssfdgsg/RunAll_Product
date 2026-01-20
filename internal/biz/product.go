package biz

import (
	"context"
	"errors"

	"github.com/go-kratos/kratos/v2/log"
)

var (
	ErrInvalidProduct      = errors.New("invalid product")
	ErrInvalidProductSpec  = errors.New("invalid product spec")
	ErrProductNameRequired = errors.New("product name is required")
	ErrInvalidPrice        = errors.New("price must be greater than 0")
	ErrInvalidSpec         = errors.New("cpu and memory must be greater than 0")
	ErrImageRequired       = errors.New("image is required")
)

// ProductSortBy defines sorting fields for product listing.
type ProductSortBy int32

const (
	// ProductSortByUnspecified leaves ordering unchanged.
	ProductSortByUnspecified ProductSortBy = iota
	ProductSortByPrice
	ProductSortByCPU
	ProductSortByMemory
	ProductSortByGPU
)

// SortOrder defines ordering direction.
type SortOrder int32

const (
	// SortOrderUnspecified leaves direction unchanged.
	SortOrderUnspecified SortOrder = iota
	SortOrderAsc
	SortOrderDesc
)

// ProductFilter defines filters and pagination for product listing.
type ProductFilter struct {
	MinPrice  *int64
	MaxPrice  *int64
	SortBy    ProductSortBy
	SortOrder SortOrder
	Page      uint32
	PageSize  uint32
}

// ProductRepo provides access to products for listing.
type ProductRepo interface {
	GetByID(ctx context.Context, productID int64) (*Product, error)
	List(ctx context.Context, filter ProductFilter) ([]*Product, int64, error)
	Create(ctx context.Context, product *Product) error
}

// ProductUsecase handles product queries.
type ProductUsecase struct {
	repo ProductRepo
	log  *log.Helper
}

// NewProductUsecase creates a ProductUsecase.
func NewProductUsecase(repo ProductRepo, logger log.Logger) *ProductUsecase {
	return &ProductUsecase{
		repo: repo,
		log:  log.NewHelper(logger),
	}
}

// ListProducts returns products with filters, sorting, and pagination.
func (uc *ProductUsecase) ListProducts(ctx context.Context, filter ProductFilter) ([]*Product, int64, error) {
	return uc.repo.List(ctx, filter)
}

// CreateProduct creates a new product with spec.
func (uc *ProductUsecase) CreateProduct(ctx context.Context, product *Product) error {
	if product == nil {
		return ErrInvalidProduct
	}
	if product.Spec == nil {
		return ErrInvalidProductSpec
	}
	if product.Name == "" {
		return ErrProductNameRequired
	}
	if product.Price <= 0 {
		return ErrInvalidPrice
	}
	if product.Spec.CPU <= 0 || product.Spec.Memory <= 0 {
		return ErrInvalidSpec
	}
	if product.Spec.Image == "" {
		return ErrImageRequired
	}

	// 默认状态为启用
	if product.Status == "" {
		product.Status = "ENABLED"
	}

	return uc.repo.Create(ctx, product)
}

package data

import (
	"context"
	"fmt"
	"time"

	"product/internal/biz"

	"github.com/go-kratos/kratos/v2/log"
	"gorm.io/gorm"
)

type productRepo struct {
	data *Data
	log  *log.Helper
}

// NewProductRepo creates a product repository.
func NewProductRepo(data *Data, logger log.Logger) biz.ProductRepo {
	return &productRepo{
		data: data,
		log:  log.NewHelper(logger),
	}
}

// List returns products with filters, sorting, and pagination.
func (r *productRepo) List(ctx context.Context, filter biz.ProductFilter) ([]*biz.Product, int64, error) {
	base := r.data.db.WithContext(ctx).
		Model(&productPO{}).
		Joins("JOIN product_specs ON product_specs.spec_id = products.spec_id")

	if filter.MinPrice != nil {
		base = base.Where("products.price >= ?", *filter.MinPrice)
	}
	if filter.MaxPrice != nil {
		base = base.Where("products.price <= ?", *filter.MaxPrice)
	}

	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	orderBy, err := buildProductOrder(filter.SortBy, filter.SortOrder)
	if err != nil {
		return nil, 0, err
	}
	if orderBy != "" {
		base = base.Order(orderBy)
	}

	if filter.PageSize > 0 {
		page := filter.Page
		if page == 0 {
			page = 1
		}
		offset := int(filter.PageSize * (page - 1))
		base = base.Offset(offset).Limit(int(filter.PageSize))
	}

	var rows []productListRow
	if err := base.Select(selectProductListColumns()).Scan(&rows).Error; err != nil {
		return nil, 0, err
	}

	products := make([]*biz.Product, 0, len(rows))
	for _, row := range rows {
		product := &biz.Product{
			ID:          row.ID,
			Name:        row.Name,
			Description: row.Description,
			Status:      row.Status,
			Price:       row.Price,
			SpecID:      row.SpecID,
			Spec: &biz.ProductSpec{
				ID:         row.SpecID,
				CPU:        row.SpecCPU,
				Memory:     row.SpecMemory,
				GPU:        row.SpecGPU,
				Image:      row.SpecImage,
				ConfigJSON: row.SpecConfigJSON,
			},
			CreatedAt: row.CreatedAt,
			UpdatedAt: row.UpdatedAt,
		}
		products = append(products, product)
	}

	return products, total, nil
}

// Create creates a new product with spec.
func (r *productRepo) Create(ctx context.Context, product *biz.Product) error {
	return r.data.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. 创建规格（spec_id 由数据库自增生成）
		specPO := &productSpecPO{
			CPU:        product.Spec.CPU,
			Memory:     product.Spec.Memory,
			GPU:        product.Spec.GPU,
			Image:      product.Spec.Image,
			ConfigJSON: product.Spec.ConfigJSON,
		}
		if err := tx.Create(specPO).Error; err != nil {
			r.log.Errorf("create product spec failed: %v", err)
			return err
		}

		// 2. 创建商品（product_id 由数据库自增生成）
		productPO := &productPO{
			// 不设置 ID，让数据库自增生成
			Name:        product.Name,
			Description: product.Description,
			Status:      product.Status,
			Price:       product.Price,
			SpecID:      specPO.ID,
		}
		if err := tx.Create(productPO).Error; err != nil {
			r.log.Errorf("create product failed: %v", err)
			return err
		}

		// 3. 更新返回值（使用数据库生成的 ID）
		product.ID = productPO.ID
		product.SpecID = specPO.ID
		product.Spec.ID = specPO.ID
		product.Spec.CreatedAt = specPO.CreatedAt
		product.CreatedAt = productPO.CreatedAt
		product.UpdatedAt = productPO.UpdatedAt

		return nil
	})
}

type productListRow struct {
	ID             int64     `gorm:"column:id"`
	Name           string    `gorm:"column:name"`
	Description    string    `gorm:"column:description"`
	Status         string    `gorm:"column:status"`
	Price          int64     `gorm:"column:price"`
	SpecID         int64     `gorm:"column:spec_id"`
	SpecCPU        int32     `gorm:"column:spec_cpu"`
	SpecMemory     int32     `gorm:"column:spec_memory"`
	SpecGPU        int32     `gorm:"column:spec_gpu"`
	SpecImage      string    `gorm:"column:spec_image"`
	SpecConfigJSON []byte    `gorm:"column:spec_config_json"`
	CreatedAt      time.Time `gorm:"column:created_at"`
	UpdatedAt      time.Time `gorm:"column:updated_at"`
}

func selectProductListColumns() string {
	return "products.product_id AS id, products.name, products.description, products.status, products.price, products.spec_id, " +
		"products.created_at, products.updated_at, " +
		"product_specs.cpu AS spec_cpu, product_specs.memory AS spec_memory, product_specs.gpu AS spec_gpu, " +
		"product_specs.image AS spec_image, product_specs.config_json AS spec_config_json"
}

func buildProductOrder(sortBy biz.ProductSortBy, order biz.SortOrder) (string, error) {
	var column string
	switch sortBy {
	case biz.ProductSortByPrice:
		column = "products.price"
	case biz.ProductSortByCPU:
		column = "product_specs.cpu"
	case biz.ProductSortByMemory:
		column = "product_specs.memory"
	case biz.ProductSortByGPU:
		column = "product_specs.gpu"
	case biz.ProductSortByUnspecified:
		return "", nil
	default:
		return "", fmt.Errorf("unknown sort field")
	}

	direction := "ASC"
	switch order {
	case biz.SortOrderAsc, biz.SortOrderUnspecified:
		direction = "ASC"
	case biz.SortOrderDesc:
		direction = "DESC"
	default:
		return "", fmt.Errorf("unknown sort order")
	}

	return fmt.Sprintf("%s %s", column, direction), nil
}

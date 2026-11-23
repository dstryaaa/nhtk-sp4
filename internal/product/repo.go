package product

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("product not found")

type Repo struct {
	pool *pgxpool.Pool
}

func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

func (r *Repo) Create(ctx context.Context, p *Product) error {
	const sql = `INSERT INTO products (sku, name, price, stock)
	               VALUES ($1, $2, $3, $4)
	               RETURNING id, created_at`
	err := r.pool.QueryRow(ctx, sql, p.SKU, p.Name, p.Price, p.Stock).Scan(&p.ID, &p.CreatedAt)
	if err != nil {
		return fmt.Errorf("create product: %w", err)
	}
	return nil
}

// GetByID возвращает товар по id. Если не найден — возвращает ErrNotFound.
func (r *Repo) GetByID(ctx context.Context, id int64) (*Product, error) {
	const sql = `SELECT id, sku, name, price, stock, created_at FROM products WHERE id=$1`
	var p Product
	err := r.pool.QueryRow(ctx, sql, id).Scan(&p.ID, &p.SKU, &p.Name, &p.Price, &p.Stock, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get product by id: %w", err)
	}
	return &p, nil
}

// List возвращает товары с пагинацией и поиском по name/sku.
func (r *Repo) List(ctx context.Context, q string, limit, offset int) ([]Product, error) {
	const sql = `SELECT id, sku, name, price, stock, created_at
	                   FROM products
	                   WHERE ($1 = '' OR name ILIKE '%'||$1||'%' OR sku ILIKE '%'||$1||'%')
	                   ORDER BY id
	                   LIMIT $2 OFFSET $3`

	rows, err := r.pool.Query(ctx, sql, q, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list products: %w", err)
	}
	defer rows.Close()

	products := make([]Product, 0)
	for rows.Next() {
		var p Product
		if err := rows.Scan(&p.ID, &p.SKU, &p.Name, &p.Price, &p.Stock, &p.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan product: %w", err)
		}
		products = append(products, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}
	return products, nil
}

// UpdateStock устанавливает stock для товара. Возвращает ErrNotFound, если нет.
func (r *Repo) UpdateStock(ctx context.Context, id int64, stock int) (*Product, error) {
	const sql = `UPDATE products SET stock=$2 WHERE id=$1
	                   RETURNING id, sku, name, price, stock, created_at`
	var p Product
	err := r.pool.QueryRow(ctx, sql, id, stock).Scan(&p.ID, &p.SKU, &p.Name, &p.Price, &p.Stock, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("update stock: %w", err)
	}
	return &p, nil
}

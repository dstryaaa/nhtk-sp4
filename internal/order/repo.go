package order

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrProductNotFound   = errors.New("product not found")
	ErrInsufficientStock = errors.New("insufficient stock")
	ErrNotFound          = errors.New("order not found")
)

type Repo struct {
	pool *pgxpool.Pool
}

func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

// CreateOrder создаёт заказ и резервирует все позиции атомарно.
func (r *Repo) CreateOrder(ctx context.Context, items []CreateOrderItem) (*Order, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var o Order
	err = tx.QueryRow(ctx,
		`INSERT INTO orders (status) VALUES ('Reserved') RETURNING id, created_at`,
	).Scan(&o.ID, &o.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("insert order: %w", err)
	}

	for _, it := range items {
		var price string
		err := tx.QueryRow(ctx,
			`UPDATE products SET stock = stock - $1
			 WHERE id = $2 AND stock >= $1
			 RETURNING price`,
			it.Quantity, it.ProductID,
		).Scan(&price)
		if errors.Is(err, pgx.ErrNoRows) {
			var exists bool
			err2 := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM products WHERE id = $1)`, it.ProductID).Scan(&exists)
			if err2 != nil {
				return nil, fmt.Errorf("check product exists: %w", err2)
			}
			if !exists {
				return nil, ErrProductNotFound // товара нет → 404
			}
			return nil, ErrInsufficientStock
		}
		if err != nil {
			return nil, fmt.Errorf("update stock product %d: %w", it.ProductID, err)
		}

		_, err = tx.Exec(ctx,
			`INSERT INTO order_items (order_id, product_id, quantity, price_at_order)
    		 VALUES ($1, $2, $3, $4)`,
			o.ID, it.ProductID, it.Quantity, price,
		)
		if err != nil {
			return nil, fmt.Errorf("insert order_item %d: %w", it.ProductID, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return r.GetByID(ctx, o.ID)
}

// GetByID — пока TODO, напишем после CreateOrder
func (r *Repo) GetByID(ctx context.Context, id int64) (*Order, error) {
	const sql = `
		SELECT o.id, o.status, o.created_at, o.cancelled_at,
		       oi.product_id, oi.quantity, oi.price_at_order,
		       p.sku, p.name
		FROM orders o
		LEFT JOIN order_items oi ON oi.order_id = o.id
		LEFT JOIN products p ON p.id = oi.product_id
		WHERE o.id = $1
		ORDER BY oi.product_id`

	rows, err := r.pool.Query(ctx, sql, id)
	if err != nil {
		return nil, fmt.Errorf("query order: %w", err)
	}
	defer rows.Close()

	var o *Order

	for rows.Next() {
		var (
			orderID      int64
			status       string
			createdAt    time.Time
			cancelledAt  *time.Time
			productID    *int64
			quantity     *int
			priceAtOrder *string
			sku          *string
			name         *string
		)
		err := rows.Scan(&orderID, &status, &createdAt, &cancelledAt, &productID, &quantity, &priceAtOrder, &sku, &name)
		if err != nil {
			return nil, fmt.Errorf("scan error: %w", err)
		}
		if o == nil {
			o = &Order{ID: orderID, Status: status, CreatedAt: &createdAt, CancelledAt: cancelledAt}
		}
		if productID != nil {
			o.Items = append(o.Items, OrderItem{
				ProductID:    *productID,
				Quantity:     *quantity,
				PriceAtOrder: *priceAtOrder,
				SKU:          *sku,
				Name:         *name,
			})
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}
	if o == nil {
		return nil, ErrNotFound
	}
	return o, nil
}

// Cancel отменяет заказ: статус → Cancelled, возврат резерва.
// Идемпотентно: повторная отмена → 200, остаток не меняется.
func (r *Repo) Cancel(ctx context.Context, id int64) (*Order, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var cancelledID int64
	err = tx.QueryRow(ctx,
		`UPDATE orders SET status='Cancelled', cancelled_at=now()
		 WHERE id=$1 AND status='Reserved'
		 RETURNING id`,
		id,
	).Scan(&cancelledID)

	if errors.Is(err, pgx.ErrNoRows) {
		var status string
		err2 := tx.QueryRow(ctx, `SELECT status FROM orders WHERE id=$1`, id).Scan(&status)
		if errors.Is(err2, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err2 != nil {
			return nil, fmt.Errorf("check order status: %w", err2)
		}
		if status == "Cancelled" {
			_ = tx.Rollback(ctx)
			return r.GetByID(ctx, id)
		}
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("update order status: %w", err)
	}

	_, err = tx.Exec(ctx,
		`UPDATE products p SET stock = stock + oi.quantity
		 FROM order_items oi
		 WHERE oi.order_id = $1 AND p.id = oi.product_id`,
		id,
	)
	if err != nil {
		return nil, fmt.Errorf("restore stock: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return r.GetByID(ctx, id)
}

package order

import (
	"fmt"
	"sort"
	"time"
)

type Order struct {
	ID          int64       `json:"id"`
	Status      string      `json:"status"`
	CreatedAt   *time.Time  `json:"created_at"`
	CancelledAt *time.Time  `json:"cancelled_at"`
	Items       []OrderItem `json:"items"`
}

type OrderItem struct {
	ProductID    int64  `json:"product_id"`
	Quantity     int    `json:"quantity"`
	PriceAtOrder string `json:"price_at_order"`
	SKU          string `json:"sku"`
	Name         string `json:"name"`
}

type CreateOrderInput struct {
	Items []CreateOrderItem `json:"items"`
}

type CreateOrderItem struct {
	ProductID int64 `json:"product_id"`
	Quantity  int   `json:"quantity"`
}

// Validate проверяет вход: items не пуст, quantity > 0, product_id > 0.
func (in *CreateOrderInput) Validate() error {
	if len(in.Items) == 0 {
		return fmt.Errorf("items is required")
	}

	for _, it := range in.Items {
		if it.ProductID <= 0 {
			return fmt.Errorf("product_id must be > 0")
		}
		if it.Quantity <= 0 {
			return fmt.Errorf("quantity must be > 0")
		}
	}

	return nil
}

func mergeItems(items []CreateOrderItem) []CreateOrderItem {
	orderItemMap := make(map[int64]int)
	for _, orderItem := range items {
		orderItemMap[orderItem.ProductID] += orderItem.Quantity
	}
	res := make([]CreateOrderItem, 0, len(orderItemMap))
	for id, quant := range orderItemMap {
		res = append(res, CreateOrderItem{ProductID: id, Quantity: quant})
	}

	sort.Slice(res, func(i, j int) bool {
		return res[i].ProductID < res[j].ProductID
	})

	return res
}

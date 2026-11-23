package product

import (
	"fmt"
	"strings"
	"time"
)

type Product struct {
	ID        int64     `json:"id"`
	SKU       string    `json:"sku"`
	Name      string    `json:"name"`
	Price     string    `json:"price"`
	Stock     int       `json:"stock"`
	CreatedAt time.Time `json:"created_at"`
}

type CreateInput struct {
	SKU   string  `json:"sku"`
	Name  string  `json:"name"`
	Price float64 `json:"price"`
	Stock int     `json:"stock"`
}

type UpdateStockInput struct {
	Stock *int `json:"stock"`
}

func (p *CreateInput) Validate() error {
	if strings.TrimSpace(p.SKU) == "" {
		return fmt.Errorf("sku is required")
	}

	if p.Name == "" {
		return fmt.Errorf("name is required")
	}

	if p.Price <= 0 {
		return fmt.Errorf("price must be > 0")
	}

	if p.Stock < 0 {
		return fmt.Errorf("stock must be >= 0")
	}
	return nil
}

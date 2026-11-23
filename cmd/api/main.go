package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/dstryaaa/nhtk-sp4/internal/order"
	"github.com/dstryaaa/nhtk-sp4/internal/product"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	db_URL := os.Getenv("DATABASE_URL")
	if db_URL == "" {
		log.Fatal("DATABASE_URL is not set or empty")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, db_URL)
	if err != nil {
		log.Fatalf("unable to create connection pool: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("database is unreachable: %v", err)
	}

	log.Println("connected to db")

	productRepo := product.NewRepo(pool)
	productHandler := product.NewHandler(productRepo)

	orderRepo := order.NewRepo(pool)
	orderHandler := order.NewHandler(orderRepo)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	productHandler.Register(mux)

	orderHandler.Register(mux)
	srv := &http.Server{Addr: ":8080", Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(srv.ListenAndServe())
}

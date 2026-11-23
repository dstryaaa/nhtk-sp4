package order

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/dstryaaa/nhtk-sp4/internal/product"
	"github.com/jackc/pgx/v5/pgxpool"
)

// testPool — общий пул для всех тестов пакета. Заполняется в TestMain.
var testPool *pgxpool.Pool

// TestMain подключается к тестовой БД, гоняет тесты, закрывает пул.
func TestMain(m *testing.M) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://semenov_n_m:password123@localhost:5432/semenov_test_db?sslmode=disable"
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		panic("connect test db: " + err.Error())
	}
	if err := pool.Ping(ctx); err != nil {
		panic("ping test db: " + err.Error())
	}
	testPool = pool

	code := m.Run()
	testPool.Close()
	os.Exit(code)
}

// truncate очищает таблицы перед каждым тестом (изоляция).
func truncate(t *testing.T) {
	t.Helper()
	_, err := testPool.Exec(context.Background(),
		`TRUNCATE products, orders, order_items RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

// createProduct создаёт товар через product.Repo — не зависим от HTTP-слоя.
func createProduct(t *testing.T, sku, name, price string, stock int) *product.Product {
	t.Helper()
	pRepo := product.NewRepo(testPool)
	p := &product.Product{SKU: sku, Name: name, Price: price, Stock: stock}
	if err := pRepo.Create(context.Background(), p); err != nil {
		t.Fatalf("create product %s: %v", sku, err)
	}
	return p
}

// getStock читает актуальный stock из БД (для проверки атомарности).
func getStock(t *testing.T, productID int64) int {
	t.Helper()
	pRepo := product.NewRepo(testPool)
	p, err := pRepo.GetByID(context.Background(), productID)
	if err != nil {
		t.Fatalf("get product %d: %v", productID, err)
	}
	return p.Stock
}

// TestCreateOrder_OK: 201, резерв, price_at_order зафиксирована, SKU/Name из JOIN.
func TestCreateOrder_OK(t *testing.T) {
	truncate(t)

	p := createProduct(t, "ORD-OK", "Widget", "10.00", 10)

	oRepo := NewRepo(testPool)
	h := NewHandler(oRepo)

	bodyJSON := `{"items":[{"product_id":` + strconv.FormatInt(p.ID, 10) + `,"quantity":3}]}`
	body := strings.NewReader(bodyJSON)
	req := httptest.NewRequest("POST", "/api/orders", body)
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var got Order
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got.Status != "Reserved" {
		t.Fatalf("status: want Reserved, got %q", got.Status)
	}
	if len(got.Items) != 1 {
		t.Fatalf("items: want 1, got %d", len(got.Items))
	}
	if got.Items[0].ProductID != p.ID {
		t.Fatalf("product_id: want %d, got %d", p.ID, got.Items[0].ProductID)
	}
	if got.Items[0].Quantity != 3 {
		t.Fatalf("quantity: want 3, got %d", got.Items[0].Quantity)
	}
	if got.Items[0].PriceAtOrder != "10.00" {
		t.Fatalf("price_at_order: want 10.00, got %q", got.Items[0].PriceAtOrder)
	}
	if got.Items[0].SKU != "ORD-OK" {
		t.Fatalf("sku: want ORD-OK, got %q", got.Items[0].SKU)
	}
	if got.Items[0].Name != "Widget" {
		t.Fatalf("name: want Widget, got %q", got.Items[0].Name)
	}

	if stock := getStock(t, p.ID); stock != 7 {
		t.Fatalf("stock: want 7, got %d", stock)
	}
}

// TestCreateOrder_InsufficientStock: 409, stock не тронут, заказ не создан (атомарность).
func TestCreateOrder_InsufficientStock(t *testing.T) {
	truncate(t)

	p := createProduct(t, "ORD-NO", "W", "10.00", 5)

	oRepo := NewRepo(testPool)
	h := NewHandler(oRepo)

	bodyJSON := `{"items":[{"product_id":` + strconv.FormatInt(p.ID, 10) + `,"quantity":100}]}`
	req := httptest.NewRequest("POST", "/api/orders", strings.NewReader(bodyJSON))
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d: %s", rec.Code, rec.Body.String())
	}

	var errBody map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&errBody); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if errBody["error"] != "insufficient stock" {
		t.Fatalf("want 'insufficient stock', got %q", errBody["error"])
	}

	if stock := getStock(t, p.ID); stock != 5 {
		t.Fatalf("stock should be unchanged: want 5, got %d", stock)
	}

	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM orders`).Scan(&count); err != nil {
		t.Fatalf("count orders: %v", err)
	}
	if count != 0 {
		t.Fatalf("order should not be created, got %d", count)
	}
}

// TestCreateOrder_ProductNotFound: 404, заказ не создан.
func TestCreateOrder_ProductNotFound(t *testing.T) {
	truncate(t)

	oRepo := NewRepo(testPool)
	h := NewHandler(oRepo)

	body := strings.NewReader(`{"items":[{"product_id":999,"quantity":1}]}`)
	req := httptest.NewRequest("POST", "/api/orders", body)
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d: %s", rec.Code, rec.Body.String())
	}

	var errBody map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&errBody); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if errBody["error"] != "product not found" {
		t.Fatalf("want 'product not found', got %q", errBody["error"])
	}

	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM orders`).Scan(&count); err != nil {
		t.Fatalf("count orders: %v", err)
	}
	if count != 0 {
		t.Fatalf("order should not be created, got %d", count)
	}
}

// TestCancel: 200, stock возвращается, cancelled_at заполнен.
func TestCancel(t *testing.T) {
	truncate(t)

	p := createProduct(t, "CANCEL-1", "W", "10.00", 10)

	oRepo := NewRepo(testPool)
	h := NewHandler(oRepo)

	body := strings.NewReader(`{"items":[{"product_id":` + strconv.FormatInt(p.ID, 10) + `,"quantity":3}]}`)
	req := httptest.NewRequest("POST", "/api/orders", body)
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create order: want 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var created Order
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("decode created: %v", err)
	}

	req = httptest.NewRequest("POST", "/api/orders/"+strconv.FormatInt(created.ID, 10)+"/cancel", nil)
	req.SetPathValue("id", strconv.FormatInt(created.ID, 10))
	rec = httptest.NewRecorder()
	h.Cancel(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("cancel: want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var cancelled Order
	if err := json.NewDecoder(rec.Body).Decode(&cancelled); err != nil {
		t.Fatalf("decode cancelled: %v", err)
	}
	if cancelled.Status != "Cancelled" {
		t.Fatalf("status: want Cancelled, got %q", cancelled.Status)
	}
	if cancelled.CancelledAt == nil {
		t.Fatalf("cancelled_at should be set, got nil")
	}

	if stock := getStock(t, p.ID); stock != 10 {
		t.Fatalf("stock after cancel: want 10, got %d", stock)
	}
}

// TestCancel_Idempotent: повторная отмена → 200, cancelled_at не меняется, stock не меняется.
func TestCancel_Idempotent(t *testing.T) {
	truncate(t)

	p := createProduct(t, "CANCEL-2", "W", "10.00", 10)

	oRepo := NewRepo(testPool)
	h := NewHandler(oRepo)

	body := strings.NewReader(`{"items":[{"product_id":` + strconv.FormatInt(p.ID, 10) + `,"quantity":3}]}`)
	req := httptest.NewRequest("POST", "/api/orders", body)
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	var created Order
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("decode created: %v", err)
	}

	idStr := strconv.FormatInt(created.ID, 10)
	req = httptest.NewRequest("POST", "/api/orders/"+idStr+"/cancel", nil)
	req.SetPathValue("id", idStr)
	rec = httptest.NewRecorder()
	h.Cancel(rec, req)
	var firstCancel Order
	if err := json.NewDecoder(rec.Body).Decode(&firstCancel); err != nil {
		t.Fatalf("decode first cancel: %v", err)
	}
	if firstCancel.CancelledAt == nil {
		t.Fatalf("first cancel: cancelled_at should be set")
	}
	t1 := *firstCancel.CancelledAt

	req = httptest.NewRequest("POST", "/api/orders/"+idStr+"/cancel", nil)
	req.SetPathValue("id", idStr)
	rec = httptest.NewRecorder()
	h.Cancel(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("second cancel: want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var secondCancel Order
	if err := json.NewDecoder(rec.Body).Decode(&secondCancel); err != nil {
		t.Fatalf("decode second cancel: %v", err)
	}
	if secondCancel.CancelledAt == nil {
		t.Fatalf("second cancel: cancelled_at should be set")
	}

	if !secondCancel.CancelledAt.Equal(t1) {
		t.Fatalf("cancelled_at changed: first %v, second %v", t1, *secondCancel.CancelledAt)
	}

	if stock := getStock(t, p.ID); stock != 10 {
		t.Fatalf("stock after idempotent cancel: want 10, got %d", stock)
	}
}

// TestCancel_NotFound: отмена несуществующего заказа → 404.
func TestCancel_NotFound(t *testing.T) {
	truncate(t)

	oRepo := NewRepo(testPool)
	h := NewHandler(oRepo)

	req := httptest.NewRequest("POST", "/api/orders/999/cancel", nil)
	req.SetPathValue("id", "999")
	rec := httptest.NewRecorder()
	h.Cancel(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", rec.Code)
	}

	var errBody map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&errBody); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if errBody["error"] != "order not found" {
		t.Fatalf("want 'order not found', got %q", errBody["error"])
	}
}

// TestConcurrentReserve: две горутины резервируют последнюю единицу.
// Одна получает 201, другая — 409; stock = 0 (не -1).
func TestConcurrentReserve(t *testing.T) {
	truncate(t)

	p := createProduct(t, "LAST-1", "Last", "10.00", 1)

	oRepo := NewRepo(testPool)
	h := NewHandler(oRepo)

	const goroutines = 2
	var wg sync.WaitGroup
	// results — буферизованный канал: горутины не блокируются на отправке.
	results := make(chan int, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			body := strings.NewReader(`{"items":[{"product_id":` + strconv.FormatInt(p.ID, 10) + `,"quantity":1}]}`)
			req := httptest.NewRequest("POST", "/api/orders", body)
			rec := httptest.NewRecorder()
			h.Create(rec, req)

			results <- rec.Code
		}()
	}

	wg.Wait()
	close(results)

	var created, conflict int
	// Проверки — после wg.Wait(), в main-горутине (t.Fatalf в горутине не останавливает тест корректно).
	for code := range results {
		switch code {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
			conflict++
		default:
			t.Fatalf("unexpected status: %d", code)
		}
	}

	if created != 1 {
		t.Fatalf("want exactly 1 created (201), got %d", created)
	}
	if conflict != 1 {
		t.Fatalf("want exactly 1 conflict (409), got %d", conflict)
	}

	if stock := getStock(t, p.ID); stock != 0 {
		t.Fatalf("stock after concurrent reserve: want 0, got %d", stock)
	}

	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM orders`).Scan(&count); err != nil {
		t.Fatalf("count orders: %v", err)
	}
	if count != 1 {
		t.Fatalf("want exactly 1 order, got %d", count)
	}
}

func TestGetByID_OK(t *testing.T) {
	truncate(t)
	p := createProduct(t, "GET-1", "W", "10.00", 10)
	oRepo := NewRepo(testPool)
	h := NewHandler(oRepo)

	// создаём заказ
	body := strings.NewReader(`{"items":[{"product_id":` + strconv.FormatInt(p.ID, 10) + `,"quantity":2}]}`)
	req := httptest.NewRequest("POST", "/api/orders", body)
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	var created Order
	json.NewDecoder(rec.Body).Decode(&created)

	// GET по id
	idStr := strconv.FormatInt(created.ID, 10)
	req = httptest.NewRequest("GET", "/api/orders/"+idStr, nil)
	req.SetPathValue("id", idStr)
	rec = httptest.NewRecorder()
	h.GetByID(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got Order
	json.NewDecoder(rec.Body).Decode(&got)
	if got.ID != created.ID {
		t.Fatalf("id: want %d, got %d", created.ID, got.ID)
	}
	if len(got.Items) != 1 {
		t.Fatalf("items: want 1, got %d", len(got.Items))
	}
	if got.Items[0].SKU != "GET-1" {
		t.Fatalf("sku: want GET-1, got %q", got.Items[0].SKU)
	}
}

func TestGetByID_NotFound(t *testing.T) {
	truncate(t)
	oRepo := NewRepo(testPool)
	h := NewHandler(oRepo)
	req := httptest.NewRequest("GET", "/api/orders/999", nil)
	req.SetPathValue("id", "999")
	rec := httptest.NewRecorder()
	h.GetByID(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", rec.Code)
	}
	var errBody map[string]string
	json.NewDecoder(rec.Body).Decode(&errBody)
	if errBody["error"] != "order not found" {
		t.Fatalf("want 'order not found', got %q", errBody["error"])
	}
}

func TestGetByID_InvalidID(t *testing.T) {
	truncate(t)
	oRepo := NewRepo(testPool)
	h := NewHandler(oRepo)
	req := httptest.NewRequest("GET", "/api/orders/abc", nil)
	req.SetPathValue("id", "abc")
	rec := httptest.NewRecorder()
	h.GetByID(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestCreateOrder_BadJSON(t *testing.T) {
	truncate(t)
	oRepo := NewRepo(testPool)
	h := NewHandler(oRepo)
	body := strings.NewReader(`{not json}`)
	req := httptest.NewRequest("POST", "/api/orders", body)
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
	var errBody map[string]string
	json.NewDecoder(rec.Body).Decode(&errBody)
	if errBody["error"] != "invalid request body" {
		t.Fatalf("want 'invalid request body', got %q", errBody["error"])
	}
}

func TestCreateOrder_EmptyItems(t *testing.T) {
	truncate(t)
	oRepo := NewRepo(testPool)
	h := NewHandler(oRepo)
	body := strings.NewReader(`{"items":[]}`)
	req := httptest.NewRequest("POST", "/api/orders", body)
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
	var errBody map[string]string
	json.NewDecoder(rec.Body).Decode(&errBody)
	if errBody["error"] != "items is required" {
		t.Fatalf("want 'items is required', got %q", errBody["error"])
	}
}

func TestCreateOrder_InvalidItem(t *testing.T) {
	truncate(t)
	oRepo := NewRepo(testPool)
	h := NewHandler(oRepo)

	// product_id <= 0
	body := strings.NewReader(`{"items":[{"product_id":0,"quantity":1}]}`)
	req := httptest.NewRequest("POST", "/api/orders", body)
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
	var errBody map[string]string
	json.NewDecoder(rec.Body).Decode(&errBody)
	if errBody["error"] != "product_id must be > 0" {
		t.Fatalf("want 'product_id must be > 0', got %q", errBody["error"])
	}
}

func TestCreateOrder_InvalidQuantity(t *testing.T) {
	truncate(t)
	oRepo := NewRepo(testPool)
	h := NewHandler(oRepo)
	body := strings.NewReader(`{"items":[{"product_id":1,"quantity":0}]}`)
	req := httptest.NewRequest("POST", "/api/orders", body)
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
	var errBody map[string]string
	json.NewDecoder(rec.Body).Decode(&errBody)
	if errBody["error"] != "quantity must be > 0" {
		t.Fatalf("want 'quantity must be > 0', got %q", errBody["error"])
	}
}

func TestCancel_InvalidID(t *testing.T) {
	truncate(t)
	oRepo := NewRepo(testPool)
	h := NewHandler(oRepo)
	req := httptest.NewRequest("POST", "/api/orders/abc/cancel", nil)
	req.SetPathValue("id", "abc")
	rec := httptest.NewRecorder()
	h.Cancel(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestCreateOrder_MergeDuplicates(t *testing.T) {
	truncate(t)
	p := createProduct(t, "MERGE-1", "W", "10.00", 10)
	oRepo := NewRepo(testPool)
	h := NewHandler(oRepo)

	// два раза product_id=p.ID с quantity 2 и 3 → должно слиться в 5
	body := strings.NewReader(`{"items":[{"product_id":` + strconv.FormatInt(p.ID, 10) + `,"quantity":2},{"product_id":` + strconv.FormatInt(p.ID, 10) + `,"quantity":3}]}`)
	req := httptest.NewRequest("POST", "/api/orders", body)
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var got Order
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Items) != 1 {
		t.Fatalf("items: want 1 (merged), got %d", len(got.Items))
	}
	if got.Items[0].Quantity != 5 {
		t.Fatalf("quantity: want 5 (2+3), got %d", got.Items[0].Quantity)
	}
	if stock := getStock(t, p.ID); stock != 5 {
		t.Fatalf("stock: want 5 (10-5), got %d", stock)
	}
}

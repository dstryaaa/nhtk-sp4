package product

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

var testPool *pgxpool.Pool

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

func truncate(t *testing.T) {
	t.Helper()
	_, err := testPool.Exec(context.Background(),
		`TRUNCATE products, orders, order_items RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

func TestGetByID_NotFound(t *testing.T) {
	truncate(t)
	repo := NewRepo(testPool)
	h := NewHandler(repo)

	req := httptest.NewRequest("GET", "/api/products/999", nil)
	req.SetPathValue("id", "999")
	rec := httptest.NewRecorder()

	h.GetByID(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["error"] != "product not found" {
		t.Fatalf("want 'product not found', got %q", body["error"])
	}
}

func TestGetByID_InvalidID(t *testing.T) {
	truncate(t)
	repo := NewRepo(testPool)
	h := NewHandler(repo)

	req := httptest.NewRequest("GET", "/api/products/abc", nil)
	req.SetPathValue("id", "abc") // ← передаём нечисловой id
	rec := httptest.NewRecorder()

	h.GetByID(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", rec.Code, rec.Body.String())
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["error"] != "invalid id" {
		t.Fatalf("want 'invalid id', got %q", body["error"])
	}
}

func TestGetByID_OK(t *testing.T) {
	truncate(t)
	repo := NewRepo(testPool)
	h := NewHandler(repo)

	p := &Product{
		SKU:   "TEST-OK",
		Name:  "Test Product",
		Price: "12.34",
		Stock: 5,
	}
	if err := repo.Create(context.Background(), p); err != nil {
		t.Fatalf("create: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/products/"+strconv.FormatInt(p.ID, 10), nil)
	req.SetPathValue("id", strconv.FormatInt(p.ID, 10))
	rec := httptest.NewRecorder()

	h.GetByID(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var got Product
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID != p.ID {
		t.Fatalf("want id %d, got %d", p.ID, got.ID)
	}
	if got.SKU != "TEST-OK" {
		t.Fatalf("want SKU TEST-OK, got %q", got.SKU)
	}
	if got.Name != "Test Product" {
		t.Fatalf("want name 'Test Product', got %q", got.Name)
	}
	if got.Price != "12.34" {
		t.Fatalf("want price '12.34', got %q", got.Price)
	}
	if got.Stock != 5 {
		t.Fatalf("want stock 5, got %d", got.Stock)
	}
}

func TestList_Empty(t *testing.T) {
	truncate(t)
	repo := NewRepo(testPool)
	h := NewHandler(repo)

	req := httptest.NewRequest("GET", "/api/products", nil)
	rec := httptest.NewRecorder()

	h.List(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var got []Product
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want empty, got %d items", len(got))
	}
}

func TestList_Pagination(t *testing.T) {
	truncate(t)
	repo := NewRepo(testPool)
	h := NewHandler(repo)

	// создаём 3 товара
	for i := 1; i <= 3; i++ {
		p := &Product{
			SKU:   "SKU-" + strconv.Itoa(i),
			Name:  "Product " + strconv.Itoa(i),
			Price: "10.00",
			Stock: 1,
		}
		if err := repo.Create(context.Background(), p); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}

	// limit=2 → 2 товара (id 1 и 2)
	req := httptest.NewRequest("GET", "/api/products?limit=2", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var got []Product
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 products, got %d", len(got))
	}
	if got[0].SKU != "SKU-1" {
		t.Fatalf("want SKU-1 first, got %q", got[0].SKU)
	}
	if got[1].SKU != "SKU-2" {
		t.Fatalf("want SKU-2 second, got %q", got[1].SKU)
	}
}

func TestList_Search(t *testing.T) {
	truncate(t)
	repo := NewRepo(testPool)
	h := NewHandler(repo)

	items := []struct {
		SKU  string
		Name string
	}{
		{"SKU-A", "Widget"},
		{"SKU-B", "Gadget"},
		{"SKU-C", "Widget Pro"},
	}
	for _, it := range items {
		p := &Product{SKU: it.SKU, Name: it.Name, Price: "10.00", Stock: 1}
		if err := repo.Create(context.Background(), p); err != nil {
			t.Fatalf("create %s: %v", it.SKU, err)
		}
	}

	req := httptest.NewRequest("GET", "/api/products?q=Widget", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got []Product
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 products, got %d: %+v", len(got), got)
	}
	// порядок — ORDER BY id: SKU-A (id=1), SKU-C (id=3)
	if got[0].SKU != "SKU-A" {
		t.Fatalf("want SKU-A first, got %q", got[0].SKU)
	}
	if got[1].SKU != "SKU-C" {
		t.Fatalf("want SKU-C second, got %q", got[1].SKU)
	}
}

func TestUpdateStock_OK(t *testing.T) {
	truncate(t)
	repo := NewRepo(testPool)
	h := NewHandler(repo)

	// 1. создаём товар
	p := &Product{SKU: "UPD-1", Name: "Upd", Price: "10.00", Stock: 5}
	if err := repo.Create(context.Background(), p); err != nil {
		t.Fatalf("create: %v", err)
	}

	// 2. PATCH /api/products/{id}/stock  body: {"stock": 42}
	body := strings.NewReader(`{"stock":42}`)
	req := httptest.NewRequest("PATCH", "/api/products/"+strconv.FormatInt(p.ID, 10)+"/stock", body)
	req.SetPathValue("id", strconv.FormatInt(p.ID, 10))
	rec := httptest.NewRecorder()

	h.UpdateStock(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got Product
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Stock != 42 {
		t.Fatalf("want stock = 42, got %d", got.Stock)
	}

	dbP, err := repo.GetByID(context.Background(), p.ID)
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if dbP.Stock != 42 {
		t.Fatalf("db stock: want 42, got %d", dbP.Stock)
	}
}

func TestUpdateStock_NotFound(t *testing.T) {
	truncate(t)
	repo := NewRepo(testPool)
	h := NewHandler(repo)

	body := strings.NewReader(`{"stock":10}`)
	req := httptest.NewRequest("PATCH", "/api/products/999/stock", body)
	req.SetPathValue("id", "999")
	rec := httptest.NewRecorder()

	h.UpdateStock(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", rec.Code)
	}

	var errBody map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&errBody); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if errBody["error"] != "product not found" {
		t.Fatalf("want 'product not found', got %q", errBody["error"])
	}
}

func TestUpdateStock_NegativeStock(t *testing.T) {
	truncate(t)
	repo := NewRepo(testPool)
	h := NewHandler(repo)

	p := &Product{SKU: "UPD-NEG", Name: "Neg", Price: "10.00", Stock: 5}
	if err := repo.Create(context.Background(), p); err != nil {
		t.Fatalf("create: %v", err)
	}

	body := strings.NewReader(`{"stock":-1}`)
	req := httptest.NewRequest("PATCH", "/api/products/"+strconv.FormatInt(p.ID, 10)+"/stock", body)
	req.SetPathValue("id", strconv.FormatInt(p.ID, 10))
	rec := httptest.NewRecorder()

	h.UpdateStock(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}

	var errBody map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&errBody); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if errBody["error"] != "stock must be >= 0" {
		t.Fatalf("want 'stock must be >= 0', got %q", errBody["error"])
	}

	dbP, err := repo.GetByID(context.Background(), p.ID)
	if err != nil {
		t.Fatalf("get after failed update: %v", err)
	}
	if dbP.Stock != 5 {
		t.Fatalf("db stock should be unchanged: want 5, got %d", dbP.Stock)
	}
}

func TestCreate_OK(t *testing.T) {
	truncate(t)
	repo := NewRepo(testPool)
	h := NewHandler(repo)

	body := strings.NewReader(`{"sku":"NEW-1","name":"New","price":10.5,"stock":3}`)
	req := httptest.NewRequest("POST", "/api/products", body)
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d", rec.Code)
	}
	var got Product
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.SKU != "NEW-1" {
		t.Fatalf("want 'NEW-1', got %q", got.SKU)
	}
	if got.Price != "10.50" {
		t.Fatalf("want 10.50, got %s", got.Price)
	}
	if got.Stock != 3 {
		t.Fatalf("want 3, got %d", got.Stock)
	}
	if got.ID <= 0 {
		t.Fatalf("want > 0, got %d", got.ID)
	}
}

func TestCreate_DuplicateSKU(t *testing.T) {
	truncate(t)
	repo := NewRepo(testPool)
	h := NewHandler(repo)

	// 1. создаём первый товар
	body1 := strings.NewReader(`{"sku":"DUP-1","name":"A","price":10,"stock":1}`)
	req1 := httptest.NewRequest("POST", "/api/products", body1)
	rec1 := httptest.NewRecorder()
	h.Create(rec1, req1)
	if rec1.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d", rec1.Code)
	}

	// 2. второй с тем же SKU
	body2 := strings.NewReader(`{"sku":"DUP-1","name":"B","price":20,"stock":2}`)
	req2 := httptest.NewRequest("POST", "/api/products", body2)
	rec2 := httptest.NewRecorder()
	h.Create(rec2, req2)

	if rec2.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d", rec2.Code)
	}

	var errBody map[string]string
	if err := json.NewDecoder(rec2.Body).Decode(&errBody); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if errBody["error"] != "sku already exists" {
		t.Fatalf("want 'sku already exists', got %q", errBody["error"])
	}
}

func TestCreate_InvalidPrice(t *testing.T) {
	truncate(t)
	repo := NewRepo(testPool)
	h := NewHandler(repo)

	body := strings.NewReader(`{"sku":"BAD","name":"X","price":-1,"stock":1}`)
	req := httptest.NewRequest("POST", "/api/products", body)
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}

	var errBody map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&errBody); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if errBody["error"] != "price must be > 0" {
		t.Fatalf("want 'price must be > 0', got %q", errBody["error"])
	}

	list, err := repo.List(context.Background(), "", 10, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("should not create on invalid price, got %d products", len(list))
	}
}

func TestCreate_BadJSON(t *testing.T) {
	truncate(t)
	repo := NewRepo(testPool)
	h := NewHandler(repo)

	body := strings.NewReader(`{not json}`)
	req := httptest.NewRequest("POST", "/api/products", body)
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}

	var errBody map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&errBody); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if errBody["error"] != "invalid request body" {
		t.Fatalf("want 'invalid request body', got %q", errBody["error"])
	}
}

func TestCreate_EmptySKU(t *testing.T) {
	truncate(t)
	repo := NewRepo(testPool)
	h := NewHandler(repo)

	// SKU из одних пробелов — TrimSpace в хендлере должен превратить в ""
	body := strings.NewReader(`{"sku":"   ","name":"X","price":1,"stock":1}`)
	req := httptest.NewRequest("POST", "/api/products", body)
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}

	var errBody map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&errBody); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if errBody["error"] != "sku is required" {
		t.Fatalf("want 'sku is required', got %q", errBody["error"])
	}

	list, err := repo.List(context.Background(), "", 10, 0)
	if err != nil {
		t.Fatalf("list after invalid create: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("should not create on empty sku, got %d products", len(list))
	}
}

func TestList_Search_NoMatch(t *testing.T) {
	truncate(t)
	repo := NewRepo(testPool)
	h := NewHandler(repo)

	// создаём пару товаров, ни один не содержит "Nope"
	for _, it := range []struct{ sku, name string }{
		{"SKU-A", "Widget"},
		{"SKU-B", "Gadget"},
	} {
		p := &Product{SKU: it.sku, Name: it.name, Price: "10.00", Stock: 1}
		if err := repo.Create(context.Background(), p); err != nil {
			t.Fatalf("create %s: %v", it.sku, err)
		}
	}

	req := httptest.NewRequest("GET", "/api/products?q=Nope", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}

	bodyBytes, err := io.ReadAll(rec.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if strings.TrimSpace(string(bodyBytes)) == "null" {
		t.Fatalf("want [], got null — repo.List must return make([]Product,0)")
	}
	var got []Product
	if err := json.Unmarshal(bodyBytes, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want 0 products, got %d: %+v", len(got), got)
	}
}

func TestList_Clamps(t *testing.T) {
	truncate(t)
	repo := NewRepo(testPool)
	h := NewHandler(repo)

	// создаём 3 товара
	for i := 1; i <= 3; i++ {
		p := &Product{SKU: "C-" + strconv.Itoa(i), Name: "N" + strconv.Itoa(i), Price: "1.00", Stock: 1}
		if err := repo.Create(context.Background(), p); err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	req := httptest.NewRequest("GET", "/api/products?limit=abc", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var got []Product
	json.NewDecoder(rec.Body).Decode(&got)
	if len(got) != 3 {
		t.Fatalf("want 3 (default limit), got %d", len(got))
	}

	// limit=0 → дефолт 20
	req = httptest.NewRequest("GET", "/api/products?limit=0", nil)
	rec = httptest.NewRecorder()
	h.List(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	got = nil
	json.NewDecoder(rec.Body).Decode(&got)
	if len(got) != 3 {
		t.Fatalf("limit=0: want 3 (default 20), got %d", len(got))
	}

	// limit=1000 → кламп до 100, вернёт 3
	req = httptest.NewRequest("GET", "/api/products?limit=1000", nil)
	rec = httptest.NewRecorder()
	h.List(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	got = nil
	json.NewDecoder(rec.Body).Decode(&got)
	if len(got) != 3 {
		t.Fatalf("limit=1000: want 3, got %d", len(got))
	}

	// offset=-1 → кламп до 0, вернёт 3
	req = httptest.NewRequest("GET", "/api/products?offset=-1", nil)
	rec = httptest.NewRecorder()
	h.List(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	got = nil
	json.NewDecoder(rec.Body).Decode(&got)
	if len(got) != 3 {
		t.Fatalf("offset=-1: want 3, got %d", len(got))
	}
}

func TestCreate_NameRequired(t *testing.T) {
	truncate(t)
	repo := NewRepo(testPool)
	h := NewHandler(repo)

	body := strings.NewReader(`{"sku":"N-1","name":"","price":1,"stock":1}`)
	req := httptest.NewRequest("POST", "/api/products", body)
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
	var errBody map[string]string
	json.NewDecoder(rec.Body).Decode(&errBody)
	if errBody["error"] != "name is required" {
		t.Fatalf("want 'name is required', got %q", errBody["error"])
	}
}

func TestUpdateStock_BadJSON(t *testing.T) {
	truncate(t)
	repo := NewRepo(testPool)
	h := NewHandler(repo)

	body := strings.NewReader(`{bad}`)
	req := httptest.NewRequest("PATCH", "/api/products/1/stock", body)
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()
	h.UpdateStock(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}

	var errBody map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&errBody); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if errBody["error"] != "invalid request body" {
		t.Fatalf("want 'invalid request body', got %q", errBody["error"])
	}
}

func TestUpdateStock_InvalidID(t *testing.T) {
	truncate(t)
	repo := NewRepo(testPool)
	h := NewHandler(repo)

	body := strings.NewReader(`{"stock":10}`)
	req := httptest.NewRequest("PATCH", "/api/products/abc/stock", body)
	req.SetPathValue("id", "abc")
	rec := httptest.NewRecorder()

	h.UpdateStock(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}

	var errBody map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&errBody); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if errBody["error"] != "invalid id" {
		t.Fatalf("want 'invalid id', got %q", errBody["error"])
	}
}

func TestUpdateStock_StockRequired(t *testing.T) {
	truncate(t)
	repo := NewRepo(testPool)
	h := NewHandler(repo)

	body := strings.NewReader(`{}`)
	req := httptest.NewRequest("PATCH", "/api/products/1/stock", body)
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()
	h.UpdateStock(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
	var errBody map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&errBody); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if errBody["error"] != "stock is required" {
		t.Fatalf("want 'stock is required', got %q", errBody["error"])
	}
}

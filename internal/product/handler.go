package product

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

type Handler struct {
	repo *Repo
}

func NewHandler(repo *Repo) *Handler {
	return &Handler{
		repo: repo,
	}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/products", h.Create)
	mux.HandleFunc("GET /api/products", h.List)
	mux.HandleFunc("GET /api/products/{id}", h.GetByID)
	mux.HandleFunc("PATCH /api/products/{id}/stock", h.UpdateStock)
}

// Create: POST /api/products
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var in CreateInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	in.SKU = strings.TrimSpace(in.SKU)

	if err := in.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	p := &Product{
		SKU:   in.SKU,
		Name:  in.Name,
		Price: strconv.FormatFloat(in.Price, 'f', 2, 64),
		Stock: in.Stock,
	}

	if err := h.repo.Create(r.Context(), p); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			switch pgErr.Code {
			case "23505":
				writeError(w, http.StatusConflict, "sku already exists")
				return
			case "23514":
				writeError(w, http.StatusBadRequest, "invalid product data")
				return
			}
		}
		log.Printf("create product: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

// writeError — единый формат {"error":"..."}
func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// writeJSON — ответ с JSON и заданным статусом
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// GetByID: GET /api/products/{id}
func (h *Handler) GetByID(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	p, err := h.repo.GetByID(r.Context(), id)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "product not found")
		return
	}
	if err != nil {
		log.Printf("get product by id: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, p)
}

// List: GET /api/products?limit=&offset=&q=
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")

	limit := parseIntDefault(r.URL.Query().Get("limit"), 20)
	offset := parseIntDefault(r.URL.Query().Get("offset"), 0)

	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	products, err := h.repo.List(r.Context(), q, limit, offset)
	if err != nil {
		log.Printf("list products: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, products)
}

func parseIntDefault(s string, def int) int {
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return v
}

// UpdateStock: PATCH /api/products/{id}/stock
// Тело: {"stock": 10} — УСТАНАВЛИВАЕТ остаток (не прибавляет).
func (h *Handler) UpdateStock(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	var in UpdateStockInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if in.Stock == nil {
		writeError(w, http.StatusBadRequest, "stock is required")
		return
	}
	if *in.Stock < 0 {
		writeError(w, http.StatusBadRequest, "stock must be >= 0")
		return
	}

	p, err := h.repo.UpdateStock(r.Context(), id, *in.Stock)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "product not found")
		return
	}
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23514" {
			writeError(w, http.StatusBadRequest, "invalid stock")
			return
		}
		log.Printf("update stock: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, p)
}

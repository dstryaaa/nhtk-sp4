package order

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/dstryaaa/nhtk-sp4/internal/httpx"
)

type Handler struct {
	repo *Repo
}

func NewHandler(repo *Repo) *Handler { return &Handler{repo: repo} }

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/orders", h.Create)
	mux.HandleFunc("GET /api/orders/{id}", h.GetByID)
	mux.HandleFunc("POST /api/orders/{id}/cancel", h.Cancel)
}

// Create: POST /api/orders
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var in CreateOrderInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := in.Validate(); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	items := mergeItems(in.Items)

	o, err := h.repo.CreateOrder(r.Context(), items)
	if errors.Is(err, ErrProductNotFound) {
		httpx.WriteError(w, http.StatusNotFound, "product not found")
		return
	}
	if errors.Is(err, ErrInsufficientStock) {
		httpx.WriteError(w, http.StatusConflict, "insufficient stock")
		return
	}
	if err != nil {
		log.Printf("create order: %v", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, o)
}

// GetByID: GET /api/orders/{id}
func (h *Handler) GetByID(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid id")
		return
	}

	o, err := h.repo.GetByID(r.Context(), id)
	if errors.Is(err, ErrNotFound) {
		httpx.WriteError(w, http.StatusNotFound, "order not found")
		return
	}
	if err != nil {
		log.Printf("get order by id: %v", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	httpx.WriteJSON(w, http.StatusOK, o)
}

// Cancel: POST /api/orders/{id}/cancel
func (h *Handler) Cancel(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid id")
		return
	}

	o, err := h.repo.Cancel(r.Context(), id)
	if errors.Is(err, ErrNotFound) {
		httpx.WriteError(w, http.StatusNotFound, "order not found")
		return
	}
	if err != nil {
		log.Printf("cancel order: %v", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	httpx.WriteJSON(w, http.StatusOK, o)
}

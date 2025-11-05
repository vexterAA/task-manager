package httpx

import (
	"example.com/yourapp/pkg/response"
	"net/http"
)

type Store interface {
	ListUsers() ([]string, error)
	CreateUser(name string) error
}

type Handler struct {
	mux   *http.ServeMux
	store Store
}

func New(s Store) http.Handler {
	h := &Handler{
		mux:   http.NewServeMux(),
		store: s,
	}
	h.routes()
	return h
}

func (h *Handler) routes() {
	h.mux.HandleFunc("GET /healthz", h.health)
	h.mux.HandleFunc("GET /users", h.users)
	h.mux.HandleFunc("POST /users", h.createUser)
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	response.JSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

func (h *Handler) users(w http.ResponseWriter, r *http.Request) {
	items, err := h.store.ListUsers()
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": "store"})
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) createUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	name := r.URL.Query().Get("name")
	if name == "" {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "name"})
		return
	}
	if err := h.store.CreateUser(name); err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": "store"})
		return
	}
	response.JSON(w, http.StatusCreated, map[string]string{"created": name})
}

package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"example.com/yourapp/internal/domain"
	"example.com/yourapp/internal/storage"
	"example.com/yourapp/pkg/response"
)

type Store interface {
	ListUsers() ([]domain.User, error)
	CreateUser(user domain.User) (domain.User, error)
	ListTasks(userID int64, status string) ([]domain.Task, error)
	GetTask(id int64) (domain.Task, error)
	CreateTask(task domain.Task) (domain.Task, error)
	UpdateTask(task domain.Task) (domain.Task, error)
	DeleteTask(id int64) error
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
	h.mux.HandleFunc("GET /tasks", h.tasks)
	h.mux.HandleFunc("POST /tasks", h.createTask)
	h.mux.HandleFunc("GET /tasks/{id}", h.task)
	h.mux.HandleFunc("PATCH /tasks/{id}", h.updateTask)
	h.mux.HandleFunc("DELETE /tasks/{id}", h.deleteTask)
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
	var req struct {
		TelegramUserID int64  `json:"telegram_user_id"`
		ChatID         int64  `json:"chat_id"`
		Timezone       string `json:"timezone"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "json")
		return
	}
	if req.TelegramUserID == 0 || req.ChatID == 0 {
		writeError(w, http.StatusBadRequest, "telegram_user_id/chat_id")
		return
	}
	if req.Timezone == "" {
		req.Timezone = "UTC"
	}
	user, err := h.store.CreateUser(domain.User{
		TelegramUserID: req.TelegramUserID,
		ChatID:         req.ChatID,
		Timezone:       req.Timezone,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store")
		return
	}
	response.JSON(w, http.StatusCreated, user)
}

func (h *Handler) tasks(w http.ResponseWriter, r *http.Request) {
	userID, err := parseInt64Query(r, "user_id")
	if err != nil || userID <= 0 {
		writeError(w, http.StatusBadRequest, "user_id")
		return
	}
	status := r.URL.Query().Get("status")
	if status != "" && !validTaskStatus(status) {
		writeError(w, http.StatusBadRequest, "status")
		return
	}
	items, err := h.store.ListTasks(userID, status)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store")
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) task(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "id")
		return
	}
	item, err := h.store.GetTask(id)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		writeError(w, http.StatusInternalServerError, "store")
		return
	}
	response.JSON(w, http.StatusOK, item)
}

func (h *Handler) createTask(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID     int64      `json:"user_id"`
		Text       string     `json:"text"`
		Status     string     `json:"status"`
		DueAt      *time.Time `json:"due_at"`
		RemindAt   *time.Time `json:"remind_at"`
		NotifiedAt *time.Time `json:"notified_at"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "json")
		return
	}
	if req.UserID <= 0 {
		writeError(w, http.StatusBadRequest, "user_id")
		return
	}
	req.Text = strings.TrimSpace(req.Text)
	if req.Text == "" {
		writeError(w, http.StatusBadRequest, "text")
		return
	}
	if req.Status == "" {
		req.Status = domain.TaskStatusActive
	}
	if !validTaskStatus(req.Status) {
		writeError(w, http.StatusBadRequest, "status")
		return
	}
	item, err := h.store.CreateTask(domain.Task{
		UserID:     req.UserID,
		Text:       req.Text,
		Status:     req.Status,
		DueAt:      req.DueAt,
		RemindAt:   req.RemindAt,
		NotifiedAt: req.NotifiedAt,
	})
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, http.StatusNotFound, "user")
			return
		}
		writeError(w, http.StatusInternalServerError, "store")
		return
	}
	response.JSON(w, http.StatusCreated, item)
}

func (h *Handler) updateTask(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "id")
		return
	}
	var req struct {
		Text       *string    `json:"text"`
		Status     *string    `json:"status"`
		DueAt      *time.Time `json:"due_at"`
		RemindAt   *time.Time `json:"remind_at"`
		NotifiedAt *time.Time `json:"notified_at"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "json")
		return
	}
	item, err := h.store.GetTask(id)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		writeError(w, http.StatusInternalServerError, "store")
		return
	}
	if req.Text != nil {
		trimmed := strings.TrimSpace(*req.Text)
		if trimmed == "" {
			writeError(w, http.StatusBadRequest, "text")
			return
		}
		item.Text = trimmed
	}
	if req.Status != nil {
		if !validTaskStatus(*req.Status) {
			writeError(w, http.StatusBadRequest, "status")
			return
		}
		item.Status = *req.Status
	}
	if req.DueAt != nil {
		item.DueAt = req.DueAt
	}
	if req.RemindAt != nil {
		item.RemindAt = req.RemindAt
	}
	if req.NotifiedAt != nil {
		item.NotifiedAt = req.NotifiedAt
	}
	item, err = h.store.UpdateTask(item)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		writeError(w, http.StatusInternalServerError, "store")
		return
	}
	response.JSON(w, http.StatusOK, item)
}

func (h *Handler) deleteTask(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "id")
		return
	}
	if err := h.store.DeleteTask(id); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		writeError(w, http.StatusInternalServerError, "store")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return errors.New("extra data")
	}
	return nil
}

func parseID(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.PathValue("id"), 10, 64)
}

func parseInt64Query(r *http.Request, key string) (int64, error) {
	return strconv.ParseInt(r.URL.Query().Get(key), 10, 64)
}

func validTaskStatus(s string) bool {
	return s == domain.TaskStatusActive || s == domain.TaskStatusDone
}

func writeError(w http.ResponseWriter, code int, msg string) {
	response.JSON(w, code, map[string]string{"error": msg})
}

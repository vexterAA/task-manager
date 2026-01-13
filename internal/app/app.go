package app

import (
	"net/http"

	"example.com/yourapp/internal/config"
	httphandlers "example.com/yourapp/internal/handler/http"
	"example.com/yourapp/internal/repository"
	"example.com/yourapp/internal/storage/memory"
	sqlstore "example.com/yourapp/internal/storage/sql"
)

type Store interface {
	httphandlers.Store
	repository.TaskRepository
	repository.UserRepository
}

type App struct {
	Config config.Config
	Router http.Handler
	Store  Store
}

func New(cfg config.Config) *App {
	var store Store
	switch cfg.Storage {
	case "sql":
		store = sqlstore.New(cfg.DBDriver, cfg.DBDSN)
	default:
		store = memory.New()
	}
	h := httphandlers.New(store)
	return &App{
		Config: cfg,
		Router: h,
		Store:  store,
	}
}

package app

import (
	"example.com/yourapp/internal/config"
	httphandlers "example.com/yourapp/internal/handler/http"
	"example.com/yourapp/internal/storage/memory"
	sqlstore "example.com/yourapp/internal/storage/sql"
	"net/http"
)

type App struct {
	Config config.Config
	Router http.Handler
}

func New(cfg config.Config) *App {
	var store interface {
		ListUsers() ([]string, error)
		CreateUser(name string) error
	}
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
	}
}

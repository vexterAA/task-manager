package main

import (
	"context"
	"example.com/yourapp/internal/app"
	"example.com/yourapp/internal/config"
	"example.com/yourapp/internal/server"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	cfg := config.Load()
	a := app.New(cfg)
	srv := server.New(cfg.HTTPAddr, a.Router)
	go func() {
		_ = srv.Start()
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	_ = srv.Stop(ctx)
}

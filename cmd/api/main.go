package main

import (
	"context"
	"errors"
	"example.com/yourapp/internal/app"
	"example.com/yourapp/internal/config"
	"example.com/yourapp/internal/server"
	"example.com/yourapp/internal/telegram"
	"example.com/yourapp/internal/usecase"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	cfg := config.Load()
	a := app.New(cfg)
	srv := server.New(cfg.HTTPAddr, a.Router)
	botCtx, botCancel := context.WithCancel(context.Background())
	if cfg.TelegramToken != "" {
		taskService := usecase.NewTaskService(a.Store)
		bot := telegram.NewBot(cfg.TelegramToken, taskService, a.Store, cfg.TelegramPoll)
		go func() {
			if err := bot.Run(botCtx); err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("telegram bot error: %v", err)
			}
		}()
	} else {
		botCancel()
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start()
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(stop)
	select {
	case sig := <-stop:
		log.Printf("signal %s received, shutting down", sig)
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("server error: %v", err)
		}
		botCancel()
		return
	}
	botCancel()
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := srv.Stop(ctx); err != nil {
		log.Printf("shutdown error: %v", err)
	}
	if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Printf("server error: %v", err)
	}
}

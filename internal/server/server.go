package server

import (
	"context"
	"net/http"
)

type Server struct {
	http *http.Server
}

func New(addr string, h http.Handler) *Server {
	return &Server{http: &http.Server{Addr: addr, Handler: h}}
}

func (s *Server) Start() error {
	return s.http.ListenAndServe()
}

func (s *Server) Stop(ctx context.Context) error {
	return s.http.Shutdown(ctx)
}

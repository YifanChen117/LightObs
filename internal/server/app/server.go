package app

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"lightobs/internal/server/api"
	"lightobs/internal/server/storage/duckdb"
)

type Server struct {
	httpServer *http.Server
	store      *duckdb.Store
}

func NewServer(cfg Config) (*Server, error) {
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = ":8080"
	}
	if cfg.DBPath == "" {
		cfg.DBPath = "./traffic.duckdb"
	}

	store, err := duckdb.NewStore(cfg.DBPath)
	if err != nil {
		return nil, err
	}

	router := gin.New()
	router.Use(gin.Recovery())

	h := api.NewHandlers(store)
	v1 := router.Group("/api/v1")
	{
		v1.POST("/upload", h.Upload)
		v1.GET("/query", h.Query)
	}

	return &Server{
		store: store,
		httpServer: &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		},
	}, nil
}

func (s *Server) ListenAndServe() error {
	return s.httpServer.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	_ = s.httpServer.Shutdown(ctx)
	return s.store.Close()
}

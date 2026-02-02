package app

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"lightobs/internal/server/api"
	"lightobs/internal/server/storage"
	"lightobs/internal/server/storage/duckdb"
	"lightobs/internal/server/storage/sqlite"
)

type Server struct {
	httpServer *http.Server
	store      storage.Store
}

func NewServer(cfg Config) (*Server, error) {
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = ":8080"
	}
	if cfg.DBDriver == "" {
		cfg.DBDriver = "duckdb"
	}
	if cfg.DBPath == "" {
		if cfg.DBDriver == "sqlite" {
			cfg.DBPath = "./traffic.sqlite"
		} else {
			cfg.DBPath = "./traffic.duckdb"
		}
	}

	var st storage.Store
	var err error
	switch cfg.DBDriver {
	case "sqlite":
		st, err = sqlite.NewStore(cfg.DBPath)
	default:
		st, err = duckdb.NewStore(cfg.DBPath)
	}
	if err != nil {
		return nil, err
	}

	router := gin.New()
	router.Use(gin.Recovery())

	h := api.NewHandlers(st)
	v1 := router.Group("/api/v1")
	{
		v1.POST("/upload", h.Upload)
		v1.GET("/query", h.Query)
	}

	return &Server{
		store: st,
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

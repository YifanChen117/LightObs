package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"lightobs/internal/server/storage"
	"lightobs/pkg/model"
)

type fakeStore struct {
	queryByIP  func(ctx context.Context, ip string, limit int) ([]model.TrafficLog, error)
	queryByPID func(ctx context.Context, pid int, limit int) ([]model.TrafficLog, error)
}

func (f *fakeStore) Insert(ctx context.Context, logEntry *model.TrafficLog) error {
	return nil
}

func (f *fakeStore) QueryByIP(ctx context.Context, ip string, limit int) ([]model.TrafficLog, error) {
	return f.queryByIP(ctx, ip, limit)
}

func (f *fakeStore) QueryByPID(ctx context.Context, pid int, limit int) ([]model.TrafficLog, error) {
	return f.queryByPID(ctx, pid, limit)
}

func (f *fakeStore) Close() error {
	return nil
}

var _ storage.Store = (*fakeStore)(nil)

func TestQueryByPID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handled := false
	store := &fakeStore{
		queryByPID: func(ctx context.Context, pid int, limit int) ([]model.TrafficLog, error) {
			handled = true
			if pid != 321 {
				t.Fatalf("pid=%d", pid)
			}
			return []model.TrafficLog{{PID: pid}}, nil
		},
		queryByIP: func(ctx context.Context, ip string, limit int) ([]model.TrafficLog, error) {
			t.Fatalf("unexpected QueryByIP")
			return nil, nil
		},
	}
	h := NewHandlers(store)
	r := gin.New()
	r.GET("/api/v1/query", h.Query)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/query?pid=321", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d", w.Code)
	}
	if !handled {
		t.Fatalf("not handled")
	}
	var rows []model.TrafficLog
	if err := json.Unmarshal(w.Body.Bytes(), &rows); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(rows) != 1 || rows[0].PID != 321 {
		t.Fatalf("rows=%v", rows)
	}
}

func TestQueryByIP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handled := false
	store := &fakeStore{
		queryByPID: func(ctx context.Context, pid int, limit int) ([]model.TrafficLog, error) {
			t.Fatalf("unexpected QueryByPID")
			return nil, nil
		},
		queryByIP: func(ctx context.Context, ip string, limit int) ([]model.TrafficLog, error) {
			handled = true
			if ip != "10.0.0.1" {
				t.Fatalf("ip=%s", ip)
			}
			return []model.TrafficLog{{SrcIP: ip}}, nil
		},
	}
	h := NewHandlers(store)
	r := gin.New()
	r.GET("/api/v1/query", h.Query)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/query?ip=10.0.0.1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d", w.Code)
	}
	if !handled {
		t.Fatalf("not handled")
	}
}

func TestQueryMissingParams(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &fakeStore{
		queryByPID: func(ctx context.Context, pid int, limit int) ([]model.TrafficLog, error) {
			return nil, nil
		},
		queryByIP: func(ctx context.Context, ip string, limit int) ([]model.TrafficLog, error) {
			return nil, nil
		},
	}
	h := NewHandlers(store)
	r := gin.New()
	r.GET("/api/v1/query", h.Query)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/query", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", w.Code)
	}
}

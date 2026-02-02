package report

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"lightobs/pkg/model"
)

func TestClient_Upload(t *testing.T) {
	// Mock Server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/upload" {
			t.Errorf("Expected path /api/v1/upload, got %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Method != http.MethodPost {
			t.Errorf("Expected method POST, got %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		var logEntry model.TrafficLog
		if err := json.NewDecoder(r.Body).Decode(&logEntry); err != nil {
			t.Errorf("Invalid JSON: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if logEntry.PID != 1234 {
			t.Errorf("Expected PID 1234, got %d", logEntry.PID)
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Extract port from mocked server
	addr := server.Listener.Addr().(*net.TCPAddr)
	
	c := NewClient("127.0.0.1", addr.Port, time.Second)
	
	logEntry := &model.TrafficLog{
		PID: 1234,
	}
	
	if err := c.Upload(context.Background(), logEntry); err != nil {
		t.Fatalf("Upload failed: %v", err)
	}
}

package sqlite

import (
	"context"
	"os"
	"testing"
	"time"

	"lightobs/pkg/model"
)

func TestStore_InsertAndQuery(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "test_traffic_*.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	s, err := NewStore(tmpFile.Name())
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	now := time.Now().Truncate(time.Second) // SQLite precision

	log1 := &model.TrafficLog{
		Timestamp:  now,
		SrcIP:      "192.168.1.10",
		SrcPort:    12345,
		DstIP:      "10.0.0.1",
		DstPort:    80,
		PID:        1001,
		HTTPMethod: "GET",
		HTTPPath:   "/test",
		StatusCode: 200,
		LatencyMS:  50,
		PacketSize: 1024,
	}

	if err := s.Insert(ctx, log1); err != nil {
		t.Fatalf("Insert failed: %v", err)
	}

	// Test QueryByIP
	logs, err := s.QueryByIP(ctx, "192.168.1.10", 10)
	if err != nil {
		t.Fatalf("QueryByIP failed: %v", err)
	}
	if len(logs) != 1 {
		t.Errorf("Expected 1 log, got %d", len(logs))
	} else {
		if logs[0].PID != 1001 {
			t.Errorf("Expected PID 1001, got %d", logs[0].PID)
		}
	}

	// Test QueryByPID
	logs, err = s.QueryByPID(ctx, 1001, 10)
	if err != nil {
		t.Fatalf("QueryByPID failed: %v", err)
	}
	if len(logs) != 1 {
		t.Errorf("Expected 1 log, got %d", len(logs))
	}

	// Test QueryByPID Miss
	logs, err = s.QueryByPID(ctx, 9999, 10)
	if err != nil {
		t.Fatalf("QueryByPID miss failed: %v", err)
	}
	if len(logs) != 0 {
		t.Errorf("Expected 0 logs, got %d", len(logs))
	}
}

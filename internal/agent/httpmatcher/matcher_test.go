package httpmatcher

import (
	"testing"
	"time"
)

func TestMatcher_Observe(t *testing.T) {
	m := NewMatcher(5 * time.Second)

	now := time.Now()
	clientIP := "192.168.1.10"
	clientPort := 12345
	serverIP := "10.0.0.1"
	serverPort := 80

	// 1. Observe Request
	reqPayload := []byte("GET /api/test HTTP/1.1\r\nHost: example.com\r\n\r\n")
	reqMeta := PacketMeta{
		Timestamp:  now,
		SrcIP:      clientIP,
		SrcPort:    clientPort,
		DstIP:      serverIP,
		DstPort:    serverPort,
		Payload:    reqPayload,
		PacketSize: len(reqPayload),
	}

	if !m.ObserveRequest(reqMeta) {
		t.Error("ObserveRequest should return true for valid HTTP request")
	}

	// Verify request is stored
	key := flowKey(clientIP, clientPort, serverIP, serverPort)
	m.mu.Lock()
	if _, ok := m.requests[key]; !ok {
		t.Error("Request should be stored in map")
	}
	m.mu.Unlock()

	// 2. Observe Response
	respPayload := []byte("HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n")
	respMeta := PacketMeta{
		Timestamp:  now.Add(100 * time.Millisecond),
		SrcIP:      serverIP,   // Response src is Server
		SrcPort:    serverPort,
		DstIP:      clientIP,   // Response dst is Client
		DstPort:    clientPort,
		Payload:    respPayload,
		PacketSize: len(respPayload),
	}

	logEntry, ok := m.ObserveResponse(respMeta)
	if !ok {
		t.Error("ObserveResponse should return true for matched response")
	}
	if logEntry == nil {
		t.Fatal("LogEntry should not be nil")
	}

	if logEntry.HTTPMethod != "GET" {
		t.Errorf("Expected method GET, got %s", logEntry.HTTPMethod)
	}
	if logEntry.HTTPPath != "/api/test" {
		t.Errorf("Expected path /api/test, got %s", logEntry.HTTPPath)
	}
	if logEntry.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", logEntry.StatusCode)
	}
	if logEntry.LatencyMS != 100 {
		t.Errorf("Expected latency 100ms, got %d", logEntry.LatencyMS)
	}

	// Verify request is removed
	m.mu.Lock()
	if _, ok := m.requests[key]; ok {
		t.Error("Request should be removed after matching response")
	}
	m.mu.Unlock()
}

func TestMatcher_Cleanup(t *testing.T) {
	timeout := 100 * time.Millisecond
	m := NewMatcher(timeout)

	now := time.Now()
	reqPayload := []byte("GET /old HTTP/1.1\r\n\r\n")
	reqMeta := PacketMeta{
		Timestamp: now.Add(-200 * time.Millisecond), // Expired
		SrcIP:     "1.1.1.1",
		SrcPort:   1000,
		DstIP:     "2.2.2.2",
		DstPort:   80,
		Payload:   reqPayload,
	}

	m.ObserveRequest(reqMeta)

	// Before cleanup
	m.mu.Lock()
	if len(m.requests) != 1 {
		t.Errorf("Expected 1 request before cleanup, got %d", len(m.requests))
	}
	m.mu.Unlock()

	// Cleanup
	m.Cleanup(now)

	// After cleanup
	m.mu.Lock()
	if len(m.requests) != 0 {
		t.Errorf("Expected 0 requests after cleanup, got %d", len(m.requests))
	}
	m.mu.Unlock()
}

func TestMatcher_IgnoreNonHTTP(t *testing.T) {
	m := NewMatcher(time.Second)
	meta := PacketMeta{
		Payload: []byte("SSH-2.0-OpenSSH_8.2p1\r\n"),
	}
	if m.ObserveRequest(meta) {
		t.Error("Should ignore non-HTTP traffic")
	}
}

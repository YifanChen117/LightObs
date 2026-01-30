package httpmatcher

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"lightobs/pkg/model"
)

type PacketMeta struct {
	Timestamp  time.Time
	SrcIP      string
	DstIP      string
	SrcPort    int
	DstPort    int
	Payload    []byte
	PacketSize int
}

type requestState struct {
	ts     time.Time
	method string
	path   string
}

type Matcher struct {
	mu       sync.Mutex
	requests map[string]requestState
	timeout  time.Duration
}

func NewMatcher(timeout time.Duration) *Matcher {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Matcher{
		requests: make(map[string]requestState, 1024),
		timeout:  timeout,
	}
}

func (m *Matcher) ObserveRequest(p PacketMeta) bool {
	method, path, ok := parseHTTPRequestLine(p.Payload)
	if !ok {
		return false
	}

	// Best-Effort：这里不做 TCP 流重组，只要看到 payload 像 HTTP 请求就记一条。
	// Key 采用 4 元组（client -> server）：ClientIP:ClientPort-ServerIP:ServerPort
	key := flowKey(p.SrcIP, p.SrcPort, p.DstIP, p.DstPort)

	m.mu.Lock()
	m.requests[key] = requestState{ts: p.Timestamp, method: method, path: path}
	m.mu.Unlock()
	return true
}

func (m *Matcher) ObserveResponse(p PacketMeta) (*model.TrafficLog, bool) {
	status, ok := parseHTTPResponseStatus(p.Payload)
	if !ok {
		return nil, false
	}

	// Response 方向与 Request 相反，所以要把 src/dst 交换后构造 key 才能命中。
	key := flowKey(p.DstIP, p.DstPort, p.SrcIP, p.SrcPort)

	m.mu.Lock()
	req, found := m.requests[key]
	if found {
		delete(m.requests, key)
	}
	m.mu.Unlock()

	if !found {
		return nil, false
	}

	latency := p.Timestamp.Sub(req.ts).Milliseconds()
	if latency < 0 {
		latency = 0
	}

	return &model.TrafficLog{
		Timestamp:  req.ts,
		SrcIP:      p.DstIP,
		SrcPort:    p.DstPort,
		DstIP:      p.SrcIP,
		DstPort:    p.SrcPort,
		HTTPMethod: req.method,
		HTTPPath:   req.path,
		StatusCode: status,
		LatencyMS:  latency,
		PacketSize: p.PacketSize,
	}, true
}

func (m *Matcher) Cleanup(now time.Time) {
	deadline := now.Add(-m.timeout)
	m.mu.Lock()
	for k, v := range m.requests {
		if v.ts.Before(deadline) {
			delete(m.requests, k)
		}
	}
	m.mu.Unlock()
}

func flowKey(clientIP string, clientPort int, serverIP string, serverPort int) string {
	return fmt.Sprintf("%s:%d-%s:%d", clientIP, clientPort, serverIP, serverPort)
}

func parseHTTPRequestLine(payload []byte) (method string, path string, ok bool) {
	line := firstLine(payload)
	if len(line) == 0 {
		return "", "", false
	}

	// 常见方法的快速判断，避免 strings.Fields 在大量非 HTTP payload 上造成开销。
	if !(bytes.HasPrefix(line, []byte("GET ")) ||
		bytes.HasPrefix(line, []byte("POST ")) ||
		bytes.HasPrefix(line, []byte("PUT ")) ||
		bytes.HasPrefix(line, []byte("DELETE ")) ||
		bytes.HasPrefix(line, []byte("HEAD ")) ||
		bytes.HasPrefix(line, []byte("OPTIONS ")) ||
		bytes.HasPrefix(line, []byte("PATCH "))) {
		return "", "", false
	}

	parts := strings.Fields(string(line))
	if len(parts) < 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func parseHTTPResponseStatus(payload []byte) (status int, ok bool) {
	line := firstLine(payload)
	if len(line) == 0 || !bytes.HasPrefix(line, []byte("HTTP/1.")) {
		return 0, false
	}
	parts := strings.Fields(string(line))
	if len(parts) < 2 {
		return 0, false
	}
	code, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, false
	}
	return code, true
}

func firstLine(payload []byte) []byte {
	// HTTP 行以 \r\n 结尾；为了 best-effort 也兼容仅 \n 的情况。
	if i := bytes.IndexByte(payload, '\n'); i >= 0 {
		line := payload[:i]
		return bytes.TrimRight(line, "\r")
	}
	return payload
}


package model

import "time"

type TrafficLog struct {
	Timestamp  time.Time `json:"timestamp"`
	SrcIP      string    `json:"src_ip"`
	SrcPort    int       `json:"src_port"`
	DstIP      string    `json:"dst_ip"`
	DstPort    int       `json:"dst_port"`
	PID        int       `json:"pid"`
	HTTPMethod string    `json:"http_method"`
	HTTPPath   string    `json:"http_path"`
	StatusCode int       `json:"status_code"`
	LatencyMS  int64     `json:"latency_ms"`
	PacketSize int       `json:"packet_size"`
}

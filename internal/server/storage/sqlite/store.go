package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"

	"lightobs/pkg/model"
)

type Store struct {
	db  *sql.DB
	ins *sql.Stmt
}

func NewStore(path string) (*Store, error) {
	if path == "" {
		path = "./traffic.sqlite"
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("打开 SQLite 失败：%w", err)
	}
	s := &Store{db: db}
	if err := s.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) init() error {
	ddl := `
CREATE TABLE IF NOT EXISTS traffic_logs (
	timestamp   TIMESTAMP,
	src_ip      TEXT,
	src_port    INTEGER,
	dst_ip      TEXT,
	dst_port    INTEGER,
	pid         INTEGER,
	http_method TEXT,
	http_path   TEXT,
	status_code INTEGER,
	latency_ms  INTEGER,
	packet_size INTEGER
);
CREATE INDEX IF NOT EXISTS idx_traffic_src_ip ON traffic_logs(src_ip);
CREATE INDEX IF NOT EXISTS idx_traffic_dst_ip ON traffic_logs(dst_ip);
CREATE INDEX IF NOT EXISTS idx_traffic_pid    ON traffic_logs(pid);
`
	if _, err := s.db.Exec(ddl); err != nil {
		return fmt.Errorf("建表失败：%w", err)
	}
	stmt, err := s.db.Prepare(`
INSERT INTO traffic_logs (
	timestamp, src_ip, src_port, dst_ip, dst_port, pid,
	http_method, http_path, status_code, latency_ms, packet_size
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
`)
	if err != nil {
		return fmt.Errorf("准备插入语句失败：%w", err)
	}
	s.ins = stmt
	return nil
}

func (s *Store) Insert(ctx context.Context, logEntry *model.TrafficLog) error {
	if logEntry == nil {
		return fmt.Errorf("logEntry 为空")
	}
	_, err := s.ins.ExecContext(ctx,
		logEntry.Timestamp,
		logEntry.SrcIP,
		logEntry.SrcPort,
		logEntry.DstIP,
		logEntry.DstPort,
		logEntry.PID,
		logEntry.HTTPMethod,
		logEntry.HTTPPath,
		logEntry.StatusCode,
		logEntry.LatencyMS,
		logEntry.PacketSize,
	)
	if err != nil {
		return fmt.Errorf("插入失败：%w", err)
	}
	return nil
}

func (s *Store) QueryByIP(ctx context.Context, ip string, limit int) ([]model.TrafficLog, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT
	timestamp, src_ip, src_port, dst_ip, dst_port, pid,
	http_method, http_path, status_code, latency_ms, packet_size
FROM traffic_logs
WHERE src_ip = ? OR dst_ip = ?
ORDER BY timestamp DESC
LIMIT ?;
`, ip, ip, limit)
	if err != nil {
		return nil, fmt.Errorf("查询失败：%w", err)
	}
	defer rows.Close()
	out := make([]model.TrafficLog, 0, 64)
	for rows.Next() {
		var r model.TrafficLog
		if err := rows.Scan(
			&r.Timestamp,
			&r.SrcIP,
			&r.SrcPort,
			&r.DstIP,
			&r.DstPort,
			&r.PID,
			&r.HTTPMethod,
			&r.HTTPPath,
			&r.StatusCode,
			&r.LatencyMS,
			&r.PacketSize,
		); err != nil {
			return nil, fmt.Errorf("读取行失败：%w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历结果失败：%w", err)
	}
	return out, nil
}

func (s *Store) QueryByPID(ctx context.Context, pid int, limit int) ([]model.TrafficLog, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT
	timestamp, src_ip, src_port, dst_ip, dst_port, pid,
	http_method, http_path, status_code, latency_ms, packet_size
FROM traffic_logs
WHERE pid = ?
ORDER BY timestamp DESC
LIMIT ?;
`, pid, limit)
	if err != nil {
		return nil, fmt.Errorf("查询失败：%w", err)
	}
	defer rows.Close()
	out := make([]model.TrafficLog, 0, 64)
	for rows.Next() {
		var r model.TrafficLog
		if err := rows.Scan(
			&r.Timestamp,
			&r.SrcIP,
			&r.SrcPort,
			&r.DstIP,
			&r.DstPort,
			&r.PID,
			&r.HTTPMethod,
			&r.HTTPPath,
			&r.StatusCode,
			&r.LatencyMS,
			&r.PacketSize,
		); err != nil {
			return nil, fmt.Errorf("读取行失败：%w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历结果失败：%w", err)
	}
	return out, nil
}

func (s *Store) Close() error {
	var firstErr error
	if s.ins != nil {
		if err := s.ins.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if s.db != nil {
		if err := s.db.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

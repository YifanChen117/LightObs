package duckdb

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/marcboeker/go-duckdb"

	"lightobs/pkg/model"
)

type Store struct {
	db   *sql.DB
	ins  *sql.Stmt
	path string
}

func NewStore(path string) (*Store, error) {
	// DuckDB 是嵌入式分析型数据库：单文件、零依赖，适合本项目在本地/集群内快速落盘与查询。
	db, err := sql.Open("duckdb", path)
	if err != nil {
		return nil, fmt.Errorf("打开 DuckDB 失败：%w", err)
	}

	s := &Store{db: db, path: path}
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
	src_ip      VARCHAR,
	src_port    INTEGER,
	dst_ip      VARCHAR,
	dst_port    INTEGER,
	http_method VARCHAR,
	http_path   VARCHAR,
	status_code INTEGER,
	latency_ms  BIGINT,
	packet_size INTEGER
);`
	if _, err := s.db.Exec(ddl); err != nil {
		return fmt.Errorf("建表失败：%w", err)
	}

	// 插入使用 prepared statement，减少每次写入的 SQL 解析开销。
	stmt, err := s.db.Prepare(`
INSERT INTO traffic_logs (
	timestamp, src_ip, src_port, dst_ip, dst_port,
	http_method, http_path, status_code, latency_ms, packet_size
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
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
	timestamp, src_ip, src_port, dst_ip, dst_port,
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

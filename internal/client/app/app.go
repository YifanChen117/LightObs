package app

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/olekukonko/tablewriter"

	"lightobs/pkg/model"
)

func Run(cfg Config) error {
	u, err := url.Parse(cfg.Server)
	if err != nil {
		return fmt.Errorf("server 参数非法：%w", err)
	}
	u.Path = "/api/v1/query"
	q := u.Query()
	if cfg.PID > 0 {
		q.Set("pid", fmt.Sprintf("%d", cfg.PID))
	} else {
		q.Set("ip", cfg.IP)
	}
	u.RawQuery = q.Encode()

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(u.String())
	if err != nil {
		return fmt.Errorf("请求失败：%w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("查询失败：status=%s body=%s", resp.Status, string(b))
	}

	var rows []model.TrafficLog
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		return fmt.Errorf("解析响应 JSON 失败：%w", err)
	}

	renderTable(rows)
	return nil
}

func renderTable(rows []model.TrafficLog) {
	t := tablewriter.NewWriter(os.Stdout)
	t.SetHeader([]string{"Time", "PID", "Source", "Destination", "Method", "Path", "Status", "Latency(ms)", "Size"})
	t.SetAutoWrapText(false)
	t.SetRowLine(false)

	for _, r := range rows {
		t.Append([]string{
			r.Timestamp.Format(time.RFC3339Nano),
			fmt.Sprintf("%d", r.PID),
			fmt.Sprintf("%s:%d", r.SrcIP, r.SrcPort),
			fmt.Sprintf("%s:%d", r.DstIP, r.DstPort),
			r.HTTPMethod,
			r.HTTPPath,
			fmt.Sprintf("%d", r.StatusCode),
			fmt.Sprintf("%d", r.LatencyMS),
			fmt.Sprintf("%d", r.PacketSize),
		})
	}
	t.Render()
}

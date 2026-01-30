package report

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"lightobs/pkg/model"
)

type Client struct {
	url    string
	client *http.Client
}

func NewClient(serverIP string, serverPort int, timeout time.Duration) *Client {
	return &Client{
		url: fmt.Sprintf("http://%s:%d/api/v1/upload", serverIP, serverPort),
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *Client) Upload(ctx context.Context, logEntry *model.TrafficLog) error {
	body, err := json.Marshal(logEntry)
	if err != nil {
		return fmt.Errorf("序列化 JSON 失败：%w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("构造 HTTP 请求失败：%w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("POST 上报失败：%w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("POST 上报失败：status=%s", resp.Status)
	}
	return nil
}


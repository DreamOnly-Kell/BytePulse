// Package daemonclient is a small HTTP client for the daemon local API.
// daemonclient 包是 daemon 本机 API 的轻量 HTTP 客户端。
package daemonclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"bytepulse/internal/processstate"
)

// Client talks to the daemon API (default http://127.0.0.1:8988).
// Client 与 daemon API 通信（默认 http://127.0.0.1:8988）。
type Client struct {
	// BaseURL is the origin without trailing slash.
	// BaseURL 是不带尾部斜杠的源地址。
	BaseURL string
	// HTTPClient is optional; a 2s timeout client is used when nil.
	// HTTPClient 可选；为 nil 时使用 2 秒超时客户端。
	HTTPClient *http.Client
}

// New builds a client for addr (host:port or full URL).
// New 为 addr（host:port 或完整 URL）构建客户端。
func New(addr string) *Client {
	// Accept "127.0.0.1:8988" without a scheme.
	// 接受不带 scheme 的 "127.0.0.1:8988"。
	if !strings.HasPrefix(addr, "http://") && !strings.HasPrefix(addr, "https://") {
		addr = "http://" + addr
	}
	return &Client{
		// Normalize trailing slash for path joining.
		// 规范化尾部斜杠以便拼接路径。
		BaseURL: strings.TrimRight(addr, "/"),
		HTTPClient: &http.Client{
			// Keep polls snappy so TUI/CLI stay responsive when daemon is down.
			// 轮询保持短超时，daemon 宕机时 TUI/CLI 仍响应迅速。
			Timeout: 2 * time.Second,
		},
	}
}

// Health hits GET /api/health and requires HTTP 200.
// Health 请求 GET /api/health 并要求 HTTP 200。
func (c *Client) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/health", nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("daemon API health returned status %d", resp.StatusCode)
	}
	return nil
}

// Processes fetches the realtime process summary list.
// Processes 拉取实时进程摘要列表。
func (c *Client) Processes(ctx context.Context, limit int) ([]processstate.ProcessConnectionSummary, error) {
	values := url.Values{}
	// Only send limit when positive so the server can use its default.
	// 仅在 limit 为正时发送，以便服务端使用默认值。
	if limit > 0 {
		values.Set("limit", strconv.Itoa(limit))
	}
	var out []processstate.ProcessConnectionSummary
	if err := c.get(ctx, "/api/processes", values, &out); err != nil {
		return nil, err
	}
	// Normalize JSON null arrays to empty slices for callers.
	// 将 JSON null 数组规范为空 slice，方便调用方。
	if out == nil {
		out = []processstate.ProcessConnectionSummary{}
	}
	return out, nil
}

// ProcessConnections fetches socket details for one process_key.
// ProcessConnections 拉取某 process_key 的套接字明细。
func (c *Client) ProcessConnections(ctx context.Context, processKey string) ([]processstate.ProcessConnectionDetail, error) {
	values := url.Values{"process_key": []string{processKey}}
	var out []processstate.ProcessConnectionDetail
	if err := c.get(ctx, "/api/processes/connections", values, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []processstate.ProcessConnectionDetail{}
	}
	return out, nil
}

// get performs GET path?query and JSON-decodes into out.
// get 执行 GET path?query 并将 JSON 解码到 out。
func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	u := c.BaseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// Any non-2xx is treated as failure (body error JSON is not parsed here).
	// 任何非 2xx 视为失败（此处不解析 body 中的 error JSON）。
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("daemon API returned status %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// httpClient returns the configured client or a default 2s timeout client.
// httpClient 返回已配置客户端，或默认 2 秒超时客户端。
func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 2 * time.Second}
}

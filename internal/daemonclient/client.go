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

type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

func New(addr string) *Client {
	if !strings.HasPrefix(addr, "http://") && !strings.HasPrefix(addr, "https://") {
		addr = "http://" + addr
	}
	return &Client{
		BaseURL: strings.TrimRight(addr, "/"),
		HTTPClient: &http.Client{
			Timeout: 2 * time.Second,
		},
	}
}

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

func (c *Client) Processes(ctx context.Context, limit int) ([]processstate.ProcessConnectionSummary, error) {
	values := url.Values{}
	if limit > 0 {
		values.Set("limit", strconv.Itoa(limit))
	}
	var out []processstate.ProcessConnectionSummary
	if err := c.get(ctx, "/api/processes", values, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []processstate.ProcessConnectionSummary{}
	}
	return out, nil
}

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
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("daemon API returned status %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 2 * time.Second}
}

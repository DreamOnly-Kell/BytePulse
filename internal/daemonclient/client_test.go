package daemonclient

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestClientProcessesDecodesJSON(t *testing.T) {
	client := New("http://bytepulse.test")
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`[{"pid":1,"process_name":"curl","process_key":"1:0","connection_count":2}]`)),
			Request:    r,
		}, nil
	})}
	got, err := client.Processes(context.Background(), 10)
	if err != nil {
		t.Fatalf("processes: %v", err)
	}
	if len(got) != 1 || got[0].ConnectionCount != 2 {
		t.Fatalf("got=%v", got)
	}
}

func TestClientHealthReturnsErrorOnNonOKStatus(t *testing.T) {
	client := New("http://bytepulse.test")
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Body:       io.NopCloser(strings.NewReader("down")),
			Request:    r,
		}, nil
	})}
	if err := client.Health(context.Background()); err == nil {
		t.Fatalf("expected error")
	}
}

func TestClientHealthInfoDecodesIdentity(t *testing.T) {
	client := New("http://bytepulse.test")
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true,"pid":2468,"instance_id":"instance-2468"}`)),
			Request:    r,
		}, nil
	})}
	got, err := client.HealthInfo(context.Background())
	if err != nil {
		t.Fatalf("health info: %v", err)
	}
	if !got.OK || got.PID != 2468 || got.InstanceID != "instance-2468" {
		t.Fatalf("health=%+v", got)
	}
}

func TestClientUsesHTTPPrefixForPlainAddr(t *testing.T) {
	client := New("127.0.0.1:8988")
	if !strings.HasPrefix(client.BaseURL, "http://") {
		t.Fatalf("base url=%q, want http prefix", client.BaseURL)
	}
}

package animesucc

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

const (
	defaultBaseURL = "https://www.animesaturn.cx"
	userAgent      = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)

type Client struct {
	httpClient *http.Client
	baseURL    string
}

type headerTransport struct {
	base http.RoundTripper
}

func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", userAgent)
	}
	if req.Header.Get("Referer") == "" {
		req.Header.Set("Referer", defaultBaseURL+"/")
	}
	return t.base.RoundTrip(req)
}

func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{
			Transport: &headerTransport{base: http.DefaultTransport},
		},
		baseURL: defaultBaseURL,
	}
}

func (c *Client) fetchURL(ctx context.Context, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: status %d", u, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

package animesucc

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"strings"
)

const (
	defaultBaseURL = "https://www.animesaturn.net"
	userAgent      = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
)

type Client struct {
	httpClient *http.Client
	baseURL    string
}

// Option configures a Client.
type Option func(*Client)

// WithBaseURL overrides the AnimeSaturn base URL. Useful when the site moves to a
// new mirror domain, or to point tests at an httptest server.
func WithBaseURL(u string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") }
}

// WithHTTPClient injects a custom *http.Client (e.g. one wired to an httptest server).
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.httpClient = h }
}

type headerTransport struct {
	base    http.RoundTripper
	referer string
}

func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", userAgent)
	}
	if req.Header.Get("Referer") == "" && t.referer != "" {
		req.Header.Set("Referer", t.referer)
	}
	return t.base.RoundTrip(req)
}

// NewClient builds a Client. The base URL defaults to defaultBaseURL, is overridable
// via the ANIMESUCC_BASE_URL env var, and can be further overridden with WithBaseURL.
func NewClient(opts ...Option) *Client {
	base := defaultBaseURL
	if env := strings.TrimRight(os.Getenv("ANIMESUCC_BASE_URL"), "/"); env != "" {
		base = env
	}
	c := &Client{baseURL: base}
	for _, opt := range opts {
		opt(c)
	}
	if c.httpClient == nil {
		jar, _ := cookiejar.New(nil)
		c.httpClient = &http.Client{
			Jar:       jar,
			Transport: &headerTransport{base: http.DefaultTransport, referer: c.baseURL + "/"},
		}
	}
	return c
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

package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

const (
	proxyUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	proxyReferer   = "https://www.animesaturn.cx/"
)

// hopByHop headers must not be forwarded (RFC 2616 §13.5.1).
var hopByHop = map[string]bool{
	"Connection":          true,
	"Keep-Alive":          true,
	"Proxy-Authenticate":  true,
	"Proxy-Authorization": true,
	"Te":                  true,
	"Trailers":            true,
	"Transfer-Encoding":   true,
	"Upgrade":             true,
}

var proxyHTTPClient = &http.Client{}

func (s *server) proxyStream(w http.ResponseWriter, r *http.Request) {
	rawURL := r.URL.Query().Get("url")
	if rawURL == "" {
		slog.Info("proxy/stream: missing url parameter")
		http.Error(w, "missing url", http.StatusBadRequest)
		return
	}
	slog.Debug("proxy/stream", "url", rawURL, "range", r.Header.Get("Range"))
	w.Header().Set("Accept-Ranges", "bytes")
	proxyFetch(w, r, rawURL, "video/mp4")
}

func (s *server) proxyTS(w http.ResponseWriter, r *http.Request) {
	rawURL := r.URL.Query().Get("url")
	if rawURL == "" {
		slog.Info("proxy/ts: missing url parameter")
		http.Error(w, "missing url", http.StatusBadRequest)
		return
	}
	proxyFetch(w, r, rawURL, "video/mp2t")
}

func (s *server) proxyM3U8(w http.ResponseWriter, r *http.Request) {
	rawURL := r.URL.Query().Get("url")
	if rawURL == "" {
		slog.Info("proxy/m3u8: missing url parameter")
		http.Error(w, "missing url", http.StatusBadRequest)
		return
	}
	slog.Debug("proxy/m3u8", "url", rawURL)

	body, err := fetchBytes(rawURL)
	if err != nil {
		slog.Info("proxy/m3u8: fetching playlist failed", "url", rawURL, "err", err)
		http.Error(w, fmt.Sprintf("fetching playlist: %v", err), http.StatusBadGateway)
		return
	}

	base, err := url.Parse(rawURL)
	if err != nil {
		slog.Info("proxy/m3u8: invalid url", "url", rawURL, "err", err)
		http.Error(w, "invalid url", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	_, _ = w.Write([]byte(rewriteM3U8(string(body), base)))
}

// proxyFetch streams targetURL to w forwarding all browser headers.
// Tries direct HTTP first; on failure falls back to curl.
func proxyFetch(w http.ResponseWriter, r *http.Request, targetURL, fallbackType string) {
	req, err := http.NewRequest(http.MethodGet, targetURL, nil)
	if err != nil {
		proxyCurl(w, r, targetURL, fallbackType)
		return
	}
	copyRequestHeaders(req.Header, r.Header)

	resp, err := proxyHTTPClient.Do(req)
	if err != nil {
		// Connection-level failure (no response at all): try curl.
		proxyCurl(w, r, targetURL, fallbackType)
		return
	}
	defer resp.Body.Close()

	// Upstream responded — forward its status and headers transparently.
	forwardResponseHeaders(w, resp.Header, fallbackType)
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// proxyCurl streams targetURL to w via curl, using -i to capture and forward
// all upstream response headers before streaming the body.
func proxyCurl(w http.ResponseWriter, r *http.Request, targetURL, fallbackType string) {
	args := buildCurlArgs(r, targetURL)
	// -i: include response headers in stdout so we can forward them.
	args = append([]string{"-i"}, args...)

	cmd := exec.Command("curl", args...)
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		slog.Info("proxyCurl: start failed", "url", targetURL, "err", err)
		http.Error(w, "proxy error", http.StatusBadGateway)
		return
	}

	// Close the write end of the pipe when curl exits.
	curlDone := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		pw.Close()
		curlDone <- err
	}()

	// Parse headers from curl's output (-i prepends them), then stream the body.
	reader := bufio.NewReaderSize(pr, 64*1024)
	statusCode, err := parseCurlHeaders(w, reader, fallbackType)
	if err != nil {
		slog.Info("proxyCurl: parsing headers failed", "url", targetURL, "err", err)
		statusCode = http.StatusOK
	}
	w.WriteHeader(statusCode)
	_, _ = io.Copy(w, reader)

	<-curlDone
}

// parseCurlHeaders reads the HTTP response header block(s) from curl's -i output,
// skipping any intermediate redirect blocks, sets the final response headers on w,
// and returns the final status code. The reader is left positioned at the start of
// the response body.
func parseCurlHeaders(w http.ResponseWriter, r *bufio.Reader, fallbackType string) (int, error) {
	var finalStatus int
	var finalHeaders http.Header

	for {
		// Status line: "HTTP/1.1 200 OK" / "HTTP/2 206 Partial Content"
		statusLine, err := r.ReadString('\n')
		if err != nil {
			return http.StatusOK, fmt.Errorf("reading status line: %w", err)
		}
		statusLine = strings.TrimRight(statusLine, "\r\n")
		fields := strings.Fields(statusLine)
		if len(fields) < 2 {
			return http.StatusOK, fmt.Errorf("unexpected status line: %q", statusLine)
		}
		code, err := strconv.Atoi(fields[1])
		if err != nil {
			return http.StatusOK, fmt.Errorf("parsing status code from %q: %w", statusLine, err)
		}

		// Read all headers for this response block.
		headers := make(http.Header)
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				break
			}
			line = strings.TrimRight(line, "\r\n")
			if line == "" {
				break // blank line = end of headers
			}
			if idx := strings.Index(line, ": "); idx >= 0 {
				headers.Add(line[:idx], line[idx+2:])
			}
		}

		finalStatus = code
		finalHeaders = headers

		// 3xx: curl followed a redirect and will output another response block.
		if code >= 300 && code < 400 {
			continue
		}
		break
	}

	forwardResponseHeaders(w, finalHeaders, fallbackType)
	return finalStatus, nil
}

// forwardResponseHeaders copies all non-hop-by-hop headers from src to dst.
// Falls back to fallbackType for Content-Type if the upstream didn't set one.
func forwardResponseHeaders(w http.ResponseWriter, src http.Header, fallbackType string) {
	for name, values := range src {
		if hopByHop[name] {
			continue
		}
		for _, v := range values {
			w.Header().Add(name, v)
		}
	}
	if w.Header().Get("Content-Type") == "" && fallbackType != "" {
		w.Header().Set("Content-Type", fallbackType)
	}
}

// buildCurlArgs assembles the curl argument list, forwarding all browser headers.
func buildCurlArgs(r *http.Request, targetURL string) []string {
	args := []string{"-s", "-L"}
	for name, values := range r.Header {
		if hopByHop[name] {
			continue
		}
		if strings.EqualFold(name, "Referer") {
			continue // overridden below
		}
		for _, v := range values {
			args = append(args, "-H", name+": "+v)
		}
	}
	args = append(args,
		"-H", "Referer: "+proxyReferer,
		"-H", "User-Agent: "+proxyUserAgent,
		"-o", "-",
		targetURL,
	)
	return args
}

// copyRequestHeaders copies all non-hop-by-hop headers from the browser request
// to the upstream request, overriding Referer for CDN hotlink protection.
func copyRequestHeaders(dst, src http.Header) {
	for name, values := range src {
		if hopByHop[name] {
			continue
		}
		if strings.EqualFold(name, "Referer") {
			continue
		}
		for _, v := range values {
			dst.Add(name, v)
		}
	}
	dst.Set("User-Agent", proxyUserAgent)
	dst.Set("Referer", proxyReferer)
}

// fetchBytes retrieves raw bytes from targetURL (HTTP then curl fallback).
func fetchBytes(targetURL string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, targetURL, nil)
	if err == nil {
		req.Header.Set("User-Agent", proxyUserAgent)
		req.Header.Set("Referer", proxyReferer)
		resp, err := proxyHTTPClient.Do(req)
		if err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return io.ReadAll(resp.Body)
			}
		}
	}
	out, err := exec.Command("curl", "-s", "-L", "-f",
		"-H", "User-Agent: "+proxyUserAgent,
		"-H", "Referer: "+proxyReferer,
		targetURL).Output()
	if err != nil {
		slog.Info("fetchBytes: curl failed", "url", targetURL, "err", err)
	}
	return out, err
}

var extMapURIRe = regexp.MustCompile(`URI="([^"]+)"`)

// rewriteM3U8 rewrites all segment and variant playlist URLs to go through the proxy.
func rewriteM3U8(content string, base *url.URL) string {
	var sb strings.Builder
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "#EXT-X-MAP:"),
			strings.HasPrefix(line, "#EXT-X-KEY:"),
			strings.HasPrefix(line, "#EXT-X-MEDIA:"),
			strings.HasPrefix(line, "#EXT-X-I-FRAME-STREAM-INF:"):
			line = extMapURIRe.ReplaceAllStringFunc(line, func(m string) string {
				sub := extMapURIRe.FindStringSubmatch(m)
				return `URI="` + toProxyURL(base, sub[1]) + `"`
			})
		case !strings.HasPrefix(line, "#") && line != "":
			line = toProxyURL(base, line)
		}
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	return sb.String()
}

func toProxyURL(base *url.URL, ref string) string {
	r, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	resolved := base.ResolveReference(r).String()
	if strings.Contains(resolved, ".m3u8") {
		return "m3u8?url=" + url.QueryEscape(resolved)
	}
	return "ts?url=" + url.QueryEscape(resolved)
}

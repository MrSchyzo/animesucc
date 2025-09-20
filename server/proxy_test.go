package main

import (
	"bufio"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// --- rewriteM3U8 unit tests ---

func baseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u
}

func TestRewriteM3U8_CommentsPassthrough(t *testing.T) {
	input := "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:4\n"
	out := rewriteM3U8(input, baseURL(t, "https://cdn.example.com/path/playlist.m3u8"))
	if !strings.Contains(out, "#EXTM3U") || !strings.Contains(out, "#EXT-X-VERSION:3") {
		t.Errorf("comment lines should be preserved, got:\n%s", out)
	}
	if strings.Contains(out, "/proxy/") {
		t.Errorf("comment lines should not be rewritten, got:\n%s", out)
	}
}

func TestRewriteM3U8_MediaPlaylist(t *testing.T) {
	input := "#EXTM3U\n#EXTINF:4.0,\nhttps://cdn.example.com/seg-001.ts\n#EXTINF:4.0,\nhttps://cdn.example.com/seg-002.ts\n"
	out := rewriteM3U8(input, baseURL(t, "https://cdn.example.com/path/playlist.m3u8"))
	if strings.Contains(out, "cdn.example.com/seg-001.ts") {
		t.Error("segment URLs should be rewritten")
	}
	if !strings.Contains(out, "/proxy/ts?url=") {
		t.Errorf("expected /proxy/ts?url=... in output, got:\n%s", out)
	}
	if strings.Count(out, "/proxy/ts?url=") != 2 {
		t.Errorf("expected 2 rewritten segments, got:\n%s", out)
	}
}

func TestRewriteM3U8_MasterPlaylist(t *testing.T) {
	input := "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=800000\nhttps://cdn.example.com/360p/playlist.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=2000000\nhttps://cdn.example.com/720p/playlist.m3u8\n"
	out := rewriteM3U8(input, baseURL(t, "https://cdn.example.com/master.m3u8"))
	if !strings.Contains(out, "/proxy/m3u8?url=") {
		t.Errorf("expected /proxy/m3u8?url=... for variant playlists, got:\n%s", out)
	}
	if strings.Contains(out, "cdn.example.com/360p") {
		t.Error("variant playlist URLs should be rewritten")
	}
}

func TestRewriteM3U8_RelativeURLs(t *testing.T) {
	input := "#EXTM3U\n#EXTINF:4.0,\n480p-001.ts\n#EXTINF:4.0,\n480p-002.ts\n"
	out := rewriteM3U8(input, baseURL(t, "https://cdn.example.com/path/480p/playlist.m3u8"))
	// Relative URLs should be resolved against base before proxying
	if !strings.Contains(out, url.QueryEscape("https://cdn.example.com/path/480p/480p-001.ts")) {
		t.Errorf("relative segment URLs should be resolved against base URL, got:\n%s", out)
	}
}

func TestRewriteM3U8_ExtXKey(t *testing.T) {
	input := "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"https://cdn.example.com/key.bin\",IV=0x0\n#EXTINF:4.0,\nseg-001.ts\n"
	out := rewriteM3U8(input, baseURL(t, "https://cdn.example.com/path/playlist.m3u8"))
	if strings.Contains(out, "cdn.example.com/key.bin") {
		t.Error("key URI should be rewritten through proxy")
	}
	if !strings.Contains(out, `URI="/proxy/ts?url=`) {
		t.Errorf("key URI should be proxied via /proxy/ts, got:\n%s", out)
	}
	if !strings.Contains(out, "METHOD=AES-128") {
		t.Error("METHOD attribute must be preserved")
	}
}

func TestRewriteM3U8_ExtXMedia(t *testing.T) {
	input := "#EXTM3U\n#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"audio\",URI=\"audio/index.m3u8\"\n"
	out := rewriteM3U8(input, baseURL(t, "https://cdn.example.com/path/master.m3u8"))
	if strings.Contains(out, `URI="https://`) {
		t.Error("EXT-X-MEDIA URI should not be a direct CDN URL")
	}
	if !strings.Contains(out, `URI="/proxy/m3u8?url=`) {
		t.Errorf("EXT-X-MEDIA URI should be proxied via /proxy/m3u8, got:\n%s", out)
	}
}

func TestRewriteM3U8_ExtXMap(t *testing.T) {
	input := "#EXTM3U\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXTINF:4.0,\nseg-001.ts\n"
	out := rewriteM3U8(input, baseURL(t, "https://cdn.example.com/path/playlist.m3u8"))
	if !strings.Contains(out, `URI="/proxy/ts?url=`) {
		t.Errorf("EXT-X-MAP URI should be rewritten, got:\n%s", out)
	}
}

// --- proxy integration tests using a fake CDN ---

func fakeCDN(handler http.HandlerFunc) *httptest.Server {
	return httptest.NewServer(handler)
}

func proxyRequest(t *testing.T, s *server, handlerFn http.HandlerFunc, targetURL, rangeHeader string) *http.Response {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(handlerFn))
	t.Cleanup(srv.Close)

	reqURL := srv.URL + "/?url=" + url.QueryEscape(targetURL)
	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	if rangeHeader != "" {
		req.Header.Set("Range", rangeHeader)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	return resp
}

func TestProxyStream_OK(t *testing.T) {
	cdn := fakeCDN(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write([]byte("fake-mp4-bytes"))
	})
	t.Cleanup(cdn.Close)

	s := &server{}
	resp := proxyRequest(t, s, s.proxyStream, cdn.URL+"/video.mp4", "")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "fake-mp4-bytes" {
		t.Errorf("unexpected body: %q", body)
	}
	if resp.Header.Get("Content-Type") != "video/mp4" {
		t.Errorf("want Content-Type video/mp4, got %q", resp.Header.Get("Content-Type"))
	}
}

func TestProxyStream_AcceptRanges(t *testing.T) {
	cdn := fakeCDN(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("data"))
	})
	t.Cleanup(cdn.Close)

	s := &server{}
	resp := proxyRequest(t, s, s.proxyStream, cdn.URL+"/video.mp4", "")
	defer resp.Body.Close()

	if resp.Header.Get("Accept-Ranges") != "bytes" {
		t.Errorf("want Accept-Ranges: bytes, got %q", resp.Header.Get("Accept-Ranges"))
	}
}

func TestProxyStream_Range(t *testing.T) {
	cdn := fakeCDN(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") == "" {
			t.Error("expected Range header to be forwarded")
		}
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Content-Range", "bytes 0-9/100")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("0123456789"))
	})
	t.Cleanup(cdn.Close)

	s := &server{}
	resp := proxyRequest(t, s, s.proxyStream, cdn.URL+"/video.mp4", "bytes=0-9")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("want 206, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "0123456789" {
		t.Errorf("unexpected body: %q", body)
	}
}

func TestProxyTS_OK(t *testing.T) {
	cdn := fakeCDN(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp2t")
		_, _ = w.Write([]byte("ts-segment-bytes"))
	})
	t.Cleanup(cdn.Close)

	s := &server{}
	resp := proxyRequest(t, s, s.proxyTS, cdn.URL+"/seg-001.ts", "")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ts-segment-bytes" {
		t.Errorf("unexpected body: %q", body)
	}
}

func TestProxyM3U8_Rewrite(t *testing.T) {
	playlist := "#EXTM3U\n#EXTINF:4.0,\nseg-001.ts\n#EXTINF:4.0,\nseg-002.ts\n#EXT-X-ENDLIST\n"
	cdn := fakeCDN(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		_, _ = w.Write([]byte(playlist))
	})
	t.Cleanup(cdn.Close)

	s := &server{}
	resp := proxyRequest(t, s, s.proxyM3U8, cdn.URL+"/playlist.m3u8", "")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	out := string(body)
	if !strings.Contains(out, "/proxy/ts?url=") {
		t.Errorf("expected segment URLs to be rewritten, got:\n%s", out)
	}
	// Every non-comment, non-empty line must start with /proxy/
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if line != "" && !strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "/proxy/") {
			t.Errorf("unrewritten line found: %q", line)
		}
	}
	if strings.Count(out, "/proxy/ts?url=") != 2 {
		t.Errorf("expected 2 rewritten segments, got:\n%s", out)
	}
}

func TestProxyStream_MissingURL(t *testing.T) {
	s := &server{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/proxy/stream", nil)
	s.proxyStream(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestProxyTS_MissingURL(t *testing.T) {
	s := &server{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/proxy/ts", nil)
	s.proxyTS(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestProxyM3U8_MissingURL(t *testing.T) {
	s := &server{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/proxy/m3u8", nil)
	s.proxyM3U8(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

// --- parseCurlHeaders unit tests ---

func makeCurlReader(raw string) *bufio.Reader {
	return bufio.NewReader(strings.NewReader(raw))
}

func TestParseCurlHeaders_Simple200(t *testing.T) {
	input := "HTTP/1.1 200 OK\r\nContent-Type: video/mp4\r\nContent-Length: 1234\r\n\r\n"
	rec := httptest.NewRecorder()
	code, err := parseCurlHeaders(rec, makeCurlReader(input), "application/octet-stream")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 200 {
		t.Errorf("want 200, got %d", code)
	}
	if rec.Header().Get("Content-Type") != "video/mp4" {
		t.Errorf("want Content-Type video/mp4, got %q", rec.Header().Get("Content-Type"))
	}
	if rec.Header().Get("Content-Length") != "1234" {
		t.Errorf("want Content-Length 1234, got %q", rec.Header().Get("Content-Length"))
	}
}

func TestParseCurlHeaders_206WithContentRange(t *testing.T) {
	input := "HTTP/1.1 206 Partial Content\r\nContent-Range: bytes 0-999/5000000\r\nContent-Type: video/mp4\r\n\r\n"
	rec := httptest.NewRecorder()
	code, err := parseCurlHeaders(rec, makeCurlReader(input), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 206 {
		t.Errorf("want 206, got %d", code)
	}
	if rec.Header().Get("Content-Range") != "bytes 0-999/5000000" {
		t.Errorf("want Content-Range bytes 0-999/5000000, got %q", rec.Header().Get("Content-Range"))
	}
}

func TestParseCurlHeaders_SkipsRedirect(t *testing.T) {
	// curl -L output: first a 301, then the final 200.
	input := "HTTP/1.1 301 Moved Permanently\r\nLocation: https://cdn2.example.com/v.mp4\r\n\r\n" +
		"HTTP/2 200\r\nContent-Type: video/mp4\r\nX-Custom: abc\r\n\r\n"
	rec := httptest.NewRecorder()
	code, err := parseCurlHeaders(rec, makeCurlReader(input), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 200 {
		t.Errorf("want 200 after redirect, got %d", code)
	}
	if rec.Header().Get("X-Custom") != "abc" {
		t.Errorf("want X-Custom abc from final response, got %q", rec.Header().Get("X-Custom"))
	}
	if rec.Header().Get("Location") != "" {
		t.Errorf("Location from redirect should not appear in final headers")
	}
}

func TestParseCurlHeaders_FallbackContentType(t *testing.T) {
	input := "HTTP/1.1 200 OK\r\n\r\n"
	rec := httptest.NewRecorder()
	_, err := parseCurlHeaders(rec, makeCurlReader(input), "video/mp4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Header().Get("Content-Type") != "video/mp4" {
		t.Errorf("want fallback Content-Type video/mp4, got %q", rec.Header().Get("Content-Type"))
	}
}

func TestProxyStream_Upstream404(t *testing.T) {
	cdn := fakeCDN(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})
	t.Cleanup(cdn.Close)

	s := &server{}
	resp := proxyRequest(t, s, s.proxyStream, cdn.URL+"/missing.mp4", "")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404 forwarded, got %d", resp.StatusCode)
	}
}

func TestProxyTS_Upstream403(t *testing.T) {
	cdn := fakeCDN(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	})
	t.Cleanup(cdn.Close)

	s := &server{}
	resp := proxyRequest(t, s, s.proxyTS, cdn.URL+"/seg-001.ts", "")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("want 403 forwarded, got %d", resp.StatusCode)
	}
}

// TestProxyStream_ForwardsAllResponseHeaders verifies that all upstream response
// headers reach the client, not just a hardcoded whitelist.
func TestProxyStream_ForwardsAllResponseHeaders(t *testing.T) {
	cdn := fakeCDN(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("X-Upstream-Custom", "hello")
		w.Header().Set("ETag", `"abc123"`)
		w.Write([]byte("data"))
	})
	t.Cleanup(cdn.Close)

	s := &server{}
	resp := proxyRequest(t, s, s.proxyStream, cdn.URL+"/video.mp4", "")
	defer resp.Body.Close()

	if resp.Header.Get("X-Upstream-Custom") != "hello" {
		t.Errorf("want X-Upstream-Custom: hello, got %q", resp.Header.Get("X-Upstream-Custom"))
	}
	if resp.Header.Get("ETag") != `"abc123"` {
		t.Errorf("want ETag, got %q", resp.Header.Get("ETag"))
	}
}

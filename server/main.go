package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	animesucc "github.com/mrschyzo/animesucc/core"
)

type animeClient interface {
	Search(ctx context.Context, q string) ([]animesucc.SearchResult, error)
	GetEpisodes(ctx context.Context, link string) ([]animesucc.Episode, error)
	GetVideoURL(ctx context.Context, episodeURL string) (*animesucc.VideoSource, error)
}

type server struct {
	client animeClient
}

// responseWriter wraps http.ResponseWriter to capture the written status code.
type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

func noCache(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		h.ServeHTTP(w, r)
	})
}

func logging(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		h.ServeHTTP(rw, r)
		elapsed := time.Since(start).Round(time.Millisecond)
		attrs := []any{
			slog.String("method", r.Method),
			slog.String("url", r.URL.String()),
			slog.Int("status", rw.status),
			slog.Duration("elapsed", elapsed),
		}
		if rw.status >= 400 {
			slog.Info("request", attrs...)
		} else if r.URL.Path != "/proxy/ts" {
			slog.Debug("request", attrs...)
		}
	})
}

func parseLogLevel(s string) (slog.Level, error) {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	}
	var lvl slog.Level
	return lvl, lvl.UnmarshalText([]byte(s))
}

func main() {
	addr := flag.String("addr", defaultAddr(), "Listen address (host:port)")
	static := flag.String("static", "../site", "Path to static site directory")
	logLevel := flag.String("log-level", "info", "Log level: debug|info|warn|error")
	logFormat := flag.String("log-format", "json", "Log format: json|text")
	flag.Parse()

	level, err := parseLogLevel(*logLevel)
	if err != nil {
		slog.Error("invalid log level", "value", *logLevel, "err", err)
		os.Exit(2)
	}
	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if strings.ToLower(*logFormat) == "text" {
		handler = slog.NewTextHandler(os.Stderr, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	}
	slog.SetDefault(slog.New(handler))

	s := &server{client: animesucc.NewClient()}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("GET /api/search", s.apiSearch)
	mux.HandleFunc("GET /api/episodes", s.apiEpisodes)
	mux.HandleFunc("GET /api/video", s.apiVideo)
	mux.HandleFunc("GET /proxy/stream", s.proxyStream)
	mux.HandleFunc("GET /proxy/m3u8", s.proxyM3U8)
	mux.HandleFunc("GET /proxy/ts", s.proxyTS)
	mux.Handle("/", noCache(http.FileServer(http.Dir(*static))))

	slog.Info("listening", "addr", *addr, "static", *static)
	if err := http.ListenAndServe(*addr, logging(mux)); err != nil {
		slog.Error("server exited", "err", err)
		os.Exit(1)
	}
}

func defaultAddr() string {
	if p := os.Getenv("PORT"); p != "" {
		return ":" + p
	}
	return ":8080"
}

func (s *server) health(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

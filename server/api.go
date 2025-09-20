package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"

	animesucc "github.com/mrschyzo/animesucc/core"
)

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (s *server) apiSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		slog.Info("api/search: missing q parameter")
		http.Error(w, "missing q", http.StatusBadRequest)
		return
	}
	results, err := s.client.Search(r.Context(), q)
	if err != nil {
		slog.Info("api/search failed", "q", q, "err", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, results)
}

func (s *server) apiEpisodes(w http.ResponseWriter, r *http.Request) {
	u := r.URL.Query().Get("url")
	if u == "" {
		slog.Info("api/episodes: missing url parameter")
		http.Error(w, "missing url", http.StatusBadRequest)
		return
	}
	episodes, err := s.client.GetEpisodes(r.Context(), u)
	if err != nil {
		slog.Info("api/episodes failed", "url", u, "err", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, episodes)
}

type videoResponse struct {
	URL         string `json:"url"`
	OriginalURL string `json:"original_url"`
	Type        string `json:"type"`
}

func (s *server) apiVideo(w http.ResponseWriter, r *http.Request) {
	u := r.URL.Query().Get("url")
	if u == "" {
		slog.Info("api/video: missing url parameter")
		http.Error(w, "missing url", http.StatusBadRequest)
		return
	}
	src, err := s.client.GetVideoURL(r.Context(), u)
	if err != nil {
		slog.Info("api/video failed", "url", u, "err", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	proxyPath := "proxy/stream"
	if src.Type == animesucc.VideoSourceM3U8 {
		proxyPath = "proxy/m3u8"
	}

	writeJSON(w, videoResponse{
		URL:         proxyPath + "?url=" + url.QueryEscape(src.URL),
		OriginalURL: src.URL,
		Type:        string(src.Type),
	})
}

package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	animesucc "github.com/mrschyzo/animesucc/core"
)

// mockClient implements animeClient for testing.
type mockClient struct {
	searchFn   func(ctx context.Context, q string) ([]animesucc.SearchResult, error)
	episodesFn func(ctx context.Context, link string) ([]animesucc.Episode, error)
	videoFn    func(ctx context.Context, episodeURL string) (*animesucc.VideoSource, error)
}

func (m *mockClient) Search(ctx context.Context, q string) ([]animesucc.SearchResult, error) {
	return m.searchFn(ctx, q)
}
func (m *mockClient) GetEpisodes(ctx context.Context, link string) ([]animesucc.Episode, error) {
	return m.episodesFn(ctx, link)
}
func (m *mockClient) GetVideoURL(ctx context.Context, episodeURL string) (*animesucc.VideoSource, error) {
	return m.videoFn(ctx, episodeURL)
}

func newTestServer(client animeClient) *server {
	return &server{client: client}
}

// --- apiSearch ---

func TestApiSearch_OK(t *testing.T) {
	s := newTestServer(&mockClient{
		searchFn: func(_ context.Context, q string) ([]animesucc.SearchResult, error) {
			return []animesucc.SearchResult{{Name: "One Piece", Link: "one-piece"}}, nil
		},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/search?q=one+piece", nil)
	s.apiSearch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var results []animesucc.SearchResult
	if err := json.NewDecoder(rec.Body).Decode(&results); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(results) != 1 || results[0].Name != "One Piece" {
		t.Errorf("unexpected results: %+v", results)
	}
}

func TestApiSearch_MissingQ(t *testing.T) {
	s := newTestServer(&mockClient{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/search", nil)
	s.apiSearch(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestApiSearch_UpstreamError(t *testing.T) {
	s := newTestServer(&mockClient{
		searchFn: func(_ context.Context, _ string) ([]animesucc.SearchResult, error) {
			return nil, errors.New("upstream timeout")
		},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/search?q=foo", nil)
	s.apiSearch(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("want 502, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "upstream timeout") {
		t.Errorf("expected error message in body")
	}
}

// --- apiEpisodes ---

func TestApiEpisodes_OK(t *testing.T) {
	s := newTestServer(&mockClient{
		episodesFn: func(_ context.Context, link string) ([]animesucc.Episode, error) {
			return []animesucc.Episode{{Number: 1, URL: "https://animesaturn.cx/ep/op-ep-1"}}, nil
		},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/episodes?url=one-piece", nil)
	s.apiEpisodes(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var episodes []animesucc.Episode
	if err := json.NewDecoder(rec.Body).Decode(&episodes); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(episodes) != 1 || episodes[0].Number != 1 {
		t.Errorf("unexpected episodes: %+v", episodes)
	}
}

func TestApiEpisodes_MissingURL(t *testing.T) {
	s := newTestServer(&mockClient{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/episodes", nil)
	s.apiEpisodes(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

// --- apiVideo ---

func TestApiVideo_MP4(t *testing.T) {
	const cdnURL = "https://cdn.example.com/video.mp4"
	s := newTestServer(&mockClient{
		videoFn: func(_ context.Context, _ string) (*animesucc.VideoSource, error) {
			return &animesucc.VideoSource{URL: cdnURL, Type: animesucc.VideoSourceMP4}, nil
		},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/video?url=https://animesaturn.cx/ep/op-ep-1", nil)
	s.apiVideo(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var vr videoResponse
	if err := json.NewDecoder(rec.Body).Decode(&vr); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if vr.Type != "mp4" {
		t.Errorf("want type=mp4, got %q", vr.Type)
	}
	if !strings.HasPrefix(vr.URL, "/proxy/stream?url=") {
		t.Errorf("expected /proxy/stream?url=..., got %q", vr.URL)
	}
	if vr.OriginalURL != cdnURL {
		t.Errorf("want original_url=%q, got %q", cdnURL, vr.OriginalURL)
	}
}

func TestApiVideo_M3U8(t *testing.T) {
	const cdnURL = "https://cdn.example.com/playlist.m3u8"
	s := newTestServer(&mockClient{
		videoFn: func(_ context.Context, _ string) (*animesucc.VideoSource, error) {
			return &animesucc.VideoSource{URL: cdnURL, Type: animesucc.VideoSourceM3U8}, nil
		},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/video?url=https://animesaturn.cx/ep/op-ep-1", nil)
	s.apiVideo(rec, req)

	var vr videoResponse
	if err := json.NewDecoder(rec.Body).Decode(&vr); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if vr.Type != "m3u8" {
		t.Errorf("want type=m3u8, got %q", vr.Type)
	}
	if !strings.HasPrefix(vr.URL, "/proxy/m3u8?url=") {
		t.Errorf("expected /proxy/m3u8?url=..., got %q", vr.URL)
	}
	if vr.OriginalURL != cdnURL {
		t.Errorf("want original_url=%q, got %q", cdnURL, vr.OriginalURL)
	}
}

func TestApiVideo_MissingURL(t *testing.T) {
	s := newTestServer(&mockClient{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/video", nil)
	s.apiVideo(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestApiVideo_UpstreamError(t *testing.T) {
	s := newTestServer(&mockClient{
		videoFn: func(_ context.Context, _ string) (*animesucc.VideoSource, error) {
			return nil, errors.New("no video found")
		},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/video?url=https://animesaturn.cx/ep/bad", nil)
	s.apiVideo(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("want 502, got %d", rec.Code)
	}
}

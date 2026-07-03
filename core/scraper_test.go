package animesucc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestGetEpisodes(t *testing.T) {
	t.Parallel()
	srv := newSaturnServer(t, "m3u8", nil)
	c := NewClient(WithBaseURL(srv.URL))

	eps, err := c.GetEpisodes(context.Background(), "naruto-iN621")
	if err != nil {
		t.Fatalf("GetEpisodes: %v", err)
	}
	// Fixture has 3 distinct episodes (with duplicate + out-of-order links, and a
	// non-episode /anime/ link that must be ignored).
	if len(eps) != 3 {
		t.Fatalf("expected 3 episodes, got %d: %+v", len(eps), eps)
	}
	for i, ep := range eps {
		wantNum := i + 1
		if ep.Number != wantNum {
			t.Errorf("eps[%d].Number = %d, want %d (sorted ascending)", i, ep.Number, wantNum)
		}
		wantURL := srv.URL + "/anime/naruto-iN621/ep-" + strconv.Itoa(wantNum)
		if ep.URL != wantURL {
			t.Errorf("eps[%d].URL = %q, want %q (player URL, /episode/ rewritten to /anime/)", i, ep.URL, wantURL)
		}
	}
}

func TestGetVideoURL(t *testing.T) {
	t.Parallel()
	cases := []struct {
		kind     string
		wantType VideoSourceType
		checkURL func(string) bool
	}{
		{"m3u8", VideoSourceM3U8, func(u string) bool { return strings.HasSuffix(u, "/playlist.m3u8") }},
		{"mp4", VideoSourceMP4, func(u string) bool { return strings.HasSuffix(u, "/video.mp4") }},
		{"youtube", VideoSourceYouTube, func(u string) bool { return u == "https://www.youtube.com/watch?v=dQw4w9WgXcQ" }},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			t.Parallel()
			srv := newSaturnServer(t, tc.kind, nil)
			c := NewClient(WithBaseURL(srv.URL))

			src, err := c.GetVideoURL(context.Background(), srv.URL+"/anime/naruto-iN621/ep-1")
			if err != nil {
				t.Fatalf("GetVideoURL: %v", err)
			}
			if src.Type != tc.wantType {
				t.Errorf("Type = %q, want %q", src.Type, tc.wantType)
			}
			if !tc.checkURL(src.URL) {
				t.Errorf("unexpected decoded URL: %q", src.URL)
			}
		})
	}
}

// GetVideoURL must accept the /episode/<slug>/ep-N interstitial form and normalize it to
// the /anime/<slug>/ep-N player page.
func TestGetVideoURL_NormalizesInterstitialURL(t *testing.T) {
	t.Parallel()
	srv := newSaturnServer(t, "m3u8", nil)
	c := NewClient(WithBaseURL(srv.URL))

	src, err := c.GetVideoURL(context.Background(), srv.URL+"/episode/naruto-iN621/ep-1")
	if err != nil {
		t.Fatalf("GetVideoURL(interstitial form): %v", err)
	}
	if src.Type != VideoSourceM3U8 {
		t.Errorf("Type = %q, want m3u8", src.Type)
	}
}

func TestGetVideoURL_NoIframe(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html><body>no player embedded here</body></html>"))
	}))
	defer srv.Close()

	c := NewClient(WithBaseURL(srv.URL))
	if _, err := c.GetVideoURL(context.Background(), srv.URL+"/anime/x/ep-1"); err == nil {
		t.Fatal("expected an error when the player page has no watch-iframe")
	}
}

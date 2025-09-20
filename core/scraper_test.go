package animesucc

import (
	"context"
	"strings"
	"testing"
)

func TestGetEpisodes_Naruto(t *testing.T) {
	t.Parallel()
	c := NewClient()
	episodes, err := c.GetEpisodes(context.Background(), "Naruto-aaaaaaa")
	if err != nil {
		t.Fatalf("GetEpisodes failed: %v", err)
	}

	if len(episodes) != 220 {
		t.Fatalf("expected 220 episodes, got %d", len(episodes))
	}

	for i, ep := range episodes {
		expected := i + 1
		if ep.Number != expected {
			t.Errorf("episode %d: expected number %d, got %d", i, expected, ep.Number)
		}
		if ep.URL == "" {
			t.Errorf("episode %d: empty URL", ep.Number)
		}
		if ep.Slug == "" {
			t.Errorf("episode %d: empty Slug", ep.Number)
		}
	}
}

func TestGetEpisodes_ShortAnime(t *testing.T) {
	t.Parallel()
	c := NewClient()
	episodes, err := c.GetEpisodes(context.Background(), "Road-of-Naruto-aaaaaaaaa")
	if err != nil {
		t.Fatalf("GetEpisodes failed: %v", err)
	}

	if len(episodes) != 1 {
		t.Fatalf("expected 1 episode, got %d", len(episodes))
	}
	if episodes[0].Number != 1 {
		t.Errorf("expected episode number 1, got %d", episodes[0].Number)
	}
}

func TestGetVideoURL_MP4(t *testing.T) {
	t.Parallel()
	c := NewClient()
	// Frieren ep 1 is known to serve mp4
	src, err := c.GetVideoURL(context.Background(), "https://www.animesaturn.cx/ep/Frieren-Beyond-Journeys-End-ep-1")
	if err != nil {
		t.Fatalf("GetVideoURL failed: %v", err)
	}
	if src.Type != VideoSourceMP4 {
		t.Errorf("expected mp4, got %s", src.Type)
	}
	if !strings.HasSuffix(src.URL, ".mp4") {
		t.Errorf("expected URL ending with .mp4, got %s", src.URL)
	}
	if !strings.HasPrefix(src.URL, "https://") {
		t.Errorf("expected https URL, got %s", src.URL)
	}
}

func TestGetVideoURL_M3U8(t *testing.T) {
	t.Parallel()
	c := NewClient()
	// Naruto ep 1 is known to serve m3u8
	src, err := c.GetVideoURL(context.Background(), "https://www.animesaturn.cx/ep/Naruto-ep-1")
	if err != nil {
		t.Fatalf("GetVideoURL failed: %v", err)
	}
	if src.Type != VideoSourceM3U8 {
		t.Errorf("expected m3u8, got %s", src.Type)
	}
	if !strings.Contains(src.URL, ".m3u8") {
		t.Errorf("expected URL containing .m3u8, got %s", src.URL)
	}
	if !strings.HasPrefix(src.URL, "https://") {
		t.Errorf("expected https URL, got %s", src.URL)
	}
}

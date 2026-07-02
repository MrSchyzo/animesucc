package animesucc

import (
	"context"
	"strings"
	"testing"
)

func TestSearch_ParsesResultCards(t *testing.T) {
	t.Parallel()
	srv := newSaturnServer(t, "m3u8", nil)
	c := NewClient(WithBaseURL(srv.URL))

	results, err := c.Search(context.Background(), "naruto")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	// The hero/"In evidenza" slider link must be excluded; only a.ac cards count.
	if len(results) != 2 {
		t.Fatalf("expected 2 result cards, got %d: %+v", len(results), results)
	}

	got := results[0]
	if got.Name != "Naruto" {
		t.Errorf("Name = %q, want Naruto", got.Name)
	}
	if got.Link != "naruto-iN621" {
		t.Errorf("Link = %q, want naruto-iN621 (slug, no /anime/ prefix)", got.Link)
	}
	if got.Type != "TV" {
		t.Errorf("Type = %q, want TV", got.Type)
	}
	if !strings.Contains(got.Image, "17405l.jpg") {
		t.Errorf("Image = %q, want to contain 17405l.jpg", got.Image)
	}
	if !strings.Contains(got.Release, "220 ep") {
		t.Errorf("Release = %q, want to contain '220 ep'", got.Release)
	}

	if results[1].Link != "one-piece-PmTvj" {
		t.Errorf("results[1].Link = %q, want one-piece-PmTvj", results[1].Link)
	}
}

func TestSearch_NoResults(t *testing.T) {
	t.Parallel()
	srv := newSaturnServer(t, "m3u8", nil)
	c := NewClient(WithBaseURL(srv.URL))

	results, err := c.Search(context.Background(), "none")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

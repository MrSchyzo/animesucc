//go:build integration

// Live smoke tests that hit the real AnimeSaturn site + CDN. Run with:
//
//	go test -tags=integration ./core
//
// They confirm the live HTML selectors and the embed/playlist flow still match; the
// default (untagged) test run stays fully offline via testdata fixtures.
package animesucc

import (
	"context"
	"strings"
	"testing"
)

func TestSearch_Live(t *testing.T) {
	t.Parallel()
	c := NewClient()
	results, err := c.Search(context.Background(), "naruto")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result for 'naruto'")
	}
	for _, r := range results {
		if r.Name == "" || r.Link == "" {
			t.Errorf("incomplete result: %+v", r)
		}
	}
}

func TestGetEpisodes_Live(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c := NewClient()
	results, err := c.Search(ctx, "naruto")
	if err != nil || len(results) == 0 {
		t.Fatalf("search prerequisite failed: %v", err)
	}
	eps, err := c.GetEpisodes(ctx, results[0].Link)
	if err != nil {
		t.Fatalf("GetEpisodes(%s): %v", results[0].Link, err)
	}
	if len(eps) == 0 {
		t.Fatalf("expected episodes for %s", results[0].Link)
	}
	prev := 0
	for _, ep := range eps {
		if ep.Number < prev {
			t.Errorf("episodes not sorted ascending: %d after %d", ep.Number, prev)
		}
		prev = ep.Number
		if !strings.Contains(ep.URL, "/anime/") {
			t.Errorf("episode URL is not a player URL: %s", ep.URL)
		}
	}
}

func TestGetVideoURL_Live(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c := NewClient()
	results, err := c.Search(ctx, "one piece")
	if err != nil || len(results) == 0 {
		t.Fatalf("search prerequisite failed: %v", err)
	}
	eps, err := c.GetEpisodes(ctx, results[0].Link)
	if err != nil || len(eps) == 0 {
		t.Fatalf("episodes prerequisite failed: %v", err)
	}
	src, err := c.GetVideoURL(ctx, eps[0].URL)
	if err != nil {
		t.Fatalf("GetVideoURL(%s): %v", eps[0].URL, err)
	}
	if !strings.HasPrefix(src.URL, "https://") {
		t.Errorf("expected https source URL, got %q", src.URL)
	}
	switch src.Type {
	case VideoSourceM3U8, VideoSourceMP4, VideoSourceYouTube:
	default:
		t.Errorf("unexpected source type %q", src.Type)
	}
}

// liveM3U8Source resolves a real HLS source from the live site, trying the results of a
// "naruto" search until one yields an m3u8. Skips the test if none is found.
func liveM3U8Source(ctx context.Context, t *testing.T) *VideoSource {
	t.Helper()
	c := NewClient()
	results, err := c.Search(ctx, "naruto")
	if err != nil || len(results) == 0 {
		t.Fatalf("live search failed: %v", err)
	}
	for _, r := range results {
		eps, err := c.GetEpisodes(ctx, r.Link)
		if err != nil || len(eps) == 0 {
			continue
		}
		src, err := c.GetVideoURL(ctx, eps[0].URL)
		if err != nil {
			continue
		}
		if src.Type == VideoSourceM3U8 {
			return src
		}
	}
	t.Skip("no live m3u8 source found; skipping HLS test")
	return nil
}

//go:build integration

package animesucc

import (
	"context"
	"os/exec"
	"testing"
)

func TestClient_AnimeSaturnReachable(t *testing.T) {
	t.Parallel()
	c := NewClient()
	body, err := c.fetchURL(context.Background(), "https://www.animesaturn.net/")
	if err != nil {
		t.Fatalf("fetchURL(animesaturn): %v", err)
	}
	if len(body) == 0 {
		t.Fatal("empty response from animesaturn")
	}
}

func TestCDNReachableViaCurl(t *testing.T) {
	t.Parallel()
	// CDN servers reject Go's TLS but work with curl. Resolve a fresh signed URL via the
	// full scraping flow (the old hardcoded CDN URL was ephemeral) and range-fetch it.
	src := liveM3U8Source(context.Background(), t)
	cmd := exec.CommandContext(context.Background(), "curl", "-sL", "-r", "0-1023",
		"-H", "Referer: https://play.saturncdn.net/", src.URL)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("curl CDN reachability check: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("empty response from CDN")
	}
	t.Logf("CDN reachable, got %d bytes", len(out))
}

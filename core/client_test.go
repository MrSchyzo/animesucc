package animesucc

import (
	"context"
	"os/exec"
	"testing"
)

func TestClient_AnimeSaturnReachable(t *testing.T) {
	t.Parallel()
	c := NewClient()
	// animesaturn.cx works fine with Go's HTTP client
	body, err := c.fetchURL(context.Background(), "https://www.animesaturn.cx/")
	if err != nil {
		t.Fatalf("fetchURL(animesaturn): %v", err)
	}
	if len(body) == 0 {
		t.Fatal("empty response from animesaturn")
	}
}

func TestCDNReachableViaCurl(t *testing.T) {
	t.Parallel()
	// CDN servers reject Go's TLS but work with curl
	// Use -r 0-1023 to only fetch the first 1KB as a reachability check
	u := "https://srv16.kuku.streampeaker.org/DDL/ANIME/AniNiTsukeruKusuriWaNai/AniNiTsukeruKusuriWaNai_Ep_01_SUB_ITA.mp4"
	cmd := exec.CommandContext(context.Background(), "curl", "-sL", "-r", "0-1023", u)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("curl CDN reachability check: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("empty response from CDN")
	}
	t.Logf("CDN reachable, got %d bytes", len(out))
}

package animesucc

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// epNumberRe extracts the episode number from the last path segment of an episode
// link, e.g. "/episode/one-piece-PmTvj/ep-1168" -> "ep-1168" -> 1168.
var epNumberRe = regexp.MustCompile(`ep-(\d+)$`)

func (c *Client) GetEpisodes(ctx context.Context, animeLink string) ([]Episode, error) {
	u := fmt.Sprintf("%s/anime/%s", c.baseURL, animeLink)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("creating anime page request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching anime page: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anime page returned status %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("parsing anime page HTML: %w", err)
	}

	seen := make(map[string]bool)
	var episodes []Episode

	// Episodes are listed as /episode/<slug>/ep-<n> links (an interstitial page). The
	// actual player page is /anime/<slug>/ep-<n>; we store that directly so GetVideoURL
	// can skip the interstitial.
	doc.Find(`a[href*="/episode/"]`).Each(func(_ int, s *goquery.Selection) {
		href, exists := s.Attr("href")
		if !exists {
			return
		}

		parts := strings.Split(strings.Trim(href, "/"), "/")
		slug := parts[len(parts)-1] // e.g. "ep-1"

		matches := epNumberRe.FindStringSubmatch(slug)
		if matches == nil {
			return
		}
		num, err := strconv.Atoi(matches[1])
		if err != nil {
			return
		}

		playerURL := c.absURL(strings.Replace(href, "/episode/", "/anime/", 1))
		if seen[playerURL] {
			return
		}
		seen[playerURL] = true

		episodes = append(episodes, Episode{
			Number: num,
			Slug:   slug,
			URL:    playerURL,
		})
	})

	sort.Slice(episodes, func(i, j int) bool {
		return episodes[i].Number < episodes[j].Number
	})

	return episodes, nil
}

// GetVideoURL resolves an episode's playable source URL.
//
// Flow (post-2026 revamp): player page /anime/<slug>/ep-N carries an iframe pointing at
// play.saturncdn.net/embed/<id>?token=..&expires=..; that embed exposes a JSON endpoint
// /embed/<id>/playlist whose fields are base64+XOR obfuscated with the token. The decoded
// "d" field is the real .m3u8 / .mp4 (or "youtube/<id>").
func (c *Client) GetVideoURL(ctx context.Context, episodeURL string) (*VideoSource, error) {
	playerURL := c.absURL(strings.Replace(episodeURL, "/episode/", "/anime/", 1))

	embedURL, err := c.scrapeEmbedURL(ctx, playerURL)
	if err != nil {
		return nil, fmt.Errorf("finding player embed: %w", err)
	}
	return c.resolveEmbedSource(ctx, embedURL)
}

// scrapeEmbedURL fetches the player page and returns the (absolute) play.saturncdn.net
// embed iframe URL.
func (c *Client) scrapeEmbedURL(ctx context.Context, playerURL string) (string, error) {
	body, err := c.fetchURL(ctx, playerURL)
	if err != nil {
		return "", err
	}

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return "", err
	}

	src, ok := doc.Find("iframe#watch-iframe").First().Attr("src")
	if !ok || src == "" {
		return "", fmt.Errorf("no watch-iframe on player page %s", playerURL)
	}
	return src, nil
}

// embedPlaylist is the JSON returned by /embed/<id>/playlist. Every value is obfuscated
// (base64 then XOR with the embed token). Only "d" (source) is needed for playback.
type embedPlaylist struct {
	D string `json:"d"` // source URL (.m3u8 / .mp4 / "youtube/<id>")
	P string `json:"p"` // poster (unused)
	T string `json:"t"` // thumbnails .vtt (unused)
}

// resolveEmbedSource calls the embed's /playlist endpoint and decodes the source URL.
func (c *Client) resolveEmbedSource(ctx context.Context, embedURL string) (*VideoSource, error) {
	u, err := url.Parse(embedURL)
	if err != nil {
		return nil, fmt.Errorf("parsing embed url: %w", err)
	}
	token := u.Query().Get("token")
	if token == "" {
		return nil, fmt.Errorf("embed url missing token: %s", embedURL)
	}

	// /embed/<id> -> /embed/<id>/playlist, preserving the token+expires query.
	playlist := *u
	playlist.Path = strings.TrimRight(u.Path, "/") + "/playlist"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, playlist.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", embedURL)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("playlist endpoint returned status %d", resp.StatusCode)
	}

	var payload embedPlaylist
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decoding playlist json: %w", err)
	}

	src, err := deobfuscate(payload.D, token)
	if err != nil {
		return nil, fmt.Errorf("decoding source url: %w", err)
	}
	if src == "" {
		return nil, fmt.Errorf("empty source url from playlist endpoint")
	}

	if strings.HasPrefix(src, "youtube/") {
		return &VideoSource{
			URL:  "https://www.youtube.com/watch?v=" + strings.TrimPrefix(src, "youtube/"),
			Type: VideoSourceYouTube,
		}, nil
	}

	return &VideoSource{URL: src, Type: classifyURL(src)}, nil
}

// deobfuscate reverses the embed obfuscation: base64-decode, then XOR each byte with the
// token string (cycling). Mirrors the site's inline dec(b, k).
func deobfuscate(b64, key string) (string, error) {
	if b64 == "" {
		return "", nil
	}
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "", err
	}
	if key == "" {
		key = "as"
	}
	out := make([]byte, len(data))
	for i, b := range data {
		out[i] = b ^ key[i%len(key)]
	}
	return string(out), nil
}

// absURL resolves ref against the client base URL. Absolute refs are returned as-is.
func (c *Client) absURL(ref string) string {
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return ref
	}
	if !strings.HasPrefix(ref, "/") {
		ref = "/" + ref
	}
	return c.baseURL + ref
}

func classifyURL(u string) VideoSourceType {
	if strings.Contains(u, ".m3u8") {
		return VideoSourceM3U8
	}
	return VideoSourceMP4
}

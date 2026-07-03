package animesucc

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// obfuscate is the inverse of deobfuscate (XOR with key, then base64-encode). Used to
// build fixture playlist payloads that GetVideoURL must be able to decode.
func obfuscate(s, key string) string {
	b := []byte(s)
	for i := range b {
		b[i] ^= key[i%len(key)]
	}
	return base64.StdEncoding.EncodeToString(b)
}

func mustFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("reading fixture %s: %v", name, err)
	}
	return string(data)
}

// newSaturnServer emulates the post-2026 AnimeSaturn scraping surface for offline tests.
//
// The /embed/{id}/playlist endpoint resolves its (obfuscated) "d" source from sourceKind:
//   - "mp4":     an mp4 URL served by this same server at /video.mp4 (mp4Body)
//   - "youtube": "youtube/<id>"
//   - anything else: a (fake, unfetched) CDN .m3u8 URL
//
// Fixtures are read up-front on the test goroutine so handlers never touch *testing.T.
func newSaturnServer(t *testing.T, sourceKind string, mp4Body []byte) *httptest.Server {
	t.Helper()
	filterResults := mustFixture(t, "filter_results.html")
	filterEmpty := mustFixture(t, "filter_empty.html")
	animePage := mustFixture(t, "anime_page.html")
	playerPage := mustFixture(t, "player_page.html")

	mux := http.NewServeMux()

	mux.HandleFunc("/filter", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") == "none" {
			_, _ = w.Write([]byte(filterEmpty))
			return
		}
		_, _ = w.Write([]byte(filterResults))
	})

	mux.HandleFunc("/anime/{slug}", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(animePage))
	})

	mux.HandleFunc("/anime/{slug}/{ep}", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.ReplaceAll(playerPage, "__BASE__", "http://"+r.Host)))
	})

	mux.HandleFunc("/embed/{id}/playlist", func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		var src string
		switch sourceKind {
		case "mp4":
			src = "http://" + r.Host + "/video.mp4"
		case "youtube":
			src = "youtube/dQw4w9WgXcQ"
		default:
			src = "https://srv1.test.streampeaker.org/DDL/ANIME/Naruto/0001/playlist.m3u8"
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"d":"` + obfuscate(src, token) + `","p":"","t":""}`))
	})

	mux.HandleFunc("/video.mp4", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write(mp4Body)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

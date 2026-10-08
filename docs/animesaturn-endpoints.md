# AnimeSaturn scraping reference

How AnimeSucc talks to AnimeSaturn: the endpoints, the request/response shapes, and
the video-source obfuscation. This is the source of truth for the scraping layer in
`core/` — keep it updated when the site changes.

> **Last verified: 2026-07-02** against `https://www.animesaturn.ro`.
> AnimeSaturn rotates domains and periodically revamps its HTML; if scraping breaks,
> re-run the recon in [§4](#4-how-to-re-verify-replicate) and update this file.

## 1. Hosts

| Host | Role |
|------|------|
| `https://www.animesaturn.ro` | Main site (search, anime pages, player pages). `animesaturn.ro` 301-redirects here. Overridable via `ANIMESUCC_BASE_URL`. |
| `https://img.saturncdn.net` | Cover/poster/slider images. |
| `https://play.saturncdn.net` | Video player embed + the source-resolving JSON endpoint. |
| `https://*.streampeaker.org` | Actual video CDN (HLS `.m3u8` / `.mp4`). Host varies per episode (e.g. `srv37.nezumi.streampeaker.org`) and URLs are signed + short-lived. |

The homepage sets an `as_session` cookie (HttpOnly, scoped to `animesaturn.ro`). It is
not required for the `play.saturncdn.net` endpoints (different host), but the client
keeps a cookie jar so any future gating is handled transparently.

## 2. Request flow (user journey)

### 2.1 Search — `GET /filter?key=<query>`

Returns **HTML** (this replaced the old `GET /index.php?search=1&key=` JSON endpoint,
which now returns a 404 page). Result cards:

```html
<a href="/anime/naruto-iN621" class="ac group">
  <div class="ac__poster"><img src="https://img.saturncdn.net/.../17405l.jpg" alt="Naruto"></div>
  <div class="ac__topbar"><span class="ac__type-badge">TV</span></div>
  <div class="ac__caption">
    <h3 class="ac__title">Naruto Shippuden (ITA)</h3>
    <p class="ac__sub">15 Febbraio 2007 · 500 ep</p>
  </div>
</a>
```

| Field | Selector (relative to `a.ac`) |
|-------|-------------------------------|
| link (anime slug) | the `a.ac` `href` → strip leading `/anime/` |
| name | `.ac__title` text |
| image | `.ac__poster img[src]` |
| release (date · ep count) | `.ac__sub` text |
| type (TV/Movie/OVA…) | `.ac__type-badge` text |

Select **`a.ac`** specifically — the page also contains a hero/"In evidenza" slider of
`/anime/...` links (`.hero-btn`) that are not search results. No matches → HTTP 200 with
zero `a.ac` cards and the text "Nessun risultato" (parse to an empty list, not an error).

Anime slugs now carry a case-sensitive 5-char suffix, e.g. `one-piece-PmTvj`.

### 2.2 Anime page / episode list — `GET /anime/<slug>`

Returns HTML. Every episode is a link of the form:

```
/episode/<anime-slug>/ep-<n>       e.g. /episode/one-piece-PmTvj/ep-1168
```

All episodes are server-rendered inline (verified up to One Piece's 1168 episodes —
**no pagination / lazy-load**). Parse: match anchors whose href contains `/episode/`,
take the last path segment, extract the number with `ep-(\d+)$`.

The anime title shown by the UI comes from the search result's name (`.ac__title`),
so scraping the anime page's `<h1>` is not required. (If ever needed, the title is in
the big `<h1 class="… font-display">` on this page.)

### 2.3 Interstitial → player page

`/episode/<slug>/ep-N` is an **interstitial** ("Stai per guardare") whose play button
links to the real **player page `/anime/<slug>/ep-N`**:

```html
<a href="/anime/one-piece-PmTvj/ep-1" class="ept-btn ept-btn--play">Guarda lo streaming</a>
```

The interstitial can be skipped entirely by transforming the path:
`/episode/<slug>/ep-N` → `/anime/<slug>/ep-N`.

### 2.4 Player page — `GET /anime/<slug>/ep-N`

Contains a single embed iframe (the old inline JWPlayer `file:"…"` is gone):

```html
<iframe id="watch-iframe"
        src="https://play.saturncdn.net/embed/13230?token=<TOKEN>&expires=<EXPIRES>">
```

- `13230` — numeric embed id
- `TOKEN` — 32-hex token; **also the XOR key** used to decode the source (§3)
- `EXPIRES` — unix timestamp; the token/embed is valid until then (~1h)

### 2.5 Source resolution — `GET https://play.saturncdn.net/embed/<id>/playlist?token=<TOKEN>&expires=<EXPIRES>`

The embed page does this XHR (add `X-Requested-With: XMLHttpRequest`; `Referer` = the
embed URL). It returns JSON with **obfuscated** values:

```json
{ "d": "<base64…>", "p": "<base64…>", "t": "<base64…>" }
```

| Field | Decoded value |
|-------|---------------|
| `d` | the video source URL — a `streampeaker.org` `.m3u8` (HLS) or `.mp4`, **or** `youtube/<videoId>` |
| `p` | poster `.jpg` |
| `t` | thumbnails `.vtt` (seek previews) |

Build the playlist URL from the iframe `src` by inserting `/playlist` before the query
(the `token` + `expires` query is kept unchanged).

### 2.6 Delivery (unchanged)

The decoded `d` is a normal HLS **master** playlist (e.g. 480p/360p/240p/144p variants
as relative URIs) or a direct `.mp4`. This is the same shape as the pre-revamp CDN, so
the existing HLS resolve/download/remux (`core/hls.go`) and the server's playlist/segment
proxy + rewrite (`server/proxy.go`) work without changes. The CDN gates on the signed
`_t/<expires>/<hmac>/…` path, not on cookies or Referer, and a no-Referer `curl` fallback
is used for hosts that reject Go's TLS.

## 3. Source obfuscation (`d`/`p`/`t` decode)

Mirrors the site's inline `dec(b, k)`: **base64-decode, then XOR each byte with the token
string (cycling)**. The key is the `token` from the embed iframe / playlist URL.

Go (`core/scraper.go` → `deobfuscate`):

```go
func deobfuscate(b64, key string) (string, error) {
	if b64 == "" { return "", nil }
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil { return "", err }
	if key == "" { key = "as" } // site's fallback
	out := make([]byte, len(data))
	for i, b := range data {
		out[i] = b ^ key[i%len(key)]
	}
	return string(out), nil
}
```

## 4. How to re-verify (replicate)

```sh
UA="Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"

# search
curl -sL -A "$UA" "https://www.animesaturn.ro/filter?key=one%20piece" | grep -oE '<a href="/anime/[^"]+" class="ac'

# episodes
curl -sL -A "$UA" "https://www.animesaturn.ro/anime/one-piece-PmTvj" | grep -oE '/episode/[A-Za-z0-9-]+/ep-[0-9]+' | sort -u

# player page -> embed iframe (note: /episode/ -> /anime/)
curl -sL -A "$UA" "https://www.animesaturn.ro/anime/one-piece-PmTvj/ep-1" | grep -oE '<iframe id="watch-iframe" src="[^"]+"'

# source JSON (insert /playlist before the query)
curl -sL -A "$UA" -H "X-Requested-With: XMLHttpRequest" \
  -e "https://play.saturncdn.net/embed/13230?token=<TOK>&expires=<EXP>" \
  "https://play.saturncdn.net/embed/13230/playlist?token=<TOK>&expires=<EXP>"

# decode d (base64 -> XOR with token)
python3 - <<'PY'
import base64
tok, d = "<TOK>", "<d>"
s = base64.b64decode(d)
print("".join(chr(c ^ ord(tok[i % len(tok)])) for i, c in enumerate(s)))
PY
```

# AnimeSucc — it sucks a lot

![AnimeSucc](./site/PlanetASucc.png)

**AnimeSucc** is a crawler for [AnimeSaturn](https://www.animesaturn.cx) giving you ad-free search and playback. It ships in four pieces:

| Piece      | What it is                                                               |
|------------|--------------------------------------------------------------------------|
| `site/`    | Static, client-only web page (search + HLS/MP4 playback)                 |
| `server/`  | Go HTTP server — proxies AnimeSaturn so the browser can actually play    |
| `cli/`     | Go CLI — download episodes locally                                       |
| `core/`    | Shared Go library (scraping, HLS rewriting, downloading)                 |

## Static site only (no server)

Open `./site/index.html` in any browser. This works for browsing the UI but will fail on video playback for most streams due to CORS and hotlink protection — that's exactly what `server/` is there to solve.

---

## Server

A tiny Go HTTP server that serves `site/` and proxies AnimeSaturn API and video requests. All URLs emitted (by the API and inside rewritten HLS playlists) are relative, so it can be mounted at any subpath behind a reverse proxy without configuration.

### Requirements

| Purpose | Dependency          |
|---------|---------------------|
| Build   | Go **1.26+**        |
| Run     | `curl` on `$PATH` — used as a fallback for requests the Go HTTP client can't complete due to origin anti-scraping measures |

### Build

```sh
cd server
go build -o succ-server .
```

### Run

```sh
./succ-server -addr=:8080 -static=../site
```

| Flag      | Purpose                                                                 | Default                      |
|-----------|-------------------------------------------------------------------------|------------------------------|
| `-addr`   | Listen address (`host:port`)                                            | `:$PORT` if set, else `:8080` |
| `-static` | Path to the static site directory                                       | `../site`                    |

Then open `http://localhost:8080/`.

### Docker

A `Dockerfile` is included. It produces a multi-stage, fully static, **non-root** (`UID 10001`) Alpine image with `curl` preinstalled.

```sh
# From repo root
docker build -t animesucc-server:latest .
docker run --rm -p 8080:8080 animesucc-server:latest
```

The image runs `succ-server -addr=:8080 -static=/app/site` by default. Override via `docker run … animesucc-server:latest -addr=:9000` if needed.

### Docker Compose

```yaml
services:
  succ:
    build: .
    image: animesucc-server:latest
    container_name: animesucc
    restart: unless-stopped
    read_only: true
    tmpfs:
      - /tmp
    cap_drop: [ALL]
    security_opt:
      - no-new-privileges:true
    ports:
      # Bind to localhost when fronted by a reverse proxy on the same host.
      # Use "8080:8080" to expose on all interfaces instead.
      - "127.0.0.1:8080:8080"
```

### Reverse proxy under a subpath (nginx)

AnimeSucc can be mounted at any subpath (e.g. `/succ/`) without any code change — all URLs are already relative. Your nginx vhost needs to:

1. Strip the subpath prefix before forwarding (trailing slash on `proxy_pass` does this).
2. Redirect the non-slash form (`/succ`) to the slash form (`/succ/`) so the browser resolves relative URLs against the correct base.

```nginx
server {
    listen 80;
    server_name your.host.name;

    # Redirect /succ -> /succ/ so document.baseURI has a trailing slash.
    # Without this, in-page fetches and video URLs lose the /succ prefix.
    location = /succ { return 301 /succ/; }

    location /succ/ {
        # Trailing slash on proxy_pass strips /succ from the forwarded path.
        proxy_pass http://127.0.0.1:8080/;

        proxy_http_version 1.1;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # HLS / MP4 streaming: disable buffering, bump timeouts for long segments.
        proxy_buffering    off;
        proxy_read_timeout 1h;
        proxy_send_timeout 1h;
    }
}
```

---

## CLI

A cobra-based downloader that pulls episodes from AnimeSaturn directly to disk.

### Requirements

| Purpose | Dependency          |
|---------|---------------------|
| Build   | Go **1.26+**        |
| Run     | Nothing beyond the built binary |

### Build

```sh
cd cli
go build -o succ .
```

### Usage

```sh
# Interactive: prompts for which search result to download
./succ "cowboy bebop"

# Fully scripted
./succ "cowboy bebop" --position 1 --episodes "1-5,8" --output ./downloads --parallel 5
```

| Flag                | Purpose                                                    | Default |
|---------------------|------------------------------------------------------------|---------|
| `-e, --episodes`    | Episode filter, e.g. `"1-5,8,10-12"`                       | all     |
| `-p, --position`    | Pick the Nth (1-based) search result; skips the prompt     | prompt  |
| `-o, --output`      | Output directory                                           | `.`     |
| `-j, --parallel`    | Max concurrent downloads                                   | `3`     |

---

## Repository layout

```
.
├── core/      shared library (scraping, HLS rewriting, downloading)
├── server/    HTTP server + proxy (uses core)
├── cli/       command-line downloader (uses core)
├── site/      static web client (served by the server)
├── Dockerfile multi-stage non-root build for the server
└── go.work    Go workspace: core + server + cli
```

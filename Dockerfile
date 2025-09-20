# syntax=docker/dockerfile:1.7

# ---- Build stage ----
FROM golang:1.26-alpine AS build
WORKDIR /src

COPY core/ ./core/
COPY server/ ./server/

# Build the server without the workspace (cli/ is intentionally absent).
# CGO off -> fully static binary, safe to ship on minimal images.
WORKDIR /src/server
ENV CGO_ENABLED=0 GOOS=linux GOWORK=off
RUN go build -trimpath -ldflags="-s -w" -o /out/succ-server .

# ---- Runtime stage ----
FROM alpine:3.20

# curl is required at runtime as the proxy fallback.
# ca-certificates for HTTPS to origin. tzdata for sensible log timestamps.
RUN apk add --no-cache curl ca-certificates tzdata \
    && addgroup -g 10001 -S succ \
    && adduser  -u 10001 -S -G succ -H -D succ

WORKDIR /app
COPY --from=build --chown=succ:succ /out/succ-server /app/succ-server
COPY --chown=succ:succ site/ /app/site/

USER succ
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD curl -fsS http://localhost:8080/health || exit 1

ENTRYPOINT ["/app/succ-server"]
CMD ["-addr=:8080", "-static=/app/site"]

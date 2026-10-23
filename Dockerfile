# Backend of MySportWeb: the API, and the thumbnails of the activities drawn by a headless Chromium
FROM golang:1.24-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/mysportweb ./cmd/mysportweb \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/cleanmedia ./cmd/cleanmedia

FROM debian:bookworm-slim
# chromium: the thumbnails (chromedp finds it on the PATH); tini: reaps the exited Chromium processes,
# the Go binary as PID 1 would leave them as zombies; wget: the health check
RUN apt-get update \
 && apt-get install -y --no-install-recommends chromium tini ca-certificates fonts-liberation wget \
 && rm -rf /var/lib/apt/lists/* \
 && useradd --create-home --uid 1000 app
WORKDIR /app
# MEDIA: the uploaded files, to keep in a volume; tmp: the pages of the thumbnails
RUN mkdir MEDIA tmp && chown app:app MEDIA tmp
COPY --from=build /out/mysportweb /out/cleanmedia /usr/local/bin/
USER app
ENV LISTEN_PORT=8080
# Docker's seccomp profile forbids the sandbox of Chromium: the non root container isolates it instead
ENV CHROMIUM_NO_SANDBOX=true
EXPOSE 8080
VOLUME ["/app/MEDIA"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s \
  CMD wget -qO /dev/null "http://localhost:${LISTEN_PORT}/api/v1/healthcheck" || exit 1
ENTRYPOINT ["tini", "--"]
CMD ["mysportweb"]

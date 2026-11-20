# Backend of MySportWeb: the API, and the thumbnails of the activities (maps drawn in Go)
FROM golang:1.27-trixie AS build
# the toolchain of go.mod at least (security fixes of the standard library), downloaded when the image has an
# older one: a cached image can lag behind its tag. The golang images set GOTOOLCHAIN=local, which ignores it.
ENV GOTOOLCHAIN=auto
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# .version: written by the CI (ci/image-tags.sh), X.Y.Z or the short commit sha; dev without it
RUN version=$(cat .version 2>/dev/null || echo dev) \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X MySportWeb/internal/pkg/vars.Version=$version" \
    -o /out/mysportweb ./cmd/mysportweb \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/cleanmedia ./cmd/cleanmedia

FROM debian:trixie-slim
# ca-certificates: the downloads of map and elevation tiles; tini: forwards the signals and reaps the
# processes as PID 1; wget: the health check
RUN apt-get update \
 && apt-get install -y --no-install-recommends tini ca-certificates wget \
 && rm -rf /var/lib/apt/lists/* \
 && useradd --create-home --uid 1000 app
WORKDIR /app
# MEDIA: the uploaded files, to keep in a volume
RUN mkdir MEDIA && chown app:app MEDIA
COPY --from=build /out/mysportweb /out/cleanmedia /usr/local/bin/
USER app
ENV LISTEN_PORT=8080
# the API; the metrics (Prometheus), see METRICS_PORT
EXPOSE 8080 9090
VOLUME ["/app/MEDIA"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s \
  CMD wget -qO /dev/null "http://localhost:${LISTEN_PORT}/api/v1/healthcheck" || exit 1
ENTRYPOINT ["tini", "--"]
CMD ["mysportweb"]

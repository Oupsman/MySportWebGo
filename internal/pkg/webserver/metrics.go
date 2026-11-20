package webserver

import (
	"MySportWeb/internal/pkg/metrics"
	"context"
	"crypto/subtle"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
)

// instrument counts the requests and their duration, by route: the pattern of gin (/api/v1/activity/:id),
// never the path (one series per activity), "unmatched" for a path without route
func instrument(c *gin.Context) {
	start := time.Now()
	c.Next()
	route := c.FullPath()
	if route == "" {
		route = "unmatched"
	}
	method := c.Request.Method
	metrics.HTTPRequests.WithLabelValues(method, route, strconv.Itoa(c.Writer.Status())).Inc()
	metrics.HTTPDuration.WithLabelValues(method, route).Observe(time.Since(start).Seconds())
}

// metricsHandler serves the metrics of metrics.Registry at /metrics; with a token, to the requests sending
// it as "Authorization: Bearer <token>" only (the authorization of the scrape config of Prometheus)
func metricsHandler(token string) http.Handler {
	mux := http.NewServeMux()
	metricsPage := promhttp.HandlerFor(metrics.Registry, promhttp.HandlerOpts{})
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		if token != "" && subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="metrics"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		metricsPage.ServeHTTP(w, r)
	})
	return mux
}

// RunMetrics serves the metrics on listenAddr until ctx is done: its own listener, never published by the
// frontend (nginx forwards /api and /MEDIA only)
func RunMetrics(ctx context.Context, listenAddr, token string, logger zerolog.Logger) error {
	server := &http.Server{
		Addr:              listenAddr,
		Handler:           metricsHandler(token),
		ReadHeaderTimeout: ReadHeaderTimeout,
		ReadTimeout:       time.Minute,
		WriteTimeout:      time.Minute,
		IdleTimeout:       IdleTimeout,
		MaxHeaderBytes:    MaxHeaderBytes,
	}
	logger.Info().Str("address", listenAddr).Bool("token", token != "").Msg("serving the metrics")
	return serve(ctx, server, logger)
}

package main

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/elevation"
	"MySportWeb/internal/pkg/staticmap"
	"MySportWeb/internal/pkg/vars"
	"MySportWeb/internal/pkg/webserver"
	"MySportWeb/services/activityService"
	"context"
	"fmt"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"
	"os"
	// timezones of the users, also where the system has no timezone database (containers)
	_ "time/tzdata"
)

func main() {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	vars.Init()
	log.Info().Str("version", vars.Version).Msg("starting MySportWeb")
	// the tiles are downloaded at the first activity corrected in their area only
	demURL := vars.DemURL
	switch demURL {
	case "":
		demURL = elevation.DefaultURL
	case "none":
		demURL = ""
	}
	terrain := elevation.New(vars.DemDir, demURL)
	defer terrain.Close()
	activityService.Terrain = terrain
	tileURL := vars.MapTileURL
	switch tileURL {
	case "":
		tileURL = staticmap.DefaultURL
	case "none":
		tileURL = ""
	}
	staticmap.UserAgent = "MySportWeb/" + vars.Version + " (self-hosted sport activity tracker)"
	activityService.MapTiles = staticmap.NewTiles(vars.MapTileDir, tileURL)
	driver := "postgres"
	dsn := vars.Dsn
	App, err := app.NewApp(log.Logger, driver, dsn)
	if err != nil {
		// never the DSN: it holds the password of the database
		log.Fatal().Err(err).Msg("cannot connect to the database")
	}

	// SIGTERM (docker stop) or SIGINT (Ctrl+C): the server stops accepting requests, finishes the running ones
	// and the background work at its next step, then exits
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	g, ctx := errgroup.WithContext(ctx)
	listenAddr := fmt.Sprintf(":%s", vars.ListenPort)
	g.Go(func() error { return webserver.RunHttp(ctx, listenAddr, App) })
	g.Go(func() error { return App.Start(ctx) })
	err = g.Wait()
	if err != nil {
		log.Error().Err(err).Msg("failed to run")
	}
	// the HTTP server stopped (or failed): the background work stops too
	stop()
	if !App.Wait(BackgroundStopTimeout) {
		log.Warn().Dur("timeout", BackgroundStopTimeout).Msg("background work still running, stopping anyway")
	}
	if sqlDB, dbErr := App.DB.DB.DB(); dbErr == nil {
		_ = sqlDB.Close()
	}
	log.Info().Msg("stopped")
	if err != nil {
		os.Exit(1)
	}
}

// BackgroundStopTimeout is how long the background work (backfills, recomputes) has to stop at its next
// activity, after the HTTP server stopped
const BackgroundStopTimeout = 30 * time.Second

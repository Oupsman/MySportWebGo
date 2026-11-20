package app

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/notifications"
	"MySportWeb/services/activityService"
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

type App struct {
	DB            models.DB
	Client        *http.Client
	Logger        zerolog.Logger
	Notifications *notifications.Notifs

	// the context of Start, and the work running in the background (Go)
	ctx        context.Context
	background sync.WaitGroup
}

func NewApp(logger zerolog.Logger, driver string, dsn string) (*App, error) {
	DB, err := models.ConnectToDB(driver, dsn)
	if err != nil {
		return nil, err
	}
	err = models.CreateOrMigrate(DB)
	if err != nil {
		return nil, err
	}
	if current, latest, err := models.SchemaVersion(DB); err == nil {
		logger.Info().Int64("version", current).Int64("latest", latest).Msg("database schema up to date")
	}
	// a model changed without its migration: the queries on the missing columns will fail
	missing, err := models.SchemaDrift(DB)
	if err != nil {
		return nil, err
	}
	for _, m := range missing {
		logger.Error().Str("missing", m).Msg("the database lacks a column of the models: a migration is missing")
	}

	var httpClient = &http.Client{}

	var notif = notifications.New()

	return &App{DB: *DB, Client: httpClient, Logger: logger, Notifications: notif}, nil
}

// Start runs the backfills and the notifications in the background until ctx is done, see Go
func (app *App) Start(ctx context.Context) error {
	app.ctx = ctx
	if err := app.Notifications.Start(app.Logger, app.Client, app.DB); err != nil {
		return err
	}
	app.Go(func(ctx context.Context) { app.Notifications.Run(ctx, app.Logger, app.Client, app.DB) })
	app.Go(app.backfill)
	return nil
}

// backfill brings the activities analysed by the older versions up to date, one step after the other; each
// step stops at the next activity when ctx is done, the next start goes on
func (app *App) backfill(ctx context.Context) {
	db, logger := &app.DB, app.Logger
	steps := []struct {
		failure string
		run     func(ctx context.Context, db *models.DB, logger zerolog.Logger) error
	}{
		{"failed to create the devices of activities", func(_ context.Context, db *models.DB, logger zerolog.Logger) error {
			linked, err := db.BackfillDevices()
			if linked > 0 {
				logger.Info().Int64("activities", linked).Msg("activities linked to their device")
			}
			return err
		}},
		{"failed to assign routes to activities", activityService.BackfillRoutes},
		{"failed to re-analyse activities", activityService.BackfillTimeStamps},
		// after BackfillTimeStamps: it fixes the endpoints of the activities and routes of the old engines
		{"failed to merge similar routes", activityService.MergeRoutes},
		// after BackfillTimeStamps, which analyses with the weights too: only the activities left
		{"failed to analyse activities with their weight", activityService.BackfillWeights},
		// after BackfillTimeStamps: the TRIMP needs the time stamps of the heart rates
		{"failed to compute the TRIMP of activities", activityService.BackfillTrimp},
		// after BackfillTimeStamps too: the zones need the time stamps of the heart rates
		{"failed to compute the heart rate zones of activities", activityService.BackfillHrZones},
		{"failed to compute the best efforts of activities", activityService.BackfillBestEfforts},
		{"failed to rename activities", activityService.BackfillTitles},
	}
	for _, step := range steps {
		if ctx.Err() != nil {
			logger.Info().Msg("backfills stopped by the shutdown, they go on at the next start")
			return
		}
		if err := step.run(ctx, db, logger); err != nil {
			logger.Error().Err(err).Msg(step.failure)
		}
	}
}

// Go runs fn in the background, with the context of Start: done at the shutdown, when fn must stop soon
// (at its next step). Wait waits for the ones still running. Before Start, fn gets a context never done.
func (app *App) Go(fn func(ctx context.Context)) {
	ctx := app.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	app.background.Add(1)
	go func() {
		defer app.background.Done()
		fn(ctx)
	}()
}

// Wait waits for the work started by Go to stop, at most timeout: false when some is still running
func (app *App) Wait(timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		app.background.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

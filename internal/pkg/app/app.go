package app

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/notifications"
	"MySportWeb/services/activityService"
	"github.com/rs/zerolog"
	"net/http"
)

type App struct {
	DB            models.DB
	Client        *http.Client
	Logger        zerolog.Logger
	Notifications *notifications.Notifs
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

	var httpClient = &http.Client{}

	var notif = notifications.New()

	return &App{DB: *DB, Client: httpClient, Logger: logger, Notifications: notif}, nil
}

func (app *App) Start() error {
	go func() {
		linked, err := app.DB.BackfillDevices()
		if err != nil {
			app.Logger.Error().Err(err).Msg("failed to create the devices of activities")
		} else if linked > 0 {
			app.Logger.Info().Int64("activities", linked).Msg("activities linked to their device")
		}
		err = activityService.BackfillRoutes(&app.DB, app.Logger)
		if err != nil {
			app.Logger.Error().Err(err).Msg("failed to assign routes to activities")
		}
		err = activityService.BackfillTimeStamps(&app.DB, app.Logger)
		if err != nil {
			app.Logger.Error().Err(err).Msg("failed to re-analyse activities")
		}
		// after BackfillTimeStamps: it fixes the endpoints of the activities and routes of the old engines
		err = activityService.MergeRoutes(&app.DB, app.Logger)
		if err != nil {
			app.Logger.Error().Err(err).Msg("failed to merge similar routes")
		}
		// after BackfillTimeStamps: the TRIMP needs the time stamps of the heart rates
		err = activityService.BackfillTrimp(&app.DB, app.Logger)
		if err != nil {
			app.Logger.Error().Err(err).Msg("failed to compute the TRIMP of activities")
		}
		// after BackfillTimeStamps too: the zones need the time stamps of the heart rates
		err = activityService.BackfillHrZones(&app.DB, app.Logger)
		if err != nil {
			app.Logger.Error().Err(err).Msg("failed to compute the heart rate zones of activities")
		}
		err = activityService.BackfillTitles(&app.DB, app.Logger)
		if err != nil {
			app.Logger.Error().Err(err).Msg("failed to rename activities")
		}
	}()
	err := app.Notifications.Start(app.Logger, app.Client, app.DB)
	if err != nil {
		return err
	}

	return nil
}

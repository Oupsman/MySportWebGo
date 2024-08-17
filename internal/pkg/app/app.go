package app

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/notifications"
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
	err := app.Notifications.Start(app.Logger, app.Client, app.DB)
	if err != nil {
		return err
	}

	return nil
}

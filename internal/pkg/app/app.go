package app

import (
	"MySportWeb/internal/pkg/mail"
	"MySportWeb/internal/pkg/metrics"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/notifications"
	"MySportWeb/internal/pkg/utils"
	"MySportWeb/internal/pkg/vars"
	"MySportWeb/services/activityService"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

type App struct {
	DB            models.DB
	Client        *http.Client
	Logger        zerolog.Logger
	Notifications *notifications.Notifs
	// Mailer sends the emails, nil when they are off (no SMTP_HOST): see SendMail
	Mailer mail.Mailer
	// SetupCode registers the first admin, whatever REGISTRATION says: drawn at the start while the instance
	// has no admin, printed in the logs only, "" once there is one at the start
	SetupCode string

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
		metrics.SchemaVersion.Set(float64(current))
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

	App := &App{DB: *DB, Client: httpClient, Logger: logger, Notifications: notif}
	if err := App.prepareSetup(); err != nil {
		return nil, err
	}
	return App, nil
}

// prepareSetup draws the setup code of an instance without admin, and tells it in the logs: whoever reads
// the logs runs the instance, the first visitor of a new one doesn't become its admin
func (app *App) prepareSetup() error {
	hasAdmin, err := app.DB.HasAdmin()
	if err != nil || hasAdmin {
		return err
	}
	app.SetupCode = utils.NewCode()
	app.Logger.Warn().Str("setup_code", app.SetupCode).
		Msg("this instance has no admin yet: create it on the registration page of the web application with " +
			"this setup code (a new code at each start, until the admin exists)")
	return nil
}

// Start runs the backfills and the notifications in the background until ctx is done, see Go
func (app *App) Start(ctx context.Context) error {
	app.ctx = ctx
	if err := app.Notifications.Start(app.Logger, app.Client, app.DB); err != nil {
		return err
	}
	app.Notifications.Subject = vars.Domain
	app.Go(func(ctx context.Context) { app.Notifications.Run(ctx, app.Logger, app.Client, app.DB) })
	app.Go(func(ctx context.Context) { notifications.RunServiceAlerts(ctx, app.Logger, app.DB) })
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
		// after BackfillBestEfforts: the FTP is estimated from the best 20 minutes
		{"failed to compute the power metrics of activities", activityService.BackfillPowerMetrics},
		{"failed to compute the power curves of activities", activityService.BackfillPowerCurves},
		{"failed to rename activities", activityService.BackfillTitles},
		// last: it downloads map tiles, the longest on a first start
		{"failed to draw the thumbnails of activities again", activityService.BackfillThumbnails},
	}
	for _, step := range steps {
		if ctx.Err() != nil {
			logger.Info().Msg("backfills stopped by the shutdown, they go on at the next start")
			return
		}
		// the metrics name the step by what it does: "compute the TRIMP of activities"
		name := strings.TrimPrefix(step.failure, "failed to ")
		metrics.BackfillRunning.WithLabelValues(name).Set(1)
		start := time.Now()
		err := step.run(ctx, db, logger)
		metrics.BackfillRunning.WithLabelValues(name).Set(0)
		metrics.BackfillDuration.WithLabelValues(name).Set(time.Since(start).Seconds())
		if err != nil {
			metrics.BackfillErrors.WithLabelValues(name).Inc()
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

// ErrMailOff is the answer of SendMail when the emails are off on this instance
var ErrMailOff = errors.New("the emails are off on this instance: its admin sets SMTP_HOST to turn them on")

// MailEnabled tells whether the instance sends emails
func (app *App) MailEnabled() bool {
	return app.Mailer != nil
}

// SendMail sends an email of a kind (the label of its metric), counted and logged; ErrMailOff when off
func (app *App) SendMail(ctx context.Context, kind string, m mail.Message) error {
	if app.Mailer == nil {
		app.Logger.Warn().Str("kind", kind).Msg("email not sent: the emails are off (no SMTP_HOST)")
		return ErrMailOff
	}
	if err := app.Mailer.Send(ctx, m); err != nil {
		metrics.Emails.WithLabelValues(kind, "failed").Inc()
		app.Logger.Error().Err(err).Str("kind", kind).Msg("email not sent")
		return err
	}
	metrics.Emails.WithLabelValues(kind, "sent").Inc()
	app.Logger.Info().Str("kind", kind).Msg("email sent")
	return nil
}

// EmailNotification sends a notification (a notifications.PushMessage, as JSON) by email to a user, at their
// confirmed address: notifications.Notifs.Email. An unconfirmed address or a disabled user drops it.
func (app *App) EmailNotification(ctx context.Context, userID uint, message string) error {
	recipient, err := app.DB.GetEmailRecipient(userID)
	if err != nil {
		return err
	}
	if !recipient.Verified || recipient.Disabled {
		app.Logger.Info().Uint("user_id", userID).Msg("notification by email dropped: address not confirmed, or account disabled")
		return nil
	}
	var m notifications.PushMessage
	if err := json.Unmarshal([]byte(message), &m); err != nil {
		return fmt.Errorf("notification not readable: %w", err)
	}
	site := strings.TrimRight(vars.Domain, "/")
	return app.SendMail(ctx, "notification", mail.Notification(recipient.Username, recipient.Email, m.Title, m.Body,
		site+m.URL, site+"/#/profile/view"))
}

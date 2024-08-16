package app

import (
	"MySportWeb/pkg/internal/models"
	"MySportWeb/pkg/internal/notifications"
	"github.com/rs/zerolog"
	"net/http"
)

type App struct {
	DB            models.DB
	Client        *http.Client
	Logger        zerolog.Logger
	Notifications *notifications.Notifs
}

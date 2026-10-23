package main

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/vars"
	"MySportWeb/internal/pkg/webserver"
	"fmt"
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
	driver := "postgres"
	dsn := vars.Dsn
	App, err := app.NewApp(log.Logger, driver, dsn)
	if err != nil {
		// never the DSN: it holds the password of the database
		log.Fatal().Err(err).Msg("cannot connect to the database")
	}

	var g errgroup.Group
	var host = ""
	var listenAddr string
	listenAddr = fmt.Sprintf("%s:%s", host, vars.ListenPort)
	g.Go(func() error {
		//		return webserver.RunHttp(":8080", storeApp, update.Message.Chat.ID)
		return webserver.RunHttp(listenAddr, App)
	})
	g.Go(func() error { return App.Start() })

	if err := g.Wait(); err != nil {
		log.Error().Err(err).Msg("failed to run")

	}
}

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
)

func main() {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	vars.Init()
	driver := "postgres"
	dsn := vars.Dsn
	App, err := app.NewApp(log.Logger, driver, dsn)
	if err != nil {
		log.Panic().Err(err)
	}

	var g errgroup.Group
	var host = "127.0.0.1"
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

// MySportWeb Sync uploads to MySportWeb the activities of the devices plugged through USB, in mass
// storage or MTP mode
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"fyne.io/fyne/v2/app"

	"MySportWebSync/internal/config"
	"MySportWebSync/internal/device"
	"MySportWebSync/internal/state"
	"MySportWebSync/internal/syncer"
	"MySportWebSync/internal/ui"
)

func main() {
	hidden := flag.Bool("hidden", false, "start in the system tray, e.g. at login")
	scan := flag.Bool("scan", false, "print the devices found and their activity files, then exit")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		log.Printf("the settings could not be read, the default ones are used: %v", err)
	}
	if *scan {
		os.Exit(printScan(cfg))
	}
	st, err := state.Load()
	if err != nil {
		log.Fatal(err)
	}

	a := app.NewWithID("io.github.oupsman.mysportwebsync")
	u := ui.New(a)
	manager := syncer.NewManager(cfg, st, device.NewMTPScanner(), u.Changed, u.Notify)
	u.Attach(manager)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go manager.Run(ctx)
	u.Run(ctx)

	if !*hidden || cfg.Token == "" {
		u.Show()
	}
	a.Run()
}

// printScan prints what a synchronization would see, to check a device
func printScan(cfg config.Config) int {
	ctx := context.Background()
	mtp := device.NewMTPScanner()
	status := 0
	found := 0
	for _, scanner := range []device.Scanner{device.NewFSScanner(), mtp} {
		devices, err := scanner.Scan(ctx, cfg.Folders)
		if err != nil {
			fmt.Fprintln(os.Stderr, "scan:", err)
			status = 1
		}
		for _, d := range devices {
			found++
			files, err := d.Files(ctx)
			d.Close()
			fmt.Printf("%s (%s) id=%s\n", d.Name(), d.Kind(), d.ID())
			if err != nil {
				fmt.Printf("  error: %v\n", err)
				status = 1
				continue
			}
			fmt.Printf("  %d activity files\n", len(files))
			for _, f := range files[max(0, len(files)-5):] {
				fmt.Printf("  %s  %8d bytes  %s\n", f.ModTime.Format("2006-01-02 15:04"), f.Size, f.Path)
			}
		}
	}
	for _, notice := range mtp.Notices() {
		fmt.Println("not listed:", notice)
	}
	if found == 0 {
		fmt.Println("no device with an activity folder among", cfg.Folders)
	}
	return status
}

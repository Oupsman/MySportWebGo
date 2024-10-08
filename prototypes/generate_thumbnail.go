package main

import (
	"context"
	"fmt"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/muktihari/fit/decoder"
	"github.com/muktihari/fit/profile/filedef"
	"math"
	"os"
	"time"
)

func main() {
	var Lats, Lons []float64

	filePath := "2022-08-30-18-12-37.fit"
	f, err := os.Open(filePath)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	dec := decoder.New(f)
	// Read the fit file
	gpsPoints := "["
	for dec.Next() {
		fit, err := dec.Decode()
		if err != nil {
			panic(err)
		}
		activity := filedef.NewActivity(fit.Messages...)

		for _, record := range activity.Records {
			if record.PositionLat != math.MaxInt32 &&
				record.PositionLong != math.MaxInt32 &&
				float64(500) < record.DistanceScaled() &&
				activity.Sessions[0].TotalDistanceScaled()-record.DistanceScaled() > float64(500) {
				gpsPoints += fmt.Sprintf("[ %f, %f ],", SemiCircleToDegres(record.PositionLat), SemiCircleToDegres(record.PositionLong))
				Lats = append(Lats, SemiCircleToDegres(record.PositionLat))
				Lons = append(Lons, SemiCircleToDegres(record.PositionLong))
			}
		}
	}
	gpsPoints += "]"
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	fileName := wd + "/test.html"
	htmlFile, err := os.Create(fileName)
	if err != nil {
		panic(err)
	}
	htmlPage := "<html><head><link rel=\"stylesheet\" href=\"https://unpkg.com/leaflet@1.9.4/dist/leaflet.css\"\n     integrity=\"sha256-p4NxAoJBhIIN+hmNHrzRCf9tD/miZyoHS5obTRR9BMY=\"\n     crossorigin=\"\"/><script src=\"https://unpkg.com/leaflet@1.9.4/dist/leaflet.js\"\n     integrity=\"sha256-20nQCchB9co0qIjJZRGuk2/Z9VM+kNiyxNV1lvTlZBo=\"\n     crossorigin=\"\"></script></head><body>"
	htmlPage += "<div id=\"map\" style=\"width: 600px; height: 400px; position: absolute;\"></div>\n"
	htmlPage += "<script>"
	htmlPage += "var map = new L.map('map', { zoomControl: false });\n"
	htmlPage += "var tile_layer = L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png', {\n    maxZoom: 19}).addTo(map);"
	htmlPage += fmt.Sprintf("var trace = L.polyline(%s).addTo(map)\n", gpsPoints)
	htmlPage += "map.fitBounds(trace.getBounds());\n"
	htmlPage += "tile_layer.on(\"load\",function() { console.log(\"all visible tiles have been loaded\") });"
	htmlPage += "</script>"
	htmlPage += "</body></html>"
	htmlFile.Write([]byte(htmlPage))
	htmlFile.Close()
	/*	opts := append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.Flag("headless", false),
		)
		allocCtx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
		defer cancel()*/
	ctx, cancel := chromedp.NewContext(context.Background())
	defer cancel()
	var screenshotBuffer []byte

	messageChan := make(chan bool, 1)
	chromedp.ListenTarget(ctx, func(ev interface{}) {
		if ev, ok := ev.(*runtime.EventConsoleAPICalled); ok {
			for _, arg := range ev.Args {
				if arg.Value != nil {
					message := string(arg.Value)
					if message == "\"all visible tiles have been loaded\"" {
						messageChan <- true
						return
					}
				}
			}
		}
	})
	err = chromedp.Run(ctx,
		chromedp.Navigate("file://	"+fileName),
		chromedp.Sleep(50*time.Millisecond),
		chromedp.Screenshot("#map", &screenshotBuffer, chromedp.NodeVisible),
	)
	if err != nil {
		panic(err)
	}
	err = os.WriteFile("activity.png", screenshotBuffer, 0644)
	if err != nil {
		panic(err)
	}

}

func SemiCircleToDegres(semi int32) float64 {
	return float64(semi) * (180.0 / math.Pow(2.0, 31.0))
}

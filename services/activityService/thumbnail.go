package activityService

import (
	"MySportWeb/internal/pkg/models"
	"context"
	"fmt"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"os"
	"path/filepath"
	"time"
)

func GenerateThumbnail(activity models.Activity) error {
	gpsPoints := "["

	for _, record := range activity.PublicGpsPoints {
		gpsPoints += fmt.Sprintf("[ %f, %f ],", record.Lat, record.Lon)
	}
	gpsPoints += "]"
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	fileName := wd + "/tmp/" + fmt.Sprintf("%s", activity.ID.String()+".html")
	htmlFile, err := os.Create(fileName)
	if err != nil {
		return err
	}
	htmlPage := "<html><head><link rel=\"stylesheet\" href=\"https://unpkg.com/leaflet@1.9.4/dist/leaflet.css\"\n     integrity=\"sha256-p4NxAoJBhIIN+hmNHrzRCf9tD/miZyoHS5obTRR9BMY=\"\n     crossorigin=\"\"/><script src=\"https://unpkg.com/leaflet@1.9.4/dist/leaflet.js\"\n     integrity=\"sha256-20nQCchB9co0qIjJZRGuk2/Z9VM+kNiyxNV1lvTlZBo=\"\n     crossorigin=\"\"></script></head><body>"
	htmlPage += "<div id=\"map\" style=\"width: 300px; height: 200px; position: absolute;\"></div>\n"
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
		return err
	}
	baseDir := fmt.Sprintf("%s/MEDIA/%s/thumbnails", wd, activity.User.UUID.String())
	fmt.Printf("baseDir: %s %s", baseDir, filepath.Dir(baseDir))
	if err = os.MkdirAll(baseDir, 0770); err != nil {
		return err
	}
	dstFile := fmt.Sprintf("%s/%s.png", baseDir, activity.ID.String())

	err = os.WriteFile(dstFile, screenshotBuffer, 0644)

	if err != nil {
		return err
	}
	return nil
}

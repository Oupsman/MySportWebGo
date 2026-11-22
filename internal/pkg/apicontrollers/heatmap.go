package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/utils"
	"MySportWeb/services/activityService"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// heatmapActivity is an activity of the heatmap: its trace encoded (Google's polyline format), its bounds
// [south, west, north, east] to skip it out of view
type heatmapActivity struct {
	ID       uuid.UUID  `json:"id"`
	Title    string     `json:"title"`
	Date     string     `json:"date"`
	Sport    string     `json:"sport"`
	Distance float64    `json:"distance"`
	Bounds   [4]float64 `json:"bounds"`
	Trace    string     `json:"trace"`
}

// Heatmap answers the traces of the activities of the user, for their heatmap: ?sport= (several, none for
// all), ?from=&to= (their days, included), ?hide_ends=true cuts the first and last meters of each one (their
// privacy distance). Their own traces, whole: the page is theirs. An ETag answers 304 while nothing changed.
func Heatmap(c *gin.Context) {
	App := c.MustGet("App").(*app.App)
	user, location := currentUserLocation(c)
	sports := c.QueryArray("sport")
	for _, sport := range sports {
		if !activityService.IsSportName(sport) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "unknown sport " + strconv.Quote(sport)})
			return
		}
	}
	from, to, ok := periodQuery(c, location)
	if !ok {
		return
	}
	hideEnds := 0.0
	if c.Query("hide_ends") == "true" {
		hideEnds = float64(user.SecurityDistance)
	}

	count, latest, err := App.DB.HeatmapVersion(user.ID, sports, from, to)
	if err != nil {
		internalError(c, err)
		return
	}
	// the version of the traces, of the request and of the user (the same URL for another account differs)
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d|%d|%d|%s|%v", user.ID, count, latest.UnixNano(),
		c.Request.URL.RawQuery, hideEnds)))
	etag := `"` + hex.EncodeToString(sum[:12]) + `"`
	c.Header("ETag", etag)
	c.Header("Cache-Control", "private, no-cache")
	if match := c.GetHeader("If-None-Match"); match != "" && strings.Contains(match, etag) {
		c.Status(http.StatusNotModified)
		return
	}

	traces, err := App.DB.GetHeatmapTraces(user.ID, sports, from, to)
	if err != nil {
		internalError(c, err)
		return
	}
	activities := make([]heatmapActivity, 0, len(traces))
	for _, t := range traces {
		points := activityService.HideEnds(t.SimplifiedGpsPoints, hideEnds)
		if len(points) < 2 {
			continue
		}
		bounds := [4]float64{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
		for _, p := range points {
			bounds[0], bounds[1] = math.Min(bounds[0], p.Lat), math.Min(bounds[1], p.Lon)
			bounds[2], bounds[3] = math.Max(bounds[2], p.Lat), math.Max(bounds[3], p.Lon)
		}
		activities = append(activities, heatmapActivity{ID: t.ID, Title: t.Title, Date: t.Date.In(location).Format("2006-01-02"),
			Sport: t.Sport, Distance: t.Distance, Bounds: bounds, Trace: utils.EncodePolyline(points)})
	}
	c.JSON(http.StatusOK, gin.H{"activities": activities})
}

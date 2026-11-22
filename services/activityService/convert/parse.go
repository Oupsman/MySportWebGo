package convert

import (
	"bytes"
	"encoding/xml"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/muktihari/fit/profile/typedef"
)

// number is an XML value read as a number, nil when missing or not a number
type number struct{ v *float64 }

func (n *number) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	var text string
	if err := d.DecodeElement(&text, &start); err != nil {
		return err
	}
	if v, err := strconv.ParseFloat(strings.TrimSpace(text), 64); err == nil {
		n.v = &v
	}
	return nil
}

// first returns the first of values that is set
func first(values ...number) *float64 {
	for _, v := range values {
		if v.v != nil {
			return v.v
		}
	}
	return nil
}

func parseTime(text string) time.Time {
	text = strings.TrimSpace(text)
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02T15:04:05.999999999"} {
		if t, err := time.Parse(layout, text); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

func decode(content []byte, v any) error {
	d := xml.NewDecoder(bytes.NewReader(content))
	// the files say UTF-8 or ISO-8859-1, without the characters that differ in the values read
	d.CharsetReader = func(_ string, r io.Reader) (io.Reader, error) { return r, nil }
	if err := d.Decode(v); err != nil {
		return invalid("the file isn't valid XML: %v", err)
	}
	return nil
}

// the elements are matched on their local name: the files put them in namespaces, with prefixes that vary
type gpxExtensions struct {
	Power number `xml:"power"`
	// Garmin's TrackPointExtension (gpxtpx:), written by Garmin, Strava and most applications
	TPX struct {
		HR          number `xml:"hr"`
		Cad         number `xml:"cad"`
		ATemp       number `xml:"atemp"`
		Speed       number `xml:"speed"`
		Power       number `xml:"power"`
		Temperature number `xml:"wtemp"`
	} `xml:"TrackPointExtension"`
	// older files: the values right in the extensions
	HR    number `xml:"hr"`
	Cad   number `xml:"cad"`
	ATemp number `xml:"atemp"`
}

type gpxPoint struct {
	Lat        number        `xml:"lat,attr"`
	Lon        number        `xml:"lon,attr"`
	Ele        number        `xml:"ele"`
	Time       string        `xml:"time"`
	Extensions gpxExtensions `xml:"extensions"`
}

type gpxFile struct {
	Creator string `xml:"creator,attr"`
	Tracks  []struct {
		Type     string `xml:"type"`
		Segments []struct {
			Points []gpxPoint `xml:"trkpt"`
		} `xml:"trkseg"`
	} `xml:"trk"`
	// a route without track: the points of a planned route, with times when recorded by some applications
	Routes []struct {
		Type   string     `xml:"type"`
		Points []gpxPoint `xml:"rtept"`
	} `xml:"rte"`
}

func (p *number) UnmarshalXMLAttr(attr xml.Attr) error {
	if v, err := strconv.ParseFloat(strings.TrimSpace(attr.Value), 64); err == nil {
		p.v = &v
	}
	return nil
}

func gpxTrackPoint(p gpxPoint) point {
	e := p.Extensions
	pt := point{
		time:        parseTime(p.Time),
		altitude:    p.Ele.v,
		heartRate:   first(e.TPX.HR, e.HR),
		cadence:     first(e.TPX.Cad, e.Cad),
		power:       first(e.Power, e.TPX.Power),
		temperature: first(e.TPX.ATemp, e.ATemp, e.TPX.Temperature),
		speed:       e.TPX.Speed.v,
	}
	if p.Lat.v != nil && p.Lon.v != nil && *p.Lat.v >= -90 && *p.Lat.v <= 90 && *p.Lon.v >= -180 && *p.Lon.v <= 180 {
		pt.lat, pt.lon = p.Lat.v, p.Lon.v
	}
	return pt
}

func parseGPX(content []byte) (track, error) {
	var file gpxFile
	if err := decode(content, &file); err != nil {
		return track{}, err
	}
	t := track{sport: typedef.SportInvalid, creator: file.Creator}
	for _, trk := range file.Tracks {
		if t.sport == typedef.SportInvalid {
			t.sport = sportOf(trk.Type)
		}
		for _, segment := range trk.Segments {
			for _, p := range segment.Points {
				t.points = append(t.points, gpxTrackPoint(p))
			}
		}
	}
	if len(t.points) == 0 {
		for _, rte := range file.Routes {
			if t.sport == typedef.SportInvalid {
				t.sport = sportOf(rte.Type)
			}
			for _, p := range rte.Points {
				t.points = append(t.points, gpxTrackPoint(p))
			}
		}
	}
	if len(t.points) == 0 {
		return track{}, invalid("the GPX file has no track")
	}
	return t, nil
}

type tcxFile struct {
	Activities []struct {
		Sport string `xml:"Sport,attr"`
		Laps  []struct {
			Calories number `xml:"Calories"`
			Tracks   []struct {
				Points []struct {
					Time     string `xml:"Time"`
					Position struct {
						Lat number `xml:"LatitudeDegrees"`
						Lon number `xml:"LongitudeDegrees"`
					} `xml:"Position"`
					Altitude   number `xml:"AltitudeMeters"`
					Distance   number `xml:"DistanceMeters"`
					HeartRate  number `xml:"HeartRateBpm>Value"`
					Cadence    number `xml:"Cadence"`
					Extensions struct {
						TPX struct {
							Speed      number `xml:"Speed"`
							Watts      number `xml:"Watts"`
							RunCadence number `xml:"RunCadence"`
						} `xml:"TPX"`
					} `xml:"Extensions"`
				} `xml:"Trackpoint"`
			} `xml:"Track"`
		} `xml:"Lap"`
		Creator struct {
			Name string `xml:"Name"`
		} `xml:"Creator"`
	} `xml:"Activities>Activity"`
	Author struct {
		Name string `xml:"Name"`
	} `xml:"Author"`
}

// tcxSports are the sports of the TCX format: Running, Biking, Other
var tcxSports = map[string]typedef.Sport{"running": typedef.SportRunning, "biking": typedef.SportCycling}

func parseTCX(content []byte) (track, error) {
	var file tcxFile
	if err := decode(content, &file); err != nil {
		return track{}, err
	}
	if len(file.Activities) == 0 {
		return track{}, invalid("the TCX file has no activity")
	}
	// the first activity: a TCX file holds one, the format allows several
	a := file.Activities[0]
	t := track{sport: typedef.SportInvalid, creator: a.Creator.Name}
	if sport, ok := tcxSports[strings.ToLower(strings.TrimSpace(a.Sport))]; ok {
		t.sport = sport
	}
	if t.creator == "" {
		t.creator = file.Author.Name
	}
	for _, lap := range a.Laps {
		if lap.Calories.v != nil {
			t.calories += *lap.Calories.v
		}
		for _, trk := range lap.Tracks {
			for _, p := range trk.Points {
				pt := point{
					time:      parseTime(p.Time),
					altitude:  p.Altitude.v,
					distance:  p.Distance.v,
					heartRate: p.HeartRate.v,
					cadence:   first(p.Cadence, p.Extensions.TPX.RunCadence),
					power:     p.Extensions.TPX.Watts.v,
					speed:     p.Extensions.TPX.Speed.v,
				}
				if p.Position.Lat.v != nil && p.Position.Lon.v != nil {
					pt.lat, pt.lon = p.Position.Lat.v, p.Position.Lon.v
				}
				t.points = append(t.points, pt)
			}
		}
	}
	if len(t.points) == 0 {
		return track{}, invalid("the TCX file has no track point")
	}
	return t, nil
}

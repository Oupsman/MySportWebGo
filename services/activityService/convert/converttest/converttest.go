// Package converttest writes the GPX and TCX activities of the tests
package converttest

import (
	"fmt"
	"strings"
	"time"
)

// Start is the start of the sample activities
var Start = time.Date(2025, 6, 1, 7, 0, 0, 0, time.UTC)

// metersPerDegree is the length of a degree of latitude, about
const metersPerDegree = 111195.0

// GPX is a ride of 600 points, 1 s and 5 m apart going north, climbing 0.5 m a point, with a pause of
// 2 minutes after the 300th point.
func GPX(withType bool) []byte {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<gpx creator="StravaGPX iPhone" version="1.1" xmlns="http://www.topografix.com/GPX/1/1"
 xmlns:gpxtpx="http://www.garmin.com/xmlschemas/TrackPointExtension/v1">
 <metadata><time>2025-06-01T07:00:00Z</time></metadata>
 <trk><name>Morning Ride</name>`)
	if withType {
		b.WriteString(`<type>cycling</type>`)
	}
	b.WriteString(`<trkseg>`)
	at := Start
	for i := 0; i < 600; i++ {
		if i == 300 {
			at = at.Add(2 * time.Minute)
		}
		lat := 45.0 + float64(i)*5/metersPerDegree
		fmt.Fprintf(&b, `<trkpt lat="%.7f" lon="6.0000000"><ele>%.1f</ele><time>%s</time>`+
			`<extensions><power>%d</power><gpxtpx:TrackPointExtension><gpxtpx:hr>%d</gpxtpx:hr>`+
			`<gpxtpx:cad>85</gpxtpx:cad></gpxtpx:TrackPointExtension></extensions></trkpt>`,
			lat, 100+float64(i)*0.5, at.Format(time.RFC3339), 200, 140+i%10)
		at = at.Add(time.Second)
	}
	b.WriteString(`</trkseg></trk></gpx>`)
	return []byte(b.String())
}

// TCX is a run of 300 points, 1 s and 3 m apart, its distance given by the file
func TCX() []byte {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<TrainingCenterDatabase xmlns="http://www.garmin.com/xmlschemas/TrainingCenterDatabase/v2"
 xmlns:ns3="http://www.garmin.com/xmlschemas/ActivityExtension/v2">
 <Activities><Activity Sport="Running"><Id>2025-06-01T07:00:00Z</Id>
 <Lap StartTime="2025-06-01T07:00:00Z"><TotalTimeSeconds>299</TotalTimeSeconds><Calories>75</Calories><Track>`)
	for i := 0; i < 300; i++ {
		fmt.Fprintf(&b, `<Trackpoint><Time>%s</Time><Position><LatitudeDegrees>%.7f</LatitudeDegrees>`+
			`<LongitudeDegrees>6.0</LongitudeDegrees></Position><AltitudeMeters>200</AltitudeMeters>`+
			`<DistanceMeters>%d</DistanceMeters><HeartRateBpm><Value>150</Value></HeartRateBpm>`+
			`<Extensions><ns3:TPX><ns3:RunCadence>88</ns3:RunCadence></ns3:TPX></Extensions></Trackpoint>`,
			Start.Add(time.Duration(i)*time.Second).Format(time.RFC3339), 45+float64(i)*3/metersPerDegree, i*3)
	}
	b.WriteString(`</Track></Lap><Creator><Name>Forerunner 245</Name></Creator></Activity></Activities>
</TrainingCenterDatabase>`)
	return []byte(b.String())
}

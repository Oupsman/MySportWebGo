package activityService

import (
	"MySportWeb/internal/pkg/types"
	"math"
	"testing"
	"time"

	"github.com/muktihari/fit/profile/basetype"
	"github.com/muktihari/fit/profile/filedef"
	"github.com/muktihari/fit/profile/mesgdef"
	"github.com/muktihari/fit/proto"
)

// radarActivity builds the records of a ride with EIradar: one per second from start, the threat level,
// vehicle count and nearest distance of each; nil leaves a record without radar fields. EIradar is on the
// developer data index 2, after a field of another application with the same number on the index 1, as on a
// Karoo.
func radarActivity(start time.Time, samples [][3]int, gapAfter int) *filedef.Activity {
	fit := &filedef.Activity{
		FieldDescriptions: []*mesgdef.FieldDescription{
			{DeveloperDataIndex: 1, FieldDefinitionNumber: 0, FieldName: []string{"charge"}, FitBaseTypeId: basetype.Uint8},
			{DeveloperDataIndex: 2, FieldDefinitionNumber: 0, FieldName: []string{radarThreatField}, FitBaseTypeId: basetype.Enum},
			{DeveloperDataIndex: 2, FieldDefinitionNumber: 1, FieldName: []string{radarVehiclesField}, FitBaseTypeId: basetype.Enum},
			{DeveloperDataIndex: 2, FieldDefinitionNumber: 2, FieldName: []string{radarDistanceField}, FitBaseTypeId: basetype.Uint16},
		},
	}
	at := start
	for i, s := range samples {
		record := mesgdef.NewRecord(nil)
		record.Timestamp = at
		// a position along a line, the 4th record without fix
		if i != 3 {
			record.PositionLat = int32(i * 1000)
			record.PositionLong = int32(i * 2000)
		} else {
			record.PositionLat, record.PositionLong = math.MaxInt32, math.MaxInt32
		}
		record.DeveloperFields = []proto.DeveloperField{
			{DeveloperDataIndex: 1, Num: 0, Value: proto.Uint8(77)}, // the battery, never a threat
		}
		if s[0] >= 0 {
			record.DeveloperFields = append(record.DeveloperFields,
				proto.DeveloperField{DeveloperDataIndex: 2, Num: 0, Value: proto.Uint8(uint8(s[0]))},
				proto.DeveloperField{DeveloperDataIndex: 2, Num: 1, Value: proto.Uint8(uint8(s[1]))},
				proto.DeveloperField{DeveloperDataIndex: 2, Num: 2, Value: proto.Uint16(uint16(s[2]))})
		}
		fit.Records = append(fit.Records, record)
		at = at.Add(time.Second)
		if i == gapAfter {
			at = at.Add(RadarMaxGap + time.Second)
		}
	}
	return fit
}

func TestRadarEvents(t *testing.T) {
	start := time.Date(2026, 11, 24, 15, 53, 27, 0, time.UTC)
	samples := [][3]int{
		{0, 0, 0},  // 0: clear
		{1, 1, 87}, // 1: a vehicle behind
		{1, 1, 40},
		{2, 2, 21}, // 3: closing in fast, no GPS fix: the position of a record beside it
		{2, 3, 9},  // 4: the fastest and the nearest: the marker
		{1, 2, 3},
		{0, 0, 0},  // 6: clear
		{-1, 0, 0}, // 7: no radar field: not an event
		{1, 1, 50}, // 8: another vehicle
		{1, 1, 30}, // 9: a gap after it ends the stretch
		{1, 1, 20}, // 10
		{1, 0, 0},  // 11: approaching without distance
	}
	events := RadarEvents(radarActivity(start, samples, 9), start)
	if len(events) != 3 {
		t.Fatalf("%d events, want 3: %+v", len(events), events)
	}
	first := events[0]
	if first.Offset != 1 || first.Duration != 5 || first.Threat != types.RadarFast || first.Vehicles != 3 ||
		first.Distance != 9 {
		t.Errorf("first event %+v, want offset 1, 5 s, fast, 3 vehicles, 9 m", first)
	}
	if want := recordPosition(&mesgdef.Record{PositionLat: 4000, PositionLong: 8000}); first.Position == nil ||
		*first.Position != want {
		t.Errorf("first event at %v, want %v (the record of the marker)", first.Position, want)
	}
	second := events[1]
	if second.Offset != 8 || second.Duration != 2 || second.Threat != types.RadarApproaching || second.Distance != 30 {
		t.Errorf("second event %+v, want offset 8, 2 s, approaching, 30 m", second)
	}
	// after the gap of 6 s: offset 10 + 6
	third := events[2]
	if third.Offset != 16 || third.Duration != 2 || third.Distance != 20 || third.Vehicles != 1 {
		t.Errorf("third event %+v, want offset 16, 2 s, 20 m, 1 vehicle", third)
	}
}

func TestRadarEventsMarkerWithoutFix(t *testing.T) {
	start := time.Date(2026, 11, 24, 15, 53, 27, 0, time.UTC)
	// the marker is the 4th record, without fix: the nearest record with one, the 3rd
	events := RadarEvents(radarActivity(start, [][3]int{{0, 0, 0}, {1, 1, 80}, {1, 1, 40}, {2, 1, 20}, {0, 0, 0}}, -1), start)
	if len(events) != 1 || events[0].Position == nil {
		t.Fatalf("events %+v, want one with a position", events)
	}
	if want := recordPosition(&mesgdef.Record{PositionLat: 2000, PositionLong: 4000}); *events[0].Position != want {
		t.Errorf("event at %v, want %v", *events[0].Position, want)
	}
}

func TestRadarEventsWithoutRadar(t *testing.T) {
	start := time.Now()
	fit := radarActivity(start, [][3]int{{1, 1, 50}, {2, 1, 10}}, -1)
	// the same developer fields under other names: another extension
	for _, d := range fit.FieldDescriptions {
		d.FieldName = []string{"other_" + d.FieldName[0]}
	}
	events := RadarEvents(fit, start)
	if events == nil || len(events) != 0 {
		t.Errorf("events %+v, want empty and not nil (computed)", events)
	}
}

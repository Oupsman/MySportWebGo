package activityService

import (
	"MySportWeb/internal/pkg/types"
	"time"

	"github.com/muktihari/fit/profile/filedef"
	"github.com/muktihari/fit/profile/mesgdef"
	"github.com/muktihari/fit/profile/typedef"
	"github.com/muktihari/fit/proto"
)

// The developer fields EIradar (https://github.com/yrkan/eiradar, a Karoo extension) writes on each record,
// every second: the threat level of the radar (types.RadarClear...), the number of vehicles it tracks and the
// distance behind the rider of the nearest one (m, 0 without vehicle). The Karoo gives the extension a
// developer data index after its own fields, and no application id: they are found by their names.
const (
	radarThreatField   = "radar_threat_level"
	radarVehiclesField = "radar_vehicle_count"
	radarDistanceField = "radar_nearest_distance"
)

// RadarMaxGap is the longest time between two records of a radar event: a longer one (a pause, a
// recording that skipped) ends it
const RadarMaxGap = 5 * time.Second

// radarSample is what the radar said at a record
type radarSample struct {
	threat   uint8
	vehicles uint8
	distance uint16
	found    bool // the record has the threat level
}

// RadarEvents returns the stretches of the records during which a rear radar recorded by EIradar saw
// vehicles behind the rider, empty without such a radar. The offsets are seconds since start.
func RadarEvents(fit *filedef.Activity, start time.Time) types.RadarEventArray {
	events := types.RadarEventArray{}
	fields := radarFields(fit.FieldDescriptions)
	if !hasRadarThreat(fields) {
		return events
	}
	var stretch []*mesgdef.Record
	var samples []radarSample
	flush := func() {
		if len(stretch) > 0 {
			events = append(events, radarEvent(stretch, samples, start))
		}
		stretch, samples = stretch[:0], samples[:0]
	}
	for _, record := range fit.Records {
		sample := readRadar(record, fields)
		if !sample.found || sample.threat == types.RadarClear {
			flush()
			continue
		}
		if len(stretch) > 0 && record.Timestamp.Sub(stretch[len(stretch)-1].Timestamp) > RadarMaxGap {
			flush()
		}
		stretch = append(stretch, record)
		samples = append(samples, sample)
	}
	flush()
	return events
}

// RadarConnected tells whether a rear radar was connected during an activity: a radar among the sensors of
// the device (the Garmin devices list their Varia as an ANT+ bike radar), or radar events (a Karoo doesn't
// list the radar, EIradar records it)
func RadarConnected(fit *filedef.Activity, events types.RadarEventArray) bool {
	if len(events) > 0 {
		return true
	}
	for _, info := range fit.DeviceInfos {
		if typedef.AntplusDeviceType(info.DeviceType) == typedef.AntplusDeviceTypeBikeRadar {
			return true
		}
	}
	return false
}

// radarKey is a developer field: its developer data index and its number
type radarKey struct{ index, num uint8 }

// radarFields returns the names of the developer fields of EIradar, by field
func radarFields(descriptions []*mesgdef.FieldDescription) map[radarKey]string {
	fields := map[radarKey]string{}
	for _, d := range descriptions {
		for _, name := range d.FieldName {
			switch name {
			case radarThreatField, radarVehiclesField, radarDistanceField:
				fields[radarKey{d.DeveloperDataIndex, d.FieldDefinitionNumber}] = name
			}
		}
	}
	return fields
}

func hasRadarThreat(fields map[radarKey]string) bool {
	for _, name := range fields {
		if name == radarThreatField {
			return true
		}
	}
	return false
}

// readRadar reads the radar fields of a record; an unknown threat level is RadarClear, as EIradar shows it
func readRadar(record *mesgdef.Record, fields map[radarKey]string) radarSample {
	var sample radarSample
	for _, df := range record.DeveloperFields {
		value, ok := developerUint(df.Value)
		switch fields[radarKey{df.DeveloperDataIndex, df.Num}] {
		case radarThreatField:
			if ok {
				sample.found = true
				if value <= uint64(types.RadarCritical) {
					sample.threat = uint8(value)
				}
			}
		case radarVehiclesField:
			if ok && value < 0xFF {
				sample.vehicles = uint8(value)
			}
		case radarDistanceField:
			if ok && value < 0xFFFF {
				sample.distance = uint16(value)
			}
		}
	}
	return sample
}

// developerUint returns the value of a developer field holding an unsigned integer, false when it holds
// something else or the invalid value of its type
func developerUint(v proto.Value) (uint64, bool) {
	switch x := v.Any().(type) {
	case uint8:
		return uint64(x), x != 0xFF
	case uint16:
		return uint64(x), x != 0xFFFF
	case uint32:
		return uint64(x), x != 0xFFFFFFFF
	case int8:
		return uint64(x), x >= 0 && x != 0x7F
	}
	return 0, false
}

// radarEvent sums up a stretch: the event is marked where its threat was highest, there at the nearest
// distance
func radarEvent(records []*mesgdef.Record, samples []radarSample, start time.Time) types.RadarEvent {
	first, last := records[0], records[len(records)-1]
	event := types.RadarEvent{
		Offset: first.Timestamp.Sub(start).Seconds(),
		// each record is a second of the radar
		Duration: last.Timestamp.Sub(first.Timestamp).Seconds() + 1,
	}
	marker := 0
	for i, s := range samples {
		if s.vehicles > event.Vehicles {
			event.Vehicles = s.vehicles
		}
		best := samples[marker]
		closer := s.distance > 0 && (best.distance == 0 || s.distance < best.distance)
		if s.threat > best.threat || (s.threat == best.threat && closer) {
			marker = i
		}
	}
	event.Threat = samples[marker].threat
	event.Distance = samples[marker].distance
	event.Position = nearestPosition(records, marker)
	return event
}

// nearestPosition returns the position of the record i, else the one of the nearest record of the stretch
// with a GPS fix, nil without any
func nearestPosition(records []*mesgdef.Record, i int) *types.GpsPoint {
	for d := range len(records) {
		for _, j := range []int{i - d, i + d} {
			if j >= 0 && j < len(records) && hasPosition(records[j]) {
				p := recordPosition(records[j])
				return &p
			}
		}
	}
	return nil
}

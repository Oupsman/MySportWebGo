package health

import (
	"math"
	"time"
)

// The readiness follows the impulse-response model of Banister: the training loads of the days make a
// fitness, their average over FitnessDays (chronic load), and a fatigue, their average over FatigueDays
// (acute load), both exponentially weighted; the form of a day is the fitness minus the fatigue of the
// day before. The loads are the training scores of the activities: the ones recorded by the devices
// (Garmin's training load, the TSS) when they have one, else the Banister TRIMP of their heart rate. Those
// scales are close but not the same: the trend is the most consistent with the same kind of device.
const (
	FitnessDays = 42
	FatigueDays = 7
	// WarmUpDays of loads are computed before the first day returned: the averages start from 0 there and
	// have caught up with the loads by the first day (99% of a steady load after 5 time constants)
	WarmUpDays = 5 * FitnessDays
	// MinHistoryDays: with a first load more recent than that, the fitness is still too low to tell
	MinHistoryDays = 28
	// MaxReadinessDays is the longest series that can be asked for
	MaxReadinessDays = 365

	// the resting heart rate of the last RecentRestDays is compared to the one of the BaselineRestDays
	// before them, with at least MinRecentRests and MinBaselineRests measures
	RecentRestDays   = 7
	BaselineRestDays = 30
	MinRecentRests   = 3
	MinBaselineRests = 10

	// the form limits of the statuses
	FreshForm      = 5.0
	ProductiveForm = -10.0
	OverreachForm  = -30.0
	// ElevatedRestHR is how far above the baseline the recent resting heart rate is a warning, in bpm
	ElevatedRestHR = 5.0
	// LoadSpikeRatio is the acute:chronic load ratio above which the load rose too fast
	LoadSpikeRatio = 1.5
)

// The statuses of a readiness, by form
const (
	Fresh        = "fresh"        // form above FreshForm: rested, maybe detraining
	Neutral      = "neutral"      // between ProductiveForm and FreshForm
	Productive   = "productive"   // between OverreachForm and ProductiveForm: training load that builds
	Overreaching = "overreaching" // below OverreachForm: rest is needed
)

// The flags of a readiness
const (
	ElevatedRestHRFlag = "elevated_rest_hr"
	LoadSpikeFlag      = "load_spike"
)

// Load is the training score of an activity at its date
type Load struct {
	Date  time.Time
	Score float64
}

// RestMeasure is a resting heart rate measured at a date, in bpm
type RestMeasure struct {
	Date time.Time
	BPM  float64
}

// ReadinessDay is the training state at the end of a day (YYYY-MM-DD); the form is the one of its
// morning, before its load
type ReadinessDay struct {
	Date    string  `json:"date"`
	Load    float64 `json:"load"`
	Fitness float64 `json:"fitness"`
	Fatigue float64 `json:"fatigue"`
	Form    float64 `json:"form"`
}

// ReadinessToday is the training state of the last day, and what tells to rest
type ReadinessToday struct {
	ReadinessDay
	ACWR        *float64 `json:"acwr"` // fatigue / fitness, nil while the fitness is below 1
	RestHR7d    *float64 `json:"rest_hr_7d"`
	RestHR30d   *float64 `json:"rest_hr_30d"`
	RestHRDelta *float64 `json:"rest_hr_delta"` // nil without enough measures
	Status      string   `json:"status"`
	Flags       []string `json:"flags"`
}

// Readiness is the training state of each day of a period, and of its last day
type Readiness struct {
	Series []ReadinessDay `json:"series"`
	Today  ReadinessToday `json:"today"`
}

// ReadinessFrom returns from when the loads of a readiness of days are needed
func ReadinessFrom(now time.Time, days int) time.Time {
	return dayOf(now).AddDate(0, 0, -(days - 1 + WarmUpDays))
}

// RestFrom returns from when the resting heart rates of a readiness are needed
func RestFrom(now time.Time) time.Time {
	return dayOf(now).AddDate(0, 0, -(RecentRestDays + BaselineRestDays - 1))
}

// ComputeReadiness returns the training state of the days ending with the one of now, days in the location of
// now. The loads and the measures may be in any order; the ones after now are ignored.
func ComputeReadiness(loads []Load, rests []RestMeasure, now time.Time, days int) Readiness {
	today := dayOf(now)
	first := today.AddDate(0, 0, -(days - 1))
	start := first.AddDate(0, 0, -WarmUpDays)
	daily := map[string]float64{}
	var earliest *time.Time
	for _, load := range loads {
		day := dayOf(load.Date.In(now.Location()))
		if load.Score <= 0 || day.After(today) || day.Before(start) {
			continue
		}
		daily[day.Format(time.DateOnly)] += load.Score
		if earliest == nil || day.Before(*earliest) {
			earliest = &day
		}
	}

	fitnessDecay := 1 - math.Exp(-1.0/FitnessDays)
	fatigueDecay := 1 - math.Exp(-1.0/FatigueDays)
	var fitness, fatigue float64
	readiness := Readiness{Series: make([]ReadinessDay, 0, days)}
	recentLoad := false
	// AddDate keeps midnight across daylight saving changes
	for day := start; !day.After(today); day = day.AddDate(0, 0, 1) {
		key := day.Format(time.DateOnly)
		load := daily[key]
		form := fitness - fatigue
		fitness += (load - fitness) * fitnessDecay
		fatigue += (load - fatigue) * fatigueDecay
		if load > 0 && day.After(today.AddDate(0, 0, -FitnessDays)) {
			recentLoad = true
		}
		if day.Before(first) {
			continue
		}
		readiness.Series = append(readiness.Series, ReadinessDay{Date: key, Load: round1(load),
			Fitness: round1(fitness), Fatigue: round1(fatigue), Form: round1(form)})
	}

	t := &readiness.Today
	t.ReadinessDay = readiness.Series[len(readiness.Series)-1]
	t.Flags = []string{}
	if fitness >= 1 {
		acwr := math.Round(fatigue/fitness*100) / 100
		t.ACWR = &acwr
	}
	enoughHistory := recentLoad && earliest != nil && !earliest.After(today.AddDate(0, 0, -MinHistoryDays))
	switch {
	case !enoughHistory:
		t.Status = NotEnoughData
	case t.Form > FreshForm:
		t.Status = Fresh
	case t.Form >= ProductiveForm:
		t.Status = Neutral
	case t.Form >= OverreachForm:
		t.Status = Productive
	default:
		t.Status = Overreaching
	}
	if enoughHistory && t.ACWR != nil && *t.ACWR > LoadSpikeRatio {
		t.Flags = append(t.Flags, LoadSpikeFlag)
	}

	t.RestHR7d, t.RestHR30d, t.RestHRDelta = restHRTrend(rests, today)
	if t.RestHRDelta != nil && *t.RestHRDelta >= ElevatedRestHR {
		t.Flags = append(t.Flags, ElevatedRestHRFlag)
	}
	return readiness
}

// restHRTrend returns the average resting heart rate of the RecentRestDays ending with today, of the
// BaselineRestDays before them, and how much higher the recent one is; nil without enough measures
func restHRTrend(rests []RestMeasure, today time.Time) (recent, baseline, delta *float64) {
	recentFrom := today.AddDate(0, 0, -(RecentRestDays - 1))
	baselineFrom := recentFrom.AddDate(0, 0, -BaselineRestDays)
	var recentSum, baselineSum float64
	var recentN, baselineN int
	for _, rest := range rests {
		day := dayOf(rest.Date.In(today.Location()))
		switch {
		case rest.BPM <= 0 || day.After(today) || day.Before(baselineFrom):
		case day.Before(recentFrom):
			baselineSum += rest.BPM
			baselineN++
		default:
			recentSum += rest.BPM
			recentN++
		}
	}
	if recentN > 0 {
		r := round1(recentSum / float64(recentN))
		recent = &r
	}
	if baselineN > 0 {
		b := round1(baselineSum / float64(baselineN))
		baseline = &b
	}
	if recentN >= MinRecentRests && baselineN >= MinBaselineRests {
		d := round1(*recent - *baseline)
		delta = &d
	}
	return recent, baseline, delta
}

// dayOf returns the midnight of the day of t, in its location
func dayOf(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func round1(x float64) float64 {
	return math.Round(x*10) / 10
}

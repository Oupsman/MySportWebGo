// Package health reads health measures from other applications (Apple Health, CSV files) and turns them
// into the measures of MySportWeb
package health

import (
	"math"
	"sort"
	"time"
)

// Field is a value of a measure of MySportWeb
type Field int

const (
	Weight    Field = iota // kg
	Fat                    // percentage
	Muscle                 // percentage
	Bone                   // kg
	BodyWater              // percentage
	RestHR                 // bpm
	Vo2Max                 // ml/kg/min
	Systolic               // mmHg
	Diastolic              // mmHg
)

var Fields = []Field{Weight, Fat, Muscle, Bone, BodyWater, RestHR, Vo2Max, Systolic, Diastolic}

func (f Field) String() string {
	return [...]string{"Weight", "Fat", "Muscle", "Bone", "Body water", "Resting heart rate", "VO2max",
		"Systolic pressure", "Diastolic pressure"}[f]
}

func (f Field) Unit() string {
	return [...]string{"kg", "%", "%", "kg", "%", "bpm", "ml/kg/min", "mmHg", "mmHg"}[f]
}

// Measure is a measure of MySportWeb, as the API takes it: the values not measured are 0
type Measure struct {
	Date      time.Time `json:"date"`
	Weight    float64   `json:"weight,omitempty"`
	Fat       float64   `json:"fat,omitempty"`
	Muscle    float64   `json:"muscle,omitempty"`
	Bone      float64   `json:"bone,omitempty"`
	BodyWater float64   `json:"body_water,omitempty"`
	RestHR    uint16    `json:"rest_hr,omitempty"`
	Vo2Max    uint16    `json:"vo2max,omitempty"`
	Systolic  uint16    `json:"systolic,omitempty"`
	Diastolic uint16    `json:"diastolic,omitempty"`
}

// Get returns a value of the measure, 0 when not measured
func (m *Measure) Get(f Field) float64 {
	switch f {
	case Weight:
		return m.Weight
	case Fat:
		return m.Fat
	case Muscle:
		return m.Muscle
	case Bone:
		return m.Bone
	case BodyWater:
		return m.BodyWater
	case RestHR:
		return float64(m.RestHR)
	case Vo2Max:
		return float64(m.Vo2Max)
	case Systolic:
		return float64(m.Systolic)
	case Diastolic:
		return float64(m.Diastolic)
	}
	return 0
}

// Set sets a value, rounded to the precision of MySportWeb (integers for the heart and the pressure)
func (m *Measure) Set(f Field, value float64) {
	round := func(v float64) uint16 { return uint16(math.Round(math.Max(0, math.Min(v, math.MaxUint16)))) }
	switch f {
	case Weight:
		m.Weight = value
	case Fat:
		m.Fat = value
	case Muscle:
		m.Muscle = value
	case Bone:
		m.Bone = value
	case BodyWater:
		m.BodyWater = value
	case RestHR:
		m.RestHR = round(value)
	case Vo2Max:
		m.Vo2Max = round(value)
	case Systolic:
		m.Systolic = round(value)
	case Diastolic:
		m.Diastolic = round(value)
	}
}

// Empty tells whether the measure has no value
func (m *Measure) Empty() bool {
	for _, f := range Fields {
		if m.Get(f) != 0 {
			return false
		}
	}
	return true
}

// sortByDate sorts measures, oldest first
func sortByDate(measures []Measure) {
	sort.SliceStable(measures, func(i, j int) bool { return measures[i].Date.Before(measures[j].Date) })
}

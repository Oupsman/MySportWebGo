package activityService

import (
	"MySportWeb/internal/pkg/types"
	"math"

	"github.com/muktihari/fit/profile/mesgdef"
)

// runningSeries collects the running dynamics of the records, each measure a sparse series: a sensor
// may record some of them only, and none while walking or standing
type runningSeries struct {
	verticalOscillations, verticalRatios, stanceTimes, stanceTimePercents, stanceTimeBalances, stepLengths types.Series
}

// add appends the running dynamics of a record, offset seconds after the start. The invalid values are
// NaN once scaled; a 0 is a sensor that lost the foot strike, not a measure.
func (r *runningSeries) add(record *mesgdef.Record, offset float64) {
	addValid(&r.verticalOscillations, offset, record.VerticalOscillationScaled())
	addValid(&r.verticalRatios, offset, record.VerticalRatioScaled())
	addValid(&r.stanceTimes, offset, record.StanceTimeScaled())
	addValid(&r.stanceTimePercents, offset, record.StanceTimePercentScaled())
	addValid(&r.stanceTimeBalances, offset, record.StanceTimeBalanceScaled())
	addValid(&r.stepLengths, offset, record.StepLengthScaled())
}

func addValid(series *types.Series, offset, value float64) {
	if !math.IsNaN(value) && value > 0 {
		series.Add(offset, value)
	}
}

// metrics returns the running dynamics of the activity, nil without any
func (r *runningSeries) metrics() *types.RunningMetrics {
	m := types.RunningMetrics{
		VerticalOscillations: nonEmpty(r.verticalOscillations),
		VerticalRatios:       nonEmpty(r.verticalRatios),
		StanceTimes:          nonEmpty(r.stanceTimes),
		StanceTimePercents:   nonEmpty(r.stanceTimePercents),
		StanceTimeBalances:   nonEmpty(r.stanceTimeBalances),
		StepLengths:          nonEmpty(r.stepLengths),
	}
	if m.VerticalOscillations == nil && m.VerticalRatios == nil && m.StanceTimes == nil &&
		m.StanceTimePercents == nil && m.StanceTimeBalances == nil && m.StepLengths == nil {
		return nil
	}
	m.AvgVerticalOscillation = m.VerticalOscillations.Avg()
	m.AvgVerticalRatio = m.VerticalRatios.Avg()
	m.AvgStanceTime = m.StanceTimes.Avg()
	m.AvgStanceTimePercent = m.StanceTimePercents.Avg()
	m.AvgStanceTimeBalance = m.StanceTimeBalances.Avg()
	m.AvgStepLength = m.StepLengths.Avg()
	return &m
}

func nonEmpty(series types.Series) *types.Series {
	if series.Empty() {
		return nil
	}
	return &series
}

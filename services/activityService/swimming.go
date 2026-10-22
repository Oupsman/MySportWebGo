package activityService

import (
	"MySportWeb/internal/pkg/types"
	"github.com/muktihari/fit/profile/filedef"
	"math"
)

func SwimPace(poolLength float64, time float64, strokes uint16) float64 {
	if strokes == math.MaxUint16 {
		return 0
	}
	return time * 100 / poolLength
}

func Swolf(strokes uint16, time uint16) uint16 {
	if strokes == math.MaxUint16 {
		return 0
	}
	return strokes + time
}

func AnalyzeLengths(activity *filedef.Activity) []types.Length {
	var lengths []types.Length
	// the pool swim session: the first one in a multi-session file may be another sport
	poolLength := math.NaN()
	for _, session := range activity.Sessions {
		if poolLength = session.PoolLengthScaled(); !math.IsNaN(poolLength) {
			break
		}
	}
	TS := 0.0
	for _, activityLength := range activity.Lengths {
		var length = types.Length{
			Swolf:     Swolf(activityLength.TotalStrokes, uint16(activityLength.TotalElapsedTimeScaled())),
			Pace:      SwimPace(poolLength, activityLength.TotalElapsedTimeScaled(), activityLength.TotalStrokes),
			Duration:  activityLength.TotalElapsedTimeScaled(),
			Length:    poolLength,
			Strokes:   activityLength.TotalStrokes,
			TimeStamp: TS,
		}
		TS += activityLength.TotalElapsedTimeScaled()
		lengths = append(lengths, length)
	}
	return lengths
}

package main

import (
	"encoding/json"
	"fmt"
	"github.com/muktihari/fit/decoder"
	"github.com/muktihari/fit/profile/filedef"
	"math"
	"os"
)

type Length struct {
	Swolf     uint16  `json:"swolf"`
	Pace      float64 `json:"pace"`
	Length    float64 `json:"length"`
	Strokes   uint16  `json:"strokes"`
	Duration  float64 `json:"duration"`
	TimeStamp float64 `json:"timeStamp"`
}

func main() {
	var lengths []Length
	// filePath := "FIT/Karoo-Morning_Ride-Sep-06-2022-061544.fit"
	filePath := "FIT/2024-08-02-16-34-19.fit"
	f, err := os.Open(filePath)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	dec := decoder.New(f)
	// Read the fit file

	for dec.Next() {
		fit, err := dec.Decode()
		if err != nil {
			panic(err)
		}
		activity := filedef.NewActivity(fit.Messages...)

		fmt.Printf("File Type: %s\n", activity.FileId.Type)
		fmt.Printf("Sessions count: %d\n", len(activity.Sessions))
		fmt.Printf("Laps count: %d\n", len(activity.Laps))
		fmt.Printf("Records count: %d\n", len(activity.Records))

		fmt.Println("Lengths: ")
		poolLength := activity.Sessions[0].PoolLengthScaled()
		previousTS := 0.0
		for _, activityLength := range activity.Lengths {
			var length = Length{
				Swolf:     Swolf(activityLength.TotalStrokes, uint16(activityLength.TotalElapsedTimeScaled())),
				Pace:      SwimPace(activity.Sessions[0].PoolLengthScaled(), activityLength.TotalElapsedTimeScaled(), activityLength.TotalStrokes),
				Duration:  activityLength.TotalElapsedTimeScaled(),
				Length:    poolLength,
				Strokes:   activityLength.TotalStrokes,
				TimeStamp: previousTS,
			}
			previousTS += activityLength.TotalElapsedTimeScaled()
			lengths = append(lengths, length)
		}
		res, _ := json.MarshalIndent(lengths, "", "  ")
		fmt.Println(string(res))
	}

}

func SwimPace(poolLength float64, time float64, strokes uint16) float64 {
	if strokes == math.MaxUint16 {
		return 30
	}
	return time * 100 / poolLength
}

func Swolf(strokes uint16, time uint16) uint16 {
	if strokes == math.MaxUint16 {
		return 0
	}
	return strokes + time
}

func convertTime(time uint32) string {
	result := ""
	hour := time / 3600
	minute := (time % 3600) / 60
	second := time % 60
	if hour < 10 {
		result += fmt.Sprintf("0%d", hour)

	} else {
		result += fmt.Sprintf("%d", hour)
	}
	result += ":"
	if minute < 10 {
		result += fmt.Sprintf("0%d", minute)
	} else {
		result += fmt.Sprintf("%d", minute)
	}
	result += ":"
	if second < 10 {
		result += fmt.Sprintf("0%d", second)
	} else {
		result += fmt.Sprintf("%d", second)
	}
	return result
}

func mmsToKmh(mms float64) float64 {
	return mms * 3600 / 1000000
}

func msToKmH(ms float64) float64 {
	return ms * 3.6
}
func msTomKm(ms float64) float64 {
	return 16.666666667 / (ms)
}

func cmToKm(cm uint32) float64 {
	return float64(cm) / 100000
}

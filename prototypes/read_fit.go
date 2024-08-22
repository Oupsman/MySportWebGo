package main

import (
	"fmt"
	"github.com/muktihari/fit/decoder"
	"github.com/muktihari/fit/profile/filedef"
	"math"
	"os"
)

func main() {
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

		fmt.Println("Session summary:")
		for _, session := range activity.Sessions {
			fmt.Printf("  Sport: %s\n", session.Sport)
			fmt.Printf("  Start time: %s\n", session.StartTime)
			// Unit seems to be milliseconds
			fmt.Printf("  Total timer time: %s\n", convertTime(session.TotalTimerTime/1000))
			// Distance Unit is centimeters
			fmt.Printf("  Total distance: %2f\n", cmToKm(session.TotalDistance))
			fmt.Printf("  Total calories: %d\n", session.TotalCalories)
			// Unit is millimeters per second
			fmt.Printf("  Average speed: %2f\n", mmsToKmh(float64(session.AvgSpeed)))
			fmt.Printf("  Total ascent: %d\n", session.TotalAscent)
		}
		fmt.Println("Lengths: ")
		for _, length := range activity.Lengths {
			swolf := Swolf(length.TotalStrokes, uint16(math.Ceil(length.TotalElapsedTimeScaled())))
			swimPace := SwimPace(activity.Sessions[0].PoolLengthScaled(), length.TotalElapsedTimeScaled(), length.TotalStrokes)
			fmt.Printf("  %s %f %d %d %f \n", length.StartTime, math.Ceil(length.TotalElapsedTimeScaled()), length.TotalStrokes, swolf, swimPace)
		}
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

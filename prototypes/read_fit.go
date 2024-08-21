package main

import (
	"fmt"
	"github.com/muktihari/fit/decoder"
	"github.com/muktihari/fit/profile/filedef"
	"math"
	"os"
)

func main() {
	filePath := "FIT/Karoo-Morning_Ride-Sep-06-2022-061544.fit"
	// filePath := "FIT/2024-08-02-16-34-19.fit"
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
		fmt.Println("Records: ")
		for _, record := range activity.Records {
			fmt.Printf("  Time: %s\n", record.Timestamp)
			fmt.Printf("  Latitude: %f\n", semiCircleToDegres(record.PositionLat))
			fmt.Printf("  Longitude: %f\n", semiCircleToDegres(record.PositionLong))
			fmt.Printf("  Distance: %2f\n", record.DistanceScaled())
			fmt.Printf("  Altitude: %f\n", record.EnhancedAltitudeScaled())
			fmt.Printf("  Speed: %2f\n", msToKmH(record.EnhancedSpeedScaled()))
			fmt.Printf("  Heart rate: %d\n", record.HeartRate)
			fmt.Printf("  Cadence: %d\n", record.Cadence)
			fmt.Printf("  Power: %d\n", record.Power)
			fmt.Printf("  Temperature: %d\n", record.Temperature)
		}
	}

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

func semiCircleToDegres(semi int32) float64 {
	if semi > 0 {
		return float64(semi) * (180.0 / math.Pow(2.0, 31.0))
	}
	return 0
}
